package unsv2

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"backend/internal/contextx"
	"backend/internal/domain/common"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
)

// App identity comes exclusively from the authenticated key, never request JSON.
func appIdentity(ctx context.Context) string {
	subject, _ := contextx.SubjectFrom(ctx)
	if subject.APIKeyUsageType != repo.APIKeyUsageAppData {
		return ""
	}
	return subject.APIKeyAppID
}

// AppPlatformPermissions is the explicit platform capability allowlist. It is
// separate from Collection grants and never implies administrative access.
func AppPlatformPermissions() []string {
	return append([]string{"openapi.base", "platform.users.read", "platform.projects.members.read", "notifications.send", "notifications.read", "asset.file.upload", "asset.file.download", "asset.file.delete", "control.read", "control.execute", "control.write"}, AppStreamPermissions()...)
}

// CheckAppPlatformPermission checks the authenticated key/user intersection on
// every request and reloads App grants so revocation does not wait for key expiry.
func CheckAppPlatformPermission(ctx context.Context, permission string) error {
	if !contains(AppPlatformPermissions(), permission) {
		return problem("FORBIDDEN", "unsupported App platform permission")
	}
	if err := require(ctx, permission); err != nil {
		return err
	}
	subject, _ := contextx.SubjectFrom(ctx)
	if subject.MCPRestricted {
		return problem("FORBIDDEN", "MCP has no consent for App platform permissions")
	}
	if subject.APIKeyUsageType != repo.APIKeyUsageAppData {
		return nil
	}
	if subject.APIKeyAppID == "" {
		return problem("FORBIDDEN", "App identity is required")
	}
	reserved, err := repo.NewProjectRepo(ctx).ProjectAppPrincipalReserved(ctx, subject.APIKeyAppID)
	if err != nil {
		return err
	}
	if reserved {
		return hostedAppStreamAccess(ctx, subject.APIKeyAppID, permission)
	}
	if contains(AppStreamPermissions(), permission) || strings.HasPrefix(permission, "control.") {
		if err := nativeAppStreamAdmission(ctx, subject.APIKeyAppID); err != nil {
			return err
		}
		row, err := meta(ctx, "app", subject.APIKeyAppID, false)
		if err != nil {
			return err
		}
		policy, err := nativeAppPolicy(ctx, row)
		if err != nil {
			return err
		}
		if policy != nil {
			if policy.Status != "active" || !contains(policy.PlatformPermissions, permission) {
				return problem("FORBIDDEN", "App has no current Stream grant")
			}
			return nil
		}
	}
	_, app, err := appDefinition(ctx, subject.APIKeyAppID)
	if err != nil {
		return err
	}
	if !contains(app.PlatformPermissions, permission) {
		return problem("FORBIDDEN", "App has no current grant for this platform operation")
	}
	return nil
}

func appDefinition(ctx context.Context, id string) (repo.UnsMeta, AppData, error) {
	return appDefinitionFor(ctx, id, false)
}

func appDefinitionFor(ctx context.Context, id string, credentials bool) (repo.UnsMeta, AppData, error) {
	// Hosted identities must not acquire external-App authority from a legacy
	// or imported metadata row. Their policy path requires live App facts.
	reserved, err := repo.NewProjectRepo(ctx).ProjectAppPrincipalReserved(ctx, id)
	if err != nil {
		return repo.UnsMeta{}, AppData{}, err
	}
	if reserved {
		return repo.UnsMeta{}, AppData{}, problem("FORBIDDEN", "Project App identity requires a hosted policy")
	}
	row, err := meta(ctx, "app", id, false)
	var app AppData
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return row, app, err
	}
	if err != nil || json.Unmarshal(row.Data, &app) != nil || app.SchemaVersion != 1 || (app.Status != "" && app.Status != "active" && app.Status != "disabled") {
		return row, app, problem("FORBIDDEN", "App is unavailable or disabled")
	}
	policy, err := nativeAppPolicy(ctx, row)
	if err != nil {
		return row, app, err
	}
	if credentials && policy != nil {
		installation, err := repo.NewAppInstallationRepo(ctx).Get(ctx, id)
		if err != nil {
			return row, app, err
		}
		if installation.Stage == "uninstalled" {
			return row, app, problem("FORBIDDEN", "App is uninstalled")
		}
	}
	if app.Status == "disabled" {
		if !credentials || policy == nil || policy.Status != "active" || len(policy.PlatformPermissions) == 0 {
			return row, app, problem("FORBIDDEN", "App data access is disabled")
		}
		app.Collections = []Grant{}
		app.PlatformPermissions = nil
	}
	if policy != nil {
		permissions := []string{}
		for _, key := range app.PlatformPermissions {
			if !contains(AppStreamPermissions(), key) && !strings.HasPrefix(key, "control.") {
				permissions = append(permissions, key)
			}
		}
		if policy.Status == "active" {
			permissions = append(permissions, policy.PlatformPermissions...)
		}
		app.PlatformPermissions = permissions
	}
	return row, app, nil
}

