package unsv2

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"backend/internal/domain/appstatus"
	"backend/internal/repo"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const hostedAppDataVersion = "cloud-hosted-app-data/v1"
const hostedAppStreamVersion = "cloud-hosted-app-stream/v1"
const maxAppMessageTopics = 32

var appMessageTopicPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,127}$`)

func AppStreamPermissions() []string {
	return []string{"stream.requests.read", "stream.requests.invoke", "stream.requests.write", "stream.messages.publish", "stream.messages.subscribe"}
}

// Native installation identity is the persisted App UUID, not its package name
// or a caller-supplied launch URL. Credential preparation is deliberately not
// gated here: candidates need their environment before becoming healthy.
func nativeAppStreamAdmission(ctx context.Context, id string) error {
	app, err := repo.NewAppInstallationRepo(ctx).Get(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, repo.ErrNotFound) {
		return nil // Independently registered external Apps have no local process.
	}
	if err != nil {
		return err
	}
	if app.ReleaseID == "" || app.DesiredState != "running" || app.Status != "running" || app.Stage == "uninstalled" {
		return problem("FORBIDDEN", "native App is not ready for Stream interaction")
	}
	// A failed replacement can retain a healthy serving release. Stage and
	// OperationID describe the candidate, so neither alone revokes that release.
	return nil
}

// Both bound identity types use the same strict Stream policy parser.
func decodeAppStreamPolicy(raw json.RawMessage) (*AppStreamPolicy, error) {
	var policy AppStreamPolicy
	denied := problem("FORBIDDEN", "invalid App Stream policy")
	if Decode(raw, &policy) != nil || policy.Version != hostedAppStreamVersion ||
		(policy.Status != "active" && policy.Status != "disabled") || policy.PlatformPermissions == nil ||
		(policy.Status == "disabled" && len(policy.PlatformPermissions) != 0) {
		return nil, denied
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for field := range fields {
		if !contains([]string{"version", "status", "platformPermissions", "messageTopics"}, field) {
			return nil, denied
		}
	}
	seen := map[string]bool{}
	for _, key := range policy.PlatformPermissions {
		if seen[key] || !contains(AppStreamPermissions(), key) {
			return nil, denied
		}
		seen[key] = true
	}
	if seen["stream.requests.write"] && !seen["stream.requests.invoke"] {
		return nil, denied
	}
	if len(policy.MessageTopics) > maxAppMessageTopics || policy.Status == "disabled" && len(policy.MessageTopics) != 0 {
		return nil, denied
	}
	var topics []map[string]json.RawMessage
	_ = json.Unmarshal(fields["messageTopics"], &topics)
	seenTopics := map[string]bool{}
	for i, topic := range policy.MessageTopics {
		if !appMessageTopicPattern.MatchString(topic.Topic) || seenTopics[topic.Topic] || !topic.Publish && !topic.Subscribe ||
			topic.Publish && !seen["stream.messages.publish"] || topic.Subscribe && !seen["stream.messages.subscribe"] {
			return nil, denied
		}
		for field := range topics[i] {
			if !contains([]string{"topic", "publish", "subscribe"}, field) {
				return nil, denied
			}
		}
		seenTopics[topic.Topic] = true
	}
	sort.Strings(policy.PlatformPermissions)
	sort.Slice(policy.MessageTopics, func(i, j int) bool { return policy.MessageTopics[i].Topic < policy.MessageTopics[j].Topic })
	return &policy, nil
}

// Native metadata retains its display name, launch path and Collection grants.
// A policy marker is accepted only for the exact persisted installation UUID;
// an external registration cannot opt itself into native authority.
func nativeAppPolicy(ctx context.Context, row repo.UnsMeta) (*AppStreamPolicy, error) {
	policy, err := decodeNativeAppPolicy(row)
	if err != nil || policy == nil {
		return policy, err
	}
	if _, err := repo.NewAppInstallationRepo(ctx).Get(ctx, row.ID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, repo.ErrNotFound) {
			return nil, problem("FORBIDDEN", "Stream policy requires a bound native App identity")
		}
		return nil, err
	}
	return policy, nil
}

func decodeNativeAppPolicy(row repo.UnsMeta) (*AppStreamPolicy, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(row.Data, &fields) != nil {
		return nil, problem("FORBIDDEN", "invalid App definition")
	}
	for field := range fields {
		if strings.EqualFold(field, "dataProfileVersion") || strings.EqualFold(field, "hostedControl") ||
			strings.EqualFold(field, "streamInteraction") && field != "streamInteraction" {
			return nil, problem("FORBIDDEN", "invalid native App policy profile")
		}
	}
	raw, present := fields["streamInteraction"]
	if !present {
		return nil, nil
	}
	var data AppData
	identity, identityErr := uuid.Parse(row.ID)
	if identityErr != nil || identity == uuid.Nil || identity.String() != row.ID || json.Unmarshal(row.Data, &data) != nil || data.SchemaVersion != 1 ||
		(data.Status != "" && data.Status != "active" && data.Status != "disabled") {
		return nil, problem("FORBIDDEN", "invalid native App definition")
	}
	return decodeAppStreamPolicy(raw)
}

// Generic metadata replacements, including bundle updates, preserve the other
// policy. The dedicated policy writer will own changes under the metadata lock.
func preserveAppStreamPolicy(ctx context.Context, old repo.UnsMeta, next *AppData) error {
	var previous *AppStreamPolicy
	if old.ID != "" {
		var err error
		previous, err = nativeAppPolicy(ctx, old)
		if err != nil {
			return err
		}
	}
	if next.StreamInteraction != nil {
		raw, _ := json.Marshal(next.StreamInteraction)
		normalized, err := decodeAppStreamPolicy(raw)
		if err != nil || !reflect.DeepEqual(normalized, previous) {
			return problem("FORBIDDEN", "Stream policy requires its dedicated management operation")
		}
	}
	next.StreamInteraction = previous
	return nil
}

type hostedAppProfile struct {
	SchemaVersion       int               `json:"schemaVersion"`
	Name                string            `json:"name"`
	Description         string            `json:"description,omitempty"`
	DataProfileVersion  string            `json:"dataProfileVersion"`
	Status              string            `json:"status"`
	Collections         []json.RawMessage `json:"collections"`
	Roles               []json.RawMessage `json:"roles"`
	Members             []json.RawMessage `json:"members"`
	PlatformPermissions []string          `json:"platformPermissions,omitempty"`
	Entry               json.RawMessage   `json:"entry,omitempty"`
	Navigation          struct {
		TopicIDs []string `json:"topicIds"`
	} `json:"navigation"`
	StreamInteraction *AppStreamPolicy `json:"streamInteraction,omitempty"`
}

func decodeHostedAppProfile(raw []byte, id string) (hostedAppProfile, error) {
	var profile hostedAppProfile
	denied := problem("FORBIDDEN", "invalid hosted App policy")
	if Decode(raw, &profile) != nil || profile.SchemaVersion != 1 || profile.Name != id || profile.DataProfileVersion != hostedAppDataVersion ||
		(profile.Status != "active" && profile.Status != "disabled") || profile.Description != "" ||
		len(profile.Roles) != 0 || len(profile.Members) != 0 || len(profile.PlatformPermissions) != 0 || len(profile.Navigation.TopicIDs) != 0 ||
		(len(profile.Entry) != 0 && string(profile.Entry) != "null") || profile.Status == "disabled" && len(profile.Collections) != 0 {
		return profile, denied
	}
	// jsonb has unique keys, but Go's decoder accepts case variants. Stored
	// authorization fields must use the exact public spelling at every level.
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for field := range fields {
		if !contains([]string{"schemaVersion", "name", "description", "dataProfileVersion", "status", "collections", "roles", "members", "platformPermissions", "entry", "navigation", "streamInteraction"}, field) {
			return profile, denied
		}
	}
	var navigation map[string]json.RawMessage
	_ = json.Unmarshal(fields["navigation"], &navigation)
	for field := range navigation {
		if field != "topicIds" {
			return profile, denied
		}
	}
	if raw, present := fields["streamInteraction"]; present {
		policy, err := decodeAppStreamPolicy(raw)
		if err != nil {
			return profile, denied
		}
		profile.StreamInteraction = policy
	}
	return profile, nil
}

// 每次准入读取本地应用和项目事实，不能缓存启用状态或使用包里的来源 UUID。
func hostedAppStreamAccess(ctx context.Context, id, permission string) error {
	r := repo.NewProjectRepo(ctx)
	app, err := r.GetProjectAppByPrincipalID(ctx, id)
	if errors.Is(err, repo.ErrNotFound) || errors.Is(err, repo.ErrInvalidArgument) {
		return problem("FORBIDDEN", "hosted App is unavailable")
	}
	if err != nil {
		return err
	}
	project, err := r.GetProject(ctx, app.ProjectID)
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, repo.ErrNotFound) {
		return problem("FORBIDDEN", "hosted App project is unavailable")
	}
	if err != nil {
		return err
	}
	var manifest struct {
		Deploying   bool   `json:"deploying"`
		DeployError string `json:"deploy_error"`
	}
	if app.ProjectID <= 0 || project.Status != "active" || json.Unmarshal(app.ManifestJSON, &manifest) != nil ||
		appstatus.DeployStatus(app.Status, manifest.Deploying, manifest.DeployError) != "success" || app.EntryURL == "" && app.EntryPath == "" {
		return problem("FORBIDDEN", "hosted App is not ready")
	}
	row, err := meta(ctx, "app", id, false)
	if errors.Is(err, repo.ErrNotFound) {
		return problem("FORBIDDEN", "hosted App has no policy")
	}
	if err != nil {
		return err
	}
	profile, err := decodeHostedAppProfile(row.Data, id)
	if err != nil {
		return err
	}
	policy := profile.StreamInteraction
	if policy == nil || policy.Status != "active" || !contains(policy.PlatformPermissions, permission) {
		return problem("FORBIDDEN", "hosted App has no active Stream permission")
	}
	return nil
}
