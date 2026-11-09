package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backend/fleet-agent/internal/config"
	"backend/fleet-agent/internal/operation"
	"backend/fleet-agent/internal/secret"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const (
	databaseFileName              = "fleet-agent.db"
	currentSchemaVersion          = 7
	haSyncHistoryRetention        = 30 * 24 * time.Hour
	haSyncHistoryMaximumEventRows = 100000
)

type HALocalState struct {
	MemberID         string
	Role             string
	LastTransitionID string
	UpdatedAt        int64
}

type HASyncState struct {
	Peer             string
	FileSet          string
	Revision         string
	ConfigDigest     string
	ContentRevision  string
	FileCount        int64
	TotalBytes       int64
	NodeModulesFiles int64
	NodeModulesBytes int64
	Status           string
	LastStartedAt    int64
	LastSucceededAt  int64
	ErrorMessage     string
}

type HASyncEvent struct {
	ID           int64
	OperationID  string
	Peer         string
	FileSet      string
	Revision     string
	Direction    string
	TriggerType  string
	Status       string
	StartedAt    int64
	FinishedAt   int64
	ErrorMessage string
}

type HASyncEventQuery struct {
	Peer    string
	FileSet string
	Limit   int
	Offset  int
}

type HASyncEventPage struct {
	Items []HASyncEvent
	Total int64
}

type Store struct {
	db        *sql.DB
	dbPath    string
	protector *secret.Protector
}

