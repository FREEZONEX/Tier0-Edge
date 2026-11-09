package cluster

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FleetStorageDefinition struct {
	CollectionID string           `json:"collectionId"`
	Revision     int64            `json:"revision"`
	Document     unsmeta.Document `json:"document"`
}

func storageMetadataID(node, source string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("fleet-storage-definition/"+node+"/"+source)).String()
}

// Registered installations may synchronize only their own namespaced metadata.
// The receiver derives target identity; no arbitrary center ID is accepted.
func (s *Service) AcceptStorageDefinition(ctx context.Context, claims FleetServiceClaims, req FleetStorageDefinition) (any, error) {
	if claims.Scope != "storage.definition" || claims.ResourceID != "node" || !unsmeta.ValidID(req.CollectionID) || req.Revision < 1 {
		return nil, ErrUnauthorized
	}
	node, err := s.fleetRepo.CollaborationNode(ctx, claims.Subject)
	if err != nil || node.RegistrationStatus != "registered" {
		return nil, ErrUnauthorized
	}
	doc := req.Document
	if err := doc.Validate("collection"); err != nil {
		return nil, err
	}
	if doc.CollectionID != "" || len(doc.Fields) > 0 {
		return nil, ErrInvalid
	}
	sum := sha256.Sum256(storageJSON(doc))
	hash := hex.EncodeToString(sum[:])
	target := storageMetadataID(claims.Subject, req.CollectionID)
	var result repo.StorageInboundCollection
	err = repo.MetadataEventTransaction(ctx, repo.GetUnsConn(ctx), func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(814206108)").Error; err != nil {
			return err
		}
		tctx := repo.WithUnsTransaction(ctx, tx)
		service := unsmeta.New()

		var old repo.StorageInboundCollection
		e := tx.Where("node_id=? AND collection_id=?", claims.Subject, req.CollectionID).Take(&old).Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		current, currentErr := repo.NewUnsMetaRepo(tx).Get(tctx, target, true, "collection")
		if currentErr != nil && !errors.Is(currentErr, repo.ErrNotFound) {
			return currentErr
		}
		if old.TargetCollectionID != "" {
			if req.Revision < old.SourceRevision || (req.Revision == old.SourceRevision && old.SourceHash != hash) {
				return repo.ErrConflict
			}
			// Independent center edits require explicit reconciliation. If an
			// operator restored the exact source definition, accepting its newer
			// revision is safe and avoids an unresolvable version-only conflict.
			if currentErr != nil || current.DeletedAt != 0 || current.MountID != nil || current.OriginNodeID != claims.Subject {
				return repo.ErrConflict
			}
			var currentDoc unsmeta.Document
			if err := json.Unmarshal(current.Data, &currentDoc); err != nil {
				return err
			}
			equal := bytes.Equal(storageJSON(currentDoc), storageJSON(doc))
			if current.Revision != old.TargetRevision && !equal {
				return repo.ErrConflict
			}
			if req.Revision == old.SourceRevision && !equal {
				return repo.ErrConflict
			}
			if equal {
				result = old
				result.SourceRevision = req.Revision
				result.SourceHash = hash
				result.TargetRevision = current.Revision
				return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&result).Error
			}
			// Preserve both retained rows and already queued old-schema writes.
			if !unsmeta.StorageSchemaCompatible(currentDoc.RecordSchema, doc.RecordSchema) || !unsmeta.StorageSchemaCompatible(currentDoc.SampleSchema, doc.SampleSchema) {
				return unsmeta.ErrSchemaInUse
			}

		} else if currentErr == nil {
			return repo.ErrConflict
		}
		saved, err := service.SyncCollectionDefinition(tctx, target, current.Revision, doc, claims.Subject)
		if err != nil {
			return err
		}
		result = repo.StorageInboundCollection{NodeID: claims.Subject, CollectionID: req.CollectionID, TargetCollectionID: target, SourceRevision: req.Revision, TargetRevision: saved.Revision, SourceHash: hash}
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&result).Error
	})
	if err != nil {
		return nil, err
	}
	// This common-DB grant is retried after metadata commit; an incomplete grant
	// never yields a ready mapping to the sender. Explicit revocation is preserved.
	grant := fleetServiceGrant{GrantID: storageGrant(claims.Subject, target), NodeID: claims.Subject, Scope: "storage.target", ResourceID: target, Revision: 1, Enabled: true, CreatedAt: time.Now().UnixMilli()}
	if err := repo.GetCommonConn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&grant).Error; err != nil {
		return nil, err
	}
	if err := repo.GetCommonConn(ctx).Where("grant_id=? AND enabled=true", grant.GrantID).Take(&grant).Error; err != nil {
		return nil, ErrUnauthorized
	}
	return map[string]any{"collectionId": req.CollectionID, "targetCollectionId": target, "sourceRevision": result.SourceRevision, "targetRevision": result.TargetRevision}, nil
}

