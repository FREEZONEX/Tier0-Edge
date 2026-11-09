package sourcestream

import (
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Private Source metadata. Ciphertext is never returned from a domain operation.
// The ciphertext field also reads existing numeric Source credential envelopes.
type sharedCredentialSlot struct {
	Ciphertext []byte `json:"ciphertext"`
	Kind       string `json:"kind"`
	Revision   int64  `json:"revision"`
	UpdatedAt  int64  `json:"updatedAt"`
}
type SharedCredentialReference struct {
	CredentialRef string `json:"credentialRef"`
	Kind          string `json:"kind"`
	Revision      int64  `json:"revision"`
	UpdatedAt     int64  `json:"updatedAt"`
}
type sharedCredentialDocument struct {
	SchemaVersion int      `json:"schemaVersion"`
	Adapter       Protocol `json:"adapter"`
	Config        struct {
		Connection SourceDefinition `json:"connection"`
	} `json:"config"`
	Credentials map[string]sharedCredentialSlot `json:"credentials"`
}

// Native Source documents predate the shared schema. Their numeric IDs and
// selected credential use the same encryption AAD as a shared credential slot.
func (d sharedCredentialDocument) nativeMQTT(id string) bool {
	n, err := strconv.ParseInt(id, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == id && d.SchemaVersion == 0 &&
		d.Adapter == MQTT && d.Config.Connection.Protocol == MQTT && d.Config.Connection.ConnectionRole() == ClientRole
}

func (s *Service) SharedCredentialReferences(ctx context.Context, id string) (any, error) {
	if !unsmeta.ValidID(id) {
		return nil, repo.ErrInvalidArgument
	}
	var row repo.UnsMeta
	if err := repo.GetUnsConn(ctx).Where("kind='source' AND id=? AND deleted_at=0", id).Take(&row).Error; err != nil {
		return nil, err
	}
	var d sharedCredentialDocument
	if json.Unmarshal(row.Data, &d) != nil || (d.SchemaVersion != 1 && !d.nativeMQTT(id)) {
		return nil, repo.ErrInvalidArgument
	}
	items := []SharedCredentialReference{}
	for ref, slot := range d.Credentials {
		kind := slot.Kind
		if kind == "" {
			c, err := s.cipher.OpenSource(id, slot.Ciphertext)
			if err != nil {
				return nil, ErrCredential
			}
			kind = c.Kind
		}
		items = append(items, SharedCredentialReference{ref, kind, max(slot.Revision, 1), slot.UpdatedAt})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CredentialRef < items[j].CredentialRef })
	return map[string]any{"sourceId": id, "sourceRevision": row.Revision, "protocol": d.Config.Connection.Protocol, "selectedRef": d.Config.Connection.CredentialRef, "items": items}, nil
}

// Provisioning/rotation is local to the execution installation. It cannot alter
// configuration authority or select a new reference on behalf of the center.
func (s *Service) SaveSharedCredential(ctx context.Context, id string, revision int64, ref string, expected int64, credential Credential, dryRun bool) (any, error) {
	if !unsmeta.ValidID(id) || revision < 1 || expected < 0 || ref != "" && !unsmeta.ValidID(ref) {
		return nil, repo.ErrInvalidArgument
	}
	if credential.Kind != "basic" && credential.Kind != "bearer" && credential.Kind != "custom" && credential.Kind != "http" {
		return nil, ErrCredential
	}
	for k := range credential.Values {
		if strings.HasPrefix(k, "_vendor") {
			return nil, ErrCredential
		}
	}
	sealed, err := s.cipher.SealSource(id, credential)
	if err != nil {
		return nil, err
	}
	if ref == "" {
		if expected != 0 {
			return nil, repo.ErrConflict
		}
		ref = uuid.NewString()
	}
	result := SharedCredentialReference{}
	selected := false
	err = repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(814206108)").Error; err != nil {
			return err
		}
		var row repo.UnsMeta
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("kind='source' AND id=? AND deleted_at=0", id).Take(&row).Error; err != nil {
			return err
		}
		if row.Revision != revision {
			return repo.ErrConflict
		}
		var d sharedCredentialDocument
		if json.Unmarshal(row.Data, &d) != nil || validateSharedCredentialKind(d.Config.Connection, credential) != nil {
			return ErrCredential
		}
		selected = d.Config.Connection.CredentialRef == ref
		slot, exists := d.Credentials[ref]
		if d.SchemaVersion != 1 && !(d.nativeMQTT(id) && selected && exists) {
			return ErrCredential
		}
		current := int64(0)
		if exists {
			current = max(slot.Revision, 1)
		}
		if current != expected {
			return repo.ErrConflict
		}
		if !exists && len(d.Credentials) >= 32 {
			return repo.ErrConflict
		}
		result = SharedCredentialReference{ref, credential.Kind, current + 1, time.Now().UnixMilli()}
		if dryRun {
			return nil
		}
		raw, e := json.Marshal(sharedCredentialSlot{sealed, result.Kind, result.Revision, result.UpdatedAt})
		if e != nil {
			return e
		}
		return tx.Exec("UPDATE uns_meta SET data=jsonb_set(data,'{credentials}',COALESCE(data->'credentials','{}'::jsonb)||jsonb_build_object(?::text,COALESCE(data->'credentials'->?::text,'{}'::jsonb)||?::jsonb),true) WHERE kind='source' AND id=?", ref, ref, string(raw), id).Error
	})
	if err != nil {
		return nil, err
	}
	if !dryRun && selected {
		s.releaseSource(sharedDriverID(id))
	}
	return map[string]any{"sourceId": id, "sourceRevision": revision, "reference": result, "sideEffects": !dryRun}, nil
}
func (s *Service) sharedCredential(ctx context.Context, id, ref string) (Credential, error) {
	if ref == "" {
		return Credential{}, nil
	}
	if !unsmeta.ValidID(id) || !unsmeta.ValidID(ref) {
		return Credential{}, ErrCredential
	}
	var row repo.UnsMeta
	if err := repo.GetUnsConn(ctx).Where("kind='source' AND id=? AND deleted_at=0", id).Take(&row).Error; err != nil {
		return Credential{}, ErrCredential
	}
	var d sharedCredentialDocument
	if json.Unmarshal(row.Data, &d) != nil || d.SchemaVersion != 1 {
		return Credential{}, ErrCredential
	}
	slot, ok := d.Credentials[ref]
	if !ok {
		return Credential{}, ErrCredential
	}
	return s.cipher.OpenSource(id, slot.Ciphertext)
}
func (s *Service) ValidateSharedCredentialReference(ctx context.Context, id string, def SourceDefinition) error {
	credential, err := s.sharedCredential(ctx, id, def.CredentialRef)
	if err != nil {
		return err
	}
	if def.CredentialRef == "" {
		return nil
	}
	if err := validateSharedCredentialKind(def, credential); err != nil {
		return err
	}
	if def.Protocol == HTTP && credential.Kind != "custom" && credential.Kind != "http" && def.HTTPAuthentication() != credential.Kind {
		return ErrCredential
	}
	return nil
}

func validateSharedCredentialKind(def SourceDefinition, credential Credential) error {
	if def.ConnectionRole() != ClientRole || def.SourceType != "" {
		return ErrCredential
	}
	switch def.Protocol {
	case HTTP:
		if credential.Kind != "custom" && credential.Kind != "http" && credential.Kind != "basic" && credential.Kind != "bearer" {
			return ErrCredential
		}
	case MQTT:
		if credential.Kind == "custom" {
			_, err := MQTTClientTLS(def, credential)
			return err
		}
		fallthrough
	case OPCUA:
		if credential.Kind != "basic" || len(credential.Values) != 0 {
			return ErrCredential
		}
	default:
		return ErrCredential
	}
	return nil
}