func Open(ctx context.Context, dataDir string) (*Store, error) {
	dataDir = filepath.Clean(dataDir)
	if dataDir == "." || dataDir == "" {
		return nil, errors.New("Fleet Agent data directory is empty")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create Fleet Agent data directory: %w", err)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure Fleet Agent data directory: %w", err)
	}
	protector, err := secret.Open(dataDir)
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dataDir, databaseFileName)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open Fleet Agent SQLite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db, dbPath: dbPath, protector: protector}
	if err := store.initialize(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) initialize(ctx context.Context) error {
	for _, statement := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = FULL",
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure Fleet Agent SQLite: %w", err)
		}
	}
	if err := s.migrate(ctx); err != nil {
		return err
	}
	if err := s.recoverInterruptedOperations(ctx); err != nil {
		return err
	}
	return s.secureDatabaseFiles()
}

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read Fleet Agent schema version: %w", err)
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("Fleet Agent database schema %d is newer than supported version %d", version, currentSchemaVersion)
	}
	if version == currentSchemaVersion {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start Fleet Agent migration: %w", err)
	}
	defer tx.Rollback()
	if version < 1 {
		for _, statement := range []string{
			`CREATE TABLE IF NOT EXISTS agent_config (
				id INTEGER PRIMARY KEY CHECK (id = 1),
				fleet_node_id TEXT NOT NULL DEFAULT '',
				center_url TEXT NOT NULL DEFAULT '',
				access_token_ciphertext BLOB,
				local_api_token_hash BLOB,
				compose_project_name TEXT NOT NULL DEFAULT '',
				installation_id TEXT NOT NULL DEFAULT '',
				last_connected_at INTEGER NOT NULL DEFAULT 0,
				schema_version INTEGER NOT NULL DEFAULT 1,
				updated_at INTEGER NOT NULL DEFAULT 0
			)`,
			`CREATE TABLE IF NOT EXISTS operation_dedup (
				operation_id TEXT PRIMARY KEY,
				operation_type TEXT NOT NULL,
				target_id TEXT NOT NULL DEFAULT '',
				status TEXT NOT NULL,
				result_digest TEXT NOT NULL DEFAULT '',
				started_at INTEGER NOT NULL DEFAULT 0,
				finished_at INTEGER NOT NULL DEFAULT 0,
				expires_at INTEGER NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_operation_dedup_expires_at ON operation_dedup(expires_at)`,
			`PRAGMA user_version = 1`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply Fleet Agent schema v1: %w", err)
			}
		}
		version = 1
	}
	if version < 2 {
		for _, statement := range []string{
			`ALTER TABLE operation_dedup ADD COLUMN error_code TEXT NOT NULL DEFAULT ''`,
			`PRAGMA user_version = 2`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply Fleet Agent schema v2: %w", err)
			}
		}
		version = 2
	}
	if version < 3 {
		for _, statement := range []string{
			`CREATE TABLE IF NOT EXISTS ha_local_state (
				id INTEGER PRIMARY KEY CHECK (id = 1),
				member_id TEXT NOT NULL UNIQUE,
				role TEXT NOT NULL DEFAULT 'standby',
				role_epoch INTEGER NOT NULL DEFAULT 0,
				last_transition_id TEXT NOT NULL DEFAULT '',
				updated_at INTEGER NOT NULL DEFAULT 0
			)`,
			`CREATE TABLE IF NOT EXISTS artifact_cursor (
				artifact_key TEXT PRIMARY KEY,
				revision TEXT NOT NULL DEFAULT '',
				sha256 TEXT NOT NULL DEFAULT '',
				applied_at INTEGER NOT NULL DEFAULT 0
			)`,
			`PRAGMA user_version = 3`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply Fleet Agent schema v3: %w", err)
			}
		}
		version = 3
	}
	if version < 4 {
		for _, statement := range []string{
			`ALTER TABLE ha_local_state RENAME TO ha_local_state_v3`,
			`CREATE TABLE ha_local_state (
				id INTEGER PRIMARY KEY CHECK (id = 1),
				member_id TEXT NOT NULL UNIQUE,
				role TEXT NOT NULL DEFAULT 'standby',
				last_transition_id TEXT NOT NULL DEFAULT '',
				updated_at INTEGER NOT NULL DEFAULT 0
			)`,
			`INSERT INTO ha_local_state (id, member_id, role, last_transition_id, updated_at)
			 SELECT id, member_id, role, last_transition_id, updated_at FROM ha_local_state_v3`,
			`DROP TABLE ha_local_state_v3`,
			`DROP TABLE artifact_cursor`,
			`CREATE TABLE ha_sync_state (
				peer TEXT NOT NULL,
				file_set TEXT NOT NULL,
				revision TEXT NOT NULL DEFAULT '',
				status TEXT NOT NULL DEFAULT 'idle',
				last_started_at INTEGER NOT NULL DEFAULT 0,
				last_succeeded_at INTEGER NOT NULL DEFAULT 0,
				error_message TEXT NOT NULL DEFAULT '',
				PRIMARY KEY (peer, file_set)
			)`,
			`PRAGMA user_version = 4`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply Fleet Agent schema v4: %w", err)
			}
		}
		version = 4
	}
	if version < 5 {
		for _, statement := range []string{
			`CREATE TABLE ha_sync_event (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				operation_id TEXT NOT NULL,
				peer TEXT NOT NULL,
				file_set TEXT NOT NULL,
				revision TEXT NOT NULL DEFAULT '',
				direction TEXT NOT NULL,
				trigger_type TEXT NOT NULL,
				status TEXT NOT NULL,
				started_at INTEGER NOT NULL DEFAULT 0,
				finished_at INTEGER NOT NULL DEFAULT 0,
				error_message TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE INDEX idx_ha_sync_event_finished_at ON ha_sync_event(finished_at DESC)`,
			`CREATE INDEX idx_ha_sync_event_peer_file_set ON ha_sync_event(peer, file_set, finished_at DESC)`,
			`PRAGMA user_version = 5`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply Fleet Agent schema v5: %w", err)
			}
		}
		version = 5
	}
	if version < 6 {
		for _, statement := range []string{
			`ALTER TABLE ha_sync_state ADD COLUMN config_digest TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE ha_sync_state ADD COLUMN content_revision TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE ha_sync_state ADD COLUMN file_count INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE ha_sync_state ADD COLUMN total_bytes INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE ha_sync_state ADD COLUMN node_modules_files INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE ha_sync_state ADD COLUMN node_modules_bytes INTEGER NOT NULL DEFAULT 0`,
			`PRAGMA user_version = 6`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply Fleet Agent schema v6: %w", err)
			}
		}
		version = 6
	}
	if version < 7 {
		for _, statement := range []string{
			`ALTER TABLE agent_config ADD COLUMN desired_connected INTEGER NOT NULL DEFAULT 1`,
			`PRAGMA user_version = 7`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply Fleet Agent schema v7: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Fleet Agent migration: %w", err)
	}
	return nil
}

