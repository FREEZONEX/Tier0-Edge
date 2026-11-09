// Package sourcestreamworker implements the local enterprise execution loop.
// Product Streams are mappings; JetStream streams below are durable storage.
package sourcestreamworker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"backend/internal/domain/dataingest"
	"backend/internal/domain/sourcestream"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/zeromicro/go-zero/core/logx"
)

const workerOwner = "tier0.enterprise.source-stream.async-v1"

// Leave bounded time for output persistence after the maximum program budget.
const workerOperationTimeout = time.Duration(streamexec.MaxProgramTimeoutMs)*time.Millisecond + 15*time.Second
const workerAckWait = workerOperationTimeout + 15*time.Second

type Sink interface {
	sourcestream.StreamSink
	EnsureStreamStorage(context.Context) error
	SetResourceEventPublisher(repo.ResourceEventPublisher)
}

type Worker struct {
	service        *sourcestream.Service
	sink           Sink
	connection     *nats.Conn
	js             nats.JetStreamContext
	cancel         context.CancelFunc
	done           chan struct{}
	ready          atomic.Bool
	subscriptions  []*nats.Subscription
	policy         messagePolicy
	watchStatus    watchReadiness
	programWatches programWatchRetries
}

func Start(ctx context.Context, url string, service *sourcestream.Service, sink Sink) (*Worker, error) {
	if service == nil || sink == nil || url == "" {
		return nil, sourcestream.ErrUnavailable
	}
	policy, err := loadMessagePolicy()
	if err != nil {
		return nil, err
	}
	if err := sink.EnsureStreamStorage(ctx); err != nil {
		return nil, err
	}
	w := &Worker{service: service, sink: sink, done: make(chan struct{}), policy: policy}
	conn, err := nats.Connect(url, nats.Name("tier0-source-stream"), nats.Timeout(5*time.Second), nats.ReconnectWait(time.Second), nats.MaxReconnects(-1), nats.DisconnectErrHandler(func(*nats.Conn, error) { w.ready.Store(false) }), nats.ReconnectHandler(func(*nats.Conn) { w.ready.Store(true) }))
	if err != nil {
		return nil, sourcestream.ErrUnavailable
	}
	w.connection = conn
	js, err := conn.JetStream(nats.MaxWait(5 * time.Second))
	if err != nil {
		conn.Close()
		return nil, err
	}
	w.js = js
	for _, spec := range []struct {
		name, subject string
	}{
		{"TIER0_SOURCE_RAW", "tier0.raw.source.*"},
		{"TIER0_UNS_EVENTS", "tier0.events.topic.*"},
		{"TIER0_SOURCE_DELIVERY", "tier0.delivery.task.*"},
		{"TIER0_SOURCE_DLQ", "tier0.dlq.>"},
	} {
		info, infoErr := js.StreamInfo(spec.name, nats.Context(ctx))
		if errors.Is(infoErr, nats.ErrStreamNotFound) {
			config := nats.StreamConfig{Name: spec.name, Subjects: []string{spec.subject}, Storage: nats.FileStorage, Retention: nats.LimitsPolicy, Replicas: 1, Duplicates: 2 * time.Minute}
			policy.apply(&config)
			_, err = js.AddStream(&config, nats.Context(ctx))
		} else if infoErr != nil {
			err = infoErr
		} else if len(info.Config.Subjects) != 1 || info.Config.Subjects[0] != spec.subject || info.Config.Storage != nats.FileStorage {
			err = errors.New("SourceStream.natsConfigurationConflict")
		} else {
			// Apply changed bounds to existing queues too; preserve unrelated
			// storage, replication and consumer configuration. Never purge.
			config := info.Config
			policy.apply(&config)
			_, err = js.UpdateStream(&config, nats.Context(ctx))
		}
		if err != nil {
			conn.Close()
			return nil, err
		}
	}
	for _, spec := range []struct{ subject, durable, stream string }{
		{"tier0.raw.source.*", "tier0-source-raw-v1", "TIER0_SOURCE_RAW"},
		{"tier0.events.topic.*", "tier0-uns-event-match-v1", "TIER0_UNS_EVENTS"},
		{"tier0.delivery.task.*", "tier0-source-delivery-v1", "TIER0_SOURCE_DELIVERY"},
	} {
		// PullSubscribe validates existing durable configuration instead of
		// updating it. Preserve the consumer and its pending delivery state
		// while aligning the execution allowance across binary upgrades.
		info, infoErr := js.ConsumerInfo(spec.stream, spec.durable, nats.Context(ctx))
		if infoErr != nil && !errors.Is(infoErr, nats.ErrConsumerNotFound) {
			conn.Close()
			return nil, infoErr
		}
		if infoErr == nil && info.Config.AckWait != workerAckWait {
			config := info.Config
			config.AckWait = workerAckWait
			if _, err := js.UpdateConsumer(spec.stream, &config, nats.Context(ctx)); err != nil {
				conn.Close()
				return nil, err
			}
		}
		sub, err := js.PullSubscribe(spec.subject, spec.durable, nats.BindStream(spec.stream), nats.ManualAck(), nats.AckExplicit(), nats.AckWait(workerAckWait), nats.MaxAckPending(1), nats.MaxDeliver(-1))
		if err != nil {
			conn.Close()
			return nil, err
		}
		w.subscriptions = append(w.subscriptions, sub)
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	if err := repo.ConfigureResourceEventPublisher(repo.GetUnsConn(ctx), w.publish); err != nil {
		cancel()
		conn.Close()
		return nil, err
	}
	sink.SetResourceEventPublisher(w.publish)
	w.ready.Store(true)
	go w.run(runCtx)
	return w, nil
}

func (w *Worker) Ready() bool {
	return w != nil && w.connection != nil && w.ready.Load() && w.connection.IsConnected()
}

func (w *Worker) WatchState(query sourcestream.WatchQuery) string {
	if !w.Ready() {
		return "starting"
	}
	state := w.watchStatus.state(query)
	if state == "starting" {
		return w.programWatches.state(query)
	}
	return state
}
func (w *Worker) Close() {
	if w == nil {
		return
	}
	w.cancel()
	<-w.done
	w.connection.Close()
	w.ready.Store(false)
}

func (w *Worker) publish(ctx context.Context, subject, id string, payload []byte) error {
	if int64(len(payload)) > w.connection.MaxPayload() {
		return errors.New("SourceStream.messageTooLarge")
	}
	if !time.Now().Before(w.policy.deadline(subject, payload, time.Now())) {
		// Message retention is enforced at publish time as well as consumption.
		logx.WithContext(ctx).Infof("SourceStream expired message discarded: subject=%s id=%s", subject, id)
		return nil
	}
	_, err := w.js.Publish(subject, payload, nats.MsgId(id), nats.Context(ctx))
	return err
}

func (w *Worker) run(ctx context.Context) {
	defer close(w.done)
	for ctx.Err() == nil {
		owner, err := repo.NewSourceStreamRepo(ctx).OwnWorker(ctx, workerOwner)
		if err == nil {
			ownerCtx, cancel := context.WithCancel(ctx)
			var jobs sync.WaitGroup
			start := func(fn func(context.Context)) { jobs.Add(1); go func() { defer jobs.Done(); fn(ownerCtx) }() }
			start(w.sourceLoop)
			start(w.acquisitionLoop)
			start(w.service.ManagedServersLoop)
			start(w.intervalLoop)
			start(w.deliveryOutboxLoop)
			for i, sub := range w.subscriptions {
				i, sub := i, sub
				start(func(ctx context.Context) { w.consume(ctx, i, sub) })
			}
			ticker := time.NewTicker(2 * time.Second)
			monitor := true
			for monitor {
				select {
				case <-ctx.Done():
					monitor = false
				case <-ticker.C:
					pingCtx, pingCancel := context.WithTimeout(ownerCtx, time.Second)
					if owner.PingContext(pingCtx) != nil {
						monitor = false
					}
					pingCancel()
				}
			}
			ticker.Stop()
			cancel()
			jobs.Wait()
			repo.ReleaseStreamWorker(owner, workerOwner)
		} else if !errors.Is(err, repo.ErrConflict) {
			logx.WithContext(ctx).Error("SourceStream worker ownership unavailable")
		}
		if !pause(ctx, time.Second) {
			return
		}
	}
}

func (w *Worker) consume(ctx context.Context, kind int, sub *nats.Subscription) {
	for ctx.Err() == nil {
		messages, err := sub.Fetch(1, nats.MaxWait(time.Second))
		if err != nil {
			if !pause(ctx, 100*time.Millisecond) {
				return
			}
			continue
		}
		for _, message := range messages {
			if ctx.Err() != nil {
				return
			}
			metadata, err := message.Metadata()
			if err != nil {
				continue
			}
			deadline := w.policy.deadline(message.Subject, message.Data, metadata.Timestamp)
			if !time.Now().Before(deadline) {
				logx.WithContext(ctx).Infof("SourceStream expired queued message discarded: stream=%s sequence=%d", metadata.Stream, metadata.Sequence.Stream)
				_ = message.AckSync()
				continue
			}
			expiryCtx, expiryCancel := context.WithDeadline(ctx, deadline)
			opCtx, cancel := context.WithTimeout(expiryCtx, workerOperationTimeout)
			var processErr error
			switch kind {
			case 0:
				var input sourcestream.RawInput
				processErr = decodeIngress(message.Data, &input)
				if processErr == nil && message.Subject != "tier0.raw.source."+strconv.FormatInt(input.SourceID, 10) {
					processErr = repo.ErrInvalidArgument
				}
				if processErr == nil {
					processErr = w.service.ProcessRaw(opCtx, input, w.sink)
				}
			case 1:
				var event dataingest.StreamEvent
				processErr = decodeIngress(message.Data, &event)
				canonical := event.Origin == "metadata" || event.MetadataTopicID != "" || event.CollectionID != ""
				expectedSubject := "tier0.events.topic." + strconv.FormatInt(event.TopicID, 10)
				if canonical {
					expectedSubject = repo.MetadataEventSubject(event.MetadataTopicID, event.CollectionID)
				}
				if processErr == nil && message.Subject != expectedSubject {
					processErr = repo.ErrInvalidArgument
				}
				if processErr == nil && !canonical {
					processErr = w.service.MatchEvent(opCtx, event)
				}
				if processErr == nil {
					processErr = w.service.MatchProgramEvent(opCtx, event, w.sink)
				}
			case 2:
				var task struct {
					ID string `json:"id"`
				}
				processErr = decodeIngress(message.Data, &task)
				if processErr == nil && message.Subject != "tier0.delivery.task."+task.ID {
					processErr = repo.ErrInvalidArgument
				}
				if processErr == nil {
					// Old envelopes contain only id. Read the persistent ingress
					// time so upgrading cannot renew an old device command.
					stored, taskErr := repo.NewSourceStreamRepo(opCtx).Task(opCtx, task.ID)
					processErr = taskErr
					if taskErr == nil {
						if receivedDeadline := time.UnixMilli(stored.ReceivedAt).Add(w.policy.maxAge); receivedDeadline.Before(deadline) {
							deadline = receivedDeadline
						}
						if !time.Now().Before(deadline) {
							_, processErr = repo.NewSourceStreamRepo(opCtx).ExpireStreamTasks(opCtx, time.Now().Add(-w.policy.maxAge).UnixMilli())
						} else {
							deliveryCtx, deliveryCancel := context.WithDeadline(opCtx, deadline)
							processErr = w.service.Deliver(deliveryCtx, task.ID)
							deliveryCancel()
						}
					}
				}
			}
			cancel()
			expiryCancel()
			// A timeout must not restart an expired operation. Dispatching
			// commands retain the existing result-unknown safety semantics.
			if !time.Now().Before(deadline) {
				processErr = nil
			}
			if processErr == nil {
				if err := message.AckSync(); err != nil {
					logx.WithContext(ctx).Error("SourceStream consumer acknowledgement failed")
				}
				continue
			}
			terminal, failureClass, reason := failureDisposition(processErr)
			if metadata.NumDelivered >= 5 || terminal {
				// Business DLQ is explicit, not a built-in JetStream promise.
				id := fmt.Sprintf("%s:%d", metadata.Stream, metadata.Sequence.Stream)
				dead := nats.NewMsg("tier0.dlq." + metadata.Stream)
				dead.Data = message.Data
				dead.Header.Set(nats.MsgIdHdr, id)
				dead.Header.Set("Tier0-Original-Subject", message.Subject)
				dead.Header.Set("Tier0-Error", "SourceStream.executionFailed")
				dead.Header.Set("Tier0-Failure-Class", failureClass)
				dead.Header.Set("Tier0-Error-Reason", reason)
				dead.Header.Set("Tier0-Delivery-Count", strconv.FormatUint(metadata.NumDelivered, 10))
				if _, err := w.js.PublishMsg(dead, nats.Context(ctx)); err == nil {
					_ = message.AckSync()
				} else {
					logx.WithContext(ctx).Error("SourceStream failure isolation publish failed; original message remains unacknowledged")
				}
			} else {
				_ = message.NakWithDelay(time.Duration(metadata.NumDelivered) * time.Second)
			}
		}
	}
}

func (w *Worker) deliveryOutboxLoop(ctx context.Context) {
	r := repo.NewSourceStreamRepo(ctx)
	for ctx.Err() == nil {
		if _, err := r.ExpireStreamTasks(ctx, time.Now().Add(-w.policy.maxAge).UnixMilli()); err != nil {
			logx.WithContext(ctx).Error("SourceStream task expiry failed")
			if !pause(ctx, 500*time.Millisecond) {
				return
			}
			continue
		}
		tasks, err := r.PendingDelivery(ctx)
		if err == nil {
			for _, task := range tasks {
				data, _ := json.Marshal(map[string]any{"id": task.ID, "receivedAt": task.ReceivedAt})
				if err := w.publish(ctx, "tier0.delivery.task."+task.ID, task.ID, data); err != nil {
					break
				}
				if err := r.MarkDeliverySent(ctx, task.ID); err != nil {
					break
				}
			}
		}
		if !pause(ctx, 500*time.Millisecond) {
			return
		}
	}
}

type watchedSource struct {
	signature string
	cancel    context.CancelFunc
	done      chan struct{}
}

type sourceWatchSpec struct {
	Revision       int64
	Refs           []string
	QoS            int
	AcquisitionKey string
}

func (w *Worker) intervalLoop(ctx context.Context) {
	published := map[int64]string{}
	for ctx.Err() == nil {
		plans, err := w.service.Plans(ctx)
		if err == nil {
			if err := w.publishProgramIntervals(ctx, plans, time.Now().UTC()); err != nil && !errors.Is(err, repo.ErrConflict) {
				logx.WithContext(ctx).Error("SourceStream program interval publication failed")
			}
			active := map[int64]bool{}
			for _, plan := range plans {
				if plan.Definition.Program == nil || plan.Definition.Program.Trigger.Kind != sourcestream.IntervalTrigger {
					continue
				}
				active[plan.StreamID] = true
				now := time.Now().UTC()
				interval := int64(plan.Definition.Program.Trigger.IntervalMs)
				if interval < sourcestream.MinIntervalMs {
					continue
				}
				id := fmt.Sprintf("interval:%d:%d:%d", plan.StreamID, plan.VersionID, now.UnixMilli()/interval)
				if published[plan.StreamID] == id {
					continue
				}
				input := sourcestream.RawInput{MessageID: id, Trigger: sourcestream.IntervalTrigger, Plans: []sourcestream.ExecutionRef{plan.ExecutionRef}, Input: sourcestream.SourceInput{ResourceRef: "interval", ReceivedAt: now.UnixMilli(), Data: map[string]any{"timestamp": now.UnixMilli()}}}
				raw, err := json.Marshal(input)
				if err == nil {
					err = w.publish(ctx, "tier0.raw.source.0", id, raw)
				}
				if err == nil {
					published[plan.StreamID] = id
				} else {
					logx.WithContext(ctx).Errorf("SourceStream interval publication failed: stream=%d", plan.StreamID)
				}
			}
			for id := range published {
				if !active[id] {
					delete(published, id)
				}
			}
		}
		if !pause(ctx, 250*time.Millisecond) {
			return
		}
	}
}

func (w *Worker) sourceLoop(ctx context.Context) {
	watchers := map[int64]watchedSource{}
	defer func() {
		for _, watch := range watchers {
			watch.cancel()
		}
		for _, watch := range watchers {
			<-watch.done
		}
	}()
	for ctx.Err() == nil {
		plans, err := w.service.WatchPlans(ctx)
		if err == nil {
			programPlans := plans[:0]
			for _, plan := range plans {
				if (plan.Entry != nil || plan.Definition.Program != nil) && plan.SourceAcquisitionKey() == "" {
					programPlans = append(programPlans, plan)
				}
			}
			plans = programPlans
			wanted, candidateKeys := watchSpecs(plans)
			activePlans := []sourcestream.ExecutionPlan{}
			for _, plan := range plans {
				if !plan.Candidate {
					activePlans = append(activePlans, plan)
				}
				if _, _, watched := plan.SourceEventScope(); watched && plan.Entry == nil && plan.Definition.Program == nil {
					source, err := repo.NewSourceStreamRepo(ctx).Source(ctx, plan.SourceID)
					if err != nil {
						continue
					}
					var def sourcestream.SourceDefinition
					if json.Unmarshal([]byte(source.Document), &def) != nil {
						continue
					}
					spec := wanted[plan.SourceID]
					if def.MQTT != nil && spec.QoS < def.MQTT.QoS {
						spec.QoS = def.MQTT.QoS
						wanted[plan.SourceID] = spec
					}
				}
			}
			activeScopes, _ := watchSpecs(activePlans)
			expansions := map[int64]string{}
			for id, key := range candidateKeys {
				spec := wanted[id]
				active, hasActive := activeScopes[id]
				// Preserve the legacy connection's configured QoS in fallback.
				if hasActive && active.QoS < spec.QoS {
					for _, plan := range activePlans {
						if plan.SourceID == id && plan.Entry == nil && plan.Definition.Program == nil {
							active.QoS = spec.QoS
							break
						}
					}
				}
				fullJSON, _ := json.Marshal(spec)
				activeJSON, _ := json.Marshal(active)
				if hasActive && string(fullJSON) == string(activeJSON) {
					continue
				}
				if w.programWatches.allowed(id, key, time.Now()) {
					expansions[id] = key
				} else if hasActive {
					wanted[id] = active
				} else {
					delete(wanted, id)
				}
			}
			signatures := map[int64]string{}
			for id, spec := range wanted {
				seen := map[string]bool{}
				for _, ref := range spec.Refs {
					seen[ref] = true
				}
				spec.Refs = nil
				for ref := range seen {
					spec.Refs = append(spec.Refs, ref)
				}
				sort.Strings(spec.Refs)
				wanted[id] = spec
				raw, _ := json.Marshal(spec)
				signatures[id] = fmt.Sprintf("%x", sha256.Sum256(raw))
			}
			for id, watch := range watchers {
				finished := false
				select {
				case <-watch.done:
					finished = true
				default:
				}
				if signatures[id] != watch.signature || finished {
					watch.cancel()
					<-watch.done
					delete(watchers, id)
				}
			}
			for id, spec := range wanted {
				if _, exists := watchers[id]; exists {
					continue
				}
				watchCtx, cancel := context.WithCancel(ctx)
				done := make(chan struct{})
				watchers[id] = watchedSource{signature: signatures[id], cancel: cancel, done: done}
				go func(id int64, spec sourceWatchSpec, candidateKey string) {
					defer close(done)
					for watchCtx.Err() == nil {
						observe, finish := w.watchStatus.begin(watchCtx, id, spec)
						attemptCtx := sourcestream.WithWatchObserver(watchCtx, func(ready bool) {
							observe(ready)
							if ready {
								w.programWatches.clear(id, candidateKey)
								if err := w.service.ActivateReadyProgramV2(watchCtx); err != nil {
									logx.WithContext(watchCtx).Error("SourceStream candidate activation failed")
								}
							}
						})
						_ = w.service.WatchInputs(attemptCtx, id, spec.Revision, spec.Refs, spec.QoS, func(input sourcestream.SourceInput) error {
							current, err := w.service.Plans(watchCtx)
							if err != nil {
								return err
							}
							refs := []sourcestream.ExecutionRef{}
							for _, plan := range current {
								if plan.SourceAcquisitionKey() != "" || plan.Entry == nil && plan.Definition.Program == nil {
									continue
								}
								if plan.SourceRevision == spec.Revision && plan.MatchesSourceInput(id, input.ResourceRef) {
									refs = append(refs, plan.ExecutionRef)
								}
							}
							if len(refs) == 0 {
								return nil
							}
							message := sourcestream.RawInput{MessageID: uuid.NewString(), SourceID: id, Input: input, Plans: refs}
							data, err := json.Marshal(message)
							if err != nil {
								return err
							}
							if err = w.publish(watchCtx, fmt.Sprintf("tier0.raw.source.%d", id), message.MessageID, data); err != nil {
								return err
							}
							// PubAck is the durable acceptance boundary. A telemetry
							// update failure must not request another device delivery.
							if err := repo.NewSourceStreamRepo(watchCtx).ObserveSourceData(watchCtx, id, spec.Revision, input.ReceivedAt); err != nil {
								logx.WithContext(watchCtx).Error("SourceStream source observation update failed")
							}
							return nil
						})
						finish("source-watch-failed")
						if candidateKey != "" && watchCtx.Err() == nil {
							current, err := w.service.WatchPlans(watchCtx)
							if err == nil {
								_, currentKeys := watchSpecs(current)
								if currentKeys[id] == candidateKey {
									w.programWatches.record(id, candidateKey, spec, time.Now())
									return // main loop restores active scope during backoff
								}
							}
						}
						if !pause(watchCtx, time.Second) {
							return
						}
					}
				}(id, spec, expansions[id])
			}
			if err := w.service.ActivateReadyProgramV2(ctx); err != nil {
				logx.WithContext(ctx).Error("SourceStream ready candidate activation failed")
			}
		}
		if !pause(ctx, time.Second) {
			return
		}
	}
}

func pause(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Collection scopes are independent from SDK entry subscriptions.
type mappedSourceWatch struct {
	revision    int64
	fingerprint string
	cancel      context.CancelFunc
	done        chan struct{}
}

func watchFinished(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func (w *Worker) acquisitionLoop(ctx context.Context) {
	watchers := map[string]mappedSourceWatch{}
	defer func() {
		for _, watch := range watchers {
			watch.cancel()
		}
		for _, watch := range watchers {
			<-watch.done
		}
	}()
	for ctx.Err() == nil {
		plans, err := w.service.WatchPlans(ctx)
		if err == nil {
			wanted := map[string][]sourcestream.ExecutionPlan{}
			for _, plan := range plans {
				if acquisition := plan.SourceAcquisitionKey(); acquisition != "" {
					query := plan.ProgramWatchQuery()
					raw, _ := json.Marshal(query)
					key := fmt.Sprintf("program:%d:%x", plan.SourceID, sha256.Sum256(raw))
					wanted[key] = append(wanted[key], plan)
					continue
				}
				if plan.Definition.Model == sourcestream.Publish && plan.Definition.Program == nil && plan.Entry == nil {
					key := fmt.Sprint(plan.SourceID)
					if plan.Definition.Collection == nil || plan.Definition.Collection.MQTT == nil {
						data, _ := json.Marshal(plan.Definition.Collection)
						refs, _ := json.Marshal(plan.Definition.ResourceRefs())
						key += fmt.Sprintf(":%x", sha256.Sum256(append(data, refs...)))
					}
					wanted[key] = append(wanted[key], plan)
				}
			}
			for id, watch := range watchers {
				if watchFingerprint(wanted[id]) != watch.fingerprint || watchFinished(watch.done) {
					watch.cancel()
					<-watch.done
					delete(watchers, id)
				}
			}
			for key, group := range wanted {
				id, revision := group[0].SourceID, group[0].SourceRevision
				if _, exists := watchers[key]; exists {
					continue
				}
				watchCtx, cancel := context.WithCancel(ctx)
				done := make(chan struct{})
				watchers[key] = mappedSourceWatch{revision: revision, fingerprint: watchFingerprint(group), cancel: cancel, done: done}
				go func(id, revision int64, group []sourcestream.ExecutionPlan) {
					defer close(done)
					spec := sourceWatchSpec{Revision: revision}
					for _, plan := range group {
						if plan.Entry != nil {
							query := plan.ProgramWatchQuery()
							spec.Refs = append(spec.Refs, query.ResourceRefs...)
							spec.QoS, spec.AcquisitionKey = query.QoS, query.AcquisitionKey
							continue
						}
						spec.Refs = append(spec.Refs, plan.Definition.ResourceRefs()...)
						if plan.Definition.Collection != nil && plan.Definition.Collection.MQTT != nil {
							spec.QoS = max(spec.QoS, plan.Definition.Collection.MQTT.QoS)
						}
					}
					for watchCtx.Err() == nil {
						observe, finish := w.watchStatus.beginScope(watchCtx, id, "collection:"+key, spec)
						attemptCtx := sourcestream.WithWatchObserver(watchCtx, func(ready bool) {
							observe(ready)
							if ready && spec.AcquisitionKey != "" {
								if err := w.service.ActivateReadyProgramV2(watchCtx); err != nil {
									logx.WithContext(watchCtx).Error("SourceStream acquisition candidate activation failed")
								}
							}
						})
						_ = w.service.WatchMappedInputs(attemptCtx, id, revision, group, func(input sourcestream.SourceInput) error {
							current, err := w.service.Plans(watchCtx)
							if err != nil {
								return err
							}
							refs := []sourcestream.ExecutionRef{}
							for _, plan := range current {
								matches := plan.Definition.Model == sourcestream.Publish && plan.Definition.MatchesResource(input.ResourceRef)
								if plan.Entry != nil {
									matches = plan.MatchesSourceInput(id, input.ResourceRef)
								}
								if plan.SourceID == id && plan.SourceRevision == revision && groupContains(group, plan) && matches {
									refs = append(refs, plan.ExecutionRef)
								}
							}
							if len(refs) == 0 {
								return nil
							}
							message := sourcestream.RawInput{MessageID: uuid.NewString(), SourceID: id, Input: input, Plans: refs}
							data, err := json.Marshal(message)
							if err != nil {
								return err
							}
							if err = w.publish(watchCtx, fmt.Sprintf("tier0.raw.source.%d", id), message.MessageID, data); err != nil {
								return err
							}
							// PubAck is the durable acceptance boundary. A telemetry
							// update failure must not request another device delivery.
							if err := repo.NewSourceStreamRepo(watchCtx).ObserveSourceData(watchCtx, id, revision, input.ReceivedAt); err != nil {
								logx.WithContext(watchCtx).Error("SourceStream source observation update failed")
							}
							return nil
						})
						finish("source-watch-failed")
						if !pause(watchCtx, time.Second) {
							return
						}
					}
				}(id, revision, group)
			}
		}
		if !pause(ctx, time.Second) {
			return
		}
	}
}

func watchFingerprint(plans []sourcestream.ExecutionPlan) string {
	if len(plans) == 0 {
		return ""
	}
	refs := make([]string, 0, len(plans))
	for _, p := range plans {
		refs = append(refs, fmt.Sprintf("%d:%d:%d", p.StreamID, p.VersionID, p.SourceRevision))
	}
	sort.Strings(refs)
	return fmt.Sprint(refs)
}
func groupContains(group []sourcestream.ExecutionPlan, plan sourcestream.ExecutionPlan) bool {
	for _, p := range group {
		if p.ExecutionRef == plan.ExecutionRef {
			return true
		}
	}
	return false
}
