package appinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"

	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/domain/unsv2"
	"backend/internal/repo"
	"github.com/google/uuid"
)

// Namespace and key identify shared local definitions independently of App IDs.
// This deliberately excludes legacy Project, Flow, Role and business records.
type BundleMetadata struct {
	SchemaVersion int                `json:"schemaVersion"`
	Namespace     string             `json:"namespace"`
	Collections   []BundleCollection `json:"collections"`
	Sources       []BundleSource     `json:"sources,omitempty"`
	Streams       []BundleStream     `json:"streams,omitempty"`
}
type BundleCollection struct {
	Key         string               `json:"key"`
	Alias       string               `json:"alias"`
	Permissions []string             `json:"permissions"`
	Definition  CollectionDefinition `json:"definition"`
}
type CollectionDefinition struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	RecordSchema *unsmeta.Schema `json:"recordSchema"`
	SampleSchema *unsmeta.Schema `json:"sampleSchema,omitempty"`
}

func (d CollectionDefinition) document() unsmeta.Document {
	return unsmeta.Document{SchemaVersion: 1, Name: d.Name, Description: d.Description, RecordSchema: d.RecordSchema, SampleSchema: d.SampleSchema}
}

type MetadataPlanItem struct {
	Kind         string   `json:"kind"`
	Key          string   `json:"key"`
	ResourceID   string   `json:"resourceId,omitempty"`
	Alias        string   `json:"alias"`
	CollectionID string   `json:"collectionId"`
	Name         string   `json:"name"`
	Action       string   `json:"action"`
	Permissions  []string `json:"permissions"`
}

func parseBundleMetadata(raw []byte) (*BundleMetadata, error) {
	var m BundleMetadata
	if err := unsv2.Decode(raw, &m); err != nil {
		return nil, fmt.Errorf("metadata.json: %w", err)
	}
	count := len(m.Collections) + len(m.Sources) + len(m.Streams)
	if m.SchemaVersion != 1 || !unsmeta.ValidID(m.Namespace) || count == 0 || count > 100 {
		return nil, errors.New("metadata.json requires schemaVersion=1, a valid namespace and 1 to 100 resources")
	}
	keys, aliases := map[string]bool{}, map[string]bool{}
	for i := range m.Collections {
		c := &m.Collections[i]
		if !unsmeta.ValidID(c.Key) || !unsmeta.ValidID(c.Alias) || keys[c.Key] || aliases[c.Alias] || len(c.Permissions) == 0 {
			return nil, errors.New("metadata.json: invalid or duplicate collection key/alias, or missing permissions")
		}
		keys[c.Key], aliases[c.Alias] = true, true
		permissions := map[string]bool{}
		for _, p := range c.Permissions {
			switch p {
			case "record:read", "record:write", "record:delete", "sample:read", "sample:write":
			default:
				return nil, fmt.Errorf("metadata.json: unsupported permission %q", p)
			}
			if permissions[p] {
				return nil, errors.New("metadata.json: duplicate permission")
			}
			permissions[p] = true
			if len(p) >= 7 && p[:7] == "sample:" && c.Definition.SampleSchema == nil {
				return nil, errors.New("metadata.json: sample permission requires sampleSchema")
			}
		}
		sort.Strings(c.Permissions)
		if err := c.Definition.document().Validate("collection"); err != nil {
			return nil, fmt.Errorf("metadata.json collection %s: %w", c.Key, err)
		}
	}
	if err := validateBundleResources(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Management) planMetadata(ctx context.Context, metadata *BundleMetadata) ([]MetadataPlanItem, []unsv2.Change, []unsv2.Grant, error) {
	if metadata == nil {
		return nil, nil, nil, nil
	}
	actor, ok := contextx.SubjectFrom(ctx)
	if !ok || !contextx.HasResource(actor, "uns.read") || !contextx.HasResource(actor, "uns.write") {
		return nil, nil, nil, forbiddenRequest("metadata import requires uns.read and uns.write")
	}
	plan := make([]MetadataPlanItem, 0, len(metadata.Collections))
	changes := []unsv2.Change{}
	grants := []unsv2.Grant{}
	for _, c := range metadata.Collections {
		id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("native-app-metadata/"+metadata.Namespace+"/"+c.Key)).String()
		action := "reuse"
		doc := c.Definition.document()
		row, err := repo.NewUnsMetaRepo(ctx).Get(ctx, id, true, "collection")
		if errors.Is(err, repo.ErrNotFound) {
			action = "create"
			raw, _ := json.Marshal(doc)
			changes = append(changes, unsv2.Change{OpID: fmt.Sprintf("collection-%d", len(plan)), Action: "meta.create", Kind: "collection", ID: unsv2.Identity{ID: id}, Data: raw})
		} else if err != nil {
			return nil, nil, nil, err
		} else {
			var existing unsmeta.Document
			decodeErr := unsv2.Decode(row.Data, &existing)
			oldJSON, _ := json.Marshal(existing)
			newJSON, _ := json.Marshal(doc)
			if row.DeletedAt != 0 || row.MountID != nil || decodeErr != nil || !bytes.Equal(oldJSON, newJSON) {
				return nil, nil, nil, invalidRequest(fmt.Sprintf("metadata conflict for %s (%s): existing definition differs, is archived or mounted; no data was overwritten", c.Alias, id))
			}
		}
		plan = append(plan, MetadataPlanItem{Kind: "collection", Key: c.Key, ResourceID: id, Alias: c.Alias, CollectionID: id, Name: doc.Name, Action: action, Permissions: c.Permissions})
		grants = append(grants, unsv2.Grant{Alias: c.Alias, CollectionID: id, Permissions: c.Permissions})
	}
	return plan, changes, grants, nil
}

