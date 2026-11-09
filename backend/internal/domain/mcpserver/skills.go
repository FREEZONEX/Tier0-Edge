package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"backend/internal/contextx"
	"backend/internal/domain/unsv2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const skillRootURI = "tier0://mcp/skills/tier0-app-builder"

type skillDefinition struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Language    string   `json:"language"`
	ParentID    *string  `json:"parentId"`
	Children    []string `json:"children"`
	URI         string   `json:"uri"`
	References  []string `json:"references"`
}

type skillDocument struct {
	URI         string   `json:"uri"`
	SourcePath  string   `json:"sourcePath"`
	Title       string   `json:"title"`
	MIMEType    string   `json:"mimeType"`
	Permissions []string `json:"permissions"`
	References  []string `json:"references"`
	Content     string   `json:"content"`
	SHA256      string   `json:"sha256"`
	SizeBytes   int      `json:"sizeBytes"`
}

type skillBundle struct {
	SchemaVersion string            `json:"schemaVersion"`
	RootSkillID   string            `json:"rootSkillId"`
	Skills        []skillDefinition `json:"skills"`
	Documents     []skillDocument   `json:"documents"`
	byURI         map[string]skillDocument
	bundleSHA256  string
}

func loadSkillBundle() (*skillBundle, error) {
	raw, err := documentFS.ReadFile("documents/skills.json")
	if err != nil {
		return nil, err
	}
	b := &skillBundle{}
	if err := json.Unmarshal(raw, b); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	b.bundleSHA256 = hex.EncodeToString(digest[:])
	if b.SchemaVersion != "1.0" || b.RootSkillID != "tier0-app-builder" {
		return nil, fmt.Errorf("unsupported MCP skill bundle")
	}
	b.byURI = map[string]skillDocument{}
	for _, d := range b.Documents {
		digest := sha256.Sum256([]byte(d.Content))
		if _, duplicate := b.byURI[d.URI]; duplicate || !strings.HasPrefix(d.URI, "tier0://mcp/") || len(d.Permissions) == 0 || hex.EncodeToString(digest[:]) != d.SHA256 || len(d.Content) != d.SizeBytes {
			return nil, fmt.Errorf("invalid MCP skill resource: %s", d.URI)
		}
		b.byURI[d.URI] = d
	}
	for _, d := range b.Documents {
		for _, ref := range d.References {
			if _, ok := b.byURI[ref]; !ok {
				return nil, fmt.Errorf("missing MCP skill reference: %s", ref)
			}
		}
	}
	for _, skill := range b.Skills {
		if _, ok := b.byURI[skill.URI]; !ok {
			return nil, fmt.Errorf("missing MCP skill entry: %s", skill.ID)
		}
	}
	byID := map[string]skillDefinition{}
	for _, skill := range b.Skills {
		if _, exists := byID[skill.ID]; exists || skill.ID == "" {
			return nil, fmt.Errorf("duplicate/empty MCP skill ID")
		}
		byID[skill.ID] = skill
	}
	if _, ok := byID[b.RootSkillID]; !ok || len(byID) > 64 {
		return nil, fmt.Errorf("invalid MCP skill root/size")
	}
	for _, skill := range b.Skills {
		seen := map[string]bool{skill.ID: true}
		for parent := skill.ParentID; parent != nil; {
			next, ok := byID[*parent]
			if !ok || seen[*parent] {
				return nil, fmt.Errorf("invalid MCP skill parent/cycle")
			}
			seen[*parent] = true
			parent = next.ParentID
		}
		if !seen[b.RootSkillID] {
			return nil, fmt.Errorf("MCP skill is disconnected from root")
		}
		for _, id := range skill.Children {
			child, ok := byID[id]
			if !ok || child.ParentID == nil || *child.ParentID != skill.ID {
				return nil, fmt.Errorf("invalid MCP child skill")
			}
		}
	}
	return b, nil
}