func (s *Store) EnsureHAMemberID(ctx context.Context, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		if _, err := uuid.Parse(requested); err != nil {
			return "", errors.New("Fleet HA member ID must be a UUID")
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("start Fleet HA identity transaction: %w", err)
	}
	defer tx.Rollback()

	var existing string
	err = tx.QueryRowContext(ctx, "SELECT member_id FROM ha_local_state WHERE id = 1").Scan(&existing)
	switch {
	case err == nil:
		if requested != "" && requested != existing {
			return "", errors.New("configured Fleet HA member ID does not match persisted identity")
		}
		if err := tx.Commit(); err != nil {
			return "", fmt.Errorf("commit Fleet HA identity lookup: %w", err)
		}
		return existing, nil
	case !errors.Is(err, sql.ErrNoRows):
		return "", fmt.Errorf("load Fleet HA member identity: %w", err)
	}

	memberID := requested
	if memberID == "" {
		memberID = uuid.NewString()
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ha_local_state (id, member_id, role, updated_at)
		VALUES (1, ?, 'standby', ?)`,
		memberID,
		time.Now().UTC().UnixMilli(),
	); err != nil {
		return "", fmt.Errorf("persist Fleet HA member identity: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit Fleet HA member identity: %w", err)
	}
	if err := s.secureDatabaseFiles(); err != nil {
		return "", err
	}
	return memberID, nil
}

func (s *Store) LoadHALocalState(ctx context.Context) (HALocalState, bool, error) {
	var out HALocalState
	err := s.db.QueryRowContext(ctx, `
		SELECT member_id, role, last_transition_id, updated_at
		FROM ha_local_state WHERE id = 1`).Scan(
		&out.MemberID,
		&out.Role,
		&out.LastTransitionID,
		&out.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return HALocalState{}, false, nil
	}
	if err != nil {
		return HALocalState{}, false, fmt.Errorf("load Fleet HA local state: %w", err)
	}
	return out, true, nil
}

func (s *Store) SaveHALocalState(ctx context.Context, input HALocalState) error {
	input.MemberID = strings.TrimSpace(input.MemberID)
	input.Role = strings.TrimSpace(input.Role)
	input.LastTransitionID = strings.TrimSpace(input.LastTransitionID)
	if input.MemberID == "" || input.Role == "" {
		return errors.New("invalid Fleet HA local state")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE ha_local_state
		SET role = ?, last_transition_id = ?, updated_at = ?
		WHERE id = 1 AND member_id = ?`,
		input.Role,
		input.LastTransitionID,
		time.Now().UTC().UnixMilli(),
		input.MemberID,
	)
	if err != nil {
		return fmt.Errorf("save Fleet HA local state: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read Fleet HA local state result: %w", err)
	}
	if rows != 1 {
		return errors.New("Fleet HA local state identity was not found")
	}
	return s.secureDatabaseFiles()
}

