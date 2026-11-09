package sourcestream

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/streamexec"
)

// RequestGrantPlan returns an immutable local release, without granting the
// recipient any Source or UNS platform permission.
func (s *Service) RequestGrantPlan(ctx context.Context, code string) (repo.OperationDefinition, repo.StreamVersion, repo.ProgramRequestPolicy, string, error) {
	r := repo.NewSourceStreamRepo(ctx)
	_, def, err := r.Operation(ctx, code)
	if err != nil {
		return def, repo.StreamVersion{}, repo.ProgramRequestPolicy{}, "", err
	}
	if def.DeclarationKind != "request" || def.Status != "active" {
		return def, repo.StreamVersion{}, repo.ProgramRequestPolicy{}, "", ErrControlInactive
	}
	streamID, _ := strconv.ParseInt(def.Binding.StreamID, 10, 64)
	versionID, _ := strconv.ParseInt(def.Binding.DeploymentVersion, 10, 64)
	stream, err := r.Stream(ctx, streamID)
	if err != nil {
		return def, repo.StreamVersion{}, repo.ProgramRequestPolicy{}, "", err
	}
	if stream.Status != "enabled" || stream.ActiveVersion != versionID {
		return def, repo.StreamVersion{}, repo.ProgramRequestPolicy{}, "", ErrControlInactive
	}
	version, err := r.Version(ctx, streamID, versionID)
	if err != nil {
		return def, version, repo.ProgramRequestPolicy{}, "", err
	}
	policy, digest, err := version.ProgramRequestPolicy(def.Binding.EntryID)
	return def, version, policy, digest, err
}

func (s *Service) CheckRequestGrantPublisher(ctx context.Context, version repo.StreamVersion, entryID string) error {
	return s.checkRequestPublisher(ctx, version, entryID)
}

func (s *Service) checkAppRequestGrant(ctx context.Context, version repo.StreamVersion, entryID string, actions ...string) error {
	subject, _ := contextx.SubjectFrom(ctx)
	if subject.APIKeyUsageType != repo.APIKeyUsageAppData {
		return nil
	}
	if subject.APIKeyAppID == "" || subject.APIKeyID <= 0 || s.requestAccess == nil {
		return ErrForbidden
	}
	policy, digest, err := version.ProgramRequestPolicy(entryID)
	if err != nil || policy.UnsupportedReason != "" {
		return ErrForbidden
	}
	grant, err := repo.NewSourceStreamRepo(ctx).RequestGrant(ctx, subject.APIKeyAppID, policy.Code)
	if err != nil {
		return err
	}
	if !grant.Active || grant.BindingDigest != digest || len(grant.Targets) != len(policy.Targets) {
		return ErrForbidden
	}
	for _, action := range actions {
		if !slices.Contains(grant.Actions, action) {
			return ErrForbidden
		}
	}
	for i, target := range policy.Targets {
		if grant.Targets[i].Digest != target.Digest || grant.Targets[i].Action != target.Action {
			return ErrForbidden
		}
		if target.Kind == "source" {
			source, err := repo.NewSourceStreamRepo(ctx).Source(ctx, target.SourceID)
			if err != nil {
				return err
			}
			if err := version.ValidateProgramExecutionSource(source); err != nil {
				return ErrForbidden
			}
		}
	}
	manifest, err := version.ProgramV2Manifest()
	if err != nil {
		return err
	}
	bindings, err := version.ProgramV2Bindings()
	if err != nil {
		return err
	}
	if err := repo.ValidateProgramV2Bindings(ctx, manifest, bindings); err != nil {
		return ErrForbidden
	}
	return s.requestAccess(ctx, subject.APIKeyAppID, actions, policy.Targets)
}

func (s *Service) checkAppRequestRead(ctx context.Context, definition repo.OperationDefinition) error {
	subject, _ := contextx.SubjectFrom(ctx)
	if subject.APIKeyUsageType != repo.APIKeyUsageAppData {
		return nil
	}
	streamID, _ := strconv.ParseInt(definition.Binding.StreamID, 10, 64)
	versionID, _ := strconv.ParseInt(definition.Binding.DeploymentVersion, 10, 64)
	version, err := repo.NewSourceStreamRepo(ctx).Version(ctx, streamID, versionID)
	if err != nil {
		return err
	}
	return s.checkAppRequestGrant(ctx, version, definition.Binding.EntryID, "stream.requests.read")
}

