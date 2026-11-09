package unsv2

import (
	"backend/internal/domain/sourcestream"
	"backend/internal/domain/unsmeta"
	"backend/internal/fieldpath"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type acquisitionState struct {
	Coverage   acquisitionCoverage `json:"coverage"`
	Revision   int64               `json:"revision"`
	Status     string              `json:"status"`
	LeaseToken string              `json:"leaseToken,omitempty"`
	LeaseUntil int64               `json:"leaseUntil"`
	NextRunAt  int64               `json:"nextRunAt"`
	CycleID    string              `json:"cycleId"`
	Page       int                 `json:"page"`
	Cursor     string              `json:"cursor"`
	LastDataAt int64               `json:"lastDataAt"`
	Records    int64               `json:"records"`
	Sequence   int64               `json:"sequence"`
	Retries    int64               `json:"retries"`
	LastError  string              `json:"lastError"`
	Phase      string              `json:"phase"`
}
type acquisitionTask struct {
	source    repo.UnsMeta
	data      SourceData
	bindingID string
	binding   Binding
	release   Release
	a         Acquisition
	state     acquisitionState
}

// One bounded coordinator, with durable per-binding leases across processes.
// Polling and long-lived native watches have independent bounded pools.
// A full subscription pool must never starve polling Sources.
func (s *Service) runAcquisition(ctx context.Context) {
	if s.sourceRuntime == nil {
		return
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	pollSlots := make(chan struct{}, 16)
	watchSlots := make(chan struct{}, 16)
	waitingSlots := make(chan struct{}, 1)
	active := sync.Map{}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	after := ""
	turn := uint64(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		var rows []repo.UnsMeta
		err := repo.GetUnsConn(ctx).Where("kind='source' AND deleted_at=0 AND data->>'schemaVersion'='1' AND id>?", after).Order("id").Limit(100).Find(&rows).Error
		if err != nil {
			if ctx.Err() == nil {
				logx.Error("Shared acquisition coordinator query failed")
			}
			continue
		}
		if len(rows) == 100 {
			after = rows[len(rows)-1].ID
		} else {
			after = ""
		}
		// A full set of asynchronous claims can occupy every slot even when
		// those claims soon discover a not-yet-due Binding. Rotate both levels
		// so the same low IDs cannot monopolize admission on every tick.
		turn++
		if len(rows) > 0 {
			start := int(turn % uint64(len(rows)))
			rows = append(rows[start:], rows[:start]...)
		}
		for _, row := range rows {
			var data SourceData
			if json.Unmarshal(row.Data, &data) != nil {
				continue
			}
			bindingIDs := make([]string, 0, len(data.Config.Streams))
			for id := range data.Config.Streams {
				bindingIDs = append(bindingIDs, id)
			}
			sort.Strings(bindingIDs)
			if len(bindingIDs) > 0 {
				start := int(turn % uint64(len(bindingIDs)))
				bindingIDs = append(bindingIDs[start:], bindingIDs[:start]...)
			}
			for _, bindingID := range bindingIDs {
				binding := data.Config.Streams[bindingID]
				if !binding.Enabled {
					continue
				}
				a, err := acquisition(binding.Acquisition, data.Adapter, true)
				if err != nil {
					continue
				}
				slots := pollSlots
				if a.Mode == "watch" {
					slots = watchSlots
				}
				identity := row.ID + "/" + bindingID
				if _, loaded := active.LoadOrStore(identity, true); loaded {
					continue
				}
				select {
				case slots <- struct{}{}:
				default:
					active.Delete(identity)
					if a.Mode == "watch" {
						// Capacity diagnostics are bounded too. A blocked state row
						// cannot delay scanning the healthy polling pool.
						select {
						case waitingSlots <- struct{}{}:
							wg.Add(1)
							go func(row repo.UnsMeta, bindingID string) {
								defer wg.Done()
								defer func() { <-waitingSlots }()
								s.markAcquisitionCapacityWait(ctx, row, bindingID)
							}(row, bindingID)
						default:
						}
					}
					continue
				}
				wg.Add(1)
				go func(row repo.UnsMeta, data SourceData, bindingID, identity string, binding Binding) {
					defer wg.Done()
					defer func() { <-slots; active.Delete(identity) }()
					task, err := s.claimAcquisition(ctx, row, data, bindingID, binding)
					if err != nil || task == nil {
						return
					}
					s.executeAcquisition(ctx, *task)
				}(row, data, bindingID, identity, binding)
			}
		}
	}
}

func (s *Service) claimAcquisition(ctx context.Context, row repo.UnsMeta, data SourceData, bindingID string, binding Binding) (*acquisitionTask, error) {
	a, err := acquisition(binding.Acquisition, data.Adapter, true)
	if err != nil {
		return nil, err
	}
	stream, err := meta(ctx, "stream", binding.StreamID, false)
	if err != nil {
		return nil, err
	}
	var def StreamData
	if json.Unmarshal(stream.Data, &def) != nil {
		return nil, errors.New("invalid shared stream")
	}
	release, ok := def.Releases[strconv.Itoa(binding.StreamRelease)]
	if !ok {
		return nil, errors.New("missing shared release")
	}
	task := &acquisitionTask{source: row, data: data, bindingID: bindingID, binding: binding, release: release, a: a}
	err = repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock_shared(814206108)").Error; err != nil {
			return err
		}
		var current repo.UnsMeta
		if err := tx.Where("kind='source' AND id=? AND revision=? AND deleted_at=0", row.ID, row.Revision).Take(&current).Error; err != nil {
			return err
		}
		var stateRow repo.UnsMeta
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("kind='source_state' AND id=?", row.ID).Take(&stateRow).Error; err != nil {
			return err
		}
		var state struct {
			Streams map[string]json.RawMessage `json:"streams"`
		}
		if err := json.Unmarshal(stateRow.Data, &state); err != nil {
			return err
		}
		_ = json.Unmarshal(state.Streams[bindingID], &task.state)
		now := time.Now().UnixMilli()
		if task.state.Revision == row.Revision && task.state.LeaseUntil > now {
			return repo.ErrConflict
		}
		if task.state.Revision == row.Revision && task.state.NextRunAt > now {
			return repo.ErrConflict
		}
		if task.state.Revision != row.Revision {
			task.state = acquisitionState{Revision: row.Revision, Sequence: task.state.Sequence, Coverage: task.state.Coverage, LastDataAt: task.state.LastDataAt}
		}
		if task.state.CycleID == "" {
			task.state.CycleID = uuid.NewString()
		}
		if a.Mode == "watch" {
			if task.state.Coverage.WatchStartedAt > 0 {
				task.state.Coverage.interrupt(max(task.state.LastDataAt, task.state.Coverage.WatchStartedAt), "WATCH_RECONNECTED")
			}
			task.state.Coverage.WatchStartedAt = now
		}
		task.state.LeaseToken = uuid.NewString()
		task.state.LeaseUntil = now + 45000
		task.state.Status = "starting"
		return saveAcquisitionState(tx, row.ID, bindingID, task.state)
	})
	return task, err
}