func (s *Store) SaveHASyncState(ctx context.Context, input HASyncState) error {
	input.Peer = strings.TrimSpace(input.Peer)
	input.FileSet = strings.TrimSpace(input.FileSet)
	input.Revision = strings.TrimSpace(input.Revision)
	input.ConfigDigest = strings.TrimSpace(input.ConfigDigest)
	input.ContentRevision = strings.TrimSpace(input.ContentRevision)
	input.Status = strings.TrimSpace(input.Status)
	input.ErrorMessage = strings.TrimSpace(input.ErrorMessage)
	if input.Peer == "" || input.FileSet == "" || input.Status == "" {
		return errors.New("invalid Fleet HA sync state")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ha_sync_state (
			peer, file_set, revision, config_digest, content_revision,
			file_count, total_bytes, node_modules_files, node_modules_bytes,
			status, last_started_at, last_succeeded_at, error_message
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(peer, file_set) DO UPDATE SET
			revision = excluded.revision,
			config_digest = excluded.config_digest,
			content_revision = excluded.content_revision,
			file_count = excluded.file_count,
			total_bytes = excluded.total_bytes,
			node_modules_files = excluded.node_modules_files,
			node_modules_bytes = excluded.node_modules_bytes,
			status = excluded.status,
			last_started_at = excluded.last_started_at,
			last_succeeded_at = CASE
				WHEN excluded.last_succeeded_at > 0 THEN excluded.last_succeeded_at
				ELSE ha_sync_state.last_succeeded_at
			END,
			error_message = excluded.error_message`,
		input.Peer,
		input.FileSet,
		input.Revision,
		input.ConfigDigest,
		input.ContentRevision,
		input.FileCount,
		input.TotalBytes,
		input.NodeModulesFiles,
		input.NodeModulesBytes,
		input.Status,
		input.LastStartedAt,
		input.LastSucceededAt,
		input.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("save Fleet HA sync state: %w", err)
	}
	return s.secureDatabaseFiles()
}

func (s *Store) LoadHASyncStates(ctx context.Context) ([]HASyncState, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT peer, file_set, revision, config_digest, content_revision,
		       file_count, total_bytes, node_modules_files, node_modules_bytes,
		       status, last_started_at, last_succeeded_at, error_message
		FROM ha_sync_state
		ORDER BY peer, file_set`)
	if err != nil {
		return nil, fmt.Errorf("load Fleet HA sync states: %w", err)
	}
	defer rows.Close()
	result := make([]HASyncState, 0)
	for rows.Next() {
		var item HASyncState
		if err := rows.Scan(
			&item.Peer,
			&item.FileSet,
			&item.Revision,
			&item.ConfigDigest,
			&item.ContentRevision,
			&item.FileCount,
			&item.TotalBytes,
			&item.NodeModulesFiles,
			&item.NodeModulesBytes,
			&item.Status,
			&item.LastStartedAt,
			&item.LastSucceededAt,
			&item.ErrorMessage,
		); err != nil {
			return nil, fmt.Errorf("scan Fleet HA sync state: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Fleet HA sync states: %w", err)
	}
	return result, nil
}

func (s *Store) SaveHASyncEvent(ctx context.Context, input HASyncEvent) error {
	input.OperationID = strings.TrimSpace(input.OperationID)
	input.Peer = strings.TrimSpace(input.Peer)
	input.FileSet = strings.TrimSpace(input.FileSet)
	input.Revision = strings.TrimSpace(input.Revision)
	input.Direction = strings.TrimSpace(input.Direction)
	input.TriggerType = strings.TrimSpace(input.TriggerType)
	input.Status = strings.TrimSpace(input.Status)
	input.ErrorMessage = strings.TrimSpace(input.ErrorMessage)
	if input.OperationID == "" || input.Peer == "" || input.FileSet == "" ||
		input.Direction == "" || input.TriggerType == "" || input.Status == "" ||
		input.StartedAt <= 0 || input.FinishedAt < input.StartedAt {
		return errors.New("invalid Fleet HA sync event")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start Fleet HA sync event transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ha_sync_event (
			operation_id, peer, file_set, revision, direction, trigger_type,
			status, started_at, finished_at, error_message
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.OperationID,
		input.Peer,
		input.FileSet,
		input.Revision,
		input.Direction,
		input.TriggerType,
		input.Status,
		input.StartedAt,
		input.FinishedAt,
		input.ErrorMessage,
	); err != nil {
		return fmt.Errorf("save Fleet HA sync event: %w", err)
	}
	cutoff := time.Now().UTC().Add(-haSyncHistoryRetention).UnixMilli()
	if _, err := tx.ExecContext(ctx, "DELETE FROM ha_sync_event WHERE finished_at < ?", cutoff); err != nil {
		return fmt.Errorf("prune expired Fleet HA sync events: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM ha_sync_event
		WHERE id <= COALESCE((
			SELECT id FROM ha_sync_event
			ORDER BY id DESC
			LIMIT 1 OFFSET ?
		), 0)`,
		haSyncHistoryMaximumEventRows,
	); err != nil {
		return fmt.Errorf("cap Fleet HA sync events: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Fleet HA sync event: %w", err)
	}
	return s.secureDatabaseFiles()
}

func (s *Store) LoadHASyncEvents(ctx context.Context, query HASyncEventQuery) (HASyncEventPage, error) {
	query.Peer = strings.TrimSpace(query.Peer)
	query.FileSet = strings.TrimSpace(query.FileSet)
	if query.Limit < 1 {
		query.Limit = 20
	}
	if query.Limit > 100 {
		query.Limit = 100
	}
	if query.Offset < 0 {
		query.Offset = 0
	}
	where := make([]string, 0, 2)
	args := make([]any, 0, 4)
	if query.Peer != "" {
		where = append(where, "peer = ?")
		args = append(args, query.Peer)
	}
	if query.FileSet != "" {
		where = append(where, "file_set = ?")
		args = append(args, query.FileSet)
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}
	var total int64
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM ha_sync_event"+whereSQL,
		args...,
	).Scan(&total); err != nil {
		return HASyncEventPage{}, fmt.Errorf("count Fleet HA sync events: %w", err)
	}
	listArgs := append(append([]any(nil), args...), query.Limit, query.Offset)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, operation_id, peer, file_set, revision, direction,
		       trigger_type, status, started_at, finished_at, error_message
		FROM ha_sync_event`+whereSQL+`
		ORDER BY id DESC
		LIMIT ? OFFSET ?`,
		listArgs...,
	)
	if err != nil {
		return HASyncEventPage{}, fmt.Errorf("load Fleet HA sync events: %w", err)
	}
	defer rows.Close()
	result := HASyncEventPage{
		Items: make([]HASyncEvent, 0),
		Total: total,
	}
	for rows.Next() {
		var item HASyncEvent
		if err := rows.Scan(
			&item.ID,
			&item.OperationID,
			&item.Peer,
			&item.FileSet,
			&item.Revision,
			&item.Direction,
			&item.TriggerType,
			&item.Status,
			&item.StartedAt,
			&item.FinishedAt,
			&item.ErrorMessage,
		); err != nil {
			return HASyncEventPage{}, fmt.Errorf("scan Fleet HA sync event: %w", err)
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return HASyncEventPage{}, fmt.Errorf("iterate Fleet HA sync events: %w", err)
	}
	return result, nil
}

func (s *Store) recoverInterruptedOperations(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE operation_dedup
		SET status = ?, error_code = ?, finished_at = ?
		WHERE status = ?`,
		operation.StatusFailed,
		"FLEET_OPERATION_INTERRUPTED",
		time.Now().UTC().UnixMilli(),
		operation.StatusRunning,
	); err != nil {
		return fmt.Errorf("recover interrupted Fleet Agent operations: %w", err)
	}
	return nil
}

func (s *Store) ClaimOperation(ctx context.Context, input operation.Record) (operation.Record, bool, error) {
	now := time.Now().UTC().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return operation.Record{}, false, fmt.Errorf("start Fleet operation claim: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM operation_dedup WHERE expires_at <= ?", now); err != nil {
		return operation.Record{}, false, fmt.Errorf("prune Fleet operation records: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO operation_dedup (
			operation_id, operation_type, target_id, status, result_digest, error_code,
			started_at, finished_at, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(operation_id) DO NOTHING`,
		input.OperationID,
		input.Type,
		input.TargetID,
		input.Status,
		input.ResultDigest,
		input.ErrorCode,
		input.StartedAt,
		input.FinishedAt,
		input.ExpiresAt,
	)
	if err != nil {
		return operation.Record{}, false, fmt.Errorf("claim Fleet operation: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return operation.Record{}, false, fmt.Errorf("read Fleet operation claim result: %w", err)
	}
	if rows == 1 {
		if err := tx.Commit(); err != nil {
			return operation.Record{}, false, fmt.Errorf("commit Fleet operation claim: %w", err)
		}
		return input, true, nil
	}
	existing, err := scanOperation(tx.QueryRowContext(ctx, `
		SELECT operation_id, operation_type, target_id, status, result_digest, error_code,
		       started_at, finished_at, expires_at
		FROM operation_dedup WHERE operation_id = ?`, input.OperationID))
	if err != nil {
		return operation.Record{}, false, fmt.Errorf("load existing Fleet operation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return operation.Record{}, false, fmt.Errorf("commit Fleet operation lookup: %w", err)
	}
	return existing, false, nil
}

func (s *Store) CompleteOperation(ctx context.Context, input operation.Record) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE operation_dedup
		SET status = ?, result_digest = ?, error_code = ?, finished_at = ?, expires_at = ?
		WHERE operation_id = ? AND operation_type = ? AND target_id = ?`,
		input.Status,
		input.ResultDigest,
		input.ErrorCode,
		input.FinishedAt,
		input.ExpiresAt,
		input.OperationID,
		input.Type,
		input.TargetID,
	)
	if err != nil {
		return fmt.Errorf("complete Fleet operation: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read Fleet operation completion result: %w", err)
	}
	if rows != 1 {
		return errors.New("Fleet operation record was not found")
	}
	return s.secureDatabaseFiles()
}

type rowScanner interface {
	Scan(...any) error
}

func scanOperation(row rowScanner) (operation.Record, error) {
	var out operation.Record
	err := row.Scan(
		&out.OperationID,
		&out.Type,
		&out.TargetID,
		&out.Status,
		&out.ResultDigest,
		&out.ErrorCode,
		&out.StartedAt,
		&out.FinishedAt,
		&out.ExpiresAt,
	)
	return out, err
}

func (s *Store) LoadConfig(ctx context.Context) (config.Persisted, bool, error) {
	var out config.Persisted
	var ciphertext []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT fleet_node_id, center_url, access_token_ciphertext, desired_connected,
		       local_api_token_hash, compose_project_name, installation_id, last_connected_at
		FROM agent_config WHERE id = 1`).Scan(
		&out.FleetNodeID,
		&out.CenterURL,
		&ciphertext,
		&out.DesiredConnected,
		&out.LocalTokenHash,
		&out.ComposeProjectName,
		&out.InstallationID,
		&out.LastConnectedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return config.Persisted{}, false, nil
	}
	if err != nil {
		return config.Persisted{}, false, fmt.Errorf("query Fleet Agent configuration: %w", err)
	}
	out.AccessToken, err = s.protector.DecryptAccessToken(ciphertext)
	if err != nil {
		return config.Persisted{}, false, fmt.Errorf("decrypt Fleet Access Token: %w", err)
	}
	return out, true, nil
}

func (s *Store) SaveConfig(ctx context.Context, input config.Persisted) error {
	ciphertext, err := s.protector.EncryptAccessToken(input.AccessToken)
	if err != nil {
		return fmt.Errorf("encrypt Fleet Access Token: %w", err)
	}
	now := time.Now().UTC().UnixMilli()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO agent_config (
			id, fleet_node_id, center_url, access_token_ciphertext, desired_connected, local_api_token_hash,
			compose_project_name, installation_id, last_connected_at, schema_version, updated_at
		) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			fleet_node_id = excluded.fleet_node_id,
			center_url = excluded.center_url,
			access_token_ciphertext = excluded.access_token_ciphertext,
			desired_connected = excluded.desired_connected,
			local_api_token_hash = excluded.local_api_token_hash,
			compose_project_name = excluded.compose_project_name,
			installation_id = excluded.installation_id,
			last_connected_at = excluded.last_connected_at,
			schema_version = excluded.schema_version,
			updated_at = excluded.updated_at`,
		input.FleetNodeID,
		input.CenterURL,
		ciphertext,
		input.DesiredConnected,
		input.LocalTokenHash,
		input.ComposeProjectName,
		input.InstallationID,
		input.LastConnectedAt,
		currentSchemaVersion,
		now,
	)
	if err != nil {
		return fmt.Errorf("save Fleet Agent configuration: %w", err)
	}
	return s.secureDatabaseFiles()
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) secureDatabaseFiles() error {
	for _, path := range []string{s.dbPath, s.dbPath + "-wal", s.dbPath + "-shm"} {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("inspect Fleet Agent SQLite file: %w", err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("secure Fleet Agent SQLite file: %w", err)
		}
	}
	return nil
}
