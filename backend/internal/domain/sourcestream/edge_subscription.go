package sourcestream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
	"github.com/google/uuid"
)

const edgeSubscriptionLease = 120 * time.Second
const edgeSubscriptionMaxBytes = 1024 * 1024

// 只接受来自已签名中心身份的资源需求，不接受连接地址或凭据。
type EdgeSubscriptionRequest struct {
	Action             string                  `json:"action"`
	ID                 string                  `json:"id"`
	SubscriptionID     string                  `json:"subscriptionId"`
	ConfigRevision     int64                   `json:"configRevision"`
	Resources          []string                `json:"resources"`
	QoS                int                     `json:"qos"`
	SamplingIntervalMs int                     `json:"samplingIntervalMs,omitempty"`
	PollIntervalMs     int                     `json:"pollIntervalMs,omitempty"`
	Ranges             []EdgeSubscriptionRange `json:"ranges,omitempty"`
	Epoch              string                  `json:"epoch"`
	AckSequence        int64                   `json:"ackSequence"`
}

// Address zero is valid. Require the sender to supply it explicitly.
type EdgeSubscriptionRange struct {
	ID       string `json:"id"`
	Table    string `json:"table"`
	Address  *int   `json:"address"`
	Quantity int    `json:"quantity"`
}

type EdgeSubscriptionEvent struct {
	SourceID       string          `json:"sourceId"`
	SubscriptionID string          `json:"subscriptionId"`
	ConfigRevision int64           `json:"configRevision"`
	Epoch          string          `json:"epoch"`
	Sequence       int64           `json:"sequence"`
	CollectedAt    int64           `json:"collectedAt"`
	ResourceRef    string          `json:"resourceRef"`
	Data           json.RawMessage `json:"data"`
}

type EdgeSubscriptionBatch struct {
	Gaps             []EdgeAcquisitionGap    `json:"gaps,omitempty"`
	SubscriptionID   string                  `json:"subscriptionId"`
	Epoch            string                  `json:"epoch"`
	ConfigRevision   int64                   `json:"configRevision"`
	AcquisitionState string                  `json:"acquisitionState"`
	LastDataAt       int64                   `json:"lastDataAt"`
	Error            string                  `json:"error,omitempty"`
	Dropped          int64                   `json:"dropped"`
	LeaseExpiresAt   int64                   `json:"leaseExpiresAt"`
	Events           []EdgeSubscriptionEvent `json:"events"`
}

type edgeSubscription struct {
	mu          sync.Mutex
	request     EdgeSubscriptionRequest
	fingerprint string
	epoch       string
	cancel      context.CancelFunc
	expires     time.Time
	events      []EdgeSubscriptionEvent
	bytes       int
	sequence    int64
	delivered   int64
	dropped     int64
	lastDataAt  int64
	state       string
	err         string
}