func saveAcquisitionState(tx *gorm.DB, id, bindingID string, state acquisitionState) error {
	raw, _ := json.Marshal(state)
	if err := tx.Exec("UPDATE public.uns_meta SET data=jsonb_set(data,ARRAY['streams',?]::text[],?::jsonb,true),revision=revision+1,updated_at=? WHERE kind='source_state' AND id=?", bindingID, string(raw), time.Now().UnixMilli(), id).Error; err != nil {
		return err
	}
	health := ""
	if state.Status == "failed" {
		health = "unhealthy"
	} else if state.LastDataAt > 0 && state.LastError == "" {
		health = "healthy"
	}
	if health != "" {
		return tx.Exec("UPDATE public.uns_meta SET data=jsonb_set(jsonb_set(data,'{health}',to_jsonb(?::text)),'{lastDataAt}',to_jsonb(GREATEST(COALESCE((data->>'lastDataAt')::bigint,0),?::bigint))) WHERE kind='source_state' AND id=?", health, state.LastDataAt, id).Error
	}
	return nil
}

// Lock ordering matches configuration writers: metadata gate then state row.
func (s *Service) acquisitionTransaction(ctx context.Context, t *acquisitionTask, action func(context.Context, *gorm.DB) error) error {
	return repo.MetadataEventTransaction(ctx, repo.GetUnsConn(ctx), func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock_shared(814206108)").Error; err != nil {
			return err
		}
		var source repo.UnsMeta
		if err := tx.Where("kind='source' AND id=? AND revision=? AND deleted_at=0", t.source.ID, t.source.Revision).Take(&source).Error; err != nil {
			return err
		}
		var state repo.UnsMeta
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("kind='source_state' AND id=?", t.source.ID).Take(&state).Error; err != nil {
			return err
		}
		var doc struct {
			Streams map[string]acquisitionState `json:"streams"`
		}
		if err := json.Unmarshal(state.Data, &doc); err != nil {
			return err
		}
		actual := doc.Streams[t.bindingID]
		if actual.LeaseToken != t.state.LeaseToken || actual.LeaseUntil < time.Now().UnixMilli() {
			return repo.ErrConflict
		}
		return action(repo.WithUnsTransaction(ctx, tx), tx)
	})
}

