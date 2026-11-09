package cluster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backend/internal/domain/license"
	"backend/internal/domain/unsmeta"
	"backend/internal/domain/unsv2"
	"backend/internal/infra/outbox"
	"backend/internal/repo"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FleetStorageRequest struct {
	NodeID           string `json:"nodeId,omitempty"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Mode             string `json:"mode,omitempty"`
}
type storageDeliveryCounts struct {
	Received  int64 `json:"received"`
	Pending   int64 `json:"pending"`
	Committed int64 `json:"committed"`
	Failed    int64 `json:"failed"`
}
type storageHistoricalDelivery struct {
	Pending int64 `json:"pending"`
	Failed  int64 `json:"failed"`
	Centers int64 `json:"centers"`
}
type storageConnection struct {
	NetworkMode          string `json:"networkMode,omitempty"`
	NodeID               string `json:"nodeId"`
	CenterID             string `json:"centerId"`
	CenterURL            string `json:"centerUrl"`
	BusinessEndpoint     string `json:"businessEndpoint,omitempty"`
	CenterTLSFingerprint string `json:"centerTlsFingerprint,omitempty"`
}

func (c storageConnection) endpoint() string {
	if c.BusinessEndpoint != "" {
		return c.BusinessEndpoint
	}
	return c.CenterURL
}

func (s *Service) storageConnection() (storageConnection, error) {
	var c storageConnection
	// A former Edge connection must not remain usable after promotion to center.
	if license.Default().IsHost() {
		return c, ErrHostCannotConnect
	}
	raw, e := os.ReadFile(filepath.Join(s.cfg.IdentityDir, "connection.json"))
	if e == nil {
		e = json.Unmarshal(raw, &c)
	}
	if e != nil || c.CenterID == "" || c.NodeID == "" {
		return c, ErrUnauthorized
	}
	return c, nil
}
func storageGrant(node, target string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("collection-storage/"+node+"/"+target)).String()
}
func (s *Service) callStorageCenter(ctx context.Context, scope, resource string, payload any) (json.RawMessage, error) {
	c, err := s.storageConnection()
	if err != nil {
		return nil, err
	}
	return s.callStorageConnection(ctx, c, scope, resource, payload)
}
func (s *Service) callStorageConnection(ctx context.Context, c storageConnection, scope, resource string, payload any) (json.RawMessage, error) {
	ctx = context.WithValue(ctx, fleetTLSTargetKey{}, fleetTLSTarget{origin: c.endpoint(), pin: c.CenterTLSFingerprint})
	token, cache, err := s.fleetPeerToken(ctx, c.endpoint(), c.NodeID, scope, resource, "", 0)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]any{"resourceId": resource, "payload": payload})
	result, err := s.fleetHTTP(ctx, strings.TrimRight(c.endpoint(), "/")+"/api/node/v1/fleet/execution/"+strings.ReplaceAll(scope, ".", "/"), token, body)
	if err != nil {
		s.mu.Lock()
		delete(s.fleetTokens, cache)
		s.mu.Unlock()
	}
	return result, err
}

// FleetStorage always edits one installation-wide policy. Per-Collection targets
// are derived from synchronized definitions and are never supplied by a caller.
func (s *Service) FleetStorage(ctx context.Context, action string, req FleetStorageRequest) (any, error) {
	if action != "get" && action != "save" {
		return nil, ErrInvalid
	}
	if req.NodeID != "" {
		node := req.NodeID
		req.NodeID = ""
		return s.CallFleetNode(ctx, node, "storage."+action, "node", req)
	}
	c, connectionErr := s.storageConnection()
	if action == "save" {
		if req.ExpectedRevision < 0 || (req.Mode != "local" && req.Mode != "center" && req.Mode != "both") {
			return nil, ErrInvalid
		}
		prior, err := repo.GlobalStorage(ctx)
		if err != nil {
			return nil, err
		}
		if prior.Revision != req.ExpectedRevision {
			return nil, &FleetRevisionConflict{CurrentRevision: prior.Revision}
		}
		if req.Mode != "local" {
			if connectionErr != nil {
				return nil, connectionErr
			}
			if err := s.syncStorageDefinitions(ctx, true); err != nil {
				return nil, err
			}
		}
		err = repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(814206108)").Error; err != nil {
				return err
			}
			active, connectionErr := s.storageConnection()
			if req.Mode != "local" && (connectionErr != nil || active.CenterID != c.CenterID || active.NodeID != c.NodeID) {
				return repo.ErrConflict
			}
			p, err := repo.GlobalStorage(tx)
			if err != nil {
				return err
			}
			if p.Revision != req.ExpectedRevision {
				return &FleetRevisionConflict{CurrentRevision: p.Revision}
			}
			if req.Mode != "local" {
				var pending int64
				if err := repo.StorageSyncCollections(tx).Where("NOT EXISTS (SELECT 1 FROM uns_storage_collection_mapping m WHERE m.collection_id=uns_meta.id AND m.center_id=? AND m.source_revision=uns_meta.revision AND m.target_collection_id<>'')", c.CenterID).Count(&pending).Error; err != nil {
					return err
				}
				if pending > 0 {
					return repo.ErrStorageDefinitionPending
				}
				// Legacy manually granted receive targets must stay local too.
				var targets []string
				if err := repo.GetCommonConn(ctx).Model(&fleetServiceGrant{}).Where("scope='storage.target' AND enabled=true").Pluck("resource_id", &targets).Error; err != nil {
					return err
				}
				if len(targets) > 0 {
					if err := repo.StorageSyncCollections(tx).Where("id IN ?", targets).Count(&pending).Error; err != nil {
						return err
					}
					if pending > 0 {
						return repo.ErrStorageOperation
					}
				}
			}
			p = repo.GlobalStoragePolicy{ID: "edge", Mode: req.Mode, Revision: p.Revision + 1, CenterID: c.CenterID, UpdatedAt: time.Now().UnixMilli()}
			return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&p).Error
		})
		if err != nil {
			return nil, err
		}
	}
	p, err := repo.GlobalStorage(ctx)
	if err != nil {
		return nil, err
	}
	// Delivery belongs to the immutable destination captured at enqueue time.
	// Use only the saved, authenticated connection identity, never a candidate
	// center from a pending registration or the current storage policy.
	centerID := ""
	if connectionErr == nil {
		centerID = c.CenterID
	}
	var counts storageDeliveryCounts
	err = repo.GetUnsConn(ctx).Table("uns_collection_storage_outbox").
		Where("? <> '' AND payload->'policy'->>'centerId'=?", centerID, centerID).
		Select("count(*) AS received,count(*) FILTER(WHERE status='pending') AS pending,count(*) FILTER(WHERE status='done') AS committed,count(*) FILTER(WHERE status='pending' AND last_error NOT IN ('','WAITING_FOR_PRIOR_DELIVERY')) AS failed").Scan(&counts).Error
	if err != nil {
		return nil, err
	}
	var historical storageHistoricalDelivery
	err = repo.GetUnsConn(ctx).Table("uns_collection_storage_outbox").
		Where("status='pending' AND (? = '' OR COALESCE(payload->'policy'->>'centerId','') <> ?)", centerID, centerID).
		Select("count(*) AS pending,count(*) FILTER(WHERE last_error NOT IN ('','WAITING_FOR_PRIOR_DELIVERY')) AS failed,count(DISTINCT COALESCE(payload->'policy'->>'centerId','')) AS centers").Scan(&historical).Error
	if err != nil {
		return nil, err
	}
	var total, ready, failed int64
	if err := repo.StorageSyncCollections(ctx).Count(&total).Error; err != nil {
		return nil, err
	}
	if err := repo.StorageSyncCollections(ctx).Where("EXISTS (SELECT 1 FROM uns_storage_collection_mapping m WHERE m.collection_id=uns_meta.id AND m.center_id=? AND m.source_revision=uns_meta.revision AND m.target_collection_id<>'')", centerID).Count(&ready).Error; err != nil {
		return nil, err
	}
	if err := repo.StorageSyncCollections(ctx).Where("EXISTS (SELECT 1 FROM uns_storage_collection_mapping m WHERE m.collection_id=uns_meta.id AND m.center_id=? AND m.last_error<>'')", centerID).Count(&failed).Error; err != nil {
		return nil, err
	}
	return map[string]any{"policy": p, "effectiveRevision": p.Revision, "state": "effective", "connected": connectionErr == nil, "defaultPending": connectionErr == nil && p.Revision == 0, "definitions": map[string]any{"total": total, "synced": ready, "pending": total - ready, "failed": failed}, "deliveryCenterId": centerID, "delivery": counts, "historicalDelivery": historical}, nil
}
func storageJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func (s *Service) ReadCollectionStorage(ctx context.Context, p repo.CollectionStoragePolicy, op string, body json.RawMessage) (json.RawMessage, error) {
	c, err := s.storageConnection()
	if err != nil || c.CenterID != p.CenterID {
		return nil, repo.ErrStorageUnavailable
	}
	raw, err := s.callStorageCenter(ctx, "storage.target", p.TargetCollectionID, map[string]any{"action": "read", "operation": op, "body": body})
	if err != nil {
		if errors.Is(err, repo.ErrStorageOperation) {
			return nil, err
		}
		return nil, repo.ErrStorageUnavailable
	}
	return raw, nil
}
func (s *Service) StartCollectionStorage() {
	worker := outbox.NewWorker(context.Background(), outbox.WithStore(repo.NewStorageOutboxRepo(context.Background())), outbox.WithBatchSize(16))
	worker.RegisterBatch(repo.CollectionStorageEvent, s.DeliverCollectionStorageBatch)
	worker.Start(context.Background())
}
func (s *Service) DeliverCollectionStorage(ctx context.Context, event repo.OutboxEvent) error {
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ctx = bounded
	var d repo.CollectionStorageDelivery
	if json.Unmarshal(event.Payload, &d) != nil || d.EventID != event.EventID {
		return ErrInvalid
	}
	// 后续事件不越过同一 Collection 已排队的旧事件。
	var earlier int64
	if err := repo.GetUnsConn(ctx).Table("uns_collection_storage_outbox").Where("(aggregate_id=? OR aggregate_id=?) AND id<? AND status='pending' AND payload->'policy'->>'centerId'=? AND payload->'policy'->>'targetCollectionId'=?", event.AggregateID, d.Policy.CollectionID, event.ID, d.Policy.CenterID, d.Policy.TargetCollectionID).Count(&earlier).Error; err != nil {
		return err
	}
	if earlier > 0 {
		return errors.New("WAITING_FOR_PRIOR_DELIVERY")
	}
	c, err := s.deliveryConnection(d.Policy.CenterID, d.Policy.TargetCollectionID, d.Policy.CollectionID)
	if err != nil {
		return errors.New("CENTER_IDENTITY_UNAVAILABLE")
	}
	raw, err := s.callStorageConnection(ctx, c, "storage.target", d.Policy.TargetCollectionID, map[string]any{"action": "commit", "delivery": d})
	if err != nil {
		return errors.New("CENTER_UNAVAILABLE_OR_REJECTED")
	}
	var receipt struct {
		EventID   string `json:"eventId"`
		Committed bool   `json:"committed"`
	}
	if json.Unmarshal(raw, &receipt) != nil || !receipt.Committed || receipt.EventID != d.EventID {
		return errors.New("CENTER_COMMIT_NOT_CONFIRMED")
	}
	return nil
}

type FleetStorageTargetRequest struct {
	Deliveries   []repo.CollectionStorageDelivery `json:"deliveries,omitempty"`
	Action       string                           `json:"action"`
	RecordSchema *unsmeta.Schema                  `json:"recordSchema,omitempty"`
	SampleSchema *unsmeta.Schema                  `json:"sampleSchema,omitempty"`
	Operation    string                           `json:"operation,omitempty"`
	Body         json.RawMessage                  `json:"body,omitempty"`
	Delivery     repo.CollectionStorageDelivery   `json:"delivery,omitempty"`
}

func (s *Service) AcceptCollectionStorage(ctx context.Context, claims FleetServiceClaims, req FleetStorageTargetRequest, uns *unsv2.Service) (any, error) {
	if claims.Scope != "storage.target" {
		return nil, ErrUnauthorized
	}
	target := claims.ResourceID
	if err := repo.RequireLocalCollection(ctx, target); err != nil {
		return nil, err
	}
	row, err := repo.NewUnsMetaRepo(ctx).Get(ctx, target, false, "collection")
	if err != nil || row.MountID != nil {
		return nil, ErrInvalid
	}
	var doc unsmeta.Document
	_ = json.Unmarshal(row.Data, &doc)
	if req.Action == "validate" {
		if !unsmeta.StorageSchemaCompatible(req.RecordSchema, doc.RecordSchema) || !unsmeta.StorageSchemaCompatible(req.SampleSchema, doc.SampleSchema) {
			return nil, &unsv2.Fault{Code: "SCHEMA_MISMATCH", Message: "Target Collection fields are incompatible"}
		}
		return map[string]any{"compatible": true}, nil
	}
	if req.Action == "read" {
		var result any
		err := repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT pg_advisory_xact_lock_shared(814206108)").Error; err != nil {
				return err
			}
			var err error
			result, err = uns.CollectionStorageRead(repo.WithUnsTransaction(ctx, tx), target, req.Operation, req.Body)
			return err
		})
		return result, err
	}
	if req.Action == "commitBatch" {
		if len(req.Deliveries) == 0 || len(req.Deliveries) > 16 {
			return nil, ErrInvalid
		}
		receipts := []map[string]any{}
		blocked := false
		for _, delivery := range req.Deliveries {
			if blocked {
				receipts = append(receipts, map[string]any{"eventId": delivery.EventID, "committed": false})
				continue
			}
			_, err := s.AcceptCollectionStorage(ctx, claims, FleetStorageTargetRequest{Action: "commit", Delivery: delivery}, uns)
			receipts = append(receipts, map[string]any{"eventId": delivery.EventID, "committed": err == nil})
			blocked = err != nil
		}
		return map[string]any{"receipts": receipts}, nil
	}
	d := req.Delivery
	identity, err := s.fleetIdentity()
	if err != nil {
		return nil, err
	}
	if req.Action != "commit" || !unsmeta.ValidID(d.EventID) || !unsmeta.ValidID(d.Policy.CollectionID) || d.Policy.Revision < 1 || (d.Policy.Mode != "center" && d.Policy.Mode != "both") || d.Policy.TargetCollectionID != target || d.Policy.CenterID != identity.InstallationID || len(d.Value) > 256*1024 {
		return nil, ErrInvalid
	}
	var origin repo.StorageInboundCollection
	e := repo.GetUnsConn(ctx).Where("target_collection_id=?", target).Take(&origin).Error
	if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, e
	}
	if e == nil && (origin.NodeID != claims.Subject || origin.CollectionID != d.Policy.CollectionID) {
		return nil, ErrUnauthorized
	}
	var value unsmeta.IngestValue
	if unsv2.Decode(d.Value, &value) != nil {
		return nil, ErrInvalid
	}
	if !unsmeta.ValidID(value.RecordID) || (d.Kind == "sample" && value.Sample == nil) {
		return nil, ErrInvalid
	}
	raw := storageJSON(d)
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	key := claims.Subject + "/" + d.EventID
	err = repo.MetadataEventTransaction(ctx, repo.GetUnsConn(ctx), func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock_shared(814206108)").Error; err != nil {
			return err
		}
		if err := repo.RequireLocalCollection(tx, target); err != nil {
			return err
		}
		if err := repo.LockUnsRecords(tx, repo.UnsRecordLock{CollectionID: target, RecordID: value.RecordID}); err != nil {
			return err
		}
		var prior struct{ PayloadHash string }
		e := tx.Table("uns_collection_storage_receipt").Where("event_id=?", key).Take(&prior).Error
		if e == nil {
			if prior.PayloadHash != hash {
				return repo.ErrConflict
			}
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		tctx := repo.WithUnsTransaction(ctx, tx)
		service := unsmeta.New()
		if d.Kind == "create" || d.Kind == "upsert" {
			revision := int64(0)
			var old repo.UnsRecord
			e := tx.Where("collection_id=? AND id=?", target, value.RecordID).Take(&old).Error
			if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			if e == nil {
				if d.Kind == "create" {
					return repo.ErrConflict
				}
				revision = old.Revision
			}
			if _, e := service.SaveRecord(tctx, target, value.RecordID, revision, value.Record); e != nil {
				return e
			}
		} else if d.Kind != "sample" {
			return ErrInvalid
		}
		if value.Sample != nil {
			if _, e := service.AppendSamples(tctx, target, []unsmeta.SampleInput{{ID: d.EventID, RecordID: value.RecordID, Time: value.Time, Quality: value.Quality, Data: value.Sample}}); e != nil {
				return e
			}
		}
		return tx.Table("uns_collection_storage_receipt").Create(map[string]any{"event_id": key, "payload_hash": hash, "committed_at": time.Now().UnixMilli()}).Error
	})
	return map[string]any{"eventId": d.EventID, "committed": err == nil}, err
}

// Consecutive events for a Collection retain their original destination and order.
// Each receipt confirms a center transaction; a lost response is safely replayed.
func (s *Service) DeliverCollectionStorageBatch(ctx context.Context, events []repo.OutboxEvent) map[string]error {
	// Complete within the worker lease even when several destinations are down.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	results := map[string]error{}
	groups := map[string][]repo.OutboxEvent{}
	order := []string{}
	for _, e := range events {
		if _, ok := groups[e.AggregateID]; !ok {
			order = append(order, e.AggregateID)
		}
		groups[e.AggregateID] = append(groups[e.AggregateID], e)
	}
	for _, cid := range order {
		group := groups[cid]
		var first repo.CollectionStorageDelivery
		if json.Unmarshal(group[0].Payload, &first) != nil {
			for _, e := range group {
				results[e.EventID] = ErrInvalid
			}
			continue
		}
		conn, connectionErr := s.deliveryConnection(first.Policy.CenterID, first.Policy.TargetCollectionID, first.Policy.CollectionID)
		var earlier int64
		err := repo.GetUnsConn(ctx).Table("uns_collection_storage_outbox").Where("(aggregate_id=? OR aggregate_id=?) AND id<? AND status='pending' AND payload->'policy'->>'centerId'=? AND payload->'policy'->>'targetCollectionId'=?", cid, first.Policy.CollectionID, group[0].ID, first.Policy.CenterID, first.Policy.TargetCollectionID).Count(&earlier).Error
		if connectionErr != nil || err != nil || earlier > 0 {
			for _, e := range group {
				results[e.EventID] = errors.New("WAITING_FOR_PRIOR_DELIVERY_OR_CONNECTION")
			}
			continue
		}
		for start := 0; start < len(group); {
			batch := []repo.CollectionStorageDelivery{}
			bytes := 0
			end := start
			for end < len(group) {
				var d repo.CollectionStorageDelivery
				e := group[end]
				if json.Unmarshal(e.Payload, &d) != nil || d.EventID != e.EventID || conn.CenterID != d.Policy.CenterID {
					break
				}
				if len(batch) > 0 && (d.Policy.TargetCollectionID != batch[0].Policy.TargetCollectionID || bytes+len(e.Payload) > 768*1024) {
					break
				}
				batch = append(batch, d)
				bytes += len(e.Payload)
				end++
			}
			if len(batch) == 0 {
				for _, e := range group[start:] {
					results[e.EventID] = ErrInvalid
				}
				break
			}
			bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
			raw, err := s.callStorageConnection(bounded, conn, "storage.target", batch[0].Policy.TargetCollectionID, map[string]any{"action": "commitBatch", "deliveries": batch})
			cancel()
			var response struct {
				Receipts []struct {
					EventID   string `json:"eventId"`
					Committed bool   `json:"committed"`
				} `json:"receipts"`
			}
			confirmed := map[string]bool{}
			if err == nil && json.Unmarshal(raw, &response) == nil {
				for _, r := range response.Receipts {
					confirmed[r.EventID] = r.Committed
				}
			}
			failed := false
			for _, e := range group[start:end] {
				if confirmed[e.EventID] {
					results[e.EventID] = nil
				} else {
					results[e.EventID] = errors.New("CENTER_COMMIT_NOT_CONFIRMED")
					failed = true
				}
			}
			if failed {
				for _, e := range group[end:] {
					results[e.EventID] = errors.New("WAITING_FOR_PRIOR_DELIVERY")
				}
				break
			}
			start = end
		}
	}
	return results
}
