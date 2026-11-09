package unsmeta

import (
	"backend/internal/repo"
	"context"
	"gorm.io/gorm"
)

// Restoring an existing Reference may retain a readable recycled Collection.
// New or replaced References still require active targets.
func validateReferences(ctx context.Context, tx *gorm.DB, id string, doc Document, restore bool) error {
	loadCollection := collection
	if restore {
		loadCollection = readCollection
	}
	r := repo.NewUnsMetaRepo(tx)
	if doc.ParentID != "" {
		parentID := doc.ParentID
		seen := map[string]bool{id: true}
		for parentID != "" {
			if seen[parentID] {
				return ErrCycle
			}
			seen[parentID] = true
			parent, err := r.Get(ctx, parentID, false, "directory")
			if err != nil {
				return err
			}
			if parent.MountID != nil {
				return repo.ErrMountedReadOnly
			}
			if parent.Kind != "folder" {
				return ErrInvalid
			}
			parentDoc, err := decode(parent)
			if err != nil {
				return err
			}
			parentID = parentDoc.ParentID
		}
	}
	if doc.CollectionID != "" {
		if _, _, err := loadCollection(ctx, tx, doc.CollectionID); err != nil {
			return err
		}
	}
	for _, field := range doc.Fields {
		_, target, err := loadCollection(ctx, tx, field.CollectionID)
		if err != nil {
			return err
		}
		schema := target.RecordSchema
		if field.Kind == "sample" {
			schema = target.SampleSchema
		}
		if !schemaField(schema, field.FieldPath) {
			return ErrSchema
		}
		if _, err := r.Record(ctx, field.CollectionID, field.RecordID); err != nil {
			return err
		}
	}

	return nil
}

// ValidateTopic checks the same references and revision as Save without writing rows.
func (s *Service) ValidateTopic(ctx context.Context, id string, revision int64, input any) error {
	var doc Document
	if err := Parse(input, &doc); err != nil {
		return err
	}
	if err := doc.Validate("topic"); err != nil {
		return err
	}
	if revision < 0 || (id == "" && revision != 0) {
		return ErrInvalid
	}
	if id != "" && (!ValidID(id) || revision == 0) {
		return ErrInvalid
	}
	return repo.NewUnsMetaRepo(ctx).Transaction(ctx, func(tx *gorm.DB) error {
		if err := lockMetadata(tx, true); err != nil {
			return err
		}
		if id != "" {
			row, err := repo.NewUnsMetaRepo(tx).Get(ctx, id, false, "topic")
			if err != nil {
				return err
			}
			if row.Kind != "topic" || row.Revision != revision {
				return repo.ErrConflict
			}
		}
		return validateReferences(ctx, tx, id, doc, false)
	})
}