func acquisitionPath(value any, path string) any {
	if path == "" {
		return value
	}
	parts, err := fieldpath.Parse(path)
	if err != nil {
		return nil
	}
	result, _ := fieldpath.Value(value, parts)
	return result
}

func (s *Service) executeAcquisition(parent context.Context, t acquisitionTask) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	token := t.state.LeaseToken
	credentialRef, _ := t.data.Config.Connection["credentialRef"].(string)
	var private struct {
		Credentials map[string]struct {
			Revision int64 `json:"revision"`
		} `json:"credentials"`
	}
	_ = json.Unmarshal(t.source.Data, &private)
	credentialRevision := max(private.Credentials[credentialRef].Revision, 1)
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Only mutate the lease columns in JSON, leaving the callback's checkpoint intact.
				result := repo.GetUnsConn(ctx).Exec("UPDATE public.uns_meta st SET data=jsonb_set(st.data,ARRAY['streams',?,'leaseUntil']::text[],to_jsonb(?::bigint)) WHERE st.kind='source_state' AND st.id=? AND st.data#>>ARRAY['streams',?,'leaseToken']::text[]=? AND EXISTS(SELECT 1 FROM public.uns_meta src WHERE src.kind='source' AND src.id=st.id AND src.deleted_at=0 AND src.revision=? AND (?='' OR (jsonb_exists(src.data->'credentials',?) AND COALESCE((src.data#>>ARRAY['credentials',?,'revision'])::bigint,1)=?)))", t.bindingID, time.Now().UnixMilli()+45000, t.source.ID, t.bindingID, token, t.source.Revision, credentialRef, credentialRef, credentialRef, credentialRevision)
				if result.Error != nil || result.RowsAffected != 1 {
					cancel()
					return
				}
			}
		}
	}()
	var def sourcestream.SourceDefinition
	raw, _ := json.Marshal(t.data.Config.Connection)
	err := json.Unmarshal(raw, &def)
	if err == nil {
		if t.a.Mode == "poll" {
			runCtx, stop := context.WithTimeout(ctx, time.Duration(t.a.TimeoutMs)*time.Millisecond)
			err = s.pollAcquisition(runCtx, &t, def)
			stop()
		} else {
			var mu sync.Mutex
			observed := sourcestream.WithWatchObserver(ctx, func(ready bool) {
				mu.Lock()
				defer mu.Unlock()
				if ready {
					_ = s.acquisitionTransaction(ctx, &t, func(_ context.Context, tx *gorm.DB) error {
						t.state.Status = "running"
						t.state.Coverage.ready(time.Now().UnixMilli())
						t.state.LeaseUntil = time.Now().UnixMilli() + 45000
						return saveAcquisitionState(tx, t.source.ID, t.bindingID, t.state)
					})
				}
			})
			err = s.sourceRuntime.SharedWatch(observed, t.source.ID, def, t.a.Collection, t.a.NativeResourceRefs, func(input sourcestream.SourceInput) error {
				mu.Lock()
				defer mu.Unlock()
				return s.commitAcquisitionPage(ctx, &t, []any{input.Data}, input.ReceivedAt, t.state.Page+1, "", false)
			})
		}
	}
	// End renewal before final state mutation; no context-less writes after stop.
	cancel()
	<-renewDone
	finishCtx, stop := context.WithTimeout(context.WithoutCancel(parent), 3*time.Second)
	defer stop()
	_ = s.acquisitionTransaction(finishCtx, &t, func(_ context.Context, tx *gorm.DB) error {
		t.state.LeaseUntil = 0
		t.state.LeaseToken = ""
		t.state.Status = "idle"
		delay := max(t.a.PollIntervalMs, 1000)
		if err != nil {
			t.state.Status = "failed"
			previousError := t.state.LastError
			t.state.LastError = acquisitionErrorCode(err, t.state.Phase)
			if t.a.Mode == "watch" {
				t.state.Coverage.interrupt(max(t.state.LastDataAt, t.state.Coverage.WatchStartedAt), t.state.LastError)
			}
			if previousError != t.state.LastError {
				logx.Errorf("Shared acquisition failed: phase=%s code=%s", t.state.Phase, t.state.LastError)
			}
			delay = max(delay, t.a.Retry.BackoffMs)
		}
		t.state.NextRunAt = time.Now().UnixMilli() + int64(delay)
		return saveAcquisitionState(tx, t.source.ID, t.bindingID, t.state)
	})
}