type skillsListInput struct {
	ParentID          string `json:"parentId,omitempty" jsonschema:"Omit to get the complete skill tree; supply a listed skill ID to list its direct children"`
	IncludeReferences bool   `json:"includeReferences,omitempty" jsonschema:"Default false returns compact skill entries. True includes complete reference metadata; parentId scopes it to that skill subtree"`
}
type skillsReadInput struct {
	URI    string `json:"uri" jsonschema:"Exact resource URI returned by skills_list or api_docs_search; no filesystem paths or arbitrary URLs"`
	Offset int    `json:"offset,omitempty" jsonschema:"Zero-based Unicode character offset; continue with the returned nextOffset"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum Unicode characters, default 12000 and maximum 24000; follow nextOffset until truncated is false"`
}

func (s *Service) skillNavigation() map[string]any {
	return map[string]any{"schemaVersion": s.skills.SchemaVersion, "bundleSha256": s.skills.bundleSHA256, "rootSkillId": s.skills.RootSkillID, "rootUri": skillRootURI, "listTool": "skills_list", "readTool": "skills_read", "resourcesSupported": true, "nextAction": "Read the root skill, then only relevant children and references; use live tools/contracts to determine executable capabilities."}
}

func (s *Service) listSkills(ctx context.Context, input skillsListInput) (any, error) {
	if !permitted(ctx, "uns.read") {
		return nil, &unsv2.Fault{Code: "FORBIDDEN", Message: "Read permission required"}
	}
	byID := map[string]skillDefinition{}
	for _, skill := range s.skills.Skills {
		byID[skill.ID] = skill
	}
	parent := input.ParentID
	if parent == "" {
		parent = s.skills.RootSkillID
	}
	if _, ok := byID[parent]; !ok {
		return nil, &unsv2.Fault{Code: "INVALID_REQUEST", Path: "parentId", Message: "Unknown skill ID; call skills_list with an empty object first"}
	}
	var tree func(string) map[string]any
	tree = func(id string) map[string]any {
		skill := byID[id]
		children := []map[string]any{}
		for _, child := range skill.Children {
			children = append(children, tree(child))
		}
		return map[string]any{"id": skill.ID, "uri": skill.URI, "title": skill.Title, "children": children}
	}
	selected := []skillDefinition{}
	for _, skill := range s.skills.Skills {
		if input.ParentID == "" || skill.ParentID != nil && *skill.ParentID == input.ParentID {
			selected = append(selected, skill)
		}
	}
	documents := []map[string]any{}
	wanted := map[string]bool{byID[parent].URI: true}
	for _, skill := range selected {
		wanted[skill.URI] = true
	}
	if input.IncludeReferences {
		var addSkill func(string)
		addSkill = func(id string) {
			wanted[byID[id].URI] = true
			for _, child := range byID[id].Children {
				addSkill(child)
			}
		}
		addSkill(parent)
		var follow func(string)
		follow = func(uri string) {
			for _, ref := range s.skills.byURI[uri].References {
				if !wanted[ref] {
					wanted[ref] = true
					follow(ref)
				}
			}
		}
		for uri := range wanted {
			follow(uri)
		}
	}
	available := 0
	for _, d := range s.skills.Documents {
		if permitted(ctx, d.Permissions...) {
			available++
			if !wanted[d.URI] && !(input.IncludeReferences && parent == s.skills.RootSkillID) {
				continue
			}
			documents = append(documents, map[string]any{"uri": d.URI, "title": d.Title, "mimeType": d.MIMEType, "sizeBytes": d.SizeBytes, "sha256": d.SHA256, "permissions": d.Permissions, "references": d.References})
		}
	}
	toolNames := []string{}
	for _, tool := range s.allowedTools(ctx) {
		toolNames = append(toolNames, tool.name)
	}
	return map[string]any{
		"schemaVersion": s.skills.SchemaVersion, "bundleSha256": s.skills.bundleSHA256,
		"productVersion": s.version, "contractVersion": unsv2.Catalog()["contractVersion"],
		"rootSkillId": s.skills.RootSkillID, "rootUri": skillRootURI, "tree": tree(parent),
		"skills": selected, "documents": documents, "availableDocuments": available,
		"includeReferences": input.IncludeReferences, "toolNames": toolNames,
		"capabilities": unsv2.Catalog()["capabilities"],
		"interfaces": map[string]any{
			"uns":                "api_operations_list -> api_contract_get -> api_examples_get",
			"platformHTTP":       "tier0://mcp/contracts/platform-v2",
			"executionAuthority": "Current tools/list and actual granted permissions; reading documentation grants no execution rights",
		},
		"read": map[string]any{"tool": "skills_read", "resourcesMethod": "resources/read", "offsetUnit": "Unicode characters", "defaultLimit": 12000, "maxLimit": 24000},
	}, nil
}

