package repo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ProgramExecutionOwnerLease = 30 * time.Second
const maxProgramRecoveryBatch = 100
const maxProgramOwnerLineage = 32
const programOwnerLockClass int32 = 0x5354524d // STRM: separate from existing pair-lock domains.

type programExecutionOwnerRow struct {
	ID        string `gorm:"column:id;primaryKey"`
	LockID    int32  `gorm:"column:lock_id"`
	State     string `gorm:"column:state"`
	CreatedAt int64  `gorm:"column:created_at"`
	LostAt    int64  `gorm:"column:lost_at"`
}

func (programExecutionOwnerRow) TableName() string { return "public.uns_stream_execution_owner" }

type programRunOwner struct {
	RunID            string  `gorm:"column:run_id;primaryKey"`
	OwnerID          string  `gorm:"column:owner_id"`
	Lineage          string  `gorm:"column:lineage"`
	RecoveryState    string  `gorm:"column:recovery_state;default:waiting"`
	RecoveryAttempts int     `gorm:"column:recovery_attempts"`
	RecoveryAfter    int64   `gorm:"column:recovery_after"`
	RecoveryToken    *string `gorm:"column:recovery_token"`
}

func (programRunOwner) TableName() string { return "public.uns_stream_run_owner" }

// One physical session per Backend incarnation, never one per Run. A lost
// session cannot reconnect as the same incarnation or return settings to a pool.
type ProgramExecutionOwner struct {
	id   string
	mu   sync.Mutex
	conn *sql.Conn
}

func validProgramOwner(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed != uuid.Nil && parsed.String() == id
}

func programOwnerLockID(id string) int32 {
	hash := sha256.Sum256([]byte(id))
	return int32(binary.BigEndian.Uint32(hash[:4]))
}

func (o *ProgramExecutionOwner) ID() string { return o.id }

