package sourceprotocol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"backend/internal/domain/sourcestream"
)

const sourcePoolLimit = 128
const sourcePoolIdle = time.Minute

type sourceRevisionKey struct{}

// WithSourceRevision pins a managed connection to the validated configuration
// version. Without it, the full configuration and credential digest still
// prevents reuse across connection changes. No digest is exposed or logged.
func WithSourceRevision(ctx context.Context, revision int64) context.Context {
	return context.WithValue(ctx, sourceRevisionKey{}, revision)
}

func sourceConnectionKey(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential) (string, error) {
	def.Name, def.Description = "", ""
	if def.ConnectionRole() == sourcestream.ClientRole {
		// Watch scopes are derived from Stream subscriptions. They are not
		// connection identities; each operation still validates its own scope.
		if def.MQTT != nil {
			options := *def.MQTT
			options.TopicFilters, options.ServerSubscribeTopics = nil, nil
			options.RequestTopic, options.ReplyTopic, options.ReplyTopicField, options.CorrelationField = "", "", "", ""
			options.QoS = 0
			options.Subscriptions = nil
			options.Ephemeral = false
			options.PacketEnvelope = false
			def.MQTT = &options
		}
		if def.OPCUA != nil {
			options := *def.OPCUA
			options.NodeIDs = nil
			options.SamplingIntervalMs = 0
			def.OPCUA = &options
		}
		if def.Modbus != nil {
			options := *def.Modbus
			options.Ranges = nil
			options.PollIntervalMs = 0
			def.Modbus = &options
		}
	}
	revision, _ := ctx.Value(sourceRevisionKey{}).(int64)
	if revision == 0 {
		revision = sourcestream.WatchConfigRevision(ctx)
	}
	raw, err := json.Marshal(struct {
		Revision   int64
		Definition sourcestream.SourceDefinition
		Credential sourcestream.Credential
	}{revision, def, credential})
	if err != nil {
		return "", ErrProtocol
	}
	digest := sha256.Sum256(raw)
	return fmt.Sprintf("%d:%s:%s", id, def.ConnectionRole(), hex.EncodeToString(digest[:])), nil
}

type sourceConnection struct {
	id          int64
	key         string
	ctx         context.Context
	cancel      context.CancelFunc
	gate        chan struct{}
	mu          sync.Mutex
	value       any
	closeValue  func()
	timer       *time.Timer
	closed      bool
	idleVersion uint64
	watchers    int
}

func (entry *sourceConnection) close() {
	entry.mu.Lock()
	if entry.closed {
		entry.mu.Unlock()
		return
	}
	entry.closed = true
	entry.cancel()
	if entry.timer != nil {
		entry.timer.Stop()
	}
	closeValue := entry.closeValue
	entry.mu.Unlock()
	// Some protocol libraries take their operation mutex in Close. Do not
	// block a Source stop on a device withholding an operation response.
	if closeValue != nil {
		go closeValue()
	}
}

type sourceConnectionPool struct {
	mu      sync.Mutex
	entries map[string]*sourceConnection
	closed  bool
	limit   int
	idle    time.Duration
	probeID uint64
}

func newSourceConnectionPool() *sourceConnectionPool {
	return &sourceConnectionPool{entries: make(map[string]*sourceConnection), limit: sourcePoolLimit, idle: sourcePoolIdle}
}

func (pool *sourceConnectionPool) entry(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential) (*sourceConnection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key, err := sourceConnectionKey(ctx, id, def, credential)
	if err != nil {
		return nil, err
	}
	owner := fmt.Sprintf("%d:%s", id, def.ConnectionRole())
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if pool.closed {
		return nil, sourcestream.ErrUnavailable
	}
	if id <= 0 {
		pool.probeID++
		owner = fmt.Sprintf("%s:probe-%d", owner, pool.probeID)
	}
	entry := pool.entries[owner]
	if entry != nil && entry.key == key && entry.ctx.Err() == nil {
		return entry, nil
	}
	if entry != nil {
		delete(pool.entries, owner)
		entry.close()
	}
	if len(pool.entries) >= pool.limit {
		return nil, sourcestream.ErrUnavailable
	}
	life, cancel := context.WithCancel(context.Background())
	entry = &sourceConnection{id: id, key: key, ctx: life, cancel: cancel, gate: make(chan struct{}, 1)}
	entry.gate <- struct{}{}
	// Unsaved probes count toward the bound, but get unique temporary leases
	// and are retired after the operation rather than shared under ID zero.
	pool.entries[owner] = entry
	return entry, nil
}

func (pool *sourceConnectionPool) retire(entry *sourceConnection) {
	pool.mu.Lock()
	for owner, current := range pool.entries {
		if current == entry {
			delete(pool.entries, owner)
		}
	}
	pool.mu.Unlock()
	entry.close()
}

func (pool *sourceConnectionPool) expire(entry *sourceConnection, version uint64) {
	pool.mu.Lock()
	entry.mu.Lock()
	if entry.closed || entry.watchers > 0 || entry.idleVersion != version {
		entry.mu.Unlock()
		pool.mu.Unlock()
		return
	}
	entry.closed = true
	entry.cancel()
	closeValue := entry.closeValue
	for owner, current := range pool.entries {
		if current == entry {
			delete(pool.entries, owner)
		}
	}
	entry.mu.Unlock()
	pool.mu.Unlock()
	if closeValue != nil {
		go closeValue()
	}
}

