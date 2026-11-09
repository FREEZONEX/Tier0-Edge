package unsv2

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"backend/internal/contextx"
	"backend/internal/domain/sourcestream"
	"backend/internal/domain/unsmeta"
	"backend/internal/operationcontract"
	"backend/internal/repo"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var requestGrantTokenPattern = regexp.MustCompile(`^cgs1\.[a-f0-9]{64}\.[a-f0-9]{64}$`)

type appRequestGrantInput struct {
	Code                  string   `json:"code"`
	PrincipalKind         string   `json:"principalKind"`
	PrincipalID           string   `json:"principalID"`
	Active                int      `json:"active"`
	Actions               []string `json:"actions"`
	ExpectedSnapshotToken string   `json:"expectedSnapshotToken"`
}

func requestGrantActions(actions []string) bool {
	if len(actions) == 0 || len(actions) > 3 {
		return false
	}
	seen := map[string]bool{}
	for _, action := range actions {
		if seen[action] || !slices.Contains([]string{"stream.requests.read", "stream.requests.invoke", "stream.requests.write"}, action) {
			return false
		}
		seen[action] = true
	}
	return !seen["stream.requests.write"] || seen["stream.requests.invoke"]
}

func (s *Service) authorizeAppRequest(ctx context.Context, appID string, actions []string, targets []repo.ProgramRequestTarget) error {
	actor, ok := contextx.SubjectFrom(ctx)
	if !ok || actor.APIKeyUsageType != repo.APIKeyUsageAppData || actor.APIKeyAppID != appID || s.refresh == nil {
		return sourcestream.ErrForbidden
	}
	current, err := s.refresh(ctx, actor)
	if err != nil {
		return sourcestream.ErrForbidden
	}
	if current.APIKeyID != actor.APIKeyID || current.APIKeyAppID != appID || current.APIKeyUsageType != repo.APIKeyUsageAppData {
		return sourcestream.ErrForbidden
	}
	checked := contextx.WithSubject(ctx, current)
	for _, action := range actions {
		if CheckAppPlatformPermission(checked, action) != nil {
			return sourcestream.ErrForbidden
		}
	}
	err = checkAppRequestData(checked, appID, targets, true)
	var fault *Fault
	if errors.As(err, &fault) && (fault.Code == "FORBIDDEN" || fault.Code == "UNAUTHENTICATED") {
		return sourcestream.ErrForbidden
	}
	return err
}

// The code grant never replaces the App's independent Collection permission.
// Program handles may read/write both records and samples, so authorization
// covers both when that Collection has samples.
func checkAppRequestData(ctx context.Context, appID string, targets []repo.ProgramRequestTarget, keyBound bool) error {
	var app AppData
	needsData := false
	for _, target := range targets {
		needsData = needsData || target.Kind == "data"
	}
	if !needsData {
		return nil
	}
	_, app, err := appDefinition(ctx, appID)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if target.Kind != "data" {
			continue
		}
		row, err := meta(ctx, "collection", target.CollectionID, false)
		if err != nil {
			return err
		}
		var collection unsmeta.Document
		if json.Unmarshal(row.Data, &collection) != nil {
			return sourcestream.ErrForbidden
		}
		action := "read"
		resource := "uns.read"
		if target.Action == "write" {
			action = "write"
			resource = "uns.write"
		}
		permissions := []string{"record:" + action}
		if collection.SampleSchema != nil {
			permissions = append(permissions, "sample:"+action)
		}
		allowed := []string{}
		for _, grant := range app.Collections {
			if grant.CollectionID == target.CollectionID {
				allowed = append(allowed, grant.Permissions...)
			}
		}
		for _, permission := range permissions {
			if !slices.Contains(allowed, permission) {
				return sourcestream.ErrForbidden
			}
		}
		if keyBound && require(ctx, resource) != nil {
			return sourcestream.ErrForbidden
		}
	}
	return nil
}