func (s *Service) readSkillDocument(ctx context.Context, input skillsReadInput) (any, error) {
	if !permitted(ctx, "uns.read") {
		return nil, &unsv2.Fault{Code: "FORBIDDEN", Message: "Read permission required"}
	}
	d, ok := s.skills.byURI[input.URI]
	if !ok || !permitted(ctx, d.Permissions...) {
		return nil, &unsv2.Fault{Code: "NOT_FOUND", Path: "uri", Message: "Resource is unavailable; use an exact authorized URI from skills_list"}
	}
	if input.Limit == 0 {
		input.Limit = 12000
	}
	text := []rune(d.Content)
	if input.Offset < 0 || input.Offset > len(text) || input.Limit < 1 || input.Limit > 24000 {
		return nil, &unsv2.Fault{Code: "INVALID_REQUEST", Path: "offset/limit", Message: "Use a valid Unicode offset and limit between 1 and 24000"}
	}
	end := min(len(text), input.Offset+input.Limit)
	var next any
	if end < len(text) {
		next = end
	}
	return map[string]any{"uri": d.URI, "title": d.Title, "mimeType": d.MIMEType, "productVersion": s.version, "text": string(text[input.Offset:end]), "offset": input.Offset, "characters": end - input.Offset, "totalCharacters": len(text), "sizeBytes": d.SizeBytes, "sha256": d.SHA256, "truncated": next != nil, "nextOffset": next, "references": d.References}, nil
}

func (s *Service) addSkillTools(server *mcp.Server, subject contextx.Subject) {
	mcp.AddTool(server, readOnlyTool("skills_list", "Discover the version-bundled main App-building skill, child skills and complete reference resources. Returns parent/child tree, readable URIs, live tools and interface entrypoints; reading skills grants no write rights."), func(ctx context.Context, _ *mcp.CallToolRequest, input skillsListInput) (*mcp.CallToolResult, any, error) {
		value, err := s.listSkills(contextx.WithSubject(ctx, subject), input)
		return finish(value, err)
	})
	mcp.AddTool(server, readOnlyTool("skills_read", "Read a complete skill, reference, JSON example, SDK source or platform HTTP contract through MCP. Use an exact listed URI; follow nextOffset until truncated=false. Full-content SHA-256 enables verification; no arbitrary file or network access."), func(ctx context.Context, _ *mcp.CallToolRequest, input skillsReadInput) (*mcp.CallToolResult, any, error) {
		value, err := s.readSkillDocument(contextx.WithSubject(ctx, subject), input)
		return finish(value, err)
	})
	for _, document := range s.skills.Documents {
		d := document
		if strings.HasPrefix(d.URI, "tier0://mcp/docs/") || !permitted(contextx.WithSubject(context.Background(), subject), d.Permissions...) {
			continue
		}
		server.AddResource(&mcp.Resource{URI: d.URI, Name: d.Title, MIMEType: d.MIMEType, Description: "Complete version-bundled skill/reference; read via resources/read or skills_read"}, func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			if !permitted(contextx.WithSubject(ctx, subject), d.Permissions...) {
				return nil, fmt.Errorf("resource is unavailable")
			}
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: d.URI, MIMEType: d.MIMEType, Text: d.Content}}}, nil
		})
	}
}
