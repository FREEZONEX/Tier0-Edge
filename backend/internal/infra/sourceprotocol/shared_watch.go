package sourceprotocol

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"backend/internal/domain/sourcestream"
	"golang.org/x/sync/errgroup"
)

// 同一连接与采集需求只建立一个协议订阅；生命周期仍由各消费者独立持有。
type sharedWatch struct {
	mu        sync.Mutex
	consumers map[*watchConsumer]struct{}
	cancel    context.CancelFunc
	done      chan struct{}
	err       error
	ready     bool
	closing   bool
}

type watchConsumer struct {
	ctx  context.Context
	emit func(Input) error
}

// A binding can select several ranges. Share its overlap with other local and
// Fleet bindings at the range/cadence boundary, keeping caller callbacks serial.
func (r *Runtime) sharedModbusRanges(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, emit func(Input) error) error {
	if def.Modbus == nil || len(def.Modbus.Ranges) == 0 || emit == nil {
		return ErrProtocol
	}
	if len(def.Modbus.Ranges) == 1 {
		return r.sharedSourceWatch(ctx, id, def, credential, nil, emit)
	}
	group, life := errgroup.WithContext(ctx)
	var callbackMu sync.Mutex
	ready := make(map[string]bool)
	for _, resource := range def.Modbus.Ranges {
		scoped := def.Connection()
		scoped.Modbus.Ranges = []sourcestream.ModbusRange{resource}
		group.Go(func() error {
			observed := sourcestream.WithWatchObserver(life, func(value bool) {
				callbackMu.Lock()
				defer callbackMu.Unlock()
				allReady := len(ready) == len(def.Modbus.Ranges)
				if value {
					ready[resource.ID] = true
				} else {
					delete(ready, resource.ID)
				}
				if now := len(ready) == len(def.Modbus.Ranges); now != allReady {
					sourcestream.ReportWatchReady(ctx, now)
				}
			})
			return r.sharedSourceWatch(observed, id, scoped, credential, nil, func(input Input) error {
				callbackMu.Lock()
				defer callbackMu.Unlock()
				return emit(input)
			})
		})
	}
	return group.Wait()
}

func (r *Runtime) sharedSourceWatch(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, known []string, emit func(Input) error) error {
	connection, err := sourceConnectionKey(ctx, id, def, credential)
	if err != nil || emit == nil {
		return ErrProtocol
	}
	// map 的 JSON 编码稳定排序，统一 topicFilters 与 subscriptions 两种有效输入。
	var acquisition any
	if def.Protocol == sourcestream.ModbusTCP {
		acquisition = def.Modbus
	}
	if def.MQTT != nil {
		acquisition = mqttSubscriptions(def)
	}
	if def.Protocol == sourcestream.OPCUA && def.OPCUA != nil {
		nodes := append([]string(nil), def.OPCUA.NodeIDs...)
		sort.Strings(nodes)
		acquisition = struct {
			Nodes              []string
			SamplingIntervalMs int
		}{nodes, def.OPCUA.SamplingIntervalMs}
	}
	raw, err := json.Marshal(acquisition)
	if err != nil {
		return ErrProtocol
	}
	key := connection + fmt.Sprintf(":%x", sha256.Sum256(raw))
	consumer := &watchConsumer{ctx: ctx, emit: emit}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		r.sharedMu.Lock()
		if r.shared == nil {
			r.shared = make(map[string]*sharedWatch)
		}
		watch := r.shared[key]
		if watch != nil {
			watch.mu.Lock()
			if watch.closing {
				watch.mu.Unlock()
				r.sharedMu.Unlock()
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-watch.done:
					continue
				}
			}
			watch.consumers[consumer] = struct{}{}
			ready := watch.ready
			watch.mu.Unlock()
			r.sharedMu.Unlock()
			sourcestream.ReportWatchReady(ctx, ready)
		} else {
			if len(r.shared) >= sourcePoolLimit {
				r.sharedMu.Unlock()
				return sourcestream.ErrUnavailable
			}
			life, cancel := context.WithCancel(context.WithoutCancel(ctx))
			watch = &sharedWatch{consumers: map[*watchConsumer]struct{}{consumer: {}}, cancel: cancel, done: make(chan struct{})}
			r.shared[key] = watch
			r.sharedMu.Unlock()
			go func(w *sharedWatch) {
				observed := sourcestream.WithWatchObserver(life, func(ready bool) {
					w.mu.Lock()
					w.ready = ready
					listeners := make([]*watchConsumer, 0, len(w.consumers))
					for c := range w.consumers {
						listeners = append(listeners, c)
					}
					w.mu.Unlock()
					for _, c := range listeners {
						if c.ctx.Err() == nil {
							sourcestream.ReportWatchReady(c.ctx, ready)
						}
					}
				})
				err := r.watchWithKnownSubscriptions(observed, id, def, credential, known, func(input Input) error {
					w.mu.Lock()
					listeners := make([]*watchConsumer, 0, len(w.consumers))
					for c := range w.consumers {
						listeners = append(listeners, c)
					}
					w.mu.Unlock()
					// 本地回调仍在协议 ACK 之前完成持久接收，远端缓存回调必须有界且不等待网络。
					accepted := 0
					for _, c := range listeners {
						if c.ctx.Err() != nil {
							continue
						}
						raw, e := json.Marshal(input)
						if e != nil {
							return e
						}
						var copy Input
						if e = json.Unmarshal(raw, &copy); e != nil {
							return e
						}
						if e = c.emit(copy); e != nil {
							if c.ctx.Err() == nil {
								return e
							}
							continue
						}
						accepted++
					}
					if accepted == 0 {
						return context.Canceled
					}
					return nil
				})
				r.sharedMu.Lock()
				w.mu.Lock()
				w.err = err
				w.closing = true
				delete(r.shared, key)
				w.mu.Unlock()
				close(w.done)
				r.sharedMu.Unlock()
			}(watch)
		}
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-watch.done:
			watch.mu.Lock()
			err = watch.err
			watch.mu.Unlock()
		}
		watch.mu.Lock()
		delete(watch.consumers, consumer)
		if len(watch.consumers) == 0 {
			watch.closing = true
			watch.cancel()
		}
		watch.mu.Unlock()
		sourcestream.ReportWatchReady(ctx, false)
		return err
	}
}
