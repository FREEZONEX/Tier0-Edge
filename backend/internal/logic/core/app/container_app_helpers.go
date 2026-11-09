package app

import (
	"encoding/json"
	"net/url"
	"strings"

	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/types"
)

const containerAppSiteType = "externalContainer"

func validateContainerAppSave(req *types.ContainerAppSaveReq) error {
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.EntryUrl) == "" {
		return logicx.Error(repo.ErrInvalidArgument)
	}
	parsed, err := url.Parse(strings.TrimSpace(req.EntryUrl))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return logicx.Error(repo.ErrInvalidArgument)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return logicx.Error(repo.ErrInvalidArgument)
	}
	return nil
}

func containerAppManifest(description string, openInPlatform bool) (json.RawMessage, error) {
	raw, err := json.Marshal(map[string]any{
		"description":    strings.TrimSpace(description),
		"openInPlatform": openInPlatform,
	})
	return json.RawMessage(raw), err
}

func containerAppData(item repo.ProjectApp) map[string]any {
	manifest := map[string]any{}
	if len(item.ManifestJSON) > 0 {
		_ = json.Unmarshal(item.ManifestJSON, &manifest)
	}
	description, _ := manifest["description"].(string)
	openInPlatform, _ := manifest["openInPlatform"].(bool)
	enabled := item.Status == "active" || item.Status == "Active"
	return map[string]any{
		"appId":          item.ID,
		"name":           item.Name,
		"description":    description,
		"iconAssetId":    item.IconAssetID,
		"entryUrl":       item.EntryURL,
		"url":            item.EntryURL,
		"openInPlatform": openInPlatform,
		"enabled":        enabled,
		"status":         item.Status,
		"createdAt":      item.CreatedTime,
		"updatedAt":      item.UpdatedTime,
	}
}