func (s *Service) pollAcquisition(ctx context.Context, t *acquisitionTask, def sourcestream.SourceDefinition) error {
	h := t.a.HTTP
	p := h.Pagination
	for t.state.Page < p.MaxPages {
		t.state.Phase = "fetch"
		query := map[string]string{}
		for k, v := range h.Query {
			query[k] = v
		}
		switch p.Mode {
		case "page":
			query[p.PageParameter] = strconv.Itoa(p.StartPage + t.state.Page)
			query[p.SizeParameter] = strconv.Itoa(p.PageSize)
		case "cursor":
			cursor := t.state.Cursor
			if t.state.Page == 0 {
				cursor = p.InitialCursor
			}
			if cursor != "" {
				query[p.CursorParameter] = cursor
			}
		}
		var value any
		var err error
		for attempt := 0; attempt < t.a.Retry.MaxAttempts; attempt++ {
			value, err = s.sourceRuntime.SharedHTTP(ctx, t.source.ID, t.data.ConfigRevision, def, h.Path, query)
			if err == nil {
				break
			}
			t.state.Retries++
			if attempt+1 < t.a.Retry.MaxAttempts {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(t.a.Retry.BackoffMs) * time.Millisecond):
				}
			}
		}
		if err != nil {
			return err
		}
		t.state.Phase = "pagination"
		selected := acquisitionPath(value, h.ItemsPath)
		items, ok := selected.([]any)
		if !ok {
			if object, ok := selected.(map[string]any); ok {
				items = []any{object}
			} else {
				return errors.New("itemsPath must resolve to an array or object")
			}
		}
		next := ""
		done := p.Mode == "none" || len(items) == 0
		if p.Mode == "page" && len(items) < p.PageSize {
			done = true
		}
		if p.Mode == "cursor" {
			v := acquisitionPath(value, p.NextCursorPath)
			if v == nil || v == "" {
				done = true
			} else {
				var ok bool
				next, ok = v.(string)
				if !ok || len(next) > 8192 || next == t.state.Cursor {
					return errors.New("invalid or repeated cursor")
				}
			}
		}
		if !done && t.state.Page+1 >= p.MaxPages {
			return errors.New("pagination maxPages reached; increase bound or reduce source window")
		}
		if err := s.commitAcquisitionPage(ctx, t, items, time.Now().UnixMilli(), t.state.Page+1, next, done); err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	return errors.New("pagination bound reached")
}