func grantResource(permission string) string {
	switch permission {
	case "record:read", "sample:read":
		return "uns.read"
	case "record:write", "sample:write":
		return "uns.write"
	case "record:delete":
		return "uns.delete"
	}
	return ""
}

func appCollectionAccess(ctx context.Context, collectionID string, permissions ...string) error {
	if subject, _ := contextx.SubjectFrom(ctx); subject.APIKeyUsageType == repo.APIKeyUsageAppData && subject.APIKeyAppID == "" {
		return problem("FORBIDDEN", "App identity is required")
	}
	if appIdentity(ctx) == "" {
		return nil
	}
	_, app, err := appDefinition(ctx, appIdentity(ctx))
	if err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, grant := range app.Collections {
		if grant.CollectionID == collectionID {
			for _, permission := range grant.Permissions {
				allowed[permission] = true
			}
		}
	}
	for _, permission := range permissions {
		if !allowed[permission] || require(ctx, grantResource(permission)) != nil {
			return problem("FORBIDDEN", "App has no permission for this Collection operation")
		}
	}
	return nil
}

func appMutationAccess(ctx context.Context, c Change) error {
	if subject, _ := contextx.SubjectFrom(ctx); subject.APIKeyUsageType == repo.APIKeyUsageAppData && subject.APIKeyAppID == "" {
		return problem("FORBIDDEN", "App identity is required")
	}
	if appIdentity(ctx) == "" {
		return nil
	}
	if c.Kind != "" || c.ManagedBy != nil || c.CollectionID.Ref != "" {
		return problem("FORBIDDEN", "App keys can only write data in granted Collections")
	}
	permission := ""
	switch c.Action {
	case "record.create", "record.replace", "record.modify", "record.update", "record.restore":
		permission = "record:write"
	case "record.delete":
		permission = "record:delete"
	case "sample.append":
		permission = "sample:write"
	default:
		return problem("FORBIDDEN", "App keys cannot change platform configuration")
	}
	return appCollectionAccess(ctx, c.CollectionID.ID, permission)
}

func appQueryAccess(ctx context.Context, operation string, q Query) error {
	if appIdentity(ctx) == "" {
		return nil
	}
	if _, _, err := appDefinition(ctx, appIdentity(ctx)); err != nil {
		return err
	}
	switch operation {
	case "jobs":
		return nil // Job query checks both identity and every operation's current grant.
	case "records":
		permissions := []string{"record:read"}
		if q.IncludeLatestSample {
			permissions = append(permissions, "sample:read")
		}
		return appCollectionAccess(ctx, q.CollectionID, permissions...)
	case "samples":
		return appCollectionAccess(ctx, q.CollectionID, "sample:read")
	case "collections":
		if q.Mode != "detail" {
			return problem("FORBIDDEN", "Use App context to discover granted Collections")
		}
		permissions := []string{"record:read"}
		if contains(q.Include, "latestSamples") || q.SelectedRecord != nil && q.SelectedRecord.History != nil {
			permissions = append(permissions, "sample:read")
		}
		return appCollectionAccess(ctx, q.CollectionID, permissions...)
	case "topics":
		row, err := meta(ctx, "topic", q.TopicID, false)
		if err != nil {
			return problem("FORBIDDEN", "Topic is unavailable to this App")
		}
		var doc unsmeta.Document
		if json.Unmarshal(row.Data, &doc) != nil {
			return problem("FORBIDDEN", "Invalid Topic")
		}
		if doc.CollectionID != "" {
			return appCollectionAccess(ctx, doc.CollectionID, "record:read")
		}
		if len(doc.Fields) == 0 {
			return problem("FORBIDDEN", "Topic has no granted Collection")
		}
		for _, field := range doc.Fields {
			if err := appCollectionAccess(ctx, field.CollectionID, field.Kind+":read"); err != nil {
				return err
			}
		}
		return nil
	default:
		return problem("FORBIDDEN", "This operation is not available to App keys")
	}
}