func (s *Service) requestGrantRecipient(ctx context.Context, appID string) ([]string, error) {
	if err := nativeAppStreamAdmission(ctx, appID); err != nil {
		return nil, err
	}
	_, app, err := appDefinition(ctx, appID)
	if err != nil {
		return nil, err
	}
	actions := []string{}
	for _, action := range []string{"stream.requests.read", "stream.requests.invoke", "stream.requests.write"} {
		if slices.Contains(app.PlatformPermissions, action) {
			actions = append(actions, action)
		}
	}
	return actions, nil
}

func requestGrantView(input appRequestGrantInput, grant repo.RequestGrant, policy repo.ProgramRequestPolicy, binding, status string, recipient []string, eligible bool) map[string]any {
	flag := func(value bool) int {
		if value {
			return 1
		}
		return 2
	}
	targets := []map[string]any{}
	for _, target := range policy.Targets {
		active := false
		for _, saved := range grant.Targets {
			active = active || grant.Active && grant.BindingDigest == binding && saved.Digest == target.Digest && saved.Action == target.Action
		}
		kind := target.Kind
		if kind == "source" {
			kind = "device"
		}
		targets = append(targets, map[string]any{"digest": target.Digest, "action": target.Action, "revision": grant.Revision, "active": flag(active), "current": 1, "kind": kind, "reference": target.Reference})
	}
	for _, saved := range grant.Targets {
		found := false
		for _, target := range policy.Targets {
			found = found || target.Digest == saved.Digest
		}
		if !found {
			targets = append(targets, map[string]any{"digest": saved.Digest, "action": saved.Action, "revision": grant.Revision, "active": flag(grant.Active), "current": 2})
		}
	}
	return map[string]any{"code": input.Code, "principalKind": "app", "principalID": input.PrincipalID, "status": status, "bindingDigest": binding, "snapshotToken": grant.SnapshotToken(binding), "recipientEligible": flag(eligible), "recipientActions": recipient, "activationSupported": flag(status == "active" && policy.UnsupportedReason == "" && binding != ""), "registered": flag(grant.Revision > 0), "revision": grant.Revision, "active": flag(grant.Active), "actions": grant.Actions, "targets": targets, "createdPrincipal": grant.CreatedPrincipal, "updatedPrincipal": grant.UpdatedPrincipal, "createdTime": grant.CreatedTime, "updatedTime": grant.UpdatedTime}
}