// Metadata and the App grant definition commit as one UNS transaction. Runtime
// installation may subsequently fail; definitions and business data are retained
// so an explicit retry can reuse them without overwriting a shared Collection.
func (m *Management) provisionDefinitionTx(ctx context.Context, request InstallRequest, bundle Bundle) error {
	_, grants, err := m.importMetadata(ctx, bundle.Metadata)
	if err != nil {
		return err
	}
	if bundle.Metadata != nil && len(request.Collections) != 0 {
		return invalidRequest("metadata.json defines the App grants; do not also supply collections")
	}
	if bundle.Metadata == nil {
		grants = request.Collections
	}
	row, err := repo.NewUnsMetaRepo(ctx).Get(ctx, request.AppID, true, "app")
	var data unsv2.AppData
	action := "meta.create"
	var revision int64
	if errors.Is(err, repo.ErrNotFound) {
		data = unsv2.AppData{Header: unsv2.Header{SchemaVersion: 1, Name: bundle.Name, Description: bundle.Description}, Entry: &unsv2.AppEntry{BasePath: "/apps/" + request.AppID, Path: "/"}, Roles: []unsv2.Role{}, Members: []unsv2.Member{}, Collections: []unsv2.Grant{}, Status: "active"}
		data.Navigation.TopicIDs = []string{}
	} else if err != nil {
		return err
	} else {
		if row.MountID != nil || row.DeletedAt != 0 {
			return forbiddenRequest("archived or mounted App cannot be installed")
		}
		if err := json.Unmarshal(row.Data, &data); err != nil {
			return err
		}
		if !unsv2.IsNativeAppEntry(row.ID, data) {
			return invalidRequest("Choose a native App as the installation target")
		}
		if bundle.Metadata == nil && len(bundle.Roles) == 0 && len(grants) == 0 {
			return nil
		}
		action, revision = "meta.replace", row.Revision
	}
	bound, err := importRolePlan(ctx, bundle.Roles, request)
	if err != nil {
		return err
	}
	for _, r := range bound {
		found := false
		for i := range data.Roles {
			if data.Roles[i].ID == r.ID {
				data.Roles[i] = r
				found = true
				break
			}
		}
		if !found {
			data.Roles = append(data.Roles, r)
		}
	}
	if action == "meta.create" {
		if grants != nil {
			data.Collections = grants
		}
		grants = nil // Preserve duplicate validation in the UNS domain for manual grants.
	}
	for _, grant := range grants {
		found := false
		for _, old := range data.Collections {
			if old.Alias != grant.Alias {
				continue
			}
			found = true
			permissions := append([]string(nil), old.Permissions...)
			sort.Strings(permissions)
			if old.CollectionID != grant.CollectionID || !reflect.DeepEqual(permissions, grant.Permissions) {
				return invalidRequest("existing App grant conflicts with metadata alias: " + grant.Alias + "; review the App grants before replacement")
			}
		}
		if !found {
			data.Collections = append(data.Collections, grant)
		}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	changes := []unsv2.Change{{OpID: "app", Action: action, Kind: "app", ID: unsv2.Identity{ID: request.AppID}, ExpectedRevision: revision, Data: raw}}
	_, err = m.uns.Apply(ctx, unsv2.Batch{Mode: "sync", Items: changes})
	return err
}
