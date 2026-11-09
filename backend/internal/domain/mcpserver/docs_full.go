package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
)

type agentDocsInput struct {
	Query    string `json:"query,omitempty"`
	Limit    int    `json:"limit,omitempty" jsonschema:"maximum number of results (1-10, default 5)"`
	Source   string `json:"source,omitempty"`
	Offset   int    `json:"offset,omitempty"`
	PageSize int    `json:"pageSize,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
}

func (s *Service) agentDocs(ctx context.Context, in agentDocsInput) (any, error) {
	if !permitted(ctx, "uns.read") {
		return nil, errors.New("read permission required")
	}
	in.Query = strings.TrimSpace(in.Query)
	if in.Source != "" && (in.Query != "" || in.Limit != 0) {
		return nil, errors.New("use source or query, not both")
	}
	out := map[string]any{"results": []map[string]any{}}
	if in.Query != "" {
		if in.Limit <= 0 {
			in.Limit = 5
		}
		if in.Limit > 10 {
			in.Limit = 10
		}
		value, err := s.search(ctx, SearchInput{Query: in.Query, Limit: in.Limit})
		if err != nil {
			return nil, err
		}
		hits := []map[string]any{}
		for _, hit := range value.(map[string]any)["hits"].([]map[string]any) {
			source := fmt.Sprint(hit["uri"])
			for _, doc := range s.skills.Documents {
				if doc.URI == source {
					source = strings.TrimPrefix(doc.SourcePath, "doc/mcp/agent-guide/")
					break
				}
			}
			hits = append(hits, map[string]any{"title": hit["title"], "docId": source, "source": source, "score": hit["score"], "text": hit["excerpt"]})
		}
		out["results"] = hits
	}
	if in.Source == "" {
		if in.Query == "" || in.Offset != 0 || in.SHA256 != "" || in.PageSize != 0 {
			return nil, errors.New("query or source required")
		}
		return out, nil
	}
	source := strings.SplitN(in.Source, "#", 2)[0]
	if strings.Contains(source, "\\") || strings.ContainsRune(source, '\x00') || (!strings.HasPrefix(source, "tier0://") && (path.Clean(source) != source || strings.HasPrefix(source, "/") || strings.HasPrefix(source, "../"))) {
		return nil, errors.New("invalid document source")
	}
	var content string
	found := false
	for _, doc := range s.skills.Documents {
		if source == doc.URI || source == doc.SourcePath || "doc/mcp/agent-guide/"+source == doc.SourcePath {
			if !permitted(ctx, doc.Permissions...) {
				return nil, errors.New("document unavailable")
			}
			content = doc.Content
			found = true
			break
		}
	}
	if !found {
		for _, doc := range s.documents {
			if source == doc.URI || source == doc.ID {
				if !permitted(ctx, doc.Permissions...) {
					return nil, errors.New("document unavailable")
				}
				content = doc.Content
				found = true
				break
			}
		}
	}
	if !found {
		return nil, errors.New("document unavailable")
	}
	digest := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(digest[:])
	runes := []rune(content)
	if in.Offset < 0 || in.Offset > len(runes) || in.PageSize < 0 || in.PageSize > 24000 || in.Offset > 0 && in.SHA256 == "" || in.SHA256 != "" && in.SHA256 != hash {
		return nil, errors.New("invalid page or document changed; restart at offset 0")
	}
	size := in.PageSize
	if size == 0 {
		size = 12000
	}
	end := min(in.Offset+size, len(runes))
	out["document"] = map[string]any{"source": source, "sha256": hash, "text": string(runes[in.Offset:end]), "offset": in.Offset, "nextOffset": end, "totalChars": len(runes), "truncated": end < len(runes)}
	return out, nil
}