func (s *Service) ExchangeEdgeSubscription(ctx context.Context, req EdgeSubscriptionRequest) (EdgeSubscriptionBatch, error) {
	var out EdgeSubscriptionBatch
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || subject.FleetTask == nil || subject.FleetTask.NodeID == "" || subject.FleetTask.GrantID == "" || !contextx.HasResource(subject, "uns.source") {
		return out, ErrForbidden
	}
	var id int64
	if _, err := fmt.Sscan(req.ID, &id); err != nil || id <= 0 || fmt.Sprint(id) != req.ID || len(req.SubscriptionID) < 1 || len(req.SubscriptionID) > 128 || req.AckSequence < 0 {
		return out, repo.ErrInvalidArgument
	}
	owner := subject.FleetTask.NodeID + "/" + subject.FleetTask.GrantID + "/" + req.SubscriptionID
	sum := sha256.Sum256([]byte(owner))
	key := hex.EncodeToString(sum[:])
	// Durable device adapters keep per-consumer ACKs across process restarts.
	current, lookupErr := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
	if lookupErr != nil && req.Action != "cancel" {
		return out, lookupErr
	}
	if current.Protocol == string(OpenProtocol) {
		var def SourceDefinition
		if json.Unmarshal([]byte(current.Document), &def) != nil {
			return out, repo.ErrInvalidArgument
		}
		return s.exchangeOpenProtocol(ctx, req, key, current, def)
	}
	if current.Protocol == string(OAuth) {
		var def SourceDefinition
		if json.Unmarshal([]byte(current.Document), &def) != nil {
			return out, repo.ErrInvalidArgument
		}
		if def.SourceType == "salesforce" {
			return s.exchangeSalesforceCDC(ctx, req, key, current, def)
		}
	}
	if req.Action == "cancel" {
		s.subscriptionMu.Lock()
		sub := s.subscriptions[key]
		if sub != nil && sub.request.ID != req.ID {
			s.subscriptionMu.Unlock()
			return out, ErrForbidden
		}
		if sub != nil {
			// 旧云端进程不能取消已重建的新一代订阅。
			if req.Epoch == "" || req.Epoch != sub.epoch {
				s.subscriptionMu.Unlock()
				return out, repo.ErrConflict
			}
			sub.cancel()
			delete(s.subscriptions, key)
		}
		s.subscriptionMu.Unlock()
		return EdgeSubscriptionBatch{SubscriptionID: req.SubscriptionID, AcquisitionState: "stopped", Events: []EdgeSubscriptionEvent{}}, nil
	}
	if req.Action != "exchange" || req.ConfigRevision <= 0 || len(req.Resources) == 0 || len(req.Resources) > 128 || !uniqueResourceRefs(req.Resources) || req.QoS < 0 || req.QoS > 2 {
		return out, repo.ErrInvalidArgument
	}
	if current.ConfigRevision != req.ConfigRevision || current.DesiredState != "enabled" {
		return out, repo.ErrConflict
	}
	def, credential, _, err := s.connection(ctx, id, nil)
	if err != nil {
		return out, err
	}
	if (def.Protocol != MQTT && def.Protocol != OPCUA && def.Protocol != USBHID && def.Protocol != ModbusTCP) || def.ConnectionRole() == ServerRole {
		return out, invalid("protocol", "unsupportedSubscriptionProtocol")
	}
	if def.Protocol != ModbusTCP && (req.PollIntervalMs != 0 || len(req.Ranges) != 0) {
		return out, repo.ErrInvalidArgument
	}
	if def.Protocol == MQTT || def.Protocol == OPCUA {
		allowed := make(map[string]bool)
		for _, resource := range declaredDeviceResources(def, current.ConfigRevision) {
			allowed[resource.ResourceRef] = true
		}
		// Existing connection-only Sources defer to the signed acquisition.
		// Mutual TLS requires an explicit local topic declaration.
		requireDeclared := len(allowed) > 0 || (def.Protocol == MQTT && credential.Kind == "custom")
		for _, ref := range req.Resources {
			if requireDeclared && !allowed[ref] {
				return out, invalid("resources", "undeclared")
			}
		}
	}
	collection := edgeSubscriptionCollection(req)
	if def.Protocol == ModbusTCP {
		if def.Modbus == nil || req.QoS != 0 || req.SamplingIntervalMs != 0 || len(req.Ranges) != len(req.Resources) {
			return out, repo.ErrInvalidArgument
		}
		if req.PollIntervalMs == 0 {
			req.PollIntervalMs = def.Modbus.PollIntervalMs
		}
		// Resolve register addresses exclusively from this revision of the native Source.
		native := make(map[string]ModbusRange, len(def.Modbus.Ranges))
		for _, item := range def.Modbus.Ranges {
			native[item.ID] = item
		}
		selected := make(map[string]bool, len(req.Resources))
		for _, ref := range req.Resources {
			selected[ref] = true
		}
		for _, frozen := range req.Ranges {
			item, ok := native[frozen.ID]
			ref := "modbus:" + frozen.ID
			if !ok || !selected[ref] || frozen.Address == nil || item.Address != *frozen.Address || item.Quantity != frozen.Quantity || item.Table != frozen.Table {
				return out, repo.ErrInvalidArgument
			}
			delete(selected, ref)
		}
		collection = &CollectionDefinition{Modbus: &ModbusCollection{PollIntervalMs: req.PollIntervalMs, Ranges: def.Modbus.Ranges}}
	}
	if def.Protocol == USBHID && (req.QoS != 0 || req.SamplingIntervalMs != 0) {
		return out, repo.ErrInvalidArgument
	}
	if (def.Protocol == MQTT && req.SamplingIntervalMs != 0) || (def.Protocol == OPCUA && (req.QoS != 0 || req.SamplingIntervalMs < 100 || req.SamplingIntervalMs > 60000)) {
		return out, repo.ErrInvalidArgument
	}
	if _, err := (StreamDefinition{Model: Publish, NativeResourceRefs: req.Resources, Collection: collection}).EffectiveSource(def); err != nil {
		return out, err
	}
	sort.Strings(req.Resources)
	for _, ref := range req.Resources {
		if len(ref) < 6 || len(ref) > 1029 {
			return out, repo.ErrInvalidArgument
		}
	}
	identity, _ := json.Marshal(struct {
		ID                 string
		Revision           int64
		Resources          []string
		QoS                int
		SamplingIntervalMs int
		PollIntervalMs     int
	}{req.ID, req.ConfigRevision, req.Resources, req.QoS, req.SamplingIntervalMs, req.PollIntervalMs})
	fingerprint := string(identity)
	s.subscriptionMu.Lock()
	if s.subscriptions == nil {
		s.subscriptions = make(map[string]*edgeSubscription)
	}
	sub := s.subscriptions[key]
	if sub != nil && sub.fingerprint != fingerprint {
		s.subscriptionMu.Unlock()
		return out, repo.ErrConflict
	}
	if sub == nil {
		if len(s.subscriptions) >= 128 {
			s.subscriptionMu.Unlock()
			return out, ErrUnavailable
		}
		life, cancel := context.WithCancel(context.WithoutCancel(ctx))
		sub = &edgeSubscription{request: req, fingerprint: fingerprint, epoch: uuid.NewString(), cancel: cancel, expires: time.Now().Add(edgeSubscriptionLease), state: "starting"}
		s.subscriptions[key] = sub
		go s.runEdgeSubscription(life, key, id, sub, collection)
	}
	s.subscriptionMu.Unlock()
	sub.mu.Lock()
	defer sub.mu.Unlock()
	// epoch 变化表示进程/租约重建，旧游标绝不能确认新一代事件。
	if req.Epoch == sub.epoch {
		if req.AckSequence > sub.delivered {
			return out, repo.ErrInvalidArgument
		}
		for len(sub.events) > 0 && sub.events[0].Sequence <= req.AckSequence {
			sub.bytes -= len(sub.events[0].Data)
			sub.events = sub.events[1:]
		}
	}
	sub.expires = time.Now().Add(edgeSubscriptionLease)
	out = EdgeSubscriptionBatch{SubscriptionID: req.SubscriptionID, Epoch: sub.epoch, ConfigRevision: req.ConfigRevision, AcquisitionState: sub.state, LastDataAt: sub.lastDataAt, Error: sub.err, Dropped: sub.dropped, LeaseExpiresAt: sub.expires.UnixMilli(), Events: []EdgeSubscriptionEvent{}}
	batchBytes := 0
	for _, event := range sub.events {
		if len(out.Events) >= 64 || batchBytes+len(event.Data) > 256*1024 {
			break
		}
		out.Events = append(out.Events, event)
		batchBytes += len(event.Data)
		if event.Sequence > sub.delivered {
			sub.delivered = event.Sequence
		}
	}
	return out, nil
}

