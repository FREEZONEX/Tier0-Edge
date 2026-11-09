package sourcestream

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	op "backend/internal/infra/openprotocol"
	"backend/internal/repo"
)

type openProtocolProgress struct {
	FullResultIDs    []int64 `json:"fullResultIds,omitempty"`
	RecoveredThrough int64   `json:"recoveredThrough"`
	RecoveryNext     int64   `json:"recoveryNext"`
	RecoveryEnd      int64   `json:"recoveryEnd"`
}
type openProtocolSession struct {
	mu                 sync.Mutex
	sourceID, revision int64
	def                SourceDefinition
	ctx                context.Context
	cancel             context.CancelFunc
	done               chan struct{}
	changed            chan struct{}
	client             *op.Client
	err                error
	demandUntil        time.Time
	recoveryPrepared   atomic.Bool
	lease              *sql.Conn
}

func (s *Service) openProtocolSession(ctx context.Context, row repo.Source, def SourceDefinition) (*openProtocolSession, error) {
	if err := validateOpenProtocol(def); err != nil {
		return nil, err
	}
	s.openProtocolMu.Lock()
	defer s.openProtocolMu.Unlock()
	if s.openProtocolClosed {
		return nil, ErrUnavailable
	}
	if s.openProtocolSessions == nil {
		s.openProtocolSessions = map[string]*openProtocolSession{}
	}
	endpoint, _ := url.Parse(def.Endpoint)
	key := endpoint.Host
	if session := s.openProtocolSessions[key]; session != nil {
		if session.sourceID != row.ID || session.revision != row.ConfigRevision {
			return nil, errors.New("OpenProtocol.sessionOwnedByAnotherSource")
		}
		session.mu.Lock()
		session.demandUntil = time.Now().Add(12 * time.Second)
		session.mu.Unlock()
		return session, nil
	}
	if len(s.openProtocolSessions) >= 16 {
		return nil, ErrUnavailable
	}
	// A device owner outlives HTTP calls and must never retain a request's
	// transaction. Source identity, revision and leases are rechecked in storage.
	life, cancel := context.WithCancel(context.Background())
	session := &openProtocolSession{sourceID: row.ID, revision: row.ConfigRevision, def: def, ctx: life, cancel: cancel, done: make(chan struct{}), changed: make(chan struct{}), demandUntil: time.Now().Add(12 * time.Second)}
	s.openProtocolSessions[key] = session
	go func() {
		defer close(session.done)
		defer func() {
			s.openProtocolMu.Lock()
			if s.openProtocolSessions[key] == session {
				delete(s.openProtocolSessions, key)
			}
			s.openProtocolMu.Unlock()
		}()
		session.run(endpoint.Host)
	}()
	return session, nil
}