func (s *Service) syncStorageDefinitions(ctx context.Context, force bool) error {
	c, err := s.storageConnection()
	if err != nil {
		return err
	}
	var rows []repo.UnsMeta
	query := repo.StorageSyncCollections(ctx).Order("COALESCE((SELECT m.last_attempt_at FROM uns_storage_collection_mapping m WHERE m.collection_id=uns_meta.id),0),id")
	// Refresh ready definitions periodically as well: detect target deletion,
	// independent changes and revocation without relying on a success cache forever.
	if !force {
		query = query.Where("NOT EXISTS (SELECT 1 FROM uns_storage_collection_mapping m WHERE m.collection_id=uns_meta.id AND m.center_id=? AND m.source_revision=uns_meta.revision AND m.last_sync_at>? AND m.last_error='')", c.CenterID, time.Now().Add(-30*time.Second).UnixMilli()).Limit(25)
	}
	if err := query.Find(&rows).Error; err != nil {
		return err
	}
	var firstError error
	for _, row := range rows {
		// Recheck selection after the paged scan, before starting a new transfer.
		var selected int64
		if err := repo.StorageSyncCollections(ctx).Where("id=?", row.ID).Count(&selected).Error; err != nil {
			return err
		}
		if selected == 0 {
			continue
		}
		var doc unsmeta.Document
		if err := json.Unmarshal(row.Data, &doc); err != nil {
			return err
		}
		raw, e := s.callStorageCenter(ctx, "storage.definition", "node", FleetStorageDefinition{CollectionID: row.ID, Revision: row.Revision, Document: doc})
		var m repo.StorageCollectionMapping
		if e == nil {
			e = json.Unmarshal(raw, &m)
		}
		if e == nil && (m.CollectionID != row.ID || m.SourceRevision != row.Revision || m.TargetCollectionID != storageMetadataID(c.NodeID, row.ID) || m.TargetRevision < 1) {
			e = ErrInvalid
		}
		if e != nil {
			// Retain the last successful mapping for durable offline writes of the same
			// definition. A new definition has no usable version until synchronization.
			pending := repo.StorageCollectionMapping{CollectionID: row.ID, CenterID: c.CenterID, LastError: "DEFINITION_SYNC_FAILED", LastAttemptAt: time.Now().UnixMilli()}
			_ = repo.GetUnsConn(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "collection_id"}}, DoUpdates: clause.Assignments(map[string]any{"last_error": pending.LastError, "last_attempt_at": pending.LastAttemptAt})}).Create(&pending).Error
			if force {
				return e
			}
			if firstError == nil {
				firstError = e
			}
			continue
		}
		m.CenterID = c.CenterID
		m.LastSyncAt = time.Now().UnixMilli()
		m.LastAttemptAt = m.LastSyncAt
		m.LastError = ""
		if err := repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(814206108)").Error; err != nil {
				return err
			}
			active, e := s.storageConnection()
			if e != nil || active.CenterID != c.CenterID || active.NodeID != c.NodeID {
				return repo.ErrConflict
			}
			var existing repo.StorageCollectionMapping
			e = tx.Where("collection_id=?", row.ID).Take(&existing).Error
			if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			if existing.CenterID == c.CenterID && existing.SourceRevision > m.SourceRevision {
				return nil
			}
			return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&m).Error
		}); err != nil {
			return err
		}
	}
	return firstError
}

func (s *Service) ReconcileStorageDefinitions(ctx context.Context) {
	if _, err := s.storageConnection(); err != nil {
		return
	}
	if err := s.syncStorageDefinitions(ctx, false); err != nil {
		return
	}
	p, err := repo.GlobalStorage(ctx)
	if err == nil && p.Revision == 0 {
		// First connection only. A saved local policy or any later reconnect is never reset.
		_, _ = s.FleetStorage(ctx, "save", FleetStorageRequest{ExpectedRevision: 0, Mode: "both"})
	}
}
