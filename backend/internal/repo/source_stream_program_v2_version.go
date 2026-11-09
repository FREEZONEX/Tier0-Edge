package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"backend/internal/streamexec"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func programStableID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
		return 0, ErrInvalidArgument
	}
	return id, nil
}

func programV2References(manifest streamexec.ProgramManifest) ([]int64, []int64, string, error) {
	if streamexec.NormalizeOperationResources(manifest.Entries) != nil {
		return nil, nil, "", ErrInvalidArgument
	}
	encoded, err := json.Marshal(manifest)
	if err != nil || len(encoded) > 1024*1024 || manifest.Sources == nil || manifest.Topics == nil || len(manifest.Sources) > 64 || len(manifest.Topics) > 1000 || len(manifest.Entries) < 1 || len(manifest.Entries) > streamexec.MaxProgramEntries {
		return nil, nil, "", ErrInvalidArgument
	}
	sourceIDs, topicIDs := []int64{}, []int64{}
	sources, topics, entries := map[string]map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, source := range manifest.Sources {
		id, err := programStableID(source.ID)
		if err != nil || sources[source.ID] != nil || source.Resources == nil || len(source.Resources) > 1000 {
			return nil, nil, "", ErrInvalidArgument
		}
		refs := map[string]bool{}
		for _, ref := range source.Resources {
			if ref == "" || len(ref) > 1000 || refs[ref] {
				return nil, nil, "", ErrInvalidArgument
			}
			refs[ref] = true
		}
		sources[source.ID] = refs
		sourceIDs = append(sourceIDs, id)
	}
	for _, topic := range manifest.Topics {
		_, duplicate := topics[topic.Key()]
		if !topic.Valid() || duplicate {
			return nil, nil, "", ErrInvalidArgument
		}
		topics[topic.Key()] = topic.Readable() && topic.CollectionID == ""
		if !topic.Canonical() {
			id, err := programStableID(topic.TopicID)
			if err != nil {
				return nil, nil, "", err
			}
			topicIDs = append(topicIDs, id)
		}
	}
	for _, entry := range manifest.Entries {
		if strings.TrimSpace(entry.ID) == "" || len(entry.ID) > 128 || entries[entry.ID] {
			return nil, nil, "", ErrInvalidArgument
		}
		entries[entry.ID] = true
		if entry.Kind != "http-request" && (entry.Method != "" || entry.Path != "") {
			return nil, nil, "", ErrInvalidArgument
		}
		switch entry.Kind {
		case "http-request":
			if _, ok := sources[entry.SourceID]; !ok || !streamexec.ValidateHTTPRoute(entry.Method, entry.Path) || entry.IntervalMs != 0 || len(entry.ResourceRefs) > 0 || len(entry.TopicIDs) > 0 || entry.QoS != 0 {
				return nil, nil, "", ErrInvalidArgument
			}
		case "interval":
			if entry.IntervalMs < 100 || entry.IntervalMs > 86400000 || entry.SourceID != "" || len(entry.ResourceRefs) > 0 || len(entry.TopicIDs) > 0 || entry.QoS != 0 {
				return nil, nil, "", ErrInvalidArgument
			}
		case "request":
			if entry.IntervalMs != 0 || entry.SourceID != "" || len(entry.ResourceRefs) > 0 || len(entry.TopicIDs) > 0 || entry.QoS != 0 {
				return nil, nil, "", ErrInvalidArgument
			}
		case "source-event":
			refs, ok := sources[entry.SourceID]
			if !ok || entry.IntervalMs != 0 || len(entry.ResourceRefs) < 1 || len(entry.ResourceRefs) > 1000 || len(entry.TopicIDs) > 0 || entry.QoS < 0 || entry.QoS > 2 {
				return nil, nil, "", ErrInvalidArgument
			}
			seen := map[string]bool{}
			for _, ref := range entry.ResourceRefs {
				if !refs[ref] || seen[ref] {
					return nil, nil, "", ErrInvalidArgument
				}
				seen[ref] = true
			}
		case "uns-event":
			if entry.IntervalMs != 0 || entry.SourceID != "" || len(entry.ResourceRefs) > 0 || len(entry.TopicIDs) < 1 || len(entry.TopicIDs) > 1000 || entry.QoS != 0 {
				return nil, nil, "", ErrInvalidArgument
			}
			seen := map[string]bool{}
			for _, topic := range entry.TopicIDs {
				if !topics[topic] || seen[topic] {
					return nil, nil, "", ErrInvalidArgument
				}
				seen[topic] = true
			}
		default:
			return nil, nil, "", ErrInvalidArgument
		}
	}
	return sourceIDs, topicIDs, string(encoded), nil
}