func (s *Service) closeOpenProtocolSource(id int64) {
	s.openProtocolMu.Lock()
	var pending []*openProtocolSession
	for _, session := range s.openProtocolSessions {
		if session.sourceID == id {
			pending = append(pending, session)
			session.cancel()
		}
	}
	s.openProtocolMu.Unlock()
	for _, session := range pending {
		<-session.done
	}
}
func (s *Service) closeOpenProtocol() {
	s.openProtocolMu.Lock()
	s.openProtocolClosed = true
	var pending []*openProtocolSession
	for _, session := range s.openProtocolSessions {
		pending = append(pending, session)
		session.cancel()
	}
	s.openProtocolMu.Unlock()
	for _, session := range pending {
		<-session.done
	}
}
func (s *openProtocolSession) setClient(client *op.Client, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client = client
	s.err = err
	close(s.changed)
	s.changed = make(chan struct{})
}
func (s *openProtocolSession) wait(ctx context.Context) (*op.Client, error) {
	for {
		s.mu.Lock()
		client, err, changed := s.client, s.err, s.changed
		s.mu.Unlock()
		if client != nil {
			return client, nil
		}
		if err != nil {
			return nil, safeOpenProtocolError(err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.done:
			return nil, ErrUnavailable
		case <-changed:
		}
	}
}
func (s *openProtocolSession) journal(ctx context.Context, change func(*durableJournal) error) error {
	return withDurableJournal(ctx, s.sourceID, s.revision, openProtocolLimit(s.def), change)
}
func (s *openProtocolSession) state(state string, err error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 2*time.Second)
	defer cancel()
	_ = s.journal(ctx, func(j *durableJournal) error {
		j.State = state
		j.Error = ""
		if err != nil {
			j.Error = safeOpenProtocolError(err).Error()
		}
		return nil
	})
}
func (s *openProtocolSession) accept(ctx context.Context, result op.Result) error {
	if s.lease == nil || s.lease.PingContext(ctx) != nil {
		return errors.New("OpenProtocol.sessionLeaseLost")
	}
	raw, err := json.Marshal(openProtocolRecord(s.sourceID, result))
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%d/%s/%d/%d/%d", s.sourceID, result.Name, result.CellID, result.ChannelID, result.TighteningID)
	return s.journal(ctx, func(j *durableJournal) error {
		var progress openProtocolProgress
		if len(j.Metadata) > 0 && json.Unmarshal(j.Metadata, &progress) != nil {
			return ErrExecutionFailed
		}
		// A shorter historical representation must not overwrite a full result.
		if result.History {
			if _, seen := slices.BinarySearch(progress.FullResultIDs, result.TighteningID); seen {
				return nil
			}
			for _, e := range j.Events {
				if e.BusinessKey != key+"/full" {
					continue
				}
				var existing op.Result
				if json.Unmarshal(e.Data, &existing) == nil && !existing.History {
					return nil
				}
			}
		}
		representation := "/full"
		if result.History {
			representation = "/history"
		}
		if err := j.append(openProtocolResults, raw, time.Now().UnixMilli(), key+representation); err != nil {
			return err
		}
		if !result.History {
			index, seen := slices.BinarySearch(progress.FullResultIDs, result.TighteningID)
			if !seen {
				progress.FullResultIDs = slices.Insert(progress.FullResultIDs, index, result.TighteningID)
			}
			if len(progress.FullResultIDs) > 10000 {
				progress.FullResultIDs = progress.FullResultIDs[len(progress.FullResultIDs)-10000:]
			}
		}
		if s.recoveryPrepared.Load() && progress.RecoveryNext == 0 {
			progress.RecoveredThrough = max(progress.RecoveredThrough, result.TighteningID)
		}
		j.Metadata, _ = json.Marshal(progress)
		return nil
	})
}