func (s *Service) appContext(ctx context.Context) (any, error) {
	if appIdentity(ctx) == "" {
		return nil, problem("FORBIDDEN", "An App key is required")
	}
	row, app, err := appDefinitionFor(ctx, appIdentity(ctx), true)
	if err != nil {
		return nil, err
	}
	collections := []AppCollectionContext{}
	for _, grant := range app.Collections {
		permissions := []string{}
		for _, p := range grant.Permissions {
			if key := grantResource(p); key != "" && require(ctx, key) == nil {
				permissions = append(permissions, p)
			}
		}
		if len(permissions) == 0 {
			continue
		}
		doc, err := readDefinition(ctx, grant.CollectionID)
		if err != nil {
			return nil, err
		}
		var sampleSchema *unsmeta.Schema
		if contains(permissions, "sample:read") {
			sampleSchema = doc.SampleSchema
		}
		indexes := []unsmeta.IndexSpec{}
		for _, index := range doc.Indexes {
			if index.TargetKind() == "records" || contains(permissions, "sample:read") {
				indexes = append(indexes, index)
			}
		}
		collections = append(collections, AppCollectionContext{Alias: grant.Alias, CollectionID: grant.CollectionID, Permissions: permissions, RecordSchema: doc.RecordSchema, SampleSchema: sampleSchema, Indexes: indexes})
	}
	platformPermissions := []string{}
	for _, permission := range app.PlatformPermissions {
		if contains(AppPlatformPermissions(), permission) && require(ctx, permission) == nil {
			platformPermissions = append(platformPermissions, permission)
		}
	}
	return AppContext{AppID: row.ID, Name: app.Name, Revision: row.Revision, Collections: collections, PlatformPermissions: platformPermissions}, nil
}

type AppCollectionContext struct {
	Alias        string              `json:"alias"`
	CollectionID string              `json:"collectionId"`
	Permissions  []string            `json:"permissions"`
	RecordSchema *unsmeta.Schema     `json:"recordSchema"`
	SampleSchema *unsmeta.Schema     `json:"sampleSchema,omitempty"`
	Indexes      []unsmeta.IndexSpec `json:"indexes,omitempty"`
}
type AppContext struct {
	AppID               string                 `json:"appId"`
	Name                string                 `json:"name"`
	Revision            int64                  `json:"revision"`
	Collections         []AppCollectionContext `json:"collections"`
	PlatformPermissions []string               `json:"platformPermissions"`
}
type AppKeyCreated struct {
	KeyID        string   `json:"keyId"`
	AppID        string   `json:"appId"`
	APIKey       string   `json:"apiKey"`
	ResourceKeys []string `json:"resourceKeys"`
}
type AppKeyRevoked struct {
	KeyID   string `json:"keyId"`
	Revoked bool   `json:"revoked"`
}

type AppKeyRequest struct {
	AppID string `json:"appId"`
	Name  string `json:"name,omitempty"`
	KeyID string `json:"keyId,omitempty"`
}