func (v StreamVersion) ProgramV2Manifest() (streamexec.ProgramManifest, error) {
	var manifest streamexec.ProgramManifest
	if len(v.ProgramManifest) > 1024*1024 {
		return manifest, ErrInvalidArgument
	}
	// Publication and UNS snapshots are private persisted provenance, outside
	// the public runtime manifest. Validate before removing the known fields.
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(v.ProgramManifest), &fields) != nil || fields == nil {
		return manifest, ErrInvalidArgument
	}
	var plan ProgramExecutionPlan
	if _, exists := fields[programExecutionPlanField]; exists {
		var err error
		if plan, err = validateProgramExecutionPlan(v, fields); err != nil {
			return manifest, err
		}
		delete(fields, programExecutionPlanField)
	}
	policies, hasPolicies := fields[programRequestPoliciesField]
	delete(fields, programRequestPoliciesField)
	bindingsJSON := fields["_unsBindings"]
	delete(fields, "_unsBindings")
	raw, err := json.Marshal(fields)
	if err != nil || json.Unmarshal(raw, &manifest) != nil {
		return manifest, ErrInvalidArgument
	}

	_, _, _, err = programV2References(manifest)
	if err == nil && hasPolicies {
		bindings := []ProgramUNSBinding{}
		if plan.Contract != programExecutionPlanContract || len(bindingsJSON) > 0 && json.Unmarshal(bindingsJSON, &bindings) != nil {
			return manifest, ErrInvalidArgument
		}
		_, err = decodeProgramRequestPolicies(policies, manifest, plan.Sources, bindings, v.ID)
	}
	if err == nil && v.StreamID > 0 {
		err = validateFrozenImplicitRequests(manifest, v.StreamID, v.Document)
	}
	return manifest, err
}

func validateFrozenImplicitRequests(manifest streamexec.ProgramManifest, streamID int64, document string) error {
	var doc struct {
		TimeoutMs *int            `json:"timeoutMs"`
		Script    json.RawMessage `json:"script"`
	}
	// Old releases and raw registration manifests do not need a document here.
	_ = json.Unmarshal([]byte(document), &doc)
	timeout := streamexec.DefaultProgramTimeoutMs
	if doc.TimeoutMs != nil {
		timeout = *doc.TimeoutMs
	}
	var script struct {
		TimeoutMs int `json:"timeoutMs"`
	}
	if json.Unmarshal(doc.Script, &script) == nil && script.TimeoutMs > 0 {
		timeout = script.TimeoutMs
	}
	return streamexec.ValidateImplicitRequests(manifest, streamID, timeout)
}

func (s Stream) ProgramV2DraftManifest() (streamexec.ProgramManifest, error) {
	return (StreamVersion{ProgramManifest: s.DraftManifest}).ProgramV2Manifest()
}

func validProgramV2Document(document string) bool {
	if len(document) > 1024*1024 || !json.Valid([]byte(document)) {
		return false
	}
	var doc struct {
		Name           string                     `json:"name"`
		Description    string                     `json:"description"`
		Script         json.RawMessage            `json:"script"`
		TimeoutMs      *int                       `json:"timeoutMs"`
		Sources        json.RawMessage            `json:"sources,omitempty"`
		SourceBindings json.RawMessage            `json:"sourceBindings,omitempty"`
		Extensions     map[string]json.RawMessage `json:"extensions,omitempty"`
	}
	decoder := json.NewDecoder(strings.NewReader(document))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&doc) != nil || len(doc.Sources) > 0 && len(doc.SourceBindings) > 0 {
		return false
	}
	var code string
	if json.Unmarshal(doc.Script, &code) == nil {
		return len(code) > 0 && len(code) <= streamexec.MaxCodeBytes
	}
	var script struct {
		Code      string `json:"code"`
		TimeoutMs int    `json:"timeoutMs"`
	}
	decoder = json.NewDecoder(bytes.NewReader(doc.Script))
	decoder.DisallowUnknownFields()
	return doc.TimeoutMs == nil && decoder.Decode(&script) == nil && len(script.Code) > 0 && len(script.Code) <= streamexec.MaxCodeBytes && script.TimeoutMs > 0 && script.TimeoutMs <= streamexec.MaxProgramTimeoutMs
}