func (r *SourceStreamRepo) OpenProgramExecutionOwner(ctx context.Context, id string) (*ProgramExecutionOwner, error) {
	if !validProgramOwner(id) {
		return nil, ErrInvalidArgument
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	db, err := r.db.DB()
	if err != nil {
		return nil, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	owner := &ProgramExecutionOwner{id: id, conn: conn}
	opened := false
	defer func() {
		if !opened {
			_ = owner.Close()
		}
	}()
	var setting string
	if err = conn.QueryRowContext(ctx, `SELECT set_config('idle_session_timeout',$1,false)`, ProgramExecutionOwnerLease.String()).Scan(&setting); err != nil {
		return nil, err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Claim the immutable slot before taking the session lock. A precheck alone
	// races with another generation registering and closing the same slot.
	lockID := programOwnerLockID(id)
	var stored string
	err = tx.QueryRowContext(ctx, `INSERT INTO public.uns_stream_execution_owner(id,lock_id,state,created_at)
		VALUES ($1,$2,'active',$3) ON CONFLICT DO NOTHING RETURNING id`, id, lockID, time.Now().UnixMilli()).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	var held bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1,$2)`, programOwnerLockClass, lockID).Scan(&held); err != nil {
		return nil, err
	}
	if !held {
		return nil, ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	opened = true
	return owner, nil
}

func (o *ProgramExecutionOwner) Ping(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.conn == nil {
		return ErrConflict
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var active bool
	err := o.conn.QueryRowContext(ctx, `SELECT state='active' FROM public.uns_stream_execution_owner WHERE id=$1`, o.id).Scan(&active)
	if err != nil {
		return err
	}
	if !active {
		return ErrConflict
	}
	return nil
}

func (o *ProgramExecutionOwner) Close() error {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.conn == nil {
		return nil
	}
	// Discard the physical connection: unlocking alone would leak the lease
	// setting, and a failed unlock must never return a held lock to the pool.
	err := o.conn.Raw(func(any) error { return driver.ErrBadConn })
	closeErr := o.conn.Close()
	o.conn = nil
	if err != nil && !errors.Is(err, driver.ErrBadConn) && !errors.Is(err, sql.ErrConnDone) {
		return err
	}
	if closeErr != nil && !errors.Is(closeErr, sql.ErrConnDone) {
		return closeErr
	}
	return nil
}

// Called before Source/Stream/Run locks. Recovery takes owner -> Run in the
// same order; taking the Run first would deadlock against retirement.
func guardProgramOwner(tx *gorm.DB, id string) error {
	var owner programExecutionOwnerRow
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=?", id).Take(&owner).Error; err != nil {
		return err
	}
	if owner.State != "active" {
		return ErrConflict
	}
	var unowned bool
	if err := tx.Raw(`SELECT pg_try_advisory_xact_lock(?,?)`, programOwnerLockClass, owner.LockID).Scan(&unowned).Error; err != nil {
		return err
	}
	if unowned {
		return ErrConflict
	}
	return nil
}

func guardProgramRunOwner(tx *gorm.DB, runID string) error {
	var binding programRunOwner
	query := tx.Where("run_id=?", runID).Limit(1).Find(&binding)
	if query.Error != nil {
		return query.Error
	}
	if query.RowsAffected == 0 {
		return nil // Historical runs did not have this protocol; never adopt them.
	}
	return guardProgramOwner(tx, binding.OwnerID)
}

func (r *SourceStreamRepo) AdmitOwnedProgramV2Run(ctx context.Context, run StreamRun, activeOnly bool, ownerID string, lineage []int64) (bool, error) {
	if !validProgramOwner(ownerID) || len(lineage) >= maxProgramOwnerLineage {
		return false, ErrInvalidArgument
	}
	for _, id := range lineage {
		if id <= 0 {
			return false, ErrInvalidArgument
		}
	}
	if lineage == nil {
		lineage = []int64{}
	}
	encoded, _ := json.Marshal(lineage)
	created := false
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		if err := guardProgramOwner(tx, ownerID); err != nil {
			return err
		}
		var err error
		created, err = NewSourceStreamRepo(tx).AdmitProgramV2Run(ctx, run, activeOnly)
		if err != nil || !created {
			return err
		}
		return tx.Create(&programRunOwner{RunID: run.ID, OwnerID: ownerID, Lineage: string(encoded)}).Error
	})
	return created, normalizeDBError(err)
}

type ProgramRecoveryTask struct {
	Task    StreamTask
	Lineage []int64
}

// Recovery and message redelivery retain the admitted event provenance rather
// than accepting a replacement lineage from the latest caller.
func (r *SourceStreamRepo) ProgramRunLineage(ctx context.Context, runID string) ([]int64, bool, error) {
	if runID == "" || len(runID) > 64 {
		return nil, false, ErrInvalidArgument
	}
	var binding programRunOwner
	q := r.db.WithContext(ctx).Where("run_id=?", runID).Limit(1).Find(&binding)
	if q.Error != nil {
		return nil, false, q.Error
	}
	if q.RowsAffected == 0 {
		return nil, false, nil
	}
	var lineage []int64
	if json.Unmarshal([]byte(binding.Lineage), &lineage) != nil || lineage == nil || len(lineage) >= maxProgramOwnerLineage {
		return nil, true, ErrInvalidArgument
	}
	for _, id := range lineage {
		if id <= 0 {
			return nil, true, ErrInvalidArgument
		}
	}
	return lineage, true, nil
}

type ProgramOwnerRecovery struct {
	Lost        bool
	Interrupted []string
	Prepared    []ProgramRecoveryTask
}

// No age inference and no script execution. Run locks serialize retirement
// with output freezing; already prepared results retain their original lineage.
func (r *SourceStreamRepo) RecoverProgramExecutionOwner(ctx context.Context, id string, limit int) (ProgramOwnerRecovery, error) {
	return r.recoverProgramExecutionOwner(ctx, id, limit, false)
}

// Separate unprepared retirement from output retries so frozen failing results
// cannot occupy every position ahead of an interrupted, unprepared execution.
func (r *SourceStreamRepo) RetireUnpreparedProgramExecutionOwner(ctx context.Context, id string, limit int) (ProgramOwnerRecovery, error) {
	return r.recoverProgramExecutionOwner(ctx, id, limit, true)
}

func (r *SourceStreamRepo) recoverProgramExecutionOwner(ctx context.Context, id string, limit int, unpreparedOnly bool) (ProgramOwnerRecovery, error) {
	var result ProgramOwnerRecovery
	if !validProgramOwner(id) || limit < 1 || limit > maxProgramRecoveryBatch {
		return result, ErrInvalidArgument
	}
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		// Lock the row before probing the advisory lock. Otherwise recovery
		// could hold the advisory lock while waiting for a guard's SHARE row
		// lock, impersonating a live execution session to that guard.
		var owner programExecutionOwnerRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(&owner).Error; err != nil {
			return err
		}
		var available bool
		if err := tx.Raw(`SELECT pg_try_advisory_xact_lock(?,?)`, programOwnerLockClass, owner.LockID).Scan(&available).Error; err != nil {
			return err
		}
		if !available {
			return nil
		}
		if owner.State == "active" {
			if err := tx.Model(&owner).Updates(map[string]any{"state": "lost", "lost_at": time.Now().UnixMilli()}).Error; err != nil {
				return err
			}
		}
		result.Lost = true
		var runs []StreamRun
		query := tx.Table("public.uns_stream_run AS run").Select("run.*").
			Joins("JOIN public.uns_stream_run_owner binding ON binding.run_id=run.id").
			Where("binding.owner_id=? AND run.model='program' AND run.status IN ('processing','partial-failure')", id)
		if unpreparedOnly {
			query = query.Where("run.status='processing' AND NOT EXISTS (SELECT 1 FROM public.uns_stream_task task WHERE task.id=run.id)")
		}
		if err := query.Order("run.created_at,run.id").Limit(limit).
			Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "run"}, Options: "SKIP LOCKED"}).Find(&runs).Error; err != nil {
			return err
		}
		for _, run := range runs {
			var task StreamTask
			err := tx.Where("id=?", run.ID).Take(&task).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if run.Status != "processing" {
					return ErrConflict
				}
				if err := interruptUnpreparedRuns(tx, "execution-owner-lost", "id=?", run.ID); err != nil {
					return err
				}
				result.Interrupted = append(result.Interrupted, run.ID)
				continue
			}
			if err != nil {
				return err
			}
			if task.Model != "program" || task.StreamID != run.StreamID || task.VersionID != run.VersionID || task.EntryID != run.EntryID || task.SourceID != run.SourceID ||
				(task.Status != "pending" && task.Status != "partial-failure") {
				return ErrConflict
			}
			var binding programRunOwner
			if err := tx.Where("run_id=?", run.ID).Take(&binding).Error; err != nil {
				return err
			}
			var lineage []int64
			if json.Unmarshal([]byte(binding.Lineage), &lineage) != nil || lineage == nil || len(lineage) >= maxProgramOwnerLineage {
				return ErrInvalidArgument
			}
			result.Prepared = append(result.Prepared, ProgramRecoveryTask{Task: task, Lineage: lineage})
		}
		return nil
	})
	if err != nil {
		return ProgramOwnerRecovery{}, normalizeDBError(err)
	}
	return result, nil
}

// UUID keyset pagination keeps a bounded scanner from repeatedly inspecting
// only the oldest live owners. Empty pages wrap at the lifecycle layer.
func (r *SourceStreamRepo) UnpreparedProgramExecutionOwners(ctx context.Context, after string, limit int) ([]string, error) {
	if after != "" && !validProgramOwner(after) || limit < 1 || limit > maxProgramRecoveryBatch {
		return nil, ErrInvalidArgument
	}
	var ids []string
	query := r.db.WithContext(ctx).Table("public.uns_stream_execution_owner owner").Select("owner.id").
		Where(`EXISTS (SELECT 1 FROM public.uns_stream_run_owner binding
			JOIN public.uns_stream_run run ON run.id=binding.run_id
			WHERE binding.owner_id=owner.id AND run.model='program' AND run.status='processing'
			AND NOT EXISTS (SELECT 1 FROM public.uns_stream_task task WHERE task.id=run.id))`)
	if after != "" {
		query = query.Where("owner.id>?::uuid", after)
	}
	err := query.Order("owner.id").Limit(limit).Scan(&ids).Error
	return ids, err
}

func (r *SourceStreamRepo) ProgramExecutionOwners(ctx context.Context, limit int) ([]string, error) {
	if limit < 1 || limit > maxProgramRecoveryBatch {
		return nil, ErrInvalidArgument
	}
	var ids []string
	err := r.db.WithContext(ctx).Raw(`SELECT owner.id FROM public.uns_stream_execution_owner owner
		WHERE EXISTS (SELECT 1 FROM public.uns_stream_run_owner binding
		JOIN public.uns_stream_run run ON run.id=binding.run_id
		WHERE binding.owner_id=owner.id AND run.model='program' AND run.status IN ('processing','partial-failure'))
		ORDER BY owner.created_at,owner.id LIMIT ?`, limit).Scan(&ids).Error
	return ids, err
}
