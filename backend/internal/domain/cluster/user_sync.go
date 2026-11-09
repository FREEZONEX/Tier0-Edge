package cluster

import (
	auditdomain "backend/internal/domain/audit"
	"backend/internal/repo"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const defaultSyncedUserPassword = "tier0"

const FleetUserSyncRequestedOutboxEventType = "fleet.user.sync.requested"

type fleetUserSyncRequest struct {
	EventID     string `json:"eventID"`
	FleetNodeID string `json:"fleetNodeID"`
	Revision    int64  `json:"revision"`
	RequestedBy int64  `json:"requestedBy"`
}
type FleetUserSnapshotRequest struct {
	EventID  string                         `json:"eventId"`
	Snapshot repo.FleetAssignedUserSnapshot `json:"snapshot"`
}

func (s *Service) SyncedFleetUsers(ctx context.Context) (any, error) {
	connection, err := s.storageConnection()
	if err != nil {
		return nil, err
	}
	iam := repo.NewIAMRepo(ctx)
	ids, err := iam.FleetSyncedUserIDs(ctx, connection.CenterID, connection.NodeID)
	if err != nil {
		return nil, err
	}
	users, err := iam.ListClusterUserSnapshotByIDs(ctx, ids, false)
	if err != nil {
		return nil, err
	}
	return map[string]any{"list": users, "total": len(users)}, nil
}

// Assignments and their outbox event commit together. A failed HTTP delivery is
// retried by the existing outbox worker; the Edge acknowledges only after commit.
func (s *Service) PublishFleetUserSyncRequestedOutbox(ctx context.Context, event repo.OutboxEvent) error {
	var request fleetUserSyncRequest
	if json.Unmarshal(event.Payload, &request) != nil || request.EventID != event.EventID || request.FleetNodeID == "" || request.Revision < 1 {
		return ErrInvalid
	}
	snapshot, err := s.repo.GetFleetAssignedUserSnapshot(ctx, request.FleetNodeID)
	if err != nil {
		return err
	}
	if snapshot.Revision > request.Revision {
		return nil
	}
	if snapshot.Revision != request.Revision {
		return ErrInvalid
	}
	value, err := s.CallFleetNode(ctx, request.FleetNodeID, "users.apply", "node", FleetUserSnapshotRequest{EventID: event.EventID, Snapshot: snapshot})
	if err != nil {
		return err
	}
	var result repo.UnsCloudSyncLog
	if json.Unmarshal(value, &result) != nil || result.NodeID != request.FleetNodeID || result.SnapshotVersion != request.Revision || result.SnapshotHash != event.EventID {
		return ErrInvalid
	}

	return repo.GetCommonConn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,814206118))", event.EventID).Error; err != nil {
			return err
		}
		exists, err := repo.NewCloudSyncLogRepo(tx).ExistsBySnapshotHash(ctx, repo.CloudSyncLogSyncTypeUser, request.FleetNodeID, event.EventID)
		if err != nil || exists {
			return err
		}
		result.ID = 0
		result.Direction = repo.CloudSyncLogDirectionOutbound
		return repo.NewCloudSyncLogRepo(tx).Insert(ctx, &result)
	})
}
func (s *Service) ApplyFleetUserSnapshot(ctx context.Context, claims FleetServiceClaims, request FleetUserSnapshotRequest) (any, error) {
	var conn struct {
		NodeID   string `json:"nodeId"`
		CenterID string `json:"centerId"`
	}
	raw, err := os.ReadFile(filepath.Join(s.cfg.IdentityDir, "connection.json"))
	if err != nil || json.Unmarshal(raw, &conn) != nil || claims.Issuer != conn.CenterID || claims.Scope != "users.apply" || claims.ResourceID != "node" || request.Snapshot.FleetNodeID != conn.NodeID {
		return nil, ErrUnauthorized
	}
	snapshot := request.Snapshot
	if len(request.EventID) == 0 || len(request.EventID) > 64 || snapshot.Revision < 1 {
		return nil, ErrInvalid
	}
	body, err := bcrypt.GenerateFromPassword([]byte(defaultSyncedUserPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	var row repo.UnsCloudSyncLog
	var applied repo.ClusterUserSyncApplyResult
	err = repo.GetCommonConn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,814206119))", conn.CenterID+conn.NodeID).Error; err != nil {
			return err
		}
		previous := tx.Where("sync_type=? AND node_id=? AND snapshot_hash=?", repo.CloudSyncLogSyncTypeUser, conn.NodeID, request.EventID).Take(&row).Error
		if previous == nil {
			return nil
		}
		if !errors.Is(previous, gorm.ErrRecordNotFound) {
			return previous
		}
		var e error
		applied, e = repo.NewIAMRepo(tx).ApplyFleetUserSnapshot(ctx, repo.ClusterUserSyncApplyInput{Users: snapshot.Users, PasswordPolicy: repo.UserSyncPasswordPolicyDefault, DefaultPasswordHash: string(body), CenterIdentity: conn.CenterID, CenterType: "host", FleetNodeID: conn.NodeID, SourceRevision: snapshot.Revision, Complete: true})
		if e != nil {
			return e
		}
		input := userSyncLogInput{Started: time.Now(), SyncMode: syncModeEdgeEdge, Source: "host", Transport: "node-protocol", Endpoint: "/api/node/v1/fleet/execution/users/apply", NodeID: conn.NodeID, SnapshotVersion: snapshot.Revision, SnapshotHash: request.EventID}
		completeUserSyncLogInput(&input, applied, nil)
		row = *buildUserSyncLogRow(input)
		return repo.NewCloudSyncLogRepo(tx).Insert(ctx, &row)
	})
	if err != nil {
		return nil, err
	}
	s.recordFleetUserSyncAudit(ctx, &row, applied)
	return row, nil
}