func sameProgramDocument(left, right string) bool {
	a, err := canonicalProgramJSON([]byte(left))
	if err != nil {
		return false
	}
	b, err := canonicalProgramJSON([]byte(right))
	return err == nil && bytes.Equal(a, b)
}

// The dependency index is a deletion/impact guard, not runtime SDK authority.
// Sources always lock in numeric order before the Stream row.
func lockProgramV2Sources(tx *gorm.DB, streamID int64, ids []int64) ([]Source, error) {
	seen := map[int64]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if streamID > 0 {
		var previous []StreamSource
		if err := tx.Where("stream_id=?", streamID).Find(&previous).Error; err != nil {
			return nil, err
		}
		for _, ref := range previous {
			if _, required := seen[ref.SourceID]; !required {
				seen[ref.SourceID] = false
			}
		}
	}
	ordered := make([]int64, 0, len(seen))
	for id := range seen {
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	items := make([]Source, 0, len(ordered))
	for _, id := range ordered {
		var source Source
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(&source).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && source.DeletedAt != 0 {
			// Previous index entries only serialize changes; they cannot prevent
			// replacing a removed dependency. New execution references stay strict.
			if !seen[id] {
				continue
			}
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		items = append(items, source)
	}
	return items, nil
}

func replaceProgramV2References(tx *gorm.DB, stream Stream, manifest streamexec.ProgramManifest) error {
	ids, topics, _, err := programV2References(manifest)
	if err != nil {
		return err
	}
	sourceSet, topicSet := map[int64]bool{}, map[int64]bool{}
	for _, id := range ids {
		sourceSet[id] = true
	}
	for _, id := range topics {
		topicSet[id] = true
	}
	for _, versionID := range []int64{stream.ActiveVersion, stream.PendingVersion} {
		if versionID == 0 {
			continue
		}
		var version StreamVersion
		if err := tx.Where("id=? AND stream_id=?", versionID, stream.ID).Take(&version).Error; err != nil {
			return err
		}
		active, err := version.ProgramV2Manifest()
		if err != nil {
			return err
		}
		versionSources, versionTopics, _, err := programV2References(active)
		if err != nil {
			return err
		}
		for _, id := range versionSources {
			sourceSet[id] = true
		}
		for _, id := range versionTopics {
			topicSet[id] = true
		}
	}
	if err := tx.Where("stream_id=?", stream.ID).Delete(&StreamSource{}).Error; err != nil {
		return err
	}
	for id := range sourceSet {
		if err := tx.Create(&StreamSource{StreamID: stream.ID, SourceID: id}).Error; err != nil {
			return err
		}
	}
	topics = topics[:0]
	for id := range topicSet {
		topics = append(topics, id)
	}
	sort.Slice(topics, func(i, j int) bool { return topics[i] < topics[j] })
	return replaceStreamTopics(tx, stream.ID, topics)
}

func validateProgramV2Snapshots(manifest streamexec.ProgramManifest, sources []Source, snapshots []StreamSourceSnapshot, requireHealthy bool) error {
	if snapshots == nil || len(snapshots) != len(manifest.Sources) {
		return ErrInvalidArgument
	}
	seen := map[int64]bool{}
	for i, declared := range manifest.Sources {
		id, err := programStableID(declared.ID)
		if err != nil {
			return err
		}
		frozen := snapshots[i]
		if frozen.SourceID != id || frozen.Alias != declared.ID || frozen.ConfigRevision <= 0 || seen[id] || !reflect.DeepEqual(frozen.Resources, declared.Resources) {
			return ErrInvalidArgument
		}
		seen[id] = true
	}
	return checkProgramSnapshots(sources, snapshots, requireHealthy)
}

func (r *SourceStreamRepo) SaveProgramV2(ctx context.Context, item Stream, revision int64, manifest streamexec.ProgramManifest) (Stream, error) {
	ids, _, encoded, err := programV2References(manifest)
	if err != nil || item.SourceID != 0 || item.Model != "program" || !validProgramV2Document(item.Document) {
		return item, ErrInvalidArgument
	}
	creating := item.ID == 0
	if creating && revision != 0 {
		return item, ErrConflict
	}
	err = streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		if _, err := lockProgramV2Sources(tx, item.ID, ids); err != nil {
			return err
		}
		if creating {
			ensureID(&item.ID)
			item.Revision, item.Status = 1, "draft"
			item.ActiveVersion, item.PendingVersion = 0, 0
			item.DraftManifest = encoded
			item.CreatedAt = time.Now().UnixMilli()
			item.UpdatedAt = item.CreatedAt
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		} else {
			var old Stream
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND deleted_at=0", item.ID).Take(&old).Error; err != nil {
				return err
			}
			if old.SourceID != 0 || old.Model != "program" || old.Revision != revision {
				return ErrConflict
			}
			old.Name, old.Document = item.Name, item.Document
			old.DraftManifest = encoded
			old.Revision++
			old.PendingVersion = 0
			old.UpdatedAt = time.Now().UnixMilli()
			if err := tx.Save(&old).Error; err != nil {
				return err
			}
			item = old
		}
		return replaceProgramV2References(tx, item, manifest)
	})
	return item, normalizeDBError(err)
}

