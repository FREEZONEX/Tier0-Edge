package appinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/sourcestream"
	"backend/internal/domain/unsmeta"
	"backend/internal/domain/unsv2"
	"backend/internal/repo"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type BundleSource struct {
	Key        string                        `json:"key"`
	Definition sourcestream.SourceDefinition `json:"definition"`
}

type BundleStream struct {
	Key         string                        `json:"key"`
	Sources     []string                      `json:"sources,omitempty"`
	Collections []string                      `json:"collections,omitempty"`
	Definition  sourcestream.StreamDefinition `json:"definition"`
}

func validateBundleResources(m *BundleMetadata) error {
	sources, collections, streams := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, c := range m.Collections {
		collections[c.Key] = true
	}
	for _, s := range m.Sources {
		if !unsmeta.ValidID(s.Key) || sources[s.Key] {
			return errors.New("metadata.json: invalid or duplicate Source key")
		}
		sources[s.Key] = true
		if s.Definition.CredentialRef != "" {
			return errors.New("metadata.json: Source credentials must be configured on the target instance after import")
		}
		if err := s.Definition.Validate(); err != nil {
			return fmt.Errorf("metadata.json Source %s: %w", s.Key, err)
		}
	}
	for i := range m.Streams {
		s := &m.Streams[i]
		if !unsmeta.ValidID(s.Key) || streams[s.Key] {
			return errors.New("metadata.json: invalid or duplicate Stream key")
		}
		streams[s.Key] = true
		for _, group := range []struct {
			keys  []string
			known map[string]bool
		}{{s.Sources, sources}, {s.Collections, collections}} {
			seen := map[string]bool{}
			for _, key := range group.keys {
				if !group.known[key] || seen[key] {
					return fmt.Errorf("metadata.json Stream %s: missing or duplicate package dependency %s", s.Key, key)
				}
				seen[key] = true
			}
		}
		if err := s.Definition.ValidateDraft(); err != nil {
			return fmt.Errorf("metadata.json Stream %s: %w", s.Key, err)
		}
		sort.Strings(s.Sources)
		sort.Strings(s.Collections)
	}
	return nil
}

var previewMetadataRollback = errors.New("rollback App package preview")

// Use the same constraint gate as all UNS schema/runtime writers. The outer
// notification transaction prevents dry-run or failed imports publishing events.
func metadataTransaction(ctx context.Context, action func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return repo.MetadataEventTransaction(ctx, repo.GetUnsConn(ctx), func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(814206108)").Error; err != nil {
			return err
		}
		return action(repo.WithUnsTransaction(ctx, tx))
	})
}

func (m *Management) previewMetadata(ctx context.Context, metadata *BundleMetadata) ([]MetadataPlanItem, error) {
	if metadata == nil || len(metadata.Sources)+len(metadata.Streams) == 0 {
		plan, _, _, err := m.planMetadata(ctx, metadata)
		return plan, err
	}
	var plan []MetadataPlanItem
	err := metadataTransaction(ctx, func(ctx context.Context) error {
		var err error
		plan, _, err = m.importMetadata(ctx, metadata)
		if err != nil {
			return err
		}
		return previewMetadataRollback
	})
	if errors.Is(err, previewMetadataRollback) {
		err = nil
	}
	// IDs allocated in a rollback are not durable; only reused IDs are shown.
	for i := range plan {
		if plan[i].Kind != "collection" && plan[i].Action == "create" {
			plan[i].ResourceID = ""
		}
	}
	return plan, err
}

func (m *Management) provisionDefinition(ctx context.Context, request InstallRequest, bundle Bundle) error {
	return metadataTransaction(ctx, func(ctx context.Context) error {
		return m.provisionDefinitionTx(ctx, request, bundle)
	})
}

