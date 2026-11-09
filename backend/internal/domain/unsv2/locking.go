package unsv2

import (
	"time"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"gorm.io/gorm"
)

// Static IDs are locked before execution. Query-selected IDs are locked by the
// query, before bindings can reference them. Both paths retain a shared metadata
// gate; queries never upgrade it into a global exclusive writer lock.
func lockPlan(tx *gorm.DB, p *plan) ([]repo.UnsRecordLock, error) {
	exclusive := false
	records := make([]repo.UnsRecordLock, 0, len(p.Items))
	for _, a := range p.AssertLocks {
		records = append(records, repo.UnsRecordLock{CollectionID: a.CollectionID, RecordID: a.ID})
	}
	for _, item := range p.Items {
		if item.Action == "record.query" || hasTargetBinding(item) || len(item.Filter) > 0 && item.ID.ID == "" {
			continue
		}
		var recordID string
		switch item.Action {
		case "record.create", "record.replace", "record.delete", "record.restore", "record.check", "record.read", "record.modify", "record.update":
			recordID = item.ID.ID
		case "sample.append":
			recordID = item.RecordID.ID
		default:
			exclusive = true
		}
		if recordID != "" && unsmeta.ValidID(item.CollectionID.ID) && item.CollectionID.Ref == "" && item.ID.Ref == "" && item.RecordID.Ref == "" {
			records = append(records, repo.UnsRecordLock{CollectionID: item.CollectionID.ID, RecordID: recordID})
		} else {
			exclusive = true
		}
	}
	query := "SELECT pg_advisory_xact_lock_shared(814206108)"
	if exclusive {
		query = "SELECT pg_advisory_xact_lock(814206108)"
	}
	if err := tx.Exec(query).Error; err != nil {
		return nil, err
	}
	var times []time.Time
	for _, item := range p.Items {
		if item.Action == "sample.append" {
			times = append(times, time.UnixMilli(item.Time))
		}
	}
	if err := repo.PrepareUnsSampleWrites(tx, times...); err != nil {
		return nil, err
	}
	if err := repo.LockUnsRecords(tx, records...); err != nil {
		return nil, err
	}
	return records, nil
}
