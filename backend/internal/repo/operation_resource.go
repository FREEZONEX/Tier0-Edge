package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"backend/internal/operationcontract"
	"backend/internal/streamexec"
	"backend/internal/unsref"
	"gorm.io/gorm"
)

var ErrControlInactive = errors.New("control inactive")
var ErrControlIdempotencyConflict = errors.New("control idempotency conflict")
var ErrControlCapability = errors.New("control capability changed")
var ErrControlExpired = errors.New("control invocation expired")

type OperationBinding struct {
	Type              string `json:"type"`
	StreamID          string `json:"streamId"`
	EntryID           string `json:"entryId"`
	DeploymentVersion string `json:"deploymentVersion"`
}

// The same declaration is stored once with a platform-owned execution binding.
// Public views project the contract without exposing this private binding.
type OperationDefinition struct {
	operationcontract.Resource
	Status  string           `json:"status"`
	Binding OperationBinding `json:"binding"`
	// Empty denotes the historical control declaration. This is private
	// publication metadata, not part of the public contract or authorization.
	DeclarationKind string `json:"declarationKind,omitempty"`
}

func (r *SourceStreamRepo) Operation(ctx context.Context, id string) (UnsMeta, OperationDefinition, error) {
	var row UnsMeta
	var definition OperationDefinition
	if !unsref.ValidMetadataID(id) {
		return row, definition, ErrInvalidArgument
	}
	if err := r.db.WithContext(ctx).Where("kind='control' AND id=? AND deleted_at=0", id).Take(&row).Error; err != nil {
		return row, definition, normalizeDBError(err)
	}
	if json.Unmarshal(row.Data, &definition) != nil || definition.Code != id || definition.Resource.ValidateStored() != nil ||
		(definition.DeclarationKind != "" && definition.DeclarationKind != "request") ||
		definition.Binding.Type != "stream" || definition.Binding.EntryID == "" || (definition.Status != "active" && definition.Status != "disabled") {
		return row, definition, ErrInvalidArgument
	}
	if _, err := programStableID(definition.Binding.StreamID); err != nil {
		return row, definition, err
	}
	if _, err := programStableID(definition.Binding.DeploymentVersion); err != nil {
		return row, definition, err
	}
	return row, definition, nil
}

