package sourcestreamworker

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"backend/internal/domain/sourcestream"
	"backend/internal/repo"
	"github.com/nats-io/nats.go"
)

type programWatchFailure struct {
	key      string
	spec     sourceWatchSpec
	attempts int
	next     time.Time
}

// Failed expansions do not repeatedly interrupt an active scope. The bounded
// attempts reset only when a different candidate version is staged.
type programWatchRetries struct {
	mu     sync.RWMutex
	failed map[int64]programWatchFailure
}

func (r *programWatchRetries) allowed(id int64, key string, now time.Time) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.failed[id]
	return !ok || item.key != key || item.attempts < 3 && !now.Before(item.next)
}
func (r *programWatchRetries) record(id int64, key string, spec sourceWatchSpec, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failed == nil {
		r.failed = map[int64]programWatchFailure{}
	}
	item := r.failed[id]
	if item.key != key {
		item = programWatchFailure{key: key}
	}
	item.attempts++
	item.spec = spec
	item.spec.Refs = append([]string(nil), spec.Refs...)
	backoff := 10 * time.Second * time.Duration(1<<min(item.attempts-1, 2))
	item.next = now.Add(backoff)
	r.failed[id] = item
}
func (r *programWatchRetries) clear(id int64, key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failed[id].key == key {
		delete(r.failed, id)
	}
}
func (r *programWatchRetries) state(query sourcestream.WatchQuery) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.failed[query.SourceID]
	if !ok || item.spec.Revision != query.ConfigRevision || item.spec.QoS < query.QoS || item.spec.AcquisitionKey != query.AcquisitionKey {
		return "starting"
	}
	for _, ref := range query.ResourceRefs {
		found := false
		for _, watched := range item.spec.Refs {
			found = found || ref == watched
		}
		if !found {
			return "starting"
		}
	}
	return "source-watch-failed"
}

func normalizeWatchSpec(spec sourceWatchSpec) sourceWatchSpec {
	seen := map[string]bool{}
	for _, ref := range spec.Refs {
		seen[ref] = true
	}
	spec.Refs = nil
	for ref := range seen {
		spec.Refs = append(spec.Refs, ref)
	}
	sort.Strings(spec.Refs)
	return spec
}
func watchSpecs(plans []sourcestream.ExecutionPlan) (map[int64]sourceWatchSpec, map[int64]string) {
	specs := map[int64]sourceWatchSpec{}
	versions := map[int64]map[string]bool{}
	for _, plan := range plans {
		if plan.SourceAcquisitionKey() != "" {
			continue
		}
		refs, qos, watch := plan.SourceEventScope()
		if !watch {
			continue
		}
		spec := specs[plan.SourceID]
		if spec.Revision != 0 && spec.Revision != plan.SourceRevision {
			continue
		}
		spec.Revision = plan.SourceRevision
		spec.Refs = append(spec.Refs, refs...)
		if qos > spec.QoS {
			spec.QoS = qos
		}
		specs[plan.SourceID] = spec
		if plan.Candidate {
			if versions[plan.SourceID] == nil {
				versions[plan.SourceID] = map[string]bool{}
			}
			versions[plan.SourceID][fmt.Sprintf("%d:%d", plan.StreamID, plan.VersionID)] = true
		}
	}
	keys := map[int64]string{}
	for id, set := range versions {
		list := []string{}
		for version := range set {
			list = append(list, version)
		}
		sort.Strings(list)
		keys[id] = strings.Join(list, ",")
	}
	for id, spec := range specs {
		specs[id] = normalizeWatchSpec(spec)
	}
	return specs, keys
}

func (w *Worker) publishProgramIntervals(ctx context.Context, plans []sourcestream.ExecutionPlan, now time.Time) error {
	r := repo.NewSourceStreamRepo(ctx)
	for _, plan := range plans {
		if plan.Candidate || plan.Entry == nil || plan.Entry.Kind != "interval" {
			continue
		}
		version, err := r.Version(ctx, plan.StreamID, plan.VersionID)
		if err != nil {
			return err
		}
		if _, err = r.ScheduleProgramInterval(ctx, repo.ProgramIntervalSchedule{StreamID: plan.StreamID, VersionID: plan.VersionID, EntryID: plan.EntryID, IntervalMs: int64(plan.Entry.IntervalMs), FirstDueAt: version.ActivatedAt + int64(plan.Entry.IntervalMs), Now: now.UnixMilli(), MaxSlots: 16}); err != nil {
			return err
		}
	}
	pending, err := r.PendingProgramIntervals(ctx, 100)
	if err != nil {
		return err
	}
	for _, item := range pending {
		var slot struct {
			StreamID  int64  `json:"streamId"`
			VersionID int64  `json:"versionId"`
			EntryID   string `json:"entryId"`
			DueAt     int64  `json:"dueAt"`
		}
		if json.Unmarshal([]byte(item.Payload), &slot) != nil || slot.StreamID != item.StreamID || slot.VersionID != item.VersionID || slot.EntryID != item.EntryID || slot.DueAt != item.DueAt {
			return repo.ErrInvalidArgument
		}
		input := sourcestream.RawInput{MessageID: item.ID, Trigger: sourcestream.IntervalTrigger, Input: sourcestream.SourceInput{ResourceRef: "interval", ReceivedAt: item.DueAt, Data: map[string]any{"timestamp": item.DueAt}}}
		raw, err := json.Marshal(input)
		if err != nil {
			return err
		}
		// Offline dependencies can remove an intent's active execution plan.
		// Expire those intents too, so an old full batch cannot starve live ones.
		if !now.Before(w.policy.deadline("tier0.raw.source.0", raw, now)) {
			if err := r.MarkProgramIntervalDiscarded(ctx, item.ID); err != nil {
				return err
			}
			continue
		}
		var ref *sourcestream.ExecutionRef
		for _, plan := range plans {
			if !plan.Candidate && plan.Entry != nil && plan.Entry.Kind == "interval" && plan.StreamID == item.StreamID && plan.VersionID == item.VersionID && plan.EntryID == item.EntryID {
				copied := plan.ExecutionRef
				ref = &copied
				break
			}
		}
		if ref == nil {
			continue
		}
		input.Plans = []sourcestream.ExecutionRef{*ref}
		raw, err = json.Marshal(input)
		if err != nil {
			return err
		}
		// The cursor/intent survives a crash. MsgId is unchanged on replay and
		// the publication is marked only after durable JetStream acceptance.
		if int64(len(raw)) > w.connection.MaxPayload() {
			return sourcestream.ErrUnavailable
		}
		if _, err = w.js.Publish("tier0.raw.source.0", raw, nats.MsgId(item.ID), nats.Context(ctx)); err != nil {
			return err
		}
		if err = r.MarkProgramIntervalSent(ctx, item.ID); err != nil {
			return err
		}
	}
	return nil
}