type userSyncLogInput struct {
	Started          time.Time
	Status           string
	ResultCode       string
	SyncMode         string
	Source           string
	Transport        string
	Endpoint         string
	ConnectClientKey string
	NodeID           string
	NodeName         string
	SnapshotVersion  int64
	SnapshotHash     string
	Summary          string
	Cause            error
	ActorID          int64
	Details          map[string]any
}

func completeUserSyncLogInput(input *userSyncLogInput, result repo.ClusterUserSyncApplyResult, cause error) {
	if input == nil {
		return
	}
	input.Status = repo.CloudSyncLogStatusSuccess
	input.ResultCode = syncResultCodeUserApplied
	if cause != nil {
		input.Status = repo.CloudSyncLogStatusFailed
		input.ResultCode = syncResultCodeUserFailed
	} else if len(result.Conflicts) > 0 || len(result.SkippedUsers) > 0 {
		input.Status = repo.CloudSyncLogStatusPartial
		input.ResultCode = syncResultCodeUserPartial
	}
	input.Summary = userSyncSummary(result)
	input.Cause = cause
	input.Details = map[string]any{
		"stats":          result.Stats,
		"createdUsers":   result.CreatedUsers,
		"updatedUsers":   result.UpdatedUsers,
		"disabledUsers":  result.DisabledUsers,
		"deletedUsers":   result.DeletedUsers,
		"skippedUsers":   result.SkippedUsers,
		"conflicts":      result.Conflicts,
		"roleMappings":   result.RoleMappings,
		"passwordPolicy": "defaultPassword",
		"transport":      input.Transport,
	}
}

