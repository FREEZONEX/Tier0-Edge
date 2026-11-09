package repo

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

type streamCheckpoint struct {
	Imported   bool   `json:"imported"`
	Epoch      string `json:"epoch"`
	AckSeq     int64  `json:"ackSeq"`
	Cursor     string `json:"cursor"`
	LastDataAt int64  `json:"lastDataAt"`
}

// Lock order matches the atlas: source configuration SHARE, state UPDATE,
// business rows, then checkpoint and COMMIT. It never rewrites configuration.
func LockStreamIngest(tx *gorm.DB, task StreamTask) (int64, func() error, error) {
	_, data, err := sourceRow(tx, task.SourceID, "SHARE")
	if err != nil {
		return 0, nil, err
	}
	c, ok := data.Config.Streams[sourceKey(task.StreamID)]
	if !ok || c.DeletedAt != 0 || c.Status != "enabled" || c.ActiveVersion != task.VersionID || data.ConfigRevision != task.SourceRevision {
		return 0, nil, ErrConflict
	}
	row, state, err := stateRow(tx, task.SourceID, true)
	if err != nil {
		return 0, nil, err
	}
	key := sourceKey(task.StreamID)
	var checkpoint streamCheckpoint
	if raw := state.Streams[key]; len(raw) > 0 {
		if err = json.Unmarshal(raw, &checkpoint); err != nil {
			return 0, nil, err
		}
	}
	epoch := sourceKey(task.VersionID)
	if checkpoint.Epoch != epoch {
		checkpoint.AckSeq = 0
	}
	checkpoint.Epoch = epoch
	checkpoint.AckSeq++
	checkpoint.Imported = true
	checkpoint.Cursor = task.ID
	checkpoint.LastDataAt = task.ReceivedAt
	finish := func() error {
		if state.Streams == nil {
			state.Streams = map[string]json.RawMessage{}
		}
		raw, err := json.Marshal(checkpoint)
		if err != nil {
			return err
		}
		state.Streams[key] = raw
		if task.ReceivedAt > state.LastDataAt {
			state.LastDataAt = task.ReceivedAt
		}
		state.Health = "healthy"
		state.HealthMessage = ""
		state.CheckedAt = time.Now().UnixMilli()
		return saveMetaData(tx, &row, state)
	}
	return checkpoint.AckSeq, finish, nil
}

// Receipt is task metadata in the very same transaction as data + checkpoint.
func ReserveMetadataIngest(tx *gorm.DB, id, digest string) (bool, error) {
	created, err := putIntegrationTask(tx, "ingestReceipt", id, map[string]string{"digest": digest})
	if err != nil || created {
		return created, err
	}
	var existing map[string]string
	if err = getIntegrationTask(tx, "ingestReceipt", id, &existing); err != nil {
		return false, err
	}
	if existing["digest"] != digest {
		return false, ErrConflict
	}
	return false, nil
}
