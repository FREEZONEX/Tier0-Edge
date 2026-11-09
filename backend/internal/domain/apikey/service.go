package apikey

import (
	"context"
	"errors"
	"strings"

	domaincommon "backend/internal/domain/common"
	"backend/internal/repo"
)

type Service struct {
	keys *repo.APIKeyRepo
	iam  *repo.IAMRepo
}

func New(ctx context.Context) *Service {
	return &Service{keys: repo.NewAPIKeyRepo(ctx), iam: repo.NewIAMRepo(ctx)}
}

func (s *Service) List(ctx context.Context, userID int64, keyType, keyword string) (map[string]any, error) {
	items, err := s.keys.ListAPIKeys(ctx, userID, keyType, keyword)
	if err != nil {
		return nil, err
	}
	list := make([]map[string]any, 0, len(items))
	for _, item := range items {
		list = append(list, keyResp(item, ""))
	}
	return map[string]any{"list": list, "total": len(list)}, nil
}

func (s *Service) Create(ctx context.Context, name string, userID int64, permission, usageType, keyType string, resourceKeys []string) (map[string]any, error) {
	name = strings.TrimSpace(name)
	if name == "" || domaincommon.ExceedMaxLength(name, domaincommon.APIKeyNameMaxLen) {
		return nil, ErrInvalid
	}
	permission = repo.NormalizeAPIKeyPermission(permission)
	if len(resourceKeys) == 0 {
		resourceKeys = repo.APIKeyResourceKeysForPermission(permission)
	}
	item, raw, err := s.keys.CreateAPIKey(
		ctx,
		name,
		userID,
		domaincommon.PlatformWorkspaceID,
		permission,
		strings.TrimSpace(usageType),
		strings.TrimSpace(keyType),
		resourceKeys,
	)
	if err != nil {
		return nil, err
	}
	return keyResp(item, raw), nil
}

func (s *Service) Update(ctx context.Context, id, userID int64, name, permission string) (map[string]any, error) {
	name = strings.TrimSpace(name)
	if name != "" && domaincommon.ExceedMaxLength(name, domaincommon.APIKeyNameMaxLen) {
		return nil, ErrInvalid
	}
	permission = repo.NormalizeAPIKeyPermission(permission)
	resourceKeys := repo.APIKeyResourceKeysForPermission(permission)
	// 修改名称或保持原权限档位时，不能丢掉 MCP 等显式选择的只读范围。
	item, err := s.keys.UpdateAPIKey(ctx, id, userID, name, permission, resourceKeys, true)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return keyResp(item, ""), nil
}

func (s *Service) Delete(ctx context.Context, id, userID int64) (map[string]any, error) {
	if err := s.keys.DeleteAPIKey(ctx, id, userID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return map[string]any{"deleted": true}, nil
}

func keyResp(item repo.APIKey, raw string) map[string]any {
	masked := item.KeyPrefix + "..." + item.KeySuffix
	resp := map[string]any{
		"id":           item.ID,
		"name":         item.Name,
		"keyPrefix":    item.KeyPrefix,
		"keySuffix":    item.KeySuffix,
		"maskedKey":    masked,
		"ownerId":      item.OwnerID,
		"ownerID":      item.OwnerID,
		"ownerType":    item.OwnerType,
		"usageType":    item.UsageType,
		"keyType":      item.KeyType,
		"permission":   item.Permission,
		"resourceKeys": item.ResourceKeys,
		"status":       item.Status,
		"lastUsedTime": item.LastUsedTime,
		"createdTime":  item.CreatedTime,
		"updatedTime":  item.UpdatedTime,
	}
	if raw != "" {
		resp["apiKey"] = raw
	}
	return resp
}

var (
	ErrInvalid  = errors.New("invalid api key")
	ErrNotFound = errors.New("api key not found")
)
