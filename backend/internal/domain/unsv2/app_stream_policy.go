package unsv2

import (
	"context"
	"encoding/json"
	"errors"

	"backend/internal/contextx"
	"backend/internal/repo"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type appStreamPolicyRequest struct {
	AppID               string                             `json:"appId"`
	ExpectedRevision    int64                              `json:"expectedRevision"`
	Status              string                             `json:"status"`
	PlatformPermissions []string                           `json:"platformPermissions"`
	MessageTopics       *[]AppStreamMessageTopicPermission `json:"messageTopics,omitempty"`
	IdempotencyKey      string                             `json:"idempotencyKey"`
	DryRun              bool                               `json:"dryRun"`
}

type appStreamPolicyView struct {
	AppID               string                            `json:"appId"`
	Revision            int64                             `json:"revision"`
	Version             string                            `json:"version"`
	Status              string                            `json:"status"`
	PlatformPermissions []string                          `json:"platformPermissions"`
	MessageTopics       []AppStreamMessageTopicPermission `json:"messageTopics,omitempty"`
	AuthorityStatus     string                            `json:"authorityStatus"`
	AppName             string                            `json:"appName"`
}

func appStreamPolicyManager(ctx context.Context) error {
	actor, ok := contextx.SubjectFrom(ctx)
	if !ok || actor.UserID <= 0 || actor.MCPRestricted || actor.FleetTask != nil || actor.APIKeyUsageType == repo.APIKeyUsageAppData || actor.APIKeyAppID != "" {
		return problem("FORBIDDEN", "App Stream policy requires a platform manager")
	}
	session := actor.AuthType == "session" && actor.APIKeyID == 0
	key := actor.AuthType == "apiKey" && actor.APIKeyID > 0 && actor.APIKeyPermission == repo.APIKeyPermissionFull && (actor.KeyType == "personal" || actor.KeyType == "agent")
	if !session && !key {
		return problem("FORBIDDEN", "App Stream policy requires a manager session or full_access personal/Agent key")
	}
	return require(ctx, "uns.write", "stream.requests.manage")
}

func (s *Service) refreshAppStreamPolicyManager(ctx context.Context) (context.Context, error) {
	if err := appStreamPolicyManager(ctx); err != nil {
		return ctx, err
	}
	if s.refresh == nil {
		return ctx, problem("TEMPORARILY_UNAVAILABLE", "current manager authority is unavailable")
	}
	actor, _ := contextx.SubjectFrom(ctx)
	current, err := s.refresh(ctx, actor)
	if err != nil {
		return ctx, problem("FORBIDDEN", "App Stream policy manager authority was revoked")
	}
	checked := contextx.WithSubject(ctx, current)
	return checked, appStreamPolicyManager(checked)
}

func isAppStreamPolicyChange(c Change) bool {
	if c.Kind != "app" || c.Action != "meta.resourcePatch" {
		return false
	}
	var patch resourcePatch
	return json.Unmarshal(c.Data, &patch) == nil && patch.Operation == "appStreamPolicyUpdate"
}

func (s *Service) refreshAppStreamPolicyBatch(ctx context.Context, items []Change) (context.Context, error) {
	for _, item := range items {
		if isAppStreamPolicyChange(item) {
			return s.refreshAppStreamPolicyManager(ctx)
		}
	}
	return ctx, nil
}

type appStreamPolicyChecksKey struct{}
type appStreamPolicyCheck struct {
	AppID       string
	Permissions []string
	NeedApp     bool
}

func (s *Service) checkAppStreamPolicyWrite(ctx context.Context, check appStreamPolicyCheck) error {
	checked, err := s.refreshAppStreamPolicyManager(ctx)
	if err != nil {
		return err
	}
	if err := require(checked, check.Permissions...); err != nil {
		return err
	}
	if check.NeedApp {
		installation, err := repo.NewAppInstallationRepo(checked).Get(checked, check.AppID)
		if err != nil || installation.ReleaseID == "" || installation.DesiredState != "running" || installation.Status != "running" || installation.Stage == "uninstalled" {
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && !errors.Is(err, repo.ErrNotFound) {
				return err
			}
			return &Fault{Code: "APP_NOT_READY", Message: "native App is not ready", Reason: "native App is not ready", Repair: "Retry after the App finishes initialization", Retryable: true}
		}
	}
	return nil
}

func (s *Service) recheckAppStreamPolicyWrites(ctx context.Context) error {
	checks, _ := ctx.Value(appStreamPolicyChecksKey{}).(*[]appStreamPolicyCheck)
	if checks != nil {
		for _, check := range *checks {
			if err := s.checkAppStreamPolicyWrite(ctx, check); err != nil {
				return err
			}
		}
	}
	return nil
}

func readNativeAppStreamPolicy(ctx context.Context, id string) (repo.UnsMeta, *AppStreamPolicy, repo.AppInstallation, error) {
	var row repo.UnsMeta
	var installation repo.AppInstallation
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil || parsed.String() != id {
		return row, nil, installation, invalid("appId", "canonical App UUID required")
	}
	reserved, err := repo.NewProjectRepo(ctx).ProjectAppPrincipalReserved(ctx, id)
	if err != nil {
		return row, nil, installation, err
	}
	if reserved {
		return row, nil, installation, problem("FORBIDDEN", "legacy Project App requires its own authority")
	}
	installation, factErr := repo.NewAppInstallationRepo(ctx).Get(ctx, id)
	if factErr != nil && !errors.Is(factErr, gorm.ErrRecordNotFound) && !errors.Is(factErr, repo.ErrNotFound) {
		return row, nil, installation, factErr
	}
	row, err = repo.NewUnsMetaRepo(ctx).Get(ctx, id, true, "app")
	if errors.Is(err, repo.ErrNotFound) {
		if factErr != nil {
			return row, nil, installation, problem("NOT_FOUND", "native App not found")
		}
		return repo.UnsMeta{}, nil, installation, nil
	}
	if err != nil {
		return row, nil, installation, err
	}
	if row.DeletedAt != 0 || row.MountID != nil {
		return row, nil, installation, problem("FORBIDDEN", "archived or mounted App policy is read-only")
	}
	policy, err := decodeNativeAppPolicy(row)
	if err != nil {
		return row, nil, installation, err
	}
	// A previously managed policy can still be revoked if its installation is
	// gone. External registrations without that server-owned marker are denied.
	if factErr != nil && policy == nil {
		return row, nil, installation, problem("FORBIDDEN", "App is not a native installation")
	}
	var data AppData
	if json.Unmarshal(row.Data, &data) != nil || data.SchemaVersion != 1 || (data.Status != "" && data.Status != "active" && data.Status != "disabled") {
		return row, nil, installation, problem("FORBIDDEN", "invalid App definition")
	}
	if policy == nil && (data.Status == "" || data.Status == "active") {
		permissions := []string{}
		for _, key := range data.PlatformPermissions {
			if contains(AppStreamPermissions(), key) {
				permissions = append(permissions, key)
			}
		}
		if len(permissions) > 0 {
			policy = &AppStreamPolicy{Version: hostedAppStreamVersion, Status: "active", PlatformPermissions: permissions}
		}
	}
	return row, policy, installation, nil
}

func (s *Service) executeAppStreamPolicy(ctx context.Context, operation ResourceOperation, raw []byte) (any, error) {
	checked, err := s.refreshAppStreamPolicyManager(ctx)
	if err != nil {
		return nil, err
	}
	fields, err := validateResourceRequest(operation, raw)
	if err != nil {
		return nil, err
	}
	var request appStreamPolicyRequest
	if err := Decode(raw, &request); err != nil {
		return nil, err
	}
	if operation.Name == "appStreamPolicyGet" {
		row, policy, installation, err := readNativeAppStreamPolicy(checked, request.AppID)
		if err != nil {
			return nil, err
		}
		view := appStreamPolicyView{AppID: request.AppID, Revision: row.Revision, Version: hostedAppStreamVersion, Status: "disabled", PlatformPermissions: []string{}, AuthorityStatus: "notFound", AppName: installation.Name}
		if policy != nil {
			view.Status, view.PlatformPermissions, view.MessageTopics = policy.Status, policy.PlatformPermissions, policy.MessageTopics
		}
		if installation.AppID != "" {
			view.AuthorityStatus = "notReady"
			if installation.ReleaseID != "" && installation.DesiredState == "running" && installation.Status == "running" && installation.Stage != "uninstalled" {
				view.AuthorityStatus = "available"
			}
		}
		return view, nil
	}
	patch, _ := json.Marshal(resourcePatch{Operation: operation.Name, Fields: fields})
	change := Change{OpID: operation.Name, Action: "meta.resourcePatch", Kind: "app", ID: Identity{ID: request.AppID}, ExpectedRevision: request.ExpectedRevision, Data: patch}
	return s.Apply(checked, Batch{Mode: "sync", IdempotencyKey: request.IdempotencyKey, DryRun: request.DryRun, Items: []Change{change}})
}

func (s *Service) writeAppStreamPolicy(ctx context.Context, tx *gorm.DB, change Change) (Result, error) {
	result := Result{OpID: change.OpID, Action: change.Action, Kind: "app", ID: change.ID.ID, Status: "updated"}
	var patch resourcePatch
	if err := Decode(change.Data, &patch); err != nil {
		return result, err
	}
	raw, _ := json.Marshal(patch.Fields)
	operation, ok := FindResourceOperation("appStreamPolicyUpdate")
	if !ok {
		return result, problem("UNSUPPORTED_OPERATION", "App policy contract unavailable")
	}
	if _, err := validateResourceRequest(operation, raw); err != nil {
		return result, err
	}
	var request appStreamPolicyRequest
	if err := Decode(raw, &request); err != nil {
		return result, err
	}
	if request.AppID != change.ID.ID || request.ExpectedRevision != change.ExpectedRevision {
		return result, invalid("appId/expectedRevision", "policy identity must match the mutation")
	}
	row, old, _, err := readNativeAppStreamPolicy(ctx, request.AppID)
	if err != nil {
		return result, err
	}
	if row.Revision != request.ExpectedRevision {
		return result, problem("VERSION_CONFLICT", "App policy revision changed")
	}
	next := AppStreamPolicy{Version: hostedAppStreamVersion, Status: request.Status, PlatformPermissions: request.PlatformPermissions}
	if old != nil {
		next.MessageTopics = old.MessageTopics
	}
	if request.MessageTopics != nil {
		next.MessageTopics = *request.MessageTopics
	}
	raw, _ = json.Marshal(next)
	policy, err := decodeAppStreamPolicy(raw)
	if err != nil {
		return result, invalid("streamInteraction", "invalid Stream policy; write requires invoke, topics require matching permissions, and disabled revokes all")
	}
	check := appStreamPolicyCheck{AppID: request.AppID, NeedApp: row.ID == "" || policy.Status == "active" && (old == nil || old.Status != "active")}
	previous := map[string]bool{}
	oldTopics := map[string]AppStreamMessageTopicPermission{}
	if old != nil && old.Status == "active" {
		for _, key := range old.PlatformPermissions {
			previous[key] = true
		}
		for _, topic := range old.MessageTopics {
			oldTopics[topic.Topic] = topic
		}
	}
	add := func(key string) {
		if !contains(check.Permissions, key) {
			check.Permissions = append(check.Permissions, key)
		}
	}
	for _, key := range policy.PlatformPermissions {
		if !previous[key] {
			add(key)
			check.NeedApp = true
		}
	}
	for _, topic := range policy.MessageTopics {
		prior := oldTopics[topic.Topic]
		if topic.Publish && !prior.Publish {
			add("stream.messages.publish")
			check.NeedApp = true
		}
		if topic.Subscribe && !prior.Subscribe {
			add("stream.messages.subscribe")
			check.NeedApp = true
		}
	}
	// Request management alone cannot alter either side of message authority,
	// including reductions, or clear topics by omitting an unrelated group.
	for _, key := range []string{"stream.messages.publish", "stream.messages.subscribe"} {
		prior, after := []string{}, []string{}
		for _, topic := range oldTopics {
			if key == "stream.messages.publish" && topic.Publish || key == "stream.messages.subscribe" && topic.Subscribe {
				prior = append(prior, topic.Topic)
			}
		}
		for _, topic := range policy.MessageTopics {
			if key == "stream.messages.publish" && topic.Publish || key == "stream.messages.subscribe" && topic.Subscribe {
				after = append(after, topic.Topic)
			}
		}
		if previous[key] != contains(policy.PlatformPermissions, key) || !sameStringSet(prior, after) {
			add(key)
		}
	}
	if err := s.checkAppStreamPolicyWrite(ctx, check); err != nil {
		return result, err
	}
	data := map[string]json.RawMessage{}
	if row.ID != "" {
		if err := json.Unmarshal(row.Data, &data); err != nil {
			return result, err
		}
	} else {
		initial := AppData{Header: Header{SchemaVersion: 1, Name: request.AppID}, Roles: []Role{}, Members: []Member{}, Collections: []Grant{}, Status: "disabled"}
		initial.Navigation.TopicIDs = []string{}
		raw, _ := json.Marshal(initial)
		_ = json.Unmarshal(raw, &data)
	}
	data["streamInteraction"], _ = json.Marshal(policy)
	write := change
	write.Action = "meta.replace"
	if row.ID == "" {
		write.Action = "meta.create"
		result.Status = "created"
	}
	saved, err := writeMeta(tx, write, data)
	if err != nil {
		return result, err
	}
	checks, ok := ctx.Value(appStreamPolicyChecksKey{}).(*[]appStreamPolicyCheck)
	if !ok {
		return result, problem("INTERNAL_ERROR", "App policy transaction guard unavailable")
	}
	*checks = append(*checks, check)
	result.Revision = saved.Revision
	return result, nil
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, value := range a {
		set[value] = true
	}
	for _, value := range b {
		if !set[value] {
			return false
		}
	}
	return true
}
