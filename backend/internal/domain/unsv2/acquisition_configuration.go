package unsv2

import (
	"backend/internal/repo"
	"bytes"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reflect"
)

// Stored Source rows also contain private credential metadata. Ignore that
// envelope while retaining exact JSON numbers for configuration comparisons.
type credentialSourceKey struct{}

func decodeStoredSource(raw []byte, target *SourceData) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(target)
}

// A Source revision also changes for metadata or another Binding. Carry forward
// the checkpoint only when its exact executable configuration is unchanged.
// Old tasks remain fenced by Source revision; their retained lease prevents a
// replacement worker overlapping protocol I/O until the existing lease expires.
func preserveUnchangedAcquisition(tx *gorm.DB, before, after repo.UnsMeta) error {
	var old, next SourceData
	if decodeStoredSource(before.Data, &old) != nil || decodeStoredSource(after.Data, &next) != nil || old.SchemaVersion != 1 || next.SchemaVersion != 1 {
		return nil
	}
	if old.Adapter != next.Adapter || !reflect.DeepEqual(old.Config.Connection, next.Config.Connection) {
		return nil
	}
	var row repo.UnsMeta
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("kind='source_state' AND id=?", before.ID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var state struct {
		Streams map[string]json.RawMessage `json:"streams"`
	}
	if err := json.Unmarshal(row.Data, &state); err != nil {
		return err
	}
	for id, binding := range next.Config.Streams {
		previous, exists := old.Config.Streams[id]
		if !exists || !reflect.DeepEqual(previous, binding) {
			continue
		}
		var progress acquisitionState
		if len(state.Streams[id]) == 0 {
			continue
		}
		if err := json.Unmarshal(state.Streams[id], &progress); err != nil {
			return err
		}
		if progress.Revision != before.Revision {
			continue
		}
		progress.Revision = after.Revision
		if err := saveAcquisitionState(tx, after.ID, id, progress); err != nil {
			return err
		}
	}
	return nil
}
