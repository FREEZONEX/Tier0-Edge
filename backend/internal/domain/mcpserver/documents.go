package mcpserver

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"backend/internal/contextx"
	"backend/internal/domain/unsv2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed documents/*.md documents/manifest.json documents/skills.json
var documentFS embed.FS

type Document struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Language    string   `json:"language"`
	File        string   `json:"file"`
	Permissions []string `json:"permissions"`
	Content     string   `json:"-"`
	URI         string   `json:"uri,omitempty"`
}

func loadDocuments() ([]Document, error) {
	raw, err := documentFS.ReadFile("documents/manifest.json")
	if err != nil {
		return nil, err
	}
	var documents []Document
	if err = json.Unmarshal(raw, &documents); err != nil {
		return nil, err
	}
	for i := range documents {
		content, err := documentFS.ReadFile("documents/" + documents[i].File)
		if err != nil {
			return nil, err
		}
		documents[i].Content = string(content)
	}
	return documents, nil
}

type SearchInput struct {
	Query    string `json:"query" jsonschema:"Keywords such as Record Sample SDK or Source mapping"`
	Language string `json:"language,omitempty" jsonschema:"Optional zh-CN or en-US; omitted searches both"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Result limit from 1 to 10, default 5"`
}
type ContractInput struct {
	Operations []string `json:"operations,omitempty" jsonschema:"Exact interface operations/IDs/paths, not batch actions. Omitted selects operations for the requested kinds"`
	Kind       string   `json:"kind,omitempty" jsonschema:"Optional metadata kind for its write configuration model"`
	Kinds      []string `json:"kinds,omitempty" jsonschema:"Model kinds to obtain schemas, write/read fields, constraints and relationships without knowing operation names"`
	Offset     *int     `json:"offset,omitempty" jsonschema:"Unicode offset for complete JSON contract paging; use returned nextOffset"`
	Limit      int      `json:"limit,omitempty" jsonschema:"Page characters, default 12000, maximum 24000; forces paged JSON delivery"`
	SHA256     string   `json:"sha256,omitempty" jsonschema:"First page sha256; changed contracts or permissions require restarting at offset zero"`
}
type ExampleInput struct {
	Operations []string `json:"operations" jsonschema:"Catalog operation names or IDs, for example records, samples or apply"`
	Client     string   `json:"client" jsonschema:"curl, sdk, cli or mcp"`
	Scenario   string   `json:"scenario,omitempty" jsonschema:"Optional linked for a complete cross-kind batch, or patch for a safe revision-controlled partial update"`
}

func (s *Service) addDocumentationTools(server *mcp.Server, subject contextx.Subject) {
	s.addSkillTools(server, subject)
	mcp.AddTool(server, readOnlyTool("api_docs_search", "Search version-bundled documentation about UNS data models, explicit relations, interfaces, authentication and SDK/CLI usage. Results respect current read scopes."), func(ctx context.Context, _ *mcp.CallToolRequest, input SearchInput) (*mcp.CallToolResult, any, error) {
		value, err := s.search(contextx.WithSubject(ctx, subject), input)
		return finish(value, err)
	})
	mcp.AddTool(server, readOnlyTool("api_contract_get", "Get current UNS API request/response contracts and reachable schema definitions from the shared catalog. Multiple operation names/IDs are supported; write documentation does not enable write tools."), func(ctx context.Context, _ *mcp.CallToolRequest, input ContractInput) (*mcp.CallToolResult, any, error) {
		value, err := s.contracts(contextx.WithSubject(ctx, subject), input)
		return finish(value, err)
	})
	mcp.AddTool(server, readOnlyTool("api_examples_get", "Get real curl, @tier0/sdk configureClient/getClient().post, or tier0 CLI api examples for multiple current operations. Keys/IDs/URLs are placeholders; no SDK v2 convenience methods are invented."), func(ctx context.Context, _ *mcp.CallToolRequest, input ExampleInput) (*mcp.CallToolResult, any, error) {
		value, err := s.examples(contextx.WithSubject(ctx, subject), input)
		return finish(value, err)
	})
	for _, doc := range s.documents {
		if !permitted(contextx.WithSubject(context.Background(), subject), doc.Permissions...) {
			continue
		}
		d := doc
		uri := "tier0://mcp/docs/" + d.ID
		server.AddResource(&mcp.Resource{URI: uri, Name: d.Title, MIMEType: "text/markdown", Description: "Version-bundled documentation; not live business data"}, func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			if !permitted(contextx.WithSubject(ctx, subject), d.Permissions...) {
				return nil, fmt.Errorf("document is not available")
			}
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "text/markdown", Text: d.Content}}}, nil
		})
	}
}