// Stage is not activation. Readiness failure leaves the old version serving.
func (r *SourceStreamRepo) StageProgramV2(ctx context.Context, id, revision, existingVersionID int64, document string, manifest streamexec.ProgramManifest, snapshots []StreamSourceSnapshot, targetSnapshots ...[]ProgramUNSBinding) (Stream, StreamVersion, error) {
	return r.stageProgramV2(ctx, id, revision, existingVersionID, document, manifest, snapshots, nil, targetSnapshots...)
}

// The domain builder resolves protocol actions while the publication transaction
// holds its Source locks. Historical rollback never rebuilds target authority.
func (r *SourceStreamRepo) StageProgramV2WithRequestPolicies(ctx context.Context, id, revision, existingVersionID int64, document string, manifest streamexec.ProgramManifest, snapshots []StreamSourceSnapshot, bindings []ProgramUNSBinding, build ProgramRequestPolicyBuilder) (Stream, StreamVersion, error) {
	if build == nil {
		return Stream{}, StreamVersion{}, ErrInvalidArgument
	}
	return r.stageProgramV2(ctx, id, revision, existingVersionID, document, manifest, snapshots, build, bindings)
}

func (r *SourceStreamRepo) stageProgramV2(ctx context.Context, id, revision, existingVersionID int64, document string, manifest streamexec.ProgramManifest, snapshots []StreamSourceSnapshot, build ProgramRequestPolicyBuilder, targetSnapshots ...[]ProgramUNSBinding) (Stream, StreamVersion, error) {
	var stream Stream
	var version StreamVersion
	ids, _, encoded, err := programV2References(manifest)
	if err != nil || !validProgramV2Document(document) || validateFrozenImplicitRequests(manifest, id, document) != nil {
		return stream, version, ErrInvalidArgument
	}
	bindings := []ProgramUNSBinding{}
	if len(targetSnapshots) > 1 {
		return stream, version, ErrInvalidArgument
	}
	if len(targetSnapshots) == 1 {
		bindings = targetSnapshots[0]
	}
	encoded, err = encodeProgramV2Bindings(encoded, bindings)
	if err != nil {
		return stream, version, err
	}
	snapshotJSON, err := json.Marshal(snapshots)
	if err != nil || len(snapshotJSON) > 1024*1024 {
		return stream, version, ErrInvalidArgument
	}
	err = streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(846031001)).Error; err != nil {
			return err
		}
		if err := tx.Exec("SELECT pg_advisory_xact_lock_shared(814206108)").Error; err != nil {
			return err
		}
		if err := ValidateProgramV2Bindings(WithUnsTransaction(ctx, tx), manifest, bindings); err != nil {
			return err
		}
		if err := NewSourceStreamRepo(tx).CheckHTTPRouteConflicts(ctx, id, manifest); err != nil {
			return err
		}
		sources, err := lockProgramV2Sources(tx, id, ids)
		if err != nil {
			return err
		}
		if err := validateProgramV2Snapshots(manifest, sources, snapshots, true); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND deleted_at=0", id).Take(&stream).Error; err != nil {
			return err
		}
		if stream.SourceID != 0 || stream.Model != "program" || stream.Revision != revision {
			return ErrConflict
		}
		if existingVersionID == 0 {
			if !sameProgramDocument(stream.Document, document) {
				return ErrConflict
			}
			version = StreamVersion{StreamID: id, SourceManifest: string(snapshotJSON), ProgramManifest: encoded, Document: document, CreatedAt: time.Now().UnixMilli()}
			ensureID(&version.ID)
			if err := freezeProgramExecutionPlan(ctx, &version, sources, build); err != nil {
				return err
			}
			if err := tx.Create(&version).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Where("id=? AND stream_id=?", existingVersionID, id).Take(&version).Error; err != nil {
				return err
			}
			if version.ActivatedAt <= 0 {
				return ErrConflict
			}
			frozen, err := version.ProgramV2Manifest()
			if err != nil {
				return err
			}
			frozenSources, err := version.Sources()
			if err != nil {
				return err
			}
			frozenBindings, err := version.ProgramV2Bindings()
			if err != nil {
				return err
			}
			if !sameProgramDocument(version.Document, document) || !reflect.DeepEqual(frozen, manifest) || !reflect.DeepEqual(frozenSources, snapshots) || !reflect.DeepEqual(frozenBindings, bindings) {
				return ErrConflict
			}
		}
		stream.PendingVersion = version.ID
		stream.Revision++
		stream.UpdatedAt = time.Now().UnixMilli()
		if err := tx.Save(&stream).Error; err != nil {
			return err
		}
		draft, err := stream.ProgramV2DraftManifest()
		if err != nil {
			return err
		}
		return replaceProgramV2References(tx, stream, draft)
	})
	return stream, version, normalizeDBError(err)
}

