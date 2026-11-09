package repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"gorm.io/gorm"
)

const MaxProgramCheckpointBytes = 16 * 1024

// Cursor identity is stable across versions, but isolated between entry IDs.
type ProgramCheckpoint struct {
	Revision int64           `json:"revision"`
	Value    json.RawMessage `json:"value"`
}

type ProgramCheckpointCommit struct {
	StreamID         int64
	EntryID          string
	RunID            string
	Digest           string
	ExpectedRevision int64
	Value            json.RawMessage
}

func (r *SourceStreamRepo) ProgramCheckpoint(ctx context.Context, streamID int64, entryID string) (ProgramCheckpoint, error) {
	if streamID <= 0 || entryID == "" || len(entryID) > 128 {
		return ProgramCheckpoint{}, ErrInvalidArgument
	}
	var result ProgramCheckpoint
	err := getIntegrationTask(r.db.WithContext(ctx), "programCheckpoint", programIdentity(streamID, entryID), &result)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ProgramCheckpoint{Value: json.RawMessage(`null`)}, nil
	}
	if err == nil && (result.Revision < 0 || !json.Valid(result.Value) || len(result.Value) > MaxProgramCheckpointBytes) {
		err = ErrInvalidArgument
	}
	return result, err
}

func (r *SourceStreamRepo) ProgramCheckpointCommitted(ctx context.Context, runID, digest string) (bool, error) {
	if runID == "" || len(runID) > 64 || len(digest) != 64 {
		return false, ErrInvalidArgument
	}
	var receipt map[string]string
	err := getIntegrationTask(r.db.WithContext(ctx), "programCheckpointCommit", runID, &receipt)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if receipt["digest"] != digest {
		return false, ErrConflict
	}
	return true, nil
}

// LockProgramCheckpoint operates on the actual sink transaction: its receipt,
// cursor CAS and Topic writes cannot be committed independently. Notifications
// are best effort after commit and are not part of this transaction.
func LockProgramCheckpoint(ctx context.Context, tx pgx.Tx, change ProgramCheckpointCommit) (bool, func() error, error) {
	if change.StreamID <= 0 || change.EntryID == "" || len(change.EntryID) > 128 || change.RunID == "" || len(change.RunID) > 64 || len(change.Digest) != 64 || change.ExpectedRevision < 0 || !json.Valid(change.Value) || len(change.Value) > MaxProgramCheckpointBytes {
		return false, nil, ErrInvalidArgument
	}
	// This is also a public raw-pgx entrypoint: retain the metadata gate
	// before taking receipt/cursor locks, just like the GORM execution path.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock_shared(814206108)"); err != nil {
		return false, nil, err
	}
	now := time.Now().UnixMilli()
	receiptID := runtimeTaskID("programCheckpointCommit", change.RunID)
	receiptPayload, _ := json.Marshal(map[string]string{"digest": change.Digest})
	receipt, _ := json.Marshal(integrationTaskData{Type: "programCheckpointCommit", Identity: "programCheckpointCommit:" + change.RunID, Payload: receiptPayload})
	tag, err := tx.Exec(ctx, `INSERT INTO public.uns_stream_commit_receipt(id,data,revision,created_at,updated_at) VALUES($1,$2::jsonb,1,$3,$3) ON CONFLICT DO NOTHING`, receiptID, string(receipt), now)
	if err != nil {
		return false, nil, err
	}
	if tag.RowsAffected() == 0 {
		var digest string
		if err := tx.QueryRow(ctx, `SELECT data->'payload'->>'digest' FROM public.uns_stream_commit_receipt WHERE id=$1`, receiptID).Scan(&digest); err != nil {
			return false, nil, err
		}
		if digest != change.Digest {
			return false, nil, ErrConflict
		}
		return false, nil, nil
	}
	identity := "programCheckpoint:" + programIdentity(change.StreamID, change.EntryID)
	id := runtimeTaskID("programCheckpoint", programIdentity(change.StreamID, change.EntryID))
	empty, _ := json.Marshal(integrationTaskData{Type: "programCheckpoint", Identity: identity, Payload: json.RawMessage(`{"revision":0,"value":null}`)})
	if _, err := tx.Exec(ctx, `INSERT INTO public.uns_stream_checkpoint(id,data,revision,created_at,updated_at) VALUES($1,$2::jsonb,1,$3,$3) ON CONFLICT DO NOTHING`, id, string(empty), now); err != nil {
		return false, nil, err
	}
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT (data->'payload'->>'revision')::bigint FROM public.uns_stream_checkpoint WHERE id=$1 FOR UPDATE`, id).Scan(&revision); err != nil {
		return false, nil, err
	}
	if revision != change.ExpectedRevision {
		return false, nil, ErrConflict
	}
	finish := func() error {
		payload, err := json.Marshal(ProgramCheckpoint{Revision: revision + 1, Value: change.Value})
		if err != nil {
			return err
		}
		data, err := json.Marshal(integrationTaskData{Type: "programCheckpoint", Identity: identity, Payload: payload})
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE public.uns_stream_checkpoint SET data=$2::jsonb,revision=revision+1,updated_at=$3 WHERE id=$1 AND (data->'payload'->>'revision')::bigint=$4`, id, string(data), now, revision)
		if err == nil && tag.RowsAffected() != 1 {
			return ErrConflict
		}
		return err
	}
	return true, finish, nil
}
