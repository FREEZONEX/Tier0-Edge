package unsmeta

import "context"

// A Collection occupies its own Namespace position; Topics are explicit references.
func (s *Service) CreateCollection(ctx context.Context, parentID string, input any) (map[string]any, error) {
	var doc Document
	if err := Parse(input, &doc); err != nil {
		return nil, err
	}
	doc.ParentID = parentID
	collection, err := s.Save(ctx, "", "collection", 0, doc)
	return map[string]any{"collection": collection}, err
}
