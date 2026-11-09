package appinstall

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"backend/internal/domain/common"
	"backend/internal/domain/unsv2"
	"backend/internal/repo"
)

type BundleRole struct {
	Key         string `json:"role_key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}
type RolePlanItem struct {
	BundleRole
	ExistingName        string `json:"existingName,omitempty"`
	ExistingDescription string `json:"existingDescription,omitempty"`
	Revision            int64  `json:"revision,omitempty"`
}
type RolePlan struct {
	Items  []RolePlanItem `json:"items"`
	Digest string         `json:"digest"`
}

func readBundleRoles(raw []byte) ([]BundleRole, error) {
	var file struct {
		Roles []struct {
			Key         string          `json:"role_key"`
			Alias       string          `json:"key"`
			Name        json.RawMessage `json:"name"`
			Description string          `json:"description"`
		} `json:"roles"`
	}
	if err := json.Unmarshal(raw, &file); err != nil || file.Roles == nil || len(file.Roles) > 200 {
		return nil, errors.New("Invalid roles.json")
	}
	result := make([]BundleRole, 0, len(file.Roles))
	seen := map[string]bool{}
	for _, r := range file.Roles {
		if r.Key == "" {
			r.Key = r.Alias
		}
		var name string
		if err := json.Unmarshal(r.Name, &name); err != nil {
			var labels map[string]string
			if json.Unmarshal(r.Name, &labels) == nil {
				for _, lang := range []string{"en", "en-US", "zh", "zh-CN"} {
					if labels[lang] != "" {
						name = labels[lang]
						break
					}
				}
			}
		}
		name = strings.TrimSpace(name)
		if !businessKey.MatchString(r.Key) || len(r.Key) > 63 || seen[r.Key] || name == "" || common.ExceedNameLimit(name) || common.ExceedDescLimit(r.Description) {
			return nil, errors.New("Invalid or duplicate role in roles.json")
		}
		seen[r.Key] = true
		result = append(result, BundleRole{r.Key, name, strings.TrimSpace(r.Description)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result, nil
}
func previewRolePlan(ctx context.Context, roles []BundleRole) (*RolePlan, error) {
	if len(roles) == 0 {
		return nil, nil
	}
	keys := make([]string, len(roles))
	for i, r := range roles {
		keys[i] = r.Key
	}
	var existing []repo.BusinessRole
	if err := repo.GetUnsConn(ctx).Select("role_key,name,description,revision").Where("role_key IN ?", keys).Find(&existing).Error; err != nil {
		return nil, err
	}
	registry := map[string]repo.BusinessRole{}
	for _, r := range existing {
		registry[r.Key] = r
	}
	plan := &RolePlan{Items: make([]RolePlanItem, 0, len(roles))}
	for _, r := range roles {
		current := registry[r.Key]
		plan.Items = append(plan.Items, RolePlanItem{r, current.Name, current.Description, current.Revision})
	}
	digest, err := resourceDigest(plan.Items)
	plan.Digest = digest
	return plan, err
}
func importRolePlan(ctx context.Context, roles []BundleRole, request InstallRequest) ([]unsv2.Role, error) {
	if len(roles) == 0 {
		return nil, nil
	}
	db := repo.GetUnsConn(ctx)
	if err := repo.LockBusinessRoles(db); err != nil {
		return nil, err
	}
	plan, err := previewRolePlan(ctx, roles)
	if err != nil {
		return nil, err
	}
	if plan.Digest != request.RolePlanDigest {
		return nil, &requestError{409, "Role plan changed. Preview the package again."}
	}
	rename := map[string]bool{}
	for _, k := range request.RenameRoleKeys {
		rename[k] = true
	}
	result := make([]unsv2.Role, 0, len(roles))
	for _, r := range plan.Items {
		name := r.Name
		if r.Revision == 0 {
			if err = db.Exec("INSERT INTO workspace_business_role(role_key,name,description) VALUES (?,?,?)", r.Key, r.Name, r.Description).Error; err != nil {
				return nil, err
			}
		} else {
			name = r.ExistingName
			if rename[r.Key] && r.ExistingName != r.Name {
				name = r.Name
				delete(rename, r.Key)
			}
			description := r.ExistingDescription
			if r.Description != "" && !strings.Contains("\n"+description+"\n", "\n"+r.Description+"\n") {
				description = strings.TrimSpace(description + "\n" + r.Description)
			}
			if name != r.ExistingName || description != r.ExistingDescription {
				if err = db.Exec("UPDATE workspace_business_role SET name=?,description=?,revision=revision+1 WHERE role_key=?", name, description, r.Key).Error; err != nil {
					return nil, err
				}
			}
		}
		result = append(result, unsv2.Role{ID: r.Key, Name: name})
	}
	if len(rename) > 0 {
		return nil, invalidRequest("Unknown role rename choice")
	}
	return result, nil
}
