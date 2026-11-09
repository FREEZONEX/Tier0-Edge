package sourcestream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"time"

	"backend/internal/repo"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrEdgeJournalFull = errors.New("SourceStream.durableReceiveBackpressure")

type EdgeAcquisitionGap struct {
	Reason string `json:"reason"`
	From   string `json:"from"`
	To     string `json:"to"`
	At     int64  `json:"at"`
}

type durableEvent struct {
	EdgeSubscriptionEvent
	BusinessKey string `json:"businessKey"`
}
type durableConsumer struct {
	Resources []string `json:"resources"`
	Ack       int64    `json:"ack"`
	Delivered int64    `json:"delivered"`
	Expires   int64    `json:"expires"`
}
type durableJournal struct {
	ConfigRevision int64                      `json:"configRevision"`
	Epoch          string                     `json:"epoch"`
	Sequence       int64                      `json:"sequence"`
	Events         []durableEvent             `json:"events"`
	Consumers      map[string]durableConsumer `json:"consumers"`
	Confirmed      map[string]durableEvent    `json:"confirmed,omitempty"`
	Metadata       json.RawMessage            `json:"metadata,omitempty"`
	Gaps           []EdgeAcquisitionGap       `json:"gaps,omitempty"`
	State          string                     `json:"state"`
	Error          string                     `json:"error,omitempty"`
	Limit          int                        `json:"limit"`
	LastDataAt     int64                      `json:"lastDataAt"`
	Dropped        int64                      `json:"dropped"`
}

// The caller returns a device ACK only after this transaction succeeds.
// The source row lock fences concurrent disable, delete and configuration edits.
func withDurableJournal(ctx context.Context, id, revision int64, limit int, change func(*durableJournal) error, inactive ...bool) error {
	if limit < 64 || limit > 2048 {
		return repo.ErrInvalidArgument
	}
	return repo.GetUnsConn(ctx).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var source repo.UnsMeta
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("kind='source' AND id=? AND deleted_at=0", strconv.FormatInt(id, 10)).Take(&source).Error; err != nil {
			return err
		}
		var def struct {
			ConfigRevision int64  `json:"configRevision"`
			DesiredState   string `json:"desiredState"`
		}
		if json.Unmarshal(source.Data, &def) != nil || def.ConfigRevision != revision || (def.DesiredState != "enabled" && !(len(inactive) > 0 && inactive[0])) {
			return ErrEdgeReferenceRejected
		}
		var row repo.UnsMeta
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("kind='source_state' AND id=? AND deleted_at=0", source.ID).Take(&row).Error; err != nil {
			return err
		}
		var data struct {
			Journal *durableJournal   `json:"receiveJournal"`
			History []json.RawMessage `json:"receiveJournalHistory"`
		}
		if json.Unmarshal(row.Data, &data) != nil {
			return ErrExecutionFailed
		}
		j := data.Journal
		archived := false
		if j == nil || j.ConfigRevision != revision {
			previous := j
			j = &durableJournal{ConfigRevision: revision, Epoch: uuid.NewString(), Consumers: map[string]durableConsumer{}, Limit: limit, State: "starting"}
			if previous != nil {
				if len(data.History) >= 32 {
					return errors.New("SourceStream.durableJournalAuditFull")
				}
				audit, _ := json.Marshal(map[string]any{"configRevision": previous.ConfigRevision, "epoch": previous.Epoch, "sequence": previous.Sequence, "gaps": previous.Gaps, "confirmed": previous.Confirmed, "metadata": previous.Metadata, "lastDataAt": previous.LastDataAt, "archivedAt": time.Now().UnixMilli()})
				data.History = append(data.History, audit)
				archived = true
				j.Gaps = append(j.Gaps, previous.Gaps...)
				j.Dropped = previous.Dropped
				ack := previous.confirmedSequence()
				var first, last int64
				for _, event := range previous.Events {
					if event.Sequence > ack {
						if first == 0 {
							first = event.Sequence
						}
						last = event.Sequence
						j.Dropped++
					}
				}
				if first > 0 {
					j.addGap(EdgeAcquisitionGap{Reason: "configurationChangedBeforeCloudCommit", From: strconv.FormatInt(previous.ConfigRevision, 10) + "/" + strconv.FormatInt(first, 10), To: strconv.FormatInt(previous.ConfigRevision, 10) + "/" + strconv.FormatInt(last, 10), At: time.Now().UnixMilli()})
				}
			}
		}
		if j.Consumers == nil {
			j.Consumers = map[string]durableConsumer{}
		}
		j.Limit = limit
		j.expireConsumers()
		if err := change(j); err != nil {
			return err
		}
		raw, err := json.Marshal(j)
		if err != nil || len(raw) > 6*1024*1024 {
			return ErrEdgeJournalFull
		}
		if archived {
			history, err := json.Marshal(data.History)
			if err != nil || len(history) > 16*1024*1024 {
				return errors.New("SourceStream.durableJournalAuditFull")
			}
			return tx.Exec("UPDATE uns_meta SET data=jsonb_set(jsonb_set(data,'{receiveJournal}',?::jsonb,true),'{receiveJournalHistory}',?::jsonb,true),updated_at=? WHERE kind='source_state' AND id=?", string(raw), string(history), time.Now().UnixMilli(), source.ID).Error
		}
		return tx.Exec("UPDATE uns_meta SET data=jsonb_set(data,'{receiveJournal}',?::jsonb,true),updated_at=? WHERE kind='source_state' AND id=?", string(raw), time.Now().UnixMilli(), source.ID).Error
	})
}

