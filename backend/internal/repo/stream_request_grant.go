package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Request grants are private metadata, outside the editable Namespace kinds.
// Their authority is an exact immutable release and its complete target closure.
const RequestGrantKind = "task"

type RequestTargetGrant struct {
	Digest string `json:"digest"`
	Action string `json:"action"`
}

type RequestGrant struct {
	Type             string               `json:"type"`
	AppID            string               `json:"appId"`
	Code             string               `json:"code"`
	BindingDigest    string               `json:"bindingDigest"`
	StreamID         string               `json:"streamId"`
	VersionID        string               `json:"versionId"`
	EntryID          string               `json:"entryId"`
	Actions          []string             `json:"actions"`
	Targets          []RequestTargetGrant `json:"targets"`
	Active           bool                 `json:"active"`
	Revision         int64                `json:"revision"`
	CreatedPrincipal string               `json:"createdPrincipal"`
	UpdatedPrincipal string               `json:"updatedPrincipal"`
	CreatedTime      int64                `json:"createdTime"`
	UpdatedTime      int64                `json:"updatedTime"`
}

func requestGrantID(appID, code string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("request-grant:"+appID+":"+code)).String()
}

func (r *SourceStreamRepo) RequestGrant(ctx context.Context, appID, code string) (RequestGrant, error) {
	var row UnsMeta
	err := r.db.WithContext(ctx).Where("kind=? AND id=? AND deleted_at=0", RequestGrantKind, requestGrantID(appID, code)).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return RequestGrant{Type: "streamRequestGrant", AppID: appID, Code: code, Actions: []string{}, Targets: []RequestTargetGrant{}}, nil
	}
	if err != nil {
		return RequestGrant{}, err
	}
	var grant RequestGrant
	if json.Unmarshal(row.Data, &grant) != nil || grant.Type != "streamRequestGrant" || grant.AppID != appID || grant.Code != code || grant.Revision != row.Revision {
		return grant, ErrInvalidArgument
	}
	return grant, nil
}

// SnapshotToken is a compare-and-swap version, never an authorization proof.
func (g RequestGrant) SnapshotToken(binding string) string {
	if binding == "" {
		binding = "0000000000000000000000000000000000000000000000000000000000000000"
	}
	raw, _ := json.Marshal([]any{"enterprise-request-grant/v1", binding, g})
	sum := sha256.Sum256(raw)
	return "cgs1." + binding + "." + hex.EncodeToString(sum[:])
}

// The domain owns the transaction and refreshes manager/recipient/target rights.
// Match the existing publication lock order before taking the per-grant lock.
func (r *SourceStreamRepo) LockRequestGrant(ctx context.Context, appID, code string) error {
	if err := r.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock_shared(?)", int64(846031001)).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", "request-grant:"+appID+":"+code).Error
}

func (r *SourceStreamRepo) PutRequestGrant(ctx context.Context, grant RequestGrant, previous RequestGrant) (RequestGrant, error) {
	now := time.Now().UnixMilli()
	grant.Type = "streamRequestGrant"
	grant.Revision = previous.Revision + 1
	grant.CreatedPrincipal, grant.CreatedTime = previous.CreatedPrincipal, previous.CreatedTime
	if previous.Revision == 0 {
		grant.CreatedPrincipal, grant.CreatedTime = grant.UpdatedPrincipal, now
	}
	grant.UpdatedTime = now
	raw, err := json.Marshal(grant)
	if err != nil {
		return grant, err
	}
	row := UnsMeta{ID: requestGrantID(grant.AppID, grant.Code), Kind: RequestGrantKind, Data: raw, Revision: grant.Revision, CreatedAt: grant.CreatedTime, UpdatedAt: now}
	if previous.Revision == 0 {
		err = r.db.WithContext(ctx).Create(&row).Error
	} else {
		result := r.db.WithContext(ctx).Model(&UnsMeta{}).Where("kind=? AND id=? AND revision=?", RequestGrantKind, row.ID, previous.Revision).Updates(map[string]any{"data": raw, "revision": grant.Revision, "updated_at": now})
		err = result.Error
		if err == nil && result.RowsAffected != 1 {
			err = ErrConflict
		}
	}
	return grant, normalizeDBError(err)
}

func (r *SourceStreamRepo) AppRequestGrants(ctx context.Context, appID, streamID string, page, size int, includeInactive bool) ([]RequestGrant, int64, error) {
	query := r.db.WithContext(ctx).Model(&UnsMeta{}).Where("kind=? AND deleted_at=0 AND data->>'type'='streamRequestGrant' AND data->>'appId'=?", RequestGrantKind, appID)
	if streamID != "" {
		query = query.Where("data->>'streamId'=?", streamID)
	}
	if !includeInactive {
		query = query.Where("data->>'active'='true'")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []UnsMeta
	if err := query.Order("data->>'code'").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	grants := make([]RequestGrant, 0, len(rows))
	for _, row := range rows {
		var grant RequestGrant
		if json.Unmarshal(row.Data, &grant) != nil || grant.AppID != appID || grant.Revision != row.Revision {
			return nil, 0, ErrInvalidArgument
		}
		grants = append(grants, grant)
	}
	return grants, total, nil
}