func (pool *sourceConnectionPool) releaseSource(id int64) {
	pool.mu.Lock()
	entries := []*sourceConnection{}
	for owner, entry := range pool.entries {
		if entry.id == id {
			delete(pool.entries, owner)
			entries = append(entries, entry)
		}
	}
	pool.mu.Unlock()
	for _, entry := range entries {
		entry.close()
	}
}

func (pool *sourceConnectionPool) close() {
	pool.mu.Lock()
	pool.closed = true
	entries := pool.entries
	pool.entries = make(map[string]*sourceConnection)
	pool.mu.Unlock()
	for _, entry := range entries {
		entry.close()
	}
}

// Connections belong to Source, not an individual script call. Requests are
// serialized and cancellation invalidates only the exact leased generation.
func (pool *sourceConnectionPool) with(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential,
	connect func(context.Context) (any, func(), error), action func(context.Context, any) (any, error)) (any, error) {
	entry, err := pool.entry(ctx, id, def, credential)
	if err != nil {
		return nil, err
	}
	return pool.withEntry(ctx, entry, connect, action)
}

// A long-running watcher pins one exact generation. It must not accidentally
// reconnect using old configuration after ReleaseSource or a replacement.
func (pool *sourceConnectionPool) pinWatch(entry *sourceConnection) error {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.closed {
		return sourcestream.ErrUnavailable
	}
	entry.watchers++
	entry.idleVersion++
	if entry.timer != nil {
		entry.timer.Stop()
	}
	return nil
}

// A different range or an on-demand read may still be using this transport.
// After the last poller leaves, apply the normal idle timeout.
func (pool *sourceConnectionPool) unpinWatch(entry *sourceConnection) {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.watchers--
	if !entry.closed && entry.watchers == 0 {
		version := entry.idleVersion
		entry.timer = time.AfterFunc(pool.idle, func() { pool.expire(entry, version) })
	}
}

// replaceValue runs only inside withEntry's operation gate. Reconnecting keeps
// the same Source generation; release/cancellation must not revive an old owner.
func (pool *sourceConnectionPool) replaceValue(ctx context.Context, entry *sourceConnection,
	connect func(context.Context) (any, func(), error)) (any, error) {
	entry.mu.Lock()
	if entry.closed || sourceOperationError(ctx, entry) != nil {
		entry.mu.Unlock()
		if err := sourceOperationError(ctx, entry); err != nil {
			return nil, err
		}
		return nil, sourcestream.ErrUnavailable
	}
	closeValue := entry.closeValue
	entry.value, entry.closeValue = nil, nil
	entry.mu.Unlock()
	if closeValue != nil {
		closeValue()
	}
	value, closeValue, err := connect(ctx)
	if err != nil {
		return nil, err
	}
	entry.mu.Lock()
	if entry.closed || sourceOperationError(ctx, entry) != nil {
		entry.mu.Unlock()
		closeValue()
		if err := sourceOperationError(ctx, entry); err != nil {
			return nil, err
		}
		return nil, sourcestream.ErrUnavailable
	}
	entry.value, entry.closeValue = value, closeValue
	entry.mu.Unlock()
	return value, nil
}

// AfterFunc cancellation propagates asynchronously. The generation's own
// lifecycle is authoritative even before the operation callback has run.
func sourceOperationError(ctx context.Context, entry *sourceConnection) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return entry.ctx.Err()
}

func (pool *sourceConnectionPool) withEntry(ctx context.Context, entry *sourceConnection,
	connect func(context.Context) (any, func(), error), action func(context.Context, any) (any, error)) (any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-entry.ctx.Done():
		return nil, sourcestream.ErrUnavailable
	case <-entry.gate:
	}
	defer func() { entry.gate <- struct{}{} }()
	entry.mu.Lock()
	entry.idleVersion++
	if entry.timer != nil {
		entry.timer.Stop()
	}
	closed, value := entry.closed, entry.value
	entry.mu.Unlock()
	if closed {
		return nil, sourcestream.ErrUnavailable
	}
	opCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(entry.ctx, cancel)
	defer func() { stop(); cancel() }()
	if value == nil {
		var closeValue func()
		var err error
		value, closeValue, err = connect(opCtx)
		if err != nil {
			pool.retire(entry)
			return nil, err
		}
		entry.mu.Lock()
		if entry.closed || sourceOperationError(opCtx, entry) != nil {
			entry.mu.Unlock()
			closeValue()
			if err := sourceOperationError(opCtx, entry); err != nil {
				return nil, err
			}
			return nil, sourcestream.ErrUnavailable
		}
		entry.value, entry.closeValue = value, closeValue
		entry.mu.Unlock()
	}
	result, err := action(opCtx, value)
	if lifecycleErr := sourceOperationError(opCtx, entry); lifecycleErr != nil {
		pool.retire(entry)
		return nil, lifecycleErr
	}
	if err != nil || entry.id <= 0 {
		pool.retire(entry)
	} else {
		entry.mu.Lock()
		if !entry.closed && entry.watchers == 0 {
			version := entry.idleVersion
			entry.timer = time.AfterFunc(pool.idle, func() { pool.expire(entry, version) })
		}
		entry.mu.Unlock()
	}
	return result, err
}
