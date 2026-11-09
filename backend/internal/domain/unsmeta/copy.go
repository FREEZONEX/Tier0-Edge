package unsmeta

import (
	"context"
	"fmt"
	"time"

	"backend/internal/domain/common"
	"backend/internal/repo"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Copy duplicates namespace placement, retaining Collection and field references.
// Capture the subtree before inserting so copying cannot recursively copy its own output.
func (s *Service) Copy(ctx context.Context, id string, revision int64, parentID, name string, description *string, kinds ...string) (repo.UnsMeta, error) {
	var saved repo.UnsMeta
	if !ValidID(id) || revision <= 0 || (parentID != "" && !ValidID(parentID)) {
		return saved, ErrInvalid
	}
	err := repo.NewUnsMetaRepo(ctx).Transaction(ctx, func(tx *gorm.DB) error {
		if err := lockMetadata(tx, false); err != nil {
			return err
		}
		r := repo.NewUnsMetaRepo(tx)
		source, err := r.Get(ctx, id, false, kinds...)
		if err != nil {
			return err
		}
		if source.Kind != "folder" && source.Kind != "topic" {
			return ErrInvalid
		}
		if source.Revision != revision {
			return repo.ErrConflict
		}
		if parentID != "" {
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
		}
		rows := []repo.UnsMeta{}
		for page := 1; ; page++ {
			batch, total, err := r.List(ctx, "namespace", false, page, 500)
			if err != nil {
				return err
			}
			rows = append(rows, batch...)
			if int64(len(rows)) >= total || len(batch) == 0 {
				break
			}
		}
		docs := make(map[string]Document, len(rows))
		children := map[string][]repo.UnsMeta{}
		for _, row := range rows {
			doc, err := decode(row)
			if err != nil {
				return err
			}
			docs[row.ID] = doc
			children[doc.ParentID] = append(children[doc.ParentID], row)
		}
		pending := []repo.UnsMeta{source}
		seen := map[string]bool{source.ID: true}
		for i := 0; i < len(pending); i++ {
			for _, child := range children[pending[i].ID] {
				if seen[child.ID] {
					return ErrCycle
				}
				seen[child.ID] = true
				pending = append(pending, child)
			}
		}
		if seen[parentID] {
			return ErrCycle
		}
		root := docs[id]
		if name != "" {
			root.Name = name
		}
		for n := 2; ; n++ {
			exists, err := r.NamespaceNameExists(ctx, parentID, root.Name)
			if err != nil {
				return err
			}
			if !exists {
				break
			}
			if name != "" {
				return repo.ErrConflict
			}
			suffix := fmt.Sprintf(" (%d)", n)
			base := []rune(docs[id].Name)
			limit := common.NameMaxLen - len([]rune(suffix))
			if len(base) > limit {
				base = base[:limit]
			}
			root.Name = string(base) + suffix
		}
		if description != nil {
			root.Description = *description
		}
		root.ParentID = parentID
		if err := root.Validate(source.Kind); err != nil {
			return err
		}
		ids := make(map[string]string, len(pending))
		for _, row := range pending {
			ids[row.ID] = uuid.NewString()
		}
		now := time.Now().UnixMilli()
		for _, row := range pending {
			doc := docs[row.ID]
			if row.ID == id {
				doc = root
			} else {
				doc.ParentID = ids[doc.ParentID]
			}
			clone := repo.UnsMeta{ID: ids[row.ID], Kind: row.Kind, Data: mustJSON(doc), Revision: 1, CreatedAt: now, UpdatedAt: now}
			if row.Kind == "collection" {
				clone.Kind = "topic"
				clone.Data = mustJSON(Document{SchemaVersion: 1, Name: doc.Name, Description: doc.Description, ParentID: doc.ParentID, ViewMode: "records", CollectionID: row.ID})
			}
			cloneDoc, err := decode(clone)
			if err != nil {
				return err
			}
			if err := validateReferences(ctx, tx, clone.ID, cloneDoc, false); err != nil {
				return err
			}
			if err := tx.Create(&clone).Error; err != nil {
				return err
			}
			if row.ID == id {
				saved = clone
			}
		}
		return nil
	})
	return saved, err
}