func (s *Service) executeAppRequestGrant(ctx context.Context, op ResourceOperation, raw []byte) (any, error) {
	checked, err := s.refreshAppStreamPolicyManager(ctx)
	if err != nil {
		return nil, err
	}
	if s.sourceRuntime == nil {
		return nil, problem("TEMPORARILY_UNAVAILABLE", "Request runtime is unavailable")
	}
	if _, err := validateResourceRequest(op, raw); err != nil {
		return nil, err
	}
	var input appRequestGrantInput
	if Decode(raw, &input) != nil || input.PrincipalKind != "app" || !operationcontract.ValidRequestCode(input.Code) {
		return nil, invalid("principalKind/code", "App and valid Request code required")
	}
	appID, err := uuid.Parse(input.PrincipalID)
	if err != nil || appID == uuid.Nil || appID.String() != input.PrincipalID {
		return nil, invalid("principalID", "canonical local App UUID required")
	}
	setting := op.Name == "streamRequestGrantSet"
	if setting && (input.Active != 1 && input.Active != 2 || !requestGrantTokenPattern.MatchString(input.ExpectedSnapshotToken) || input.Active == 1 && !requestGrantActions(input.Actions) || input.Active == 2 && len(input.Actions) != 0) {
		return nil, invalid("grant", "active grant requires actions and an observed snapshot token; revoke requires empty actions")
	}
	var result any
	err = repo.GetUnsConn(checked).WithContext(checked).Transaction(func(tx *gorm.DB) error {
		txctx := repo.WithUnsTransaction(checked, tx)
		r := repo.NewSourceStreamRepo(tx)
		if err := r.LockRequestGrant(txctx, input.PrincipalID, input.Code); err != nil {
			return err
		}
		current, err := s.refreshAppStreamPolicyManager(txctx)
		if err != nil {
			return err
		}
		old, err := r.RequestGrant(current, input.PrincipalID, input.Code)
		if err != nil {
			return err
		}
		var def repo.OperationDefinition
		var version repo.StreamVersion
		var policy repo.ProgramRequestPolicy
		binding, status := "", "inactive"
		recipient := []string{}
		eligible := false
		if !setting || input.Active == 1 {
			def, version, policy, binding, err = s.sourceRuntime.RequestGrantPlan(current, input.Code)
			if err == nil {
				status = "active"
			} else if !errors.Is(err, repo.ErrNotFound) && !errors.Is(err, sourcestream.ErrControlInactive) {
				return err
			}
			recipient, err = s.requestGrantRecipient(current, input.PrincipalID)
			eligible = err == nil
			if recipient == nil {
				recipient = []string{}
			}
		} else {
			binding = strings.Split(input.ExpectedSnapshotToken, ".")[1]
		}
		if !setting {
			result = requestGrantView(input, old, policy, binding, status, recipient, eligible)
			return nil
		}
		if old.SnapshotToken(binding) != input.ExpectedSnapshotToken {
			return problem("VERSION_CONFLICT", "Request grant snapshot changed")
		}
		next := old
		if input.Active == 1 {
			if status != "active" || binding == "" || policy.UnsupportedReason != "" || !eligible {
				return problem("FORBIDDEN", "Request or App is not eligible for activation")
			}
			for _, action := range input.Actions {
				if !slices.Contains(recipient, action) || require(current, action) != nil {
					return problem("FORBIDDEN", "Requested actions exceed current manager or App authority")
				}
			}
			if err := s.sourceRuntime.CheckRequestGrantPublisher(current, version, def.Binding.EntryID); err != nil {
				return err
			}
			for _, target := range policy.Targets {
				permissions := []string{"uns.read"}
				if target.Kind == "source" {
					permissions = append(permissions, "uns.source")
				}
				if target.Effect == "write" {
					permissions = append(permissions, "uns.write")
				}
				if err := require(current, permissions...); err != nil {
					return err
				}
				if target.Kind == "source" {
					source, err := r.Source(current, target.SourceID)
					if err != nil {
						return err
					}
					if err = version.ValidateProgramExecutionSource(source); err != nil {
						return err
					}
				}
			}
			if err := checkAppRequestData(current, input.PrincipalID, policy.Targets, false); err != nil {
				return err
			}
			manifest, err := version.ProgramV2Manifest()
			if err != nil {
				return err
			}
			bindings, err := version.ProgramV2Bindings()
			if err != nil {
				return err
			}
			if err := repo.ValidateProgramV2Bindings(current, manifest, bindings); err != nil {
				return err
			}
			next = repo.RequestGrant{AppID: input.PrincipalID, Code: input.Code, BindingDigest: binding, StreamID: def.Binding.StreamID, VersionID: def.Binding.DeploymentVersion, EntryID: def.Binding.EntryID, Active: true, Actions: append([]string{}, input.Actions...), Targets: []repo.RequestTargetGrant{}}
			slices.Sort(next.Actions)
			for _, target := range policy.Targets {
				next.Targets = append(next.Targets, repo.RequestTargetGrant{Digest: target.Digest, Action: target.Action})
			}
		} else {
			if old.Revision == 0 {
				return problem("NOT_FOUND", "Request grant not found")
			}
			next.Active = false
		}
		actor, _ := contextx.SubjectFrom(current)
		next.UpdatedPrincipal = "user:" + strconv.FormatInt(actor.UserID, 10)
		if actor.APIKeyID > 0 {
			next.UpdatedPrincipal = "api_key:" + strconv.FormatInt(actor.APIKeyID, 10)
		}
		next, err = r.PutRequestGrant(current, next, old)
		if err != nil {
			return err
		}
		result = requestGrantView(input, next, policy, binding, status, recipient, eligible)
		return nil
	})
	return result, err
}

