package repo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"reflect"

	"backend/internal/streamexec"
	"backend/internal/unsref"
)

// These are trusted deployment snapshots, not extra script-declared authority.
// They live inside the existing immutable version payload, never the draft SDK manifest.
type ProgramUNSBinding struct {
	Reference        streamexec.ProgramTopic `json:"reference"`
	CollectionID     string                  `json:"collectionId,omitempty"`
	TopicBindingHash string                  `json:"topicBindingHash,omitempty"`
}

func FreezeProgramV2Bindings(ctx context.Context, manifest streamexec.ProgramManifest) ([]ProgramUNSBinding, error) {
	bindings := []ProgramUNSBinding{}
	seen := map[string]bool{}
	for _, ref := range manifest.Topics {
		if !ref.Valid() || seen[ref.Key()] {
			return nil, ErrInvalidArgument
		}
		seen[ref.Key()] = true
		if !ref.Canonical() {
			continue
		}
		item := ProgramUNSBinding{Reference: ref, CollectionID: ref.CollectionID}
		if ref.TopicID != "" {
			row, err := NewUnsMetaRepo(ctx).Get(ctx, ref.TopicID, false, "topic")
			if err != nil {
				return nil, err
			}
			var doc struct {
				ViewMode     string            `json:"viewMode"`
				CollectionID string            `json:"collectionId"`
				Fields       []json.RawMessage `json:"fields"`
			}
			if json.Unmarshal(row.Data, &doc) != nil {
				return nil, ErrInvalidArgument
			}
			if doc.ViewMode == "records" || doc.ViewMode == "" && doc.CollectionID != "" && len(doc.Fields) == 0 {
				if !unsref.ValidMetadataID(doc.CollectionID) || len(doc.Fields) != 0 {
					return nil, ErrInvalidArgument
				}
				item.CollectionID = doc.CollectionID
			} else if doc.ViewMode == "fields" || doc.ViewMode == "" && doc.CollectionID == "" && len(doc.Fields) > 0 {
				if ref.Writable() || doc.CollectionID != "" || len(doc.Fields) == 0 {
					return nil, ErrInvalidArgument
				}
				// Field views can be read, but there is no field-event fanout yet.
				for _, entry := range manifest.Entries {
					if entry.Kind != "uns-event" {
						continue
					}
					for _, id := range entry.TopicIDs {
						if id == ref.Key() {
							return nil, ErrInvalidArgument
						}
					}
				}
				for _, raw := range doc.Fields {
					var field struct {
						CollectionID string `json:"collectionId"`
					}
					if json.Unmarshal(raw, &field) != nil || !unsref.ValidMetadataID(field.CollectionID) {
						return nil, ErrInvalidArgument
					}
					if _, err := NewUnsMetaRepo(ctx).Get(ctx, field.CollectionID, false, "collection"); err != nil {
						return nil, err
					}
				}
			} else {
				return nil, ErrInvalidArgument
			}
			raw, _ := json.Marshal(doc)
			hash := sha256.Sum256(raw)
			item.TopicBindingHash = hex.EncodeToString(hash[:])
		}
		if item.CollectionID != "" {
			if _, err := NewUnsMetaRepo(ctx).Get(ctx, item.CollectionID, false, "collection"); err != nil {
				return nil, err
			}
		}
		bindings = append(bindings, item)
	}
	return bindings, nil
}

func (v StreamVersion) ProgramV2Bindings() ([]ProgramUNSBinding, error) {
	var object map[string]json.RawMessage
	if len(v.ProgramManifest) > streamexec.MaxJSONBytes || json.Unmarshal([]byte(v.ProgramManifest), &object) != nil {
		return nil, ErrInvalidArgument
	}
	items := []ProgramUNSBinding{}
	if raw := object["_unsBindings"]; len(raw) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&items) != nil || items == nil || decoder.Decode(new(any)) != io.EOF {
			return nil, ErrInvalidArgument
		}
	}
	manifest, err := v.ProgramV2Manifest()
	if err != nil {
		return nil, err
	}
	declared := map[string]streamexec.ProgramTopic{}
	for _, ref := range manifest.Topics {
		if ref.Canonical() {
			declared[ref.Key()] = ref
		}
	}
	if len(items) != len(declared) {
		return nil, ErrInvalidArgument
	}
	for _, item := range items {
		ref, ok := declared[item.Reference.Key()]
		if !ok || ref != item.Reference || !ref.Valid() || !ref.Canonical() {
			return nil, ErrInvalidArgument
		}
		delete(declared, ref.Key())
		if ref.CollectionID != "" {
			if item.CollectionID != ref.CollectionID || item.TopicBindingHash != "" {
				return nil, ErrInvalidArgument
			}
		} else {
			hash, err := hex.DecodeString(item.TopicBindingHash)
			if err != nil || len(hash) != sha256.Size || item.CollectionID != "" && !unsref.ValidMetadataID(item.CollectionID) || ref.Writable() && item.CollectionID == "" {
				return nil, ErrInvalidArgument
			}
		}
	}
	return items, nil
}

func ValidateProgramV2Bindings(ctx context.Context, manifest streamexec.ProgramManifest, bindings []ProgramUNSBinding) error {
	current, err := FreezeProgramV2Bindings(ctx, manifest)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, bindings) {
		return ErrConflict
	}
	return nil
}

func encodeProgramV2Bindings(manifest string, bindings []ProgramUNSBinding) (string, error) {
	if len(bindings) == 0 {
		return manifest, nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(manifest), &object) != nil {
		return "", ErrInvalidArgument
	}
	raw, err := json.Marshal(bindings)
	if err != nil {
		return "", err
	}
	object["_unsBindings"] = raw
	raw, err = json.Marshal(object)
	if err != nil || len(raw) > streamexec.MaxJSONBytes {
		return "", ErrInvalidArgument
	}
	return string(raw), nil
}