// recordFleetUserSyncAudit 为用户快照入站落地写一条审计汇总（含失败状态）。
// 内部节点协议使用机器身份；没有用户审计上下文时记录 system。
// 若调用方带 HTTP 上下文且审计状态存在，则交给审计中间件随响应落库。
func (s *Service) recordFleetUserSyncAudit(ctx context.Context, row *repo.UnsCloudSyncLog, result repo.ClusterUserSyncApplyResult) {
	if row == nil {
		return
	}
	input := auditdomain.RecordInput{
		ScopeType:    auditdomain.ScopeTypePlatform,
		ResType:      auditdomain.ResTypeFleet,
		ResID:        row.NodeID,
		ResName:      firstNonEmpty(row.NodeName, row.NodeID),
		BusinessType: auditdomain.BizReplace,
		Code:         syncStatusAuditCode(row.Status),
		Detail: map[string]any{
			"direction":       repo.CloudSyncLogDirectionInbound,
			"syncType":        repo.CloudSyncLogSyncTypeUser,
			"snapshotVersion": row.SnapshotVersion,
			"snapshotHash":    row.SnapshotHash,
			"fleetNodeID":     row.NodeID,
			"status":          row.Status,
			"resultCode":      row.ResultCode,
			"createdUsers":    len(result.CreatedUsers),
			"updatedUsers":    len(result.UpdatedUsers),
			"disabledUsers":   len(result.DisabledUsers),
			"deletedUsers":    len(result.DeletedUsers),
			"skippedUsers":    len(result.SkippedUsers),
			"conflicts":       len(result.Conflicts),
			"roleMappings":    len(result.RoleMappings),
		},
	}
	if auditdomain.StateFromContext(ctx) != nil {
		s.audit.Record(ctx, input)
		return
	}
	s.audit.RecordDirect(nil, input)
}

func buildUserSyncLogRow(input userSyncLogInput) *repo.UnsCloudSyncLog {
	if input.Started.IsZero() {
		input.Started = time.Now()
	}
	durationMs := time.Since(input.Started).Milliseconds()
	if input.Status == "" {
		input.Status = repo.CloudSyncLogStatusSuccess
	}
	if input.Cause != nil {
		input.Status = repo.CloudSyncLogStatusFailed
		input.ResultCode = syncResultCodeUserFailed
	}

	environment := map[string]any{
		"source":     input.Source,
		"transport":  firstNonEmpty(input.Transport, "http"),
		"endpoint":   displayEndpoint(input.Endpoint),
		"operatorId": input.ActorID,
		"durationMs": durationMs,
	}
	details := input.Details
	if details == nil {
		details = map[string]any{}
	}
	details["durationMs"] = durationMs
	row := &repo.UnsCloudSyncLog{
		SyncType:         repo.CloudSyncLogSyncTypeUser,
		Direction:        repo.CloudSyncLogDirectionInbound,
		Status:           input.Status,
		SyncMode:         normalizeSyncMode(input.SyncMode, syncModeEdgeEdge),
		ConnectClientKey: input.ConnectClientKey,
		NodeID:           input.NodeID,
		NodeName:         input.NodeName,
		SnapshotVersion:  input.SnapshotVersion,
		SnapshotHash:     input.SnapshotHash,
		ResultCode:       input.ResultCode,
		Summary:          input.Summary,
		Environment:      marshalLogJSON(environment),
		Details:          marshalLogJSON(details),
	}
	if input.Cause != nil {
		row.ErrorMessage = input.Cause.Error()
	}
	return row
}

func userSyncLogResponse(row *repo.UnsCloudSyncLog) map[string]any {
	if row == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":               row.ID,
		"syncType":         row.SyncType,
		"direction":        row.Direction,
		"status":           row.Status,
		"syncMode":         row.SyncMode,
		"connectClientKey": row.ConnectClientKey,
		"nodeID":           row.NodeID,
		"nodeName":         row.NodeName,
		"snapshotVersion":  row.SnapshotVersion,
		"snapshotHash":     row.SnapshotHash,
		"resultCode":       row.ResultCode,
		"summary":          row.Summary,
		"errorMessage":     row.ErrorMessage,
		"environment":      row.Environment,
		"details":          row.Details,
		"createdTime":      row.CreatedTime,
		"updatedTime":      row.UpdatedTime,
	}
}

func userSyncSummary(result repo.ClusterUserSyncApplyResult) string {
	return fmt.Sprintf(
		"users synchronized: created=%d updated=%d disabled=%d deleted=%d skipped=%d conflicts=%d",
		len(result.CreatedUsers),
		len(result.UpdatedUsers),
		len(result.DisabledUsers),
		len(result.DeletedUsers),
		len(result.SkippedUsers),
		len(result.Conflicts),
	)
}

func displayEndpoint(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	if parsed, err := http.NewRequest(http.MethodGet, endpoint, nil); err == nil {
		parsed.URL.RawQuery = ""
		return parsed.URL.String()
	}
	return endpoint
}
