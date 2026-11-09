package repo

import (
	"encoding/json"

	"gorm.io/gorm"
)

// A Stream is the actual configuration at config.streams[id], not a pointer to
// a second metadata row. Internal API documents remain a compatibility view.
func (c streamConfig) MarshalJSON() ([]byte, error) {
	var data map[string]json.RawMessage
	if err := json.Unmarshal(c.Document, &data); err != nil {
		return nil, err
	}
	values := map[string]any{"revision": c.Revision, "activeVersion": c.ActiveVersion, "pendingVersion": c.PendingVersion, "draftManifest": c.DraftManifest, "status": c.Status, "createdAt": c.CreatedAt, "updatedAt": c.UpdatedAt, "deletedAt": c.DeletedAt, "versions": c.Versions, "kind": c.Kind, "collectionId": c.CollectionID, "schema": c.Schema}
	values["name"], values["model"] = c.Name, c.Model
	if c.Model == "program" {
		// The registration document must stay byte-semantically separate from
		// revision/manifest fields: those fields are not user script inputs.
		data = map[string]json.RawMessage{}
		values["document"], values["model"], values["name"] = c.Document, c.Model, c.Name
	}
	for key, value := range values {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		data[key] = raw
	}
	return json.Marshal(data)
}
func (c *streamConfig) UnmarshalJSON(raw []byte) error {
	type fields streamConfig
	var meta fields
	if err := json.Unmarshal(raw, &meta); err != nil {
		return err
	}
	*c = streamConfig(meta)
	if meta.Model != "script" && len(meta.Document) > 0 {
		return nil
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	for _, key := range []string{"revision", "activeVersion", "pendingVersion", "draftManifest", "status", "createdAt", "updatedAt", "deletedAt", "versions", "kind", "collectionId", "schema"} {
		delete(data, key)
	}
	doc, err := json.Marshal(data)
	c.Document = doc
	return err
}
func bindStreamSchema(tx *gorm.DB, c *streamConfig) error {
	var doc struct {
		MetadataTarget *struct {
			CollectionID  string `json:"collectionId"`
			SampleEnabled bool   `json:"sampleEnabled"`
		} `json:"metadataTarget"`
	}
	if err := json.Unmarshal(c.Document, &doc); err != nil {
		return err
	}
	c.CollectionID = ""
	c.Schema = nil
	c.Kind = "record"
	if doc.MetadataTarget == nil {
		return nil
	}
	c.CollectionID = doc.MetadataTarget.CollectionID
	if doc.MetadataTarget.SampleEnabled {
		c.Kind = "measurement"
	}
	var row UnsMeta
	if err := metaQuery(tx, "collection").Where("id=? AND deleted_at=0", c.CollectionID).Take(&row).Error; err != nil {
		return err
	}
	var schemas map[string]json.RawMessage
	if err := json.Unmarshal(row.Data, &schemas); err != nil {
		return err
	}
	c.Schema = schemas["recordSchema"]
	if doc.MetadataTarget.SampleEnabled {
		c.Schema = schemas["sampleSchema"]
	}
	return nil
}