func (j *durableJournal) compact() {
	if len(j.Consumers) == 0 {
		return
	}
	ack := j.confirmedSequence()
	if j.Confirmed == nil {
		j.Confirmed = map[string]durableEvent{}
	}
	for _, e := range j.Events {
		if e.Sequence <= ack {
			j.Confirmed[e.ResourceRef] = e
		}
	}
	n := 0
	// Keep the latest sample for bounded previews even after every reader ACKs.
	for n < len(j.Events)-1 && j.Events[n].Sequence <= ack {
		n++
	}
	j.Events = append([]durableEvent(nil), j.Events[n:]...)
}

func (j *durableJournal) confirmedSequence() int64 {
	if len(j.Consumers) == 0 {
		return 0
	}
	ack := j.Sequence
	for _, c := range j.Consumers {
		ack = min(ack, c.Ack)
	}
	return ack
}

func (j *durableJournal) expireConsumers() {
	cutoff := time.Now().Add(-24 * time.Hour).UnixMilli()
	for key, c := range j.Consumers {
		if c.Expires >= cutoff {
			continue
		}
		if c.Ack < j.Sequence {
			j.addGap(EdgeAcquisitionGap{Reason: "consumerRetentionExpired", From: strconv.FormatInt(c.Ack+1, 10), To: strconv.FormatInt(j.Sequence, 10), At: time.Now().UnixMilli()})
		}
		delete(j.Consumers, key)
	}
}

func (j *durableJournal) append(ref string, data json.RawMessage, receivedAt int64, businessKey string) error {
	if len(data) == 0 || len(data) > 256*1024 || businessKey == "" || len(businessKey) > 1024 {
		return ErrVendorResponse
	}
	for _, e := range j.Events {
		if e.BusinessKey == businessKey {
			if durableJSONEqual(e.Data, data) {
				return nil
			}
			return errors.New("SourceStream.durableEventIdentityConflict")
		}
	}
	j.compact()
	bytes := len(data)
	for _, e := range j.Events {
		bytes += len(e.Data)
	}
	if len(j.Events) >= j.Limit || bytes > 4*1024*1024 {
		return ErrEdgeJournalFull
	}
	j.Sequence++
	j.Events = append(j.Events, durableEvent{EdgeSubscriptionEvent: EdgeSubscriptionEvent{ConfigRevision: j.ConfigRevision, Epoch: j.Epoch, Sequence: j.Sequence, CollectedAt: receivedAt, ResourceRef: ref, Data: append(json.RawMessage(nil), data...)}, BusinessKey: businessKey})
	j.LastDataAt = receivedAt
	return nil
}