func (s *Service) commitAcquisitionPage(ctx context.Context, t *acquisitionTask, items []any, receivedAt int64, page int, cursor string, done bool) error {
	if receivedAt <= 0 {
		receivedAt = time.Now().UnixMilli()
	}
	if len(items) > 1000 {
		return errors.New("page exceeds 1000 items")
	}
	// Transformation is outside the SQL transaction; protocol I/O never holds locks.
	transformed := make([]any, 0, len(items))
	t.state.Phase = "transform"
	for _, input := range items {
		r, err := s.sourceRuntime.SharedTransform(ctx, t.release.Definition.Processor.Code, t.release.Definition.Processor.TimeoutMs, input, t.binding.Parameters)
		if err != nil {
			return err
		}
		if len(r.Outputs) > 0 {
			return errors.New("shared processor cannot redirect outputs")
		}
		if !r.Drop {
			transformed = append(transformed, r.Data)
		}
	}
	t.state.Phase = "commit"
	before := t.state
	err := s.acquisitionTransaction(ctx, t, func(tctx context.Context, tx *gorm.DB) error {
		m := t.binding.Mapping
		m.CollectionID = t.binding.Target.CollectionID
		m.TopicID = t.binding.TopicID
		if err := unsmeta.PrepareMappedSamples(tctx, tx, m, transformed, receivedAt); err != nil {
			return err
		}
		for i, value := range transformed {
			hash := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%s/%d/%d", t.source.ID, t.bindingID, t.state.CycleID, page, i)))
			// All producers use the Collection commit boundary. Storage policy and
			// durable delivery are independent of Source/Stream configuration.
			if err := unsmeta.CommitSharedMapped(tctx, t.source.ID, t.binding.StreamID, t.bindingID, strconv.Itoa(t.binding.StreamRelease), hex.EncodeToString(hash[:]), t.state.Sequence+int64(i)+1, m, value, receivedAt); err != nil {
				return err
			}
		}
		t.state.Status = "running"
		t.state.Coverage.ready(time.Now().UnixMilli())
		t.state.LastError = ""
		t.state.Records += int64(len(transformed))
		t.state.Sequence += int64(len(transformed))
		if len(transformed) > 0 {
			t.state.LastDataAt = receivedAt
		}
		t.state.Page = page
		t.state.Cursor = cursor
		t.state.LeaseUntil = time.Now().UnixMilli() + 45000
		if done {
			t.state.Page = 0
			t.state.Cursor = ""
			t.state.CycleID = ""
		}
		return saveAcquisitionState(tx, t.source.ID, t.bindingID, t.state)
	})
	if err != nil {
		t.state = before
	}
	return err
}

func acquisitionErrorCode(err error, phase string) string {
	switch {
	case errors.Is(err, repo.ErrFleetStorageFull):
		return "STORAGE_CAPACITY_REACHED"
	case errors.Is(err, repo.ErrFleetStorageUnobserved):
		return "STORAGE_MONITORING_UNAVAILABLE"
	case errors.Is(err, repo.ErrFleetQueueFull):
		return "UPLOAD_QUEUE_FULL"
	case errors.Is(err, repo.ErrConflict):
		return "CONFIGURATION_OR_RECORD_CONFLICT"
	case errors.Is(err, unsmeta.ErrData), errors.Is(err, unsmeta.ErrSchema):
		return "OUTPUT_SCHEMA_MISMATCH"
	case errors.Is(err, unsmeta.ErrReferenced):
		return "RECORD_SOURCE_PROTECTED"
	case errors.Is(err, streamexec.ErrInput):
		return "PROCESSOR_INPUT_INVALID"
	case errors.Is(err, streamexec.ErrOutput):
		return "PROCESSOR_OUTPUT_INVALID"
	case errors.Is(err, streamexec.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return "ACQUISITION_TIMEOUT"
	case errors.Is(err, sourcestream.ErrUnavailable):
		return "EXECUTOR_UNAVAILABLE"
	case errors.Is(err, sourcestream.ErrCredential):
		return "SOURCE_CREDENTIAL_INVALID"
	}
	return "ACQUISITION_" + strings.ToUpper(phase) + "_FAILED"
}