func (r *SourceStreamRepo) ActivateStagedProgramV2(ctx context.Context, id, expectedRevision, pendingVersionID int64) (Stream, error) {
	var stream Stream
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(846031001)).Error; err != nil {
			return err
		}
		var version StreamVersion
		if err := tx.Where("id=? AND stream_id=?", pendingVersionID, id).Take(&version).Error; err != nil {
			return err
		}
		manifest, err := version.ProgramV2Manifest()
		if err != nil {
			return err
		}
		bindings, err := version.ProgramV2Bindings()
		if err != nil {
			return err
		}
		if err := tx.Exec("SELECT pg_advisory_xact_lock_shared(814206108)").Error; err != nil {
			return err
		}
		if err := ValidateProgramV2Bindings(WithUnsTransaction(ctx, tx), manifest, bindings); err != nil {
			return err
		}
		if err := NewSourceStreamRepo(tx).CheckHTTPRouteConflicts(ctx, id, manifest); err != nil {
			return err
		}
		ids, _, _, err := programV2References(manifest)
		if err != nil {
			return err
		}
		sources, err := lockProgramV2Sources(tx, id, ids)
		if err != nil {
			return err
		}
		snapshots, err := version.Sources()
		if err != nil {
			return err
		}
		if err := validateProgramV2Snapshots(manifest, sources, snapshots, true); err != nil {
			return err
		}
		if err := version.validateExecutionSources(sources); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND deleted_at=0", id).Take(&stream).Error; err != nil {
			return err
		}
		if stream.SourceID != 0 || stream.Model != "program" || stream.Revision != expectedRevision || stream.PendingVersion != pendingVersionID || pendingVersionID <= 0 {
			return ErrConflict
		}
		stream.ActiveVersion, stream.PendingVersion, stream.Status = pendingVersionID, 0, "enabled"
		if version.ActivatedAt == 0 {
			if err := tx.Model(&StreamVersion{}).Where("id=? AND activated_at=0", version.ID).Update("activated_at", time.Now().UnixMilli()).Error; err != nil {
				return err
			}
		}
		stream.Revision++
		stream.UpdatedAt = time.Now().UnixMilli()
		if err := tx.Save(&stream).Error; err != nil {
			return err
		}
		draft, err := stream.ProgramV2DraftManifest()
		if err != nil {
			return err
		}
		if err := replaceProgramV2References(tx, stream, draft); err != nil {
			return err
		}
		return replaceProgramOperations(tx, stream, manifest)
	})
	return stream, normalizeDBError(err)
}

// Candidates only prepare connection watches. They are never executable plans
// until activated_at is recorded by successful activation.
func (r *SourceStreamRepo) PendingProgramV2Executions(ctx context.Context) ([]StreamExecution, error) {
	var streams []Stream
	if err := r.db.WithContext(ctx).Where("model='program' AND deleted_at=0 AND pending_version>0").Order("id").Find(&streams).Error; err != nil {
		return nil, err
	}
	items := make([]StreamExecution, 0, len(streams))
	for _, stream := range streams {
		version, err := r.Version(ctx, stream.ID, stream.PendingVersion)
		if err != nil {
			return nil, err
		}
		if _, err := version.ProgramV2Manifest(); err != nil {
			return nil, err
		}
		if _, err := r.ProgramVersionSources(ctx, version); err != nil {
			if err == ErrConflict || IsSourceStreamMissing(err) {
				continue
			}
			return nil, err
		}
		items = append(items, StreamExecution{Stream: stream, Version: version})
	}
	return items, nil
}