func (s *Service) runEdgeSubscription(ctx context.Context, key string, id int64, sub *edgeSubscription, collection *CollectionDefinition) {
	defer func() {
		sub.cancel()
		s.subscriptionMu.Lock()
		if s.subscriptions[key] == sub {
			delete(s.subscriptions, key)
		}
		s.subscriptionMu.Unlock()
	}()
	life, cancel := context.WithCancel(ctx)
	defer cancel()
	guardDone := make(chan struct{})
	go func() {
		defer close(guardDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-life.Done():
				return
			case <-ticker.C:
				sub.mu.Lock()
				expired := time.Now().After(sub.expires)
				sub.mu.Unlock()
				if expired {
					cancel()
					return
				}
				check, stop := context.WithTimeout(life, 3*time.Second)
				current, err := repo.NewSourceStreamRepo(check).Source(check, id)
				stop()
				if err != nil || current.ConfigRevision != sub.request.ConfigRevision {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-guardDone }()
	for life.Err() == nil {
		observed := WithWatchObserver(life, func(ready bool) {
			sub.mu.Lock()
			if ready {
				sub.state = "running"
				sub.err = ""
			} else {
				sub.state = "disconnected"
			}
			sub.mu.Unlock()
		})
		err := s.watchInputs(observed, id, sub.request.ConfigRevision, sub.request.Resources, sub.request.QoS, collection, func(input SourceInput) error {
			raw, err := json.Marshal(input.Data)
			if err != nil {
				return err
			}
			sub.mu.Lock()
			defer sub.mu.Unlock()
			sub.sequence++
			if len(raw) > 256*1024 {
				sub.dropped++
				sub.err = "eventTooLarge"
				return nil
			}
			for len(sub.events) > 0 && (len(sub.events) >= 256 || sub.bytes+len(raw) > edgeSubscriptionMaxBytes) {
				sub.bytes -= len(sub.events[0].Data)
				sub.events = sub.events[1:]
				sub.dropped++
			}
			sub.events = append(sub.events, EdgeSubscriptionEvent{SourceID: sub.request.ID, SubscriptionID: sub.request.SubscriptionID, ConfigRevision: sub.request.ConfigRevision, Epoch: sub.epoch, Sequence: sub.sequence, CollectedAt: input.ReceivedAt, ResourceRef: input.ResourceRef, Data: raw})
			sub.bytes += len(raw)
			sub.lastDataAt = input.ReceivedAt
			return nil
		})
		if life.Err() != nil {
			return
		}
		sub.mu.Lock()
		sub.state = "disconnected"
		if err != nil {
			sub.err = "sourceWatchFailed"
		}
		sub.mu.Unlock()
		select {
		case <-life.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func edgeSubscriptionCollection(req EdgeSubscriptionRequest) *CollectionDefinition {
	if len(req.Resources) == 1 && req.Resources[0] == HIDTextResource {
		return &CollectionDefinition{}
	}
	if req.SamplingIntervalMs > 0 {
		return &CollectionDefinition{OPCUA: &OPCUACollection{SamplingIntervalMs: req.SamplingIntervalMs}}
	}
	return &CollectionDefinition{MQTT: &MQTTCollection{QoS: req.QoS}}
}