// The host also matches each call to the frozen closure. No App subject is
// replaced with the publisher or augmented with Source/data administration.
func (s *Service) checkAppRequestCall(ctx context.Context, version repo.StreamVersion, entryID string, call streamexec.ProgramCall) error {
	subject, _ := contextx.SubjectFrom(ctx)
	if subject.APIKeyUsageType != repo.APIKeyUsageAppData {
		return nil
	}
	if err := checkRequestExecution(ctx); err != nil {
		return err
	}
	policy, _, err := version.ProgramRequestPolicy(entryID)
	if err != nil {
		return ErrForbidden
	}
	if call.Kind == "checkpoint.get" || call.Kind == "checkpoint.set" {
		return nil
	}
	for _, target := range policy.Targets {
		if target.Kind == "data" && target.Reference == call.UNSReference().Key() && "uns."+target.Action == call.Kind {
			return nil
		}
		if target.Kind == "source" && strconv.FormatInt(target.SourceID, 10) == call.SourceID {
			if target.ResourceRef == call.ResourceRef && "source."+target.Action == call.Kind {
				return nil
			}
			if target.ResourceRef == "http:request" && target.Action == "invoke" && slices.Contains([]string{"source.request", "source.parameters", "source.secrets"}, call.Kind) {
				return nil
			}
		}
	}
	return ErrForbidden
}

func requestGrantDenied(err error) bool {
	return errors.Is(err, ErrForbidden) || errors.Is(err, repo.ErrNotFound)
}

var errRequestRecoveryIdentity = errors.New("request caller identity unavailable; automatic recovery stopped; inspect existing effects before explicitly submitting a new business intent")

// Persist identities, never permissions or credentials, for frozen-output
// recovery. Background recovery must not silently become the publisher.
type appRequestIdentity struct {
	AppID    string `json:"appId"`
	KeyID    int64  `json:"keyId"`
	UserID   int64  `json:"userId"`
	AuthType string `json:"authType"`
}

func (s *Service) restoreAppRequestSubject(ctx context.Context, summary string) (context.Context, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(summary), &fields) != nil {
		return ctx, repo.ErrInvalidArgument
	}
	raw, exists := fields["requestIdentity"]
	if !exists {
		return ctx, errRequestRecoveryIdentity
	}
	var identity appRequestIdentity
	if json.Unmarshal(raw, &identity) != nil || identity.KeyID < 0 || identity.UserID <= 0 {
		return ctx, errRequestRecoveryIdentity
	}
	if identity.AppID == "" {
		if s.programAuthority == nil {
			return ctx, ErrUnavailable
		}
		ref := repo.ProgramAuthorityRef{Kind: "user", ID: identity.UserID}
		if identity.KeyID > 0 {
			if identity.AuthType != "apiKey" {
				return ctx, errRequestRecoveryIdentity
			}
			ref = repo.ProgramAuthorityRef{Kind: "api_key", ID: identity.KeyID}
		} else if identity.AuthType != "session" {
			return ctx, errRequestRecoveryIdentity
		}
		current, err := s.programAuthority(ctx, ref)
		if err != nil {
			return ctx, err
		}
		if current.UserID != identity.UserID || current.APIKeyID != identity.KeyID || current.AuthType != identity.AuthType || current.APIKeyAppID != "" || current.APIKeyUsageType == repo.APIKeyUsageAppData {
			return ctx, ErrForbidden
		}
		return contextx.WithSubject(ctx, current), nil
	}
	if identity.KeyID <= 0 || identity.AuthType != "apiKey" || s.requestSubject == nil {
		return ctx, errRequestRecoveryIdentity
	}
	prior := contextx.Subject{UserID: identity.UserID, APIKeyID: identity.KeyID, APIKeyAppID: identity.AppID, APIKeyUsageType: repo.APIKeyUsageAppData, AuthType: "apiKey"}
	current, err := s.requestSubject(ctx, prior)
	if err != nil {
		return ctx, err
	}
	if current.APIKeyID != identity.KeyID || current.APIKeyAppID != identity.AppID || current.APIKeyUsageType != repo.APIKeyUsageAppData {
		return ctx, ErrForbidden
	}
	return contextx.WithSubject(ctx, current), nil
}