func (s *Service) appKey(ctx context.Context, operation string, request AppKeyRequest) (any, error) {
	if appIdentity(ctx) != "" {
		return nil, problem("FORBIDDEN", "App keys cannot issue credentials")
	}
	if err := require(ctx, "uns.app.manage", "uns.app.credentials"); err != nil {
		return nil, err
	}
	if !unsmeta.ValidID(request.AppID) || len(request.AppID) > repo.APIKeyAppIDMaxLen {
		return nil, invalid("appId", "App identity must fit the API key identity")
	}
	subject, _ := contextx.SubjectFrom(ctx)
	keys := repo.NewAPIKeyRepo(ctx)
	if operation == "appKeyRevoke" {
		id, err := strconv.ParseInt(request.KeyID, 10, 64)
		if err != nil || id <= 0 || request.Name != "" {
			return nil, invalid("keyId", "valid key ID required")
		}
		key, err := keys.ValidateAPIKeyByIDDetail(ctx, id)
		if err != nil || key.UsageType != repo.APIKeyUsageAppData || key.OwnerID != subject.UserID || key.AppID != request.AppID {
			return nil, problem("NOT_FOUND", "App key not found")
		}
		if err := keys.DeleteAppDataKey(ctx, id, subject.UserID, request.AppID); err != nil {
			return nil, err
		}
		return AppKeyRevoked{Revoked: true, KeyID: request.KeyID}, nil
	}
	if strings.TrimSpace(request.Name) != request.Name || request.Name == "" || common.ExceedMaxLength(request.Name, common.APIKeyNameMaxLen) || request.KeyID != "" {
		return nil, invalid("name", "valid credential name required")
	}
	// Installation/rotation may wait for an App lock. Recheck the issuer at
	// issuance, retaining the original request's ceiling even if roles expanded.
	if s.refresh == nil {
		return nil, problem("TEMPORARILY_UNAVAILABLE", "current credential issuer authority is unavailable")
	}
	current, err := s.refresh(ctx, subject)
	if err != nil {
		return nil, problem("FORBIDDEN", "App credential issuer authority was revoked")
	}
	resourcesNow := map[string]struct{}{}
	for key := range subject.ResourceKeys {
		if contextx.HasResource(current, key) {
			resourcesNow[key] = struct{}{}
		}
	}
	current.ResourceKeys = resourcesNow
	ctx = contextx.WithSubject(ctx, current)
	if err := require(ctx, "uns.app.manage", "uns.app.credentials"); err != nil {
		return nil, err
	}
	_, app, err := appDefinitionFor(ctx, request.AppID, true)
	if err != nil {
		return nil, err
	}
	// Grant only capabilities the issuer currently has; no default full-access profile.
	resources := []string{"uns.read"}
	for _, grant := range app.Collections {
		for _, permission := range grant.Permissions {
			key := grantResource(permission)
			if key == "" {
				return nil, invalid("permissions", "unknown App permission")
			}
			if !contains(resources, key) {
				resources = append(resources, key)
			}
		}
	}
	if err := require(ctx, resources...); err != nil {
		return nil, err
	}
	for _, permission := range app.PlatformPermissions {
		if !contains(AppPlatformPermissions(), permission) {
			return nil, invalid("platformPermissions", "unknown App platform permission")
		}
		if subject.MCPRestricted {
			return nil, problem("FORBIDDEN", "MCP cannot issue App platform permissions")
		}
		if err := require(ctx, permission); err != nil {
			return nil, err
		}
		if !contains(resources, permission) {
			resources = append(resources, permission)
		}
	}
	key, raw, err := keys.CreateAppDataKey(ctx, request.Name, request.AppID, subject.UserID, common.PlatformWorkspaceID, resources)
	if err != nil {
		return nil, classify(err)
	}
	return AppKeyCreated{KeyID: strconv.FormatInt(key.ID, 10), AppID: request.AppID, APIKey: raw, ResourceKeys: resources}, nil
}

// DiscoveryAccess validates App identity without introducing a platform grant.
// An App sees Namespace only when it has a current Collection grant; native
// Source/Stream management is not part of the existing App operation allowlist.
func DiscoveryAccess(ctx context.Context) (namespace bool, appKey bool, err error) {
	subject, _ := contextx.SubjectFrom(ctx)
	if subject.APIKeyUsageType != repo.APIKeyUsageAppData {
		return contextx.HasResource(subject, "uns.read"), false, nil
	}
	if subject.APIKeyAppID == "" {
		return false, true, problem("FORBIDDEN", "App identity is required")
	}
	_, app, err := appDefinition(ctx, subject.APIKeyAppID)
	if err != nil {
		return false, true, err
	}
	for _, grant := range app.Collections {
		for _, permission := range grant.Permissions {
			if (permission == "record:read" || permission == "sample:read") && require(ctx, "uns.read") == nil {
				return true, true, nil
			}
		}
	}
	return false, true, nil
}