func (s *Service) executeAppStreamAccess(ctx context.Context, op ResourceOperation, raw []byte) (any, error) {
	checked, err := s.refreshAppStreamPolicyManager(ctx)
	if err != nil {
		return nil, err
	}
	if s.sourceRuntime == nil {
		return nil, problem("TEMPORARILY_UNAVAILABLE", "Request runtime is unavailable")
	}
	if _, err := validateResourceRequest(op, raw); err != nil {
		return nil, err
	}
	var request struct {
		AppID           string `json:"appId"`
		StreamID        string `json:"streamId"`
		Page            int    `json:"page"`
		Size            int    `json:"size"`
		IncludeInactive int    `json:"includeInactive"`
	}
	if err := Decode(raw, &request); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(request.AppID)
	if err != nil || id == uuid.Nil || id.String() != request.AppID {
		return nil, invalid("appId", "canonical local App UUID required")
	}
	if request.Page == 0 {
		request.Page = 1
	}
	if request.Size == 0 {
		request.Size = 20
	}
	if request.IncludeInactive == 0 {
		request.IncludeInactive = 2
	}
	if request.Page < 1 || request.Page > 100000 || request.Size < 1 || request.Size > 100 || request.IncludeInactive != 1 && request.IncludeInactive != 2 {
		return nil, invalid("pagination", "invalid pagination")
	}
	if request.StreamID != "" {
		id, err := strconv.ParseInt(request.StreamID, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != request.StreamID {
			return nil, invalid("streamId", "positive decimal identifier required")
		}
	}
	grants, total, err := repo.NewSourceStreamRepo(checked).AppRequestGrants(checked, request.AppID, request.StreamID, request.Page, request.Size, request.IncludeInactive == 1)
	if err != nil {
		return nil, err
	}
	recipient, recipientErr := s.requestGrantRecipient(checked, request.AppID)
	items := []map[string]any{}
	for _, grant := range grants {
		configured := 2
		reasons := []string{}
		if grant.Active {
			configured = 1
		} else {
			reasons = append(reasons, "grant_revoked")
		}
		if recipientErr != nil {
			reasons = append(reasons, "recipient_unavailable")
		}
		item := map[string]any{"code": grant.Code, "streamId": grant.StreamID, "streamName": "", "entryId": grant.EntryID, "entryName": "", "actions": grant.Actions, "configuredActive": configured, "revision": grant.Revision, "state": "blocked"}
		def, version, policy, binding, err := s.sourceRuntime.RequestGrantPlan(checked, grant.Code)
		if err != nil {
			reasons = append(reasons, "stream_inactive")
		} else {
			item["entryName"] = def.Name
			var streamDoc struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal([]byte(version.Document), &streamDoc)
			item["streamName"] = streamDoc.Name
			if policy.UnsupportedReason != "" || binding != grant.BindingDigest {
				reasons = append(reasons, "binding_unavailable")
			}
			if s.sourceRuntime.CheckRequestGrantPublisher(checked, version, def.Binding.EntryID) != nil {
				reasons = append(reasons, "deployer_unavailable")
			}
			if checkAppRequestData(checked, request.AppID, policy.Targets, false) != nil {
				reasons = append(reasons, "target_unavailable")
			}
			for _, action := range grant.Actions {
				if !slices.Contains(recipient, action) {
					reasons = append(reasons, "recipient_unavailable")
					break
				}
			}
			for _, target := range policy.Targets {
				if target.Kind != "source" {
					continue
				}
				source, err := repo.NewSourceStreamRepo(checked).Source(checked, target.SourceID)
				if err != nil || version.ValidateProgramExecutionSource(source) != nil {
					reasons = append(reasons, "target_unavailable")
					break
				}
			}
			manifest, merr := version.ProgramV2Manifest()
			bindings, berr := version.ProgramV2Bindings()
			if merr != nil || berr != nil || repo.ValidateProgramV2Bindings(checked, manifest, bindings) != nil {
				reasons = append(reasons, "target_unavailable")
			}
		}
		slices.Sort(reasons)
		reasons = slices.Compact(reasons)
		if len(reasons) == 0 {
			item["state"] = "available"
		}
		item["blockedReasons"] = reasons
		items = append(items, item)
	}
	return map[string]any{"items": items, "page": request.Page, "size": request.Size, "total": total}, nil
}