func (r *SourceStreamRepo) Operations(ctx context.Context, search, status string, page, size int, excludeRequests ...bool) ([]UnsMeta, int64, error) {
	query := r.db.WithContext(ctx).Model(&UnsMeta{}).Where("kind='control' AND deleted_at=0")
	if len(excludeRequests) > 0 && excludeRequests[0] {
		query = query.Where("COALESCE(data->>'declarationKind','')<>'request'")
	}
	if search != "" {
		query = query.Where("id ILIKE ? OR data->>'name' ILIKE ?", "%"+search+"%", "%"+search+"%")
	}
	if status != "" {
		query = query.Where("data->>'status'=?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []UnsMeta{}
	err := query.Order("id").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}

func (r *SourceStreamRepo) StreamRequests(ctx context.Context, search string, page, size int, appIDs ...string) ([]UnsMeta, int64, error) {
	query := r.db.WithContext(ctx).Model(&UnsMeta{}).Where("kind='control' AND deleted_at=0 AND data->>'declarationKind'='request' AND data->>'status'='active'")
	if len(appIDs) > 0 {
		query = query.Where(`EXISTS (SELECT 1 FROM public.uns_meta grant_row WHERE grant_row.kind=? AND grant_row.deleted_at=0
		AND grant_row.data->>'type'='streamRequestGrant' AND grant_row.data->>'appId'=? AND grant_row.data->>'code'=uns_meta.id AND grant_row.data->>'active'='true'
		AND jsonb_exists(grant_row.data->'actions','stream.requests.read')
		AND grant_row.data->>'versionId'=uns_meta.data->'binding'->>'deploymentVersion'
		AND grant_row.data->>'streamId'=uns_meta.data->'binding'->>'streamId')`, RequestGrantKind, appIDs[0])
	}
	if search != "" {
		query = query.Where("id ILIKE ? OR data->>'name' ILIKE ?", "%"+search+"%", "%"+search+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []UnsMeta{}
	err := query.Order("id").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}

// Called only inside activation's existing exclusive deployment transaction.
// An error rolls back both the Stream activation and every resource update.
func replaceProgramOperations(tx *gorm.DB, stream Stream, manifest streamexec.ProgramManifest) error {
	if err := streamexec.ValidateOperationResources(manifest.Entries); err != nil {
		return ErrInvalidArgument
	}
	owner := strconv.FormatInt(stream.ID, 10)
	if err := disableProgramOperations(tx, stream.ID); err != nil {
		return err
	}
	for _, entry := range manifest.Entries {
		resource := entry.Operation()
		if resource == nil {
			continue
		}
		definition := OperationDefinition{Resource: *resource, Status: "active", Binding: OperationBinding{
			Type: "stream", StreamID: owner, EntryID: entry.ID, DeploymentVersion: strconv.FormatInt(stream.ActiveVersion, 10)}}
		if entry.Request != nil {
			definition.DeclarationKind = "request"
		}
		encoded, err := json.Marshal(definition)
		if err != nil {
			return err
		}
		var old UnsMeta
		err = tx.Where("kind='control' AND id=?", resource.Code).Take(&old).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			now := time.Now().UnixMilli()
			if err = tx.Create(&UnsMeta{ID: resource.Code, Kind: "control", Data: encoded, Revision: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		var prior OperationDefinition
		if old.DeletedAt != 0 || json.Unmarshal(old.Data, &prior) != nil || prior.Binding.StreamID != owner {
			return ErrConflict
		}
		// Cloud request codes stay owned by their Stream while each release
		// freezes its own contract. Keep legacy control-only immutability.
		if entry.Request == nil && prior.DeclarationKind != "request" && !sameOperationContract(prior.Resource, definition.Resource) {
			return ErrConflict
		}
		q := tx.Model(&UnsMeta{}).Where("kind='control' AND id=? AND revision=?", old.ID, old.Revision).
			Updates(map[string]any{"data": encoded, "revision": old.Revision + 1, "updated_at": time.Now().UnixMilli()})
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return ErrConflict
		}
	}
	return nil
}

func sameOperationContract(left, right operationcontract.Resource) bool {
	left.Name, left.Description, right.Name, right.Description = "", "", "", ""
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return sameProgramDocument(string(a), string(b))
}

func disableProgramOperations(tx *gorm.DB, streamID int64) error {
	return tx.Exec(`UPDATE public.uns_meta SET data=jsonb_set(data,'{status}','"disabled"'::jsonb),
 revision=revision+1,updated_at=? WHERE kind='control' AND deleted_at=0
 AND data->'binding'->>'streamId'=? AND data->>'status'='active'`, time.Now().UnixMilli(), strconv.FormatInt(streamID, 10)).Error
}

// A reservation is not a second task body. References and a digest freeze the
// first choice across deployments; the existing run/task holds the result.
type OperationInvocation struct {
	Identity          string `gorm:"column:identity;primaryKey"`
	MessageID         string `gorm:"column:message_id"`
	ResourceID        string `gorm:"column:control_code"`
	RequestHash       string `gorm:"column:request_hash"`
	StreamID          int64  `gorm:"column:stream_id"`
	VersionID         int64  `gorm:"column:version_id"`
	EntryID           string `gorm:"column:entry_id"`
	ContractVersion   string `gorm:"column:contract_version"`
	CreatedAt         int64  `gorm:"column:created_at"`
	CapabilityVersion string `gorm:"column:capability_version"`
	Contract          string `gorm:"column:contract"`
	ExpiresAt         int64  `gorm:"column:expires_at"`
	Principal         string `gorm:"column:principal"`
}

func (OperationInvocation) TableName() string { return "public.uns_control_invocation" }

func (r *SourceStreamRepo) ClaimOperationInvocation(ctx context.Context, resourceID, key, requestHash string, userID, apiKeyID int64) (OperationInvocation, bool, error) {
	return r.ClaimOperationInvocationWithOptions(ctx, resourceID, key, requestHash, userID, apiKeyID, OperationInvocationOptions{})
}

// CapabilityVersion pins both the public contract and its private deployed
// implementation without returning execution bindings to remote callers.
func (d OperationDefinition) CapabilityVersion() string {
	raw, _ := json.Marshal(struct {
		Contract operationcontract.Resource
		Binding  OperationBinding
	}{d.Resource, d.Binding})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

type OperationInvocationOptions struct {
	Principal         string
	CapabilityVersion string
	ExpiresAt         int64
	Authorize         func(*gorm.DB, OperationDefinition) error
}

func operationInvocationIdentity(resourceID, key string, userID, apiKeyID int64, principal string) (string, error) {
	if !unsref.ValidMetadataID(resourceID) || key == "" || len(key) > 128 || apiKeyID < 0 {
		return "", ErrInvalidArgument
	}
	if principal != "" {
		if userID != 0 || apiKeyID != 0 || len(principal) > 256 {
			return "", ErrInvalidArgument
		}
		return programIdentity("control-task-invocation-v1", principal, resourceID, key), nil
	}
	if userID <= 0 {
		return "", ErrInvalidArgument
	}
	return programIdentity("control-invocation-v1", userID, apiKeyID, resourceID, key), nil
}

func (r *SourceStreamRepo) OperationInvocation(ctx context.Context, resourceID, key string, userID, apiKeyID int64, principal string) (OperationInvocation, error) {
	var row OperationInvocation
	identity, err := operationInvocationIdentity(resourceID, key, userID, apiKeyID, principal)
	if err != nil {
		return row, err
	}
	err = r.db.WithContext(ctx).Where("identity=? AND control_code=?", identity, resourceID).Take(&row).Error
	return row, normalizeDBError(err)
}

func (r *SourceStreamRepo) ClaimOperationInvocationWithOptions(ctx context.Context, resourceID, key, requestHash string, userID, apiKeyID int64, options OperationInvocationOptions) (OperationInvocation, bool, error) {
	var invocation OperationInvocation
	created := false
	identity, err := operationInvocationIdentity(resourceID, key, userID, apiKeyID, options.Principal)
	if err != nil || len(requestHash) != 64 || options.ExpiresAt < 0 || options.CapabilityVersion != "" && len(options.CapabilityVersion) != 64 {
		return invocation, false, ErrInvalidArgument
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Same order as activation/disable: deployment lock before metadata.
		if err := tx.Exec("SELECT pg_advisory_xact_lock_shared(?)", int64(846031001)).Error; err != nil {
			return err
		}
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", identity).Error; err != nil {
			return err
		}
		err := tx.Where("identity=?", identity).Take(&invocation).Error
		if err == nil {
			if invocation.ResourceID != resourceID || invocation.RequestHash != requestHash ||
				options.CapabilityVersion != "" && options.CapabilityVersion != invocation.CapabilityVersion || options.ExpiresAt != invocation.ExpiresAt {
				return ErrControlIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		_, definition, err := NewSourceStreamRepo(tx).Operation(ctx, resourceID)
		if err != nil {
			return err
		}
		if definition.Status != "active" {
			return ErrControlInactive
		}
		if options.CapabilityVersion != "" && options.CapabilityVersion != definition.CapabilityVersion() {
			return ErrControlCapability
		}
		if options.ExpiresAt != 0 && options.ExpiresAt <= time.Now().UnixMilli() {
			return ErrControlExpired
		}
		if options.Authorize != nil {
			if err := options.Authorize(tx, definition); err != nil {
				return err
			}
		}
		streamID, _ := programStableID(definition.Binding.StreamID)
		versionID, _ := programStableID(definition.Binding.DeploymentVersion)
		stream, err := NewSourceStreamRepo(tx).Stream(ctx, streamID)
		if err != nil {
			return err
		}
		if stream.Status != "enabled" || stream.Model != "program" || stream.ActiveVersion != versionID {
			return ErrControlInactive
		}
		version, err := NewSourceStreamRepo(tx).Version(ctx, streamID, versionID)
		if err != nil {
			return err
		}
		manifest, err := version.ProgramV2Manifest()
		if err != nil {
			return err
		}
		bound := false
		for _, entry := range manifest.Entries {
			if entry.ID == definition.Binding.EntryID && entry.Kind == "request" && entry.Operation() != nil && sameOperationContract(*entry.Operation(), definition.Resource) {
				bound = true
				break
			}
		}
		if !bound {
			return ErrControlInactive
		}
		hash := sha256.Sum256([]byte(fmt.Sprintf("program-sync:%d:%d:%s", userID, apiKeyID, identity)))
		invocation = OperationInvocation{Identity: identity, MessageID: hex.EncodeToString(hash[:]), ResourceID: resourceID, RequestHash: requestHash,
			StreamID: streamID, VersionID: versionID, EntryID: definition.Binding.EntryID, ContractVersion: definition.ContractVersion, CreatedAt: time.Now().UnixMilli()}
		contract, _ := json.Marshal(definition.Resource)
		invocation.Contract, invocation.CapabilityVersion, invocation.ExpiresAt, invocation.Principal = string(contract), definition.CapabilityVersion(), options.ExpiresAt, options.Principal
		if err := tx.Create(&invocation).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	return invocation, created, normalizeDBError(err)
}