func (m *Management) importMetadata(ctx context.Context, metadata *BundleMetadata) ([]MetadataPlanItem, []unsv2.Grant, error) {
	plan, changes, grants, err := m.planMetadata(ctx, metadata)
	if err != nil || metadata == nil {
		return plan, grants, err
	}
	actor, _ := contextx.SubjectFrom(ctx)
	for kind, needed := range map[string]bool{"source": len(metadata.Sources) > 0, "stream": len(metadata.Streams) > 0} {
		if needed && (!contextx.HasResource(actor, "uns."+kind) || !contextx.HasResource(actor, "uns."+kind+".manage")) {
			return nil, nil, forbiddenRequest("package import requires " + kind + " read and manage permissions")
		}
	}
	if len(metadata.Sources)+len(metadata.Streams) > 0 && m.sources == nil {
		return nil, nil, errors.New("Source/Stream runtime unavailable for package import")
	}
	if len(changes) > 0 {
		if _, err := m.uns.Apply(ctx, unsv2.Batch{Mode: "sync", Items: changes}); err != nil {
			return nil, nil, err
		}
	}
	collectionIDs, sourceIDs := map[string]string{}, map[string]string{}
	for _, item := range plan {
		collectionIDs[item.Key] = item.CollectionID
	}
	for _, source := range metadata.Sources {
		item, err := m.importResource(ctx, metadata.Namespace, "source", source.Key, source.Definition.Name, source, source.Definition)
		if err != nil {
			return nil, nil, err
		}
		sourceIDs[source.Key] = item.ResourceID
		plan = append(plan, item)
	}
	for _, stream := range metadata.Streams {
		sources, collections := map[string]string{}, map[string]string{}
		for _, key := range stream.Sources {
			sources[key] = sourceIDs[key]
		}
		for _, key := range stream.Collections {
			collections[key] = collectionIDs[key]
		}
		bindings, err := json.Marshal(map[string]any{"sources": sources, "collections": collections})
		if err != nil {
			return nil, nil, err
		}
		definition := stream.Definition
		// Explicit bindings, not search/replace of arbitrary script literals. The
		// normal compiler remains authoritative for actual runtime dependencies.
		quoted, _ := json.Marshal(string(bindings))
		definition.Script.Code = "const appResources = JSON.parse(" + string(quoted) + ");\n" + definition.Script.Code
		item, err := m.importResource(ctx, metadata.Namespace, "stream", stream.Key, definition.Name, stream, definition)
		if err != nil {
			return nil, nil, err
		}
		plan = append(plan, item)
	}
	return plan, grants, nil
}

func resourceDigest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (m *Management) resourceDocument(ctx context.Context, kind string, id int64) (any, error) {
	row, err := repo.NewUnsMetaRepo(ctx).Get(ctx, strconv.FormatInt(id, 10), true, kind)
	if err != nil {
		return nil, err
	}
	if row.DeletedAt != 0 || row.MountID != nil {
		return nil, invalidRequest("package resource is archived or mounted")
	}
	r := repo.NewSourceStreamRepo(ctx)
	if kind == "source" {
		item, err := r.Source(ctx, id)
		if err != nil {
			return nil, err
		}
		var definition sourcestream.SourceDefinition
		if err := json.Unmarshal([]byte(item.Document), &definition); err != nil {
			return nil, err
		}
		definition.CredentialRef = "" // Target-instance credentials are independent.
		return definition, nil
	}
	item, err := r.Stream(ctx, id)
	if err != nil {
		return nil, err
	}
	var definition sourcestream.StreamDefinition
	if err := json.Unmarshal([]byte(item.Document), &definition); err != nil {
		return nil, err
	}
	return definition, nil
}

func (m *Management) importResource(ctx context.Context, namespace, kind, key, name string, packaged, definition any) (MetadataPlanItem, error) {
	plan := MetadataPlanItem{Kind: kind, Key: key, Alias: key, Name: name, Action: "reuse", Permissions: []string{}}
	identity, _ := json.Marshal([]string{namespace, kind, key})
	id := uuid.NewSHA1(uuid.NameSpaceOID, append([]byte("native-app-resource/"), identity...)).String()
	r := repo.NewAppPackageResourceRepo(ctx)
	saved, err := r.Get(ctx, id)
	creating := errors.Is(err, repo.ErrNotFound)
	if err != nil && !creating {
		return plan, err
	}
	digest, err := resourceDigest(packaged)
	if err != nil {
		return plan, err
	}
	if creating {
		var result map[string]any
		if kind == "source" {
			result, err = m.sources.CreateSource(ctx, definition, 0)
		} else {
			result, err = m.sources.SaveStream(ctx, 0, 0, definition)
		}
		if err != nil {
			return plan, fmt.Errorf("metadata.json %s %s: %w", kind, key, err)
		}
		saved.ResourceID, err = strconv.ParseInt(fmt.Sprint(result["id"]), 10, 64)
		if err != nil || saved.ResourceID < 1 {
			return plan, errors.New("invalid imported resource identity")
		}
		saved.PackageDigest = digest
		plan.Action = "create"
	} else if saved.PackageDigest != digest {
		return plan, invalidRequest("metadata conflict for " + kind + " " + key + ": package definition differs; no existing resource was overwritten")
	}
	current, err := m.resourceDocument(ctx, kind, saved.ResourceID)
	if err != nil {
		return plan, fmt.Errorf("metadata conflict for %s %s: %w", kind, key, err)
	}
	applied, err := resourceDigest(current)
	if err != nil {
		return plan, err
	}
	if creating {
		saved.AppliedDigest = applied
		if err := r.Create(ctx, id, saved); err != nil {
			return plan, err
		}
	} else if applied != saved.AppliedDigest {
		return plan, invalidRequest("metadata conflict for " + kind + " " + key + ": installed definition was edited; no existing resource was overwritten")
	}
	plan.ResourceID = strconv.FormatInt(saved.ResourceID, 10)
	return plan, nil
}