func (s *Service) search(ctx context.Context, input SearchInput) (any, error) {
	if !permitted(ctx, "uns.read") {
		return nil, &unsv2.Fault{Code: "FORBIDDEN", Message: "Read permission required"}
	}
	if input.Language != "" && input.Language != "zh-CN" && input.Language != "en-US" || len(input.Query) > 256 || strings.TrimSpace(input.Query) == "" || input.Limit < 0 || input.Limit > 10 {
		return nil, &unsv2.Fault{Code: "INVALID_REQUEST", Message: "Keywords, optional zh-CN/en-US and limit 1..10 required"}
	}
	if input.Limit == 0 {
		input.Limit = 5
	}
	type hit struct {
		document Document
		score    int
	}
	hits := []hit{}
	words := strings.Fields(strings.ToLower(input.Query))
	documents := append([]Document{}, s.documents...)
	for _, d := range s.skills.Documents {
		if d.MIMEType == "text/markdown" && !strings.HasPrefix(d.URI, "tier0://mcp/docs/") {
			documents = append(documents, Document{ID: d.URI, URI: d.URI, Title: d.Title, Language: "zh-CN", Permissions: d.Permissions, Content: d.Content})
		}
	}
	for _, doc := range documents {
		if input.Language != "" && doc.Language != input.Language || !permitted(ctx, doc.Permissions...) {
			continue
		}
		score := 0
		for _, word := range words {
			if strings.Contains(strings.ToLower(doc.Title), word) {
				score += 5
			}
			score += strings.Count(strings.ToLower(doc.Content), word)
		}
		if score > 0 {
			hits = append(hits, hit{doc, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > input.Limit {
		hits = hits[:input.Limit]
	}
	out := []map[string]any{}
	for _, h := range hits {
		content := h.document.Content
		if utf8.RuneCountInString(content) > 1200 {
			content = string([]rune(content)[:1200]) + "…"
		}
		uri := h.document.URI
		if uri == "" {
			uri = "tier0://mcp/docs/" + h.document.ID
		}
		out = append(out, map[string]any{"id": h.document.ID, "title": h.document.Title, "language": h.document.Language, "excerpt": content, "uri": uri, "readTool": "skills_read", "score": h.score})
	}
	return map[string]any{"productVersion": s.version, "source": "bundled", "hits": out}, nil
}

type operation struct {
	Response    string         `json:"response"`
	Summary     string         `json:"summary"`
	Example     map[string]any `json:"example"`
	Name        string         `json:"name"`
	Handler     string         `json:"handler"`
	Suffix      string         `json:"suffix"`
	PathParams  []string       `json:"pathParams,omitempty"`
	Request     string         `json:"request"`
	OpenAPIOnly bool           `json:"openapiOnly,omitempty"`
	CoreOnly    bool           `json:"coreOnly,omitempty"`
}
type contractCatalog struct {
	Operations []operation    `json:"operations"`
	Schemas    map[string]any `json:"schemas"`
}

func catalog() contractCatalog {
	return bundledCatalog
}

var bundledCatalog = func() contractCatalog {
	var c contractCatalog
	_ = json.Unmarshal([]byte(unsv2.ContractJSON), &c)
	return c
}()

func findOperation(c contractCatalog, name string) (operation, bool) {
	for _, o := range c.Operations {
		if name == o.Name || name == o.Handler || name == o.Suffix || name == strings.TrimPrefix(o.Suffix, "/uns/") || name == resourceToolName(o.Name) || !o.CoreOnly && name == "/openapi/v2"+o.Suffix || !o.OpenAPIOnly && name == "/api/core/v2"+o.Suffix {
			return o, true
		}
	}
	return operation{}, false
}

func modelPermission(kind string) string {
	switch kind {
	case "source", "source_state":
		return "uns.source"
	case "stream", "source_plan", "source_version":
		return "uns.stream"
	case "app":
		return "uns.app.read"
	case "directory", "topic", "collection", "":
		return "uns.read"
	default:
		return ""
	}
}

func (s *Service) contracts(ctx context.Context, input ContractInput) (any, error) {
	value, err := s.modelContracts(ctx, input)
	if err != nil {
		return nil, err
	}
	return pageContract(value, input)
}

func schemaClosure(schemas map[string]any, name string) map[string]any {
	out := map[string]any{}
	var visit func(any)
	visit = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, item := range v {
				if key == "$ref" {
					ref := strings.TrimPrefix(stringValue(item), "#/components/schemas/")
					if _, ok := out[ref]; !ok {
						if schema, ok := schemas[ref]; ok {
							out[ref] = schema
							visit(schema)
						}
					}
				} else {
					visit(item)
				}
			}
		case []any:
			for _, item := range v {
				visit(item)
			}
		}
	}
	if schema, ok := schemas[name]; ok {
		out[name] = schema
		visit(schema)
	}
	return out
}