func (s *openProtocolSession) needed(ctx context.Context) bool {
	var raw struct {
		Revision int64  `gorm:"column:config_revision"`
		Desired  string `gorm:"column:desired"`
		Data     []byte `gorm:"column:consumers"`
	}
	err := repo.GetUnsConn(ctx).WithContext(ctx).Raw("SELECT (src.data->>'configRevision')::bigint AS config_revision,src.data->>'desiredState' AS desired,st.data#>'{receiveJournal,consumers}' AS consumers FROM uns_meta src JOIN uns_meta st ON st.kind='source_state' AND st.id=src.id WHERE src.kind='source' AND src.id=? AND src.deleted_at=0", strconv.FormatInt(s.sourceID, 10)).Scan(&raw).Error
	if err != nil || raw.Revision != s.revision || raw.Desired != "enabled" {
		return false
	}
	var consumers map[string]durableConsumer
	if len(raw.Data) > 0 && json.Unmarshal(raw.Data, &consumers) == nil {
		for _, c := range consumers {
			if c.Expires > time.Now().UnixMilli() {
				return true
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Now().Before(s.demandUntil)
}

func (s *openProtocolSession) run(address string) {
	defer s.setClient(nil, ErrUnavailable)
	db, err := repo.GetUnsConn(s.ctx).DB()
	if err != nil {
		s.setClient(nil, err)
		return
	}
	lease, err := db.Conn(s.ctx)
	if err != nil {
		s.setClient(nil, err)
		return
	}
	defer lease.Close()
	s.lease = lease
	var acquired bool
	if err = lease.QueryRowContext(s.ctx, "SELECT pg_try_advisory_lock(hashtextextended($1,0))", "openprotocol:"+address).Scan(&acquired); err != nil || !acquired {
		s.setClient(nil, errors.New("OpenProtocol.sessionOwnedByAnotherProcess"))
		return
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = lease.ExecContext(ctx, "SELECT pg_advisory_unlock(hashtextextended($1,0))", "openprotocol:"+address)
	}()
	// database/sql uses native $1 placeholders, unlike GORM's '?' conversion.
	for s.ctx.Err() == nil {
		check, cancel := context.WithTimeout(s.ctx, 3*time.Second)
		needed := s.needed(check)
		leaseErr := lease.PingContext(check)
		cancel()
		if !needed || leaseErr != nil {
			return
		}
		s.state("connecting", nil)
		s.recoveryPrepared.Store(false)
		client, err := op.Dial(s.ctx, address, op.Controller{CellID: s.def.OpenProtocol.CellID, ChannelID: s.def.OpenProtocol.ChannelID, Name: s.def.OpenProtocol.ControllerName}, s.accept)
		if err == nil {
			request, stop := context.WithTimeout(s.ctx, 5*time.Second)
			err = client.Subscribe(request)
			stop()
		}
		if err == nil {
			s.setClient(client, nil)
			s.state("recovering", nil)
			err = s.serve(client, lease)
		}
		s.setClient(nil, err)
		if client != nil {
			client.Close()
		}
		state := "disconnected"
		if errors.Is(err, ErrEdgeJournalFull) {
			state = "backpressure"
		}
		s.state(state, err)
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (s *openProtocolSession) serve(client *op.Client, lease *sql.Conn) error {
	request, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	latest, err := client.History(request, 0)
	cancel()
	if err != nil && !missingOpenProtocol(err) {
		return err
	}
	if err == nil {
		if err = s.prepareRecovery(latest.TighteningID); err != nil {
			return err
		}
	}
	s.recoveryPrepared.Store(true)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		check, stop := context.WithTimeout(s.ctx, 5*time.Second)
		err = s.recoverOne(check, client)
		stop()
		if err != nil {
			return err
		}
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case <-client.Done():
			return client.Err()
		case <-ticker.C:
			check, stop := context.WithTimeout(s.ctx, 3*time.Second)
			needed := s.needed(check)
			err = lease.PingContext(check)
			stop()
			if err != nil {
				return err
			}
			if !needed {
				stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				client.Stop(stopCtx)
				cancel()
				return context.Canceled
			}
		}
	}
}

func (s *openProtocolSession) prepareRecovery(latest int64) error {
	reset := false
	err := s.journal(s.ctx, func(j *durableJournal) error {
		var p openProtocolProgress
		if len(j.Metadata) > 0 && json.Unmarshal(j.Metadata, &p) != nil {
			return ErrExecutionFailed
		}
		if p.RecoveredThrough == 0 {
			p.RecoveredThrough = latest
		} else if latest < p.RecoveredThrough {
			reset = true
			j.addGap(EdgeAcquisitionGap{Reason: "controllerTighteningIDReset", From: strconv.FormatInt(p.RecoveredThrough, 10), To: strconv.FormatInt(latest, 10), At: time.Now().UnixMilli()})
			j.State = "gap"
			j.Error = "OpenProtocol.controllerSequenceReset"
			return nil
		} else if p.RecoveryNext == 0 && latest > p.RecoveredThrough {
			p.RecoveryNext = p.RecoveredThrough + 1
			p.RecoveryEnd = latest
		} else if latest > p.RecoveryEnd && p.RecoveryNext > 0 {
			p.RecoveryEnd = latest
		}
		if p.RecoveryEnd-p.RecoveryNext >= 10000 && p.RecoveryNext > 0 {
			j.addGap(EdgeAcquisitionGap{Reason: "recoveryRangeExceedsReadBudget", From: strconv.FormatInt(p.RecoveryNext, 10), To: strconv.FormatInt(p.RecoveryEnd-10000, 10), At: time.Now().UnixMilli()})
			p.RecoveryNext = p.RecoveryEnd - 9999
		}
		j.Metadata, _ = json.Marshal(p)
		j.State = "running"
		if p.RecoveryNext > 0 {
			j.State = "recovering"
		}
		return nil
	})
	if err == nil && reset {
		return errors.New("OpenProtocol.controllerSequenceReset")
	}
	return err
}

func (s *openProtocolSession) recoverOne(ctx context.Context, client *op.Client) error {
	var p openProtocolProgress
	var raw []byte
	if err := repo.GetUnsConn(ctx).WithContext(ctx).Raw("SELECT data#>'{receiveJournal,metadata}' FROM uns_meta WHERE kind='source_state' AND id=?", strconv.FormatInt(s.sourceID, 10)).Row().Scan(&raw); err != nil {
		return err
	}
	if len(raw) > 0 && json.Unmarshal(raw, &p) != nil {
		return ErrExecutionFailed
	}
	if p.RecoveryNext == 0 {
		return nil
	}
	result, err := client.History(ctx, p.RecoveryNext)
	missing := missingOpenProtocol(err)
	if err != nil && !missing {
		return err
	}
	if !missing {
		if err = s.accept(ctx, result); err != nil {
			return err
		}
	}
	return s.journal(ctx, func(j *durableJournal) error {
		requestedID := p.RecoveryNext
		if json.Unmarshal(j.Metadata, &p) != nil || p.RecoveryNext != requestedID {
			return repo.ErrConflict
		}
		if missing {
			j.addGap(EdgeAcquisitionGap{Reason: "historyIdUnavailableMayBeNaturalGap", From: strconv.FormatInt(p.RecoveryNext, 10), To: strconv.FormatInt(p.RecoveryNext, 10), At: time.Now().UnixMilli()})
		}
		p.RecoveredThrough = p.RecoveryNext
		p.RecoveryNext++
		if p.RecoveryNext > p.RecoveryEnd {
			p.RecoveryNext = 0
			p.RecoveryEnd = 0
		}
		j.Metadata, _ = json.Marshal(p)
		j.State = "recovering"
		if p.RecoveryNext == 0 {
			j.State = "running"
		}
		return nil
	})
}

func (s *Service) exchangeOpenProtocol(ctx context.Context, req EdgeSubscriptionRequest, key string, row repo.Source, def SourceDefinition) (EdgeSubscriptionBatch, error) {
	out := EdgeSubscriptionBatch{}
	if req.Action != "exchange" && req.Action != "cancel" {
		return out, repo.ErrInvalidArgument
	}
	if req.Action == "cancel" {
		return s.exchangeDurableJournal(ctx, req, key, row, openProtocolLimit(def))
	}
	if req.ConfigRevision != row.ConfigRevision || row.DesiredState != "enabled" {
		return out, ErrEdgeReferenceRejected
	}
	if validateOpenProtocol(def) != nil || len(req.Resources) != 1 || req.Resources[0] != openProtocolResults || req.QoS != 0 || req.SamplingIntervalMs != 0 || req.PollIntervalMs != 0 || len(req.Ranges) > 0 || req.AckSequence < 0 {
		return out, repo.ErrInvalidArgument
	}
	out, err := s.exchangeDurableJournal(ctx, req, key, row, openProtocolLimit(def))
	if err != nil || req.Action == "cancel" {
		return out, err
	}
	_, err = s.openProtocolSession(ctx, row, def)
	return out, err
}
