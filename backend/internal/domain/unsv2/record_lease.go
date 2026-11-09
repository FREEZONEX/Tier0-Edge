package unsv2

import (
	contextx "backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	repo "backend/internal/repo"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

const recordLeaseMaxIDs = 100
const recordLeaseDefaultTTL = 30000
const recordLeaseMaxTTL = 300000

type RecordLockAssertion struct {
	CollectionID string `json:"collectionId"`
	ID           string `json:"id"`
	Token        string `json:"token"`
}

func checkRecordLockAssertions(ctx context.Context, tx *gorm.DB, assertions []RecordLockAssertion) error {
	for _, a := range assertions {
		if !unsmeta.ValidID(a.CollectionID) || !unsmeta.ValidID(a.ID) || a.Token == "" || len(a.Token) > 256 {
			return invalid("assertLocks", "valid collectionId, id and token required")
		}
		if err := authorizeCollection(ctx, "lock", a.CollectionID); err != nil {
			return err
		}
		var row repo.UnsRecord
		if err := tx.Where("collection_id=? AND id=?", a.CollectionID, a.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error; err != nil {
			return err
		}
		if err := unsmeta.CheckRecordLease(unsmeta.WithRecordLease(ctx, a.Token, false), row); err != nil {
			return err
		}
	}
	return nil
}

type recordLeaseInput struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}
type recordLeaseRequest struct {
	CollectionID string             `json:"collectionId"`
	IDs          []string           `json:"ids,omitempty"`
	Locks        []recordLeaseInput `json:"locks,omitempty"`
	Filter       json.RawMessage    `json:"filter,omitempty"`
	Sort         []RecordOrder      `json:"sort,omitempty"`
	Limit        int                `json:"limit,omitempty"`
	TTL          int64              `json:"ttlMs,omitempty"`
	Label        string             `json:"label,omitempty"`
	Force        bool               `json:"force,omitempty"`
}

// 每个集合的授权同时检查平台权限、密钥限制和当前 App 授权。
func authorizeCollection(ctx context.Context, op, id string) error {
	if !unsmeta.ValidID(id) {
		return invalid("collectionId", "valid Collection ID required")
	}
	if op == "read" {
		return recordReadPermission(ctx, id)
	}
	if op == "force-unlock" {
		s, ok := contextx.SubjectFrom(ctx)
		if !ok || s.AuthType != "session" || s.APIKeyID != 0 || s.MCPRestricted || !contextx.HasResource(s, "uns.app.manage") {
			return problem("FORBIDDEN", "force unlock requires a workspace administrator session")
		}
		admin, err := repo.NewIAMRepo(ctx).IsAdmin(ctx, s.UserID)
		if err != nil {
			return err
		}
		if !admin {
			return problem("FORBIDDEN", "force unlock requires an administrator session")
		}
	}
	action := "record.replace"
	if op == "delete" {
		action = "record.delete"
	}
	return mutationPermissions(ctx, Change{Action: action, CollectionID: Identity{ID: id}})
}
func validateRecordIDs(ids []string) error {
	if len(ids) < 1 || len(ids) > recordLeaseMaxIDs {
		return invalid("ids", "1 to 100 distinct record IDs required")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !unsmeta.ValidID(id) || seen[id] {
			return invalid("ids", "valid distinct record IDs required")
		}
		seen[id] = true
	}
	return nil
}

func (s *Service) recordLeaseOperation(ctx context.Context, action string, raw []byte) (any, error) {
	var r recordLeaseRequest
	if err := Decode(raw, &r); err != nil {
		return nil, err
	}
	op := "lock"
	if r.Force {
		op = "force-unlock"
	}
	if err := authorizeCollection(ctx, op, r.CollectionID); err != nil {
		return nil, err
	}
	if err := repo.RequireLocalCollection(ctx, r.CollectionID); err != nil {
		return nil, classify(err)
	}
	if action == "lock" {
		if err := authorizeCollection(ctx, "read", r.CollectionID); err != nil {
			return nil, err
		}
	}
	if r.Force && action != "unlock" {
		return nil, invalid("force", "only unlock accepts force")
	}
	if len(r.Label) > 64 {
		return nil, invalid("label", "at most 64 characters")
	}
	if action != "unlock" {
		if r.TTL == 0 {
			r.TTL = recordLeaseDefaultTTL
		}
		if r.TTL < 1000 || r.TTL > recordLeaseMaxTTL {
			return nil, invalid("ttlMs", "ttlMs must be 1000 to 300000")
		}
	}
	tokens := map[string]string{}
	if action != "lock" {
		if len(r.Filter) > 0 || len(r.Sort) > 0 || r.Limit != 0 {
			return nil, invalid("filter", "renew/unlock require explicit lease identities")
		}
		if r.Force {
			if len(r.Locks) > 0 {
				return nil, invalid("locks", "force unlock accepts ids only")
			}
		} else {
			if len(r.IDs) > 0 {
				return nil, invalid("ids", "renew/unlock require locks with tokens")
			}
			for _, lock := range r.Locks {
				if lock.Token == "" || len(lock.Token) > 256 {
					return nil, invalid("locks", "each lock requires its token")
				}
				r.IDs = append(r.IDs, lock.ID)
				tokens[lock.ID] = lock.Token
			}
		}
		if err := validateRecordIDs(r.IDs); err != nil {
			return nil, err
		}
	} else {
		if len(r.Locks) > 0 {
			return nil, invalid("locks", "lock accepts ids or a filter")
		}
		if len(r.IDs) > 0 {
			if len(r.Filter) > 0 || len(r.Sort) > 0 || r.Limit != 0 {
				return nil, invalid("ids", "ids and filter selection are mutually exclusive")
			}
			if err := validateRecordIDs(r.IDs); err != nil {
				return nil, err
			}
		} else {
			if len(r.Filter) == 0 {
				return nil, invalid("filter", "ids or filter required")
			}
			if r.Limit == 0 {
				r.Limit = 1
			}
			if r.Limit < 1 || r.Limit > recordLeaseMaxIDs {
				return nil, invalid("limit", "limit must be 1 to 100")
			}
			if err := authorizeCollection(ctx, "read", r.CollectionID); err != nil {
				return nil, err
			}
		}
	}
	result := map[string]any{"locks": []map[string]any{}}
	err := repo.NewUnsMetaRepo(ctx).Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL statement_timeout='15000ms'").Error; err != nil {
			return err
		}
		if err := tx.Exec("SELECT pg_advisory_xact_lock_shared(814206108)").Error; err != nil {
			return err
		}
		qctx := repo.WithUnsTransaction(ctx, tx)
		if err := authorizeCollection(qctx, op, r.CollectionID); err != nil {
			return err
		}
		if _, err := definition(qctx, r.CollectionID); err != nil {
			return err
		}
		ids := append([]string{}, r.IDs...)
		if len(ids) == 0 {
			predicate, args, err := CompileDataFilter(qctx, "records", r.CollectionID, "base:r", r.Filter)
			if err != nil {
				return err
			}
			order, err := CompileDataSort(qctx, "records", r.CollectionID, "base:r", r.Sort)
			if err != nil {
				return err
			}
			query := tx.Table("public.uns_record AS r").Where("r.collection_id=? AND r.deleted_at IS NULL AND r.source_id IS NULL AND (r.lock_expires_at IS NULL OR r.lock_expires_at<=clock_timestamp())", r.CollectionID).Where(predicate, args...)
			if order != "" {
				query = query.Order(order)
			}
			if err := query.Order("r.id").Limit(recordLeaseMaxIDs).Pluck("r.id", &ids).Error; err != nil {
				return err
			}
		}
		identities := make([]repo.UnsRecordLock, 0, len(ids))
		for _, id := range ids {
			identities = append(identities, repo.UnsRecordLock{CollectionID: r.CollectionID, RecordID: id})
		}
		if err := repo.LockUnsRecords(tx, identities...); err != nil {
			return err
		}
		locks := []map[string]any{}
		changed := 0
		for _, id := range ids {
			if action == "lock" && len(r.Filter) > 0 && changed >= r.Limit {
				break
			}
			var row repo.UnsRecord
			if err := tx.Where("collection_id=? AND id=?", r.CollectionID, id).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error; err != nil {
				return err
			}
			if row.SourceID != nil {
				return problem("REFERENCED", "Source-managed records cannot be leased")
			}
			now := time.Now().UTC()
			expiry := now.Add(time.Duration(r.TTL) * time.Millisecond)
			updates := map[string]any{}
			token := tokens[id]
			if action == "lock" {
				if row.LockExpiresAt != nil && row.LockExpiresAt.After(now) {
					if len(r.Filter) > 0 {
						continue
					}
					return problem("LOCK_CONFLICT", "record already has an active lease")
				}
				if len(r.Filter) > 0 {
					predicate, args, err := CompileDataFilter(qctx, "records", r.CollectionID, "base:r", r.Filter)
					if err != nil {
						return err
					}
					var count int64
					if err := tx.Table("public.uns_record AS r").Where("r.collection_id=? AND r.id=? AND r.deleted_at IS NULL", r.CollectionID, id).Where(predicate, args...).Count(&count).Error; err != nil {
						return err
					}
					if count == 0 {
						continue
					}
				}
				secret := make([]byte, 32)
				if _, err := rand.Read(secret); err != nil {
					return err
				}
				token = hex.EncodeToString(secret)
				hash := sha256.Sum256([]byte(token))
				updates = map[string]any{"lock_token_hash": hash[:], "lock_principal": unsmeta.RecordLeasePrincipal(ctx), "lock_label": r.Label, "lock_expires_at": expiry, "lock_since": now, "lock_fence": row.LockFence + 1}
				row.LockFence++
			} else {
				if !r.Force {
					if err := unsmeta.CheckRecordLease(unsmeta.WithRecordLease(ctx, token, false), row); err != nil {
						return err
					}
				}
				if action == "renew" {
					updates["lock_expires_at"] = expiry
				} else {
					updates = map[string]any{"lock_token_hash": nil, "lock_principal": nil, "lock_label": nil, "lock_expires_at": nil, "lock_since": nil}
				}
			}
			if err := tx.Model(&row).UpdateColumns(updates).Error; err != nil {
				return err
			}
			changed++
			if action == "lock" {
				locks = append(locks, map[string]any{"id": id, "token": token, "expiresAt": expiry.UnixMilli(), "fence": row.LockFence, "record": recordDocument(row)})
			} else if action == "renew" {
				locks = append(locks, map[string]any{"id": id, "expiresAt": expiry.UnixMilli()})
			}
		}
		result["locks"] = locks
		if action == "unlock" {
			delete(result, "locks")
			result["unlockedCount"] = changed
		}
		return nil
	})
	return result, classify(err)
}

func (s *Service) recordRestore(ctx context.Context, r novaRecordRequest) (any, error) {
	if err := authorizeCollection(ctx, "write", r.CollectionID); err != nil {
		return nil, err
	}
	if err := validateRecordIDs(r.IDs); err != nil {
		return nil, err
	}
	batch := Batch{Mode: "sync", IdempotencyKey: r.IdempotencyKey, DryRun: r.DryRun}
	for i, id := range r.IDs {
		batch.Items = append(batch.Items, Change{OpID: fmt.Sprintf("restore-%d", i), Action: "record.restore", CollectionID: Identity{ID: r.CollectionID}, ID: Identity{ID: id}})
	}
	out, err := s.Apply(ctx, batch)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, row := range out.Results {
		if row.Status == "restored" {
			ids = append(ids, row.ID)
		}
	}
	return map[string]any{"restoredCount": len(ids), "restoredIds": ids, "status": out.Status, "replayed": out.Replayed}, nil
}