func durableJSONEqual(a, b []byte) bool {
	var left, right any
	da, db := json.NewDecoder(bytes.NewReader(a)), json.NewDecoder(bytes.NewReader(b))
	da.UseNumber()
	db.UseNumber()
	return da.Decode(&left) == nil && db.Decode(&right) == nil && reflect.DeepEqual(left, right)
}

func (j *durableJournal) addGap(g EdgeAcquisitionGap) {
	if len(j.Gaps) > 0 {
		last := &j.Gaps[len(j.Gaps)-1]
		if last.Reason == g.Reason && last.From == g.From && last.To == g.To {
			return
		}
	}
	if len(j.Gaps) >= 64 {
		j.Gaps[0] = EdgeAcquisitionGap{Reason: "multipleUnresolvedRanges", From: j.Gaps[0].From, To: j.Gaps[1].To, At: j.Gaps[0].At}
		j.Gaps = append(j.Gaps[:1], j.Gaps[2:]...)
	}
	j.Gaps = append(j.Gaps, g)
}

// Caller identity is hashed by ExchangeEdgeSubscription. No consumer may ACK
// another consumer's delivery, or an event that was never offered to it.
func (s *Service) exchangeDurableJournal(ctx context.Context, req EdgeSubscriptionRequest, key string, row repo.Source, limit int) (EdgeSubscriptionBatch, error) {
	out := EdgeSubscriptionBatch{}
	err := withDurableJournal(ctx, row.ID, row.ConfigRevision, limit, func(j *durableJournal) error {
		c, exists := j.Consumers[key]
		if req.Action == "cancel" {
			if req.Epoch != j.Epoch {
				return repo.ErrConflict
			}
			if exists {
				c.Expires = time.Now().UnixMilli()
				j.Consumers[key] = c
			}
			out = EdgeSubscriptionBatch{SubscriptionID: req.SubscriptionID, Epoch: j.Epoch, AcquisitionState: "stopped", Events: []EdgeSubscriptionEvent{}}
			return nil
		}
		if !exists {
			if len(j.Consumers) >= 64 {
				return ErrUnavailable
			}
			if len(j.Events) > 0 {
				c.Ack = j.Events[0].Sequence - 1
			} else {
				c.Ack = j.Sequence
			}
			c.Resources = append([]string(nil), req.Resources...)
		} else if !slices.Equal(c.Resources, req.Resources) {
			return repo.ErrConflict
		}
		if req.Epoch == j.Epoch {
			if req.AckSequence > c.Delivered {
				return repo.ErrInvalidArgument
			}
			c.Ack = max(c.Ack, req.AckSequence)
		}
		c.Expires = time.Now().Add(edgeSubscriptionLease).UnixMilli()
		out = EdgeSubscriptionBatch{SubscriptionID: req.SubscriptionID, Epoch: j.Epoch, ConfigRevision: j.ConfigRevision, AcquisitionState: j.State, Error: j.Error, LastDataAt: j.LastDataAt, LeaseExpiresAt: c.Expires, Events: []EdgeSubscriptionEvent{}, Gaps: append([]EdgeAcquisitionGap(nil), j.Gaps...), Dropped: j.Dropped}
		bytes := 0
		for _, entry := range j.Events {
			if entry.Sequence <= c.Ack || !slices.Contains(req.Resources, entry.ResourceRef) {
				continue
			}
			if len(out.Events) >= 64 || bytes+len(entry.Data) > 256*1024 {
				break
			}
			e := entry.EdgeSubscriptionEvent
			e.SourceID = req.ID
			e.SubscriptionID = req.SubscriptionID
			out.Events = append(out.Events, e)
			bytes += len(e.Data)
			c.Delivered = max(c.Delivered, e.Sequence)
		}
		j.Consumers[key] = c
		j.compact()
		return nil
	}, req.Action == "cancel")
	return out, err
}

func (j *durableJournal) active() bool {
	now := time.Now().UnixMilli()
	for _, c := range j.Consumers {
		if c.Expires > now {
			return true
		}
	}
	return false
}
