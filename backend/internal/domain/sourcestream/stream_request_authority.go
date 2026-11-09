package sourcestream

import (
	"context"
	"errors"
	"strconv"

	"backend/internal/contextx"
	"backend/internal/repo"
)

type streamRequestAuthorityKey struct{}

func (s *Service) withRequestPublisher(ctx context.Context, version repo.StreamVersion, entryID string) (context.Context, error) {
	manifest, err := version.ProgramV2Manifest()
	if err != nil {
		return ctx, err
	}
	for _, entry := range manifest.Entries {
		if entry.ID == entryID && entry.Request != nil {
			guard := func(callCtx context.Context) error {
				if err := s.checkRequestPublisher(callCtx, version, entryID); err != nil {
					return err
				}
				actions := []string{"stream.requests.invoke"}
				if entry.Request.SideEffect == "write" {
					actions = append(actions, "stream.requests.write")
				}
				return s.checkAppRequestGrant(callCtx, version, entryID, actions...)
			}
			if err := guard(ctx); err != nil {
				return ctx, err
			}
			return context.WithValue(ctx, streamRequestAuthorityKey{}, guard), nil
		}
	}
	if isStreamRequestContext(ctx) {
		return ctx, repo.ErrNotFound
	}
	return ctx, nil
}

// This check only narrows an already authenticated caller's access. It never
// installs the publisher as the caller or grants access to an App's targets.
func (s *Service) checkRequestPublisher(ctx context.Context, version repo.StreamVersion, entryID string) error {
	plan, err := version.ProgramExecutionPlan()
	if errors.Is(err, repo.ErrNotFound) {
		// Historical publications retain their original execution path. They
		// have no provenance that could be used for new delegated grants.
		return nil
	}
	if err != nil {
		return err
	}
	if s.programAuthority == nil {
		return ErrForbidden
	}
	subject, err := s.programAuthority(ctx, plan.Authority)
	if err != nil {
		return err
	}
	if subject.FleetTask != nil || subject.MCPRestricted || subject.APIKeyAppID != "" || subject.APIKeyUsageType == repo.APIKeyUsageAppData ||
		plan.Authority.Kind == "user" && (subject.UserID != plan.Authority.ID || subject.APIKeyID != 0 || subject.AuthType != "session") ||
		plan.Authority.Kind == "api_key" && (subject.APIKeyID != plan.Authority.ID || subject.AuthType != "apiKey") {
		return ErrForbidden
	}
	manifest, err := version.ProgramV2Manifest()
	if err != nil {
		return err
	}
	found, writing := false, false
	for _, entry := range manifest.Entries {
		if entry.ID == entryID && entry.Request != nil {
			found, writing = true, entry.Request.SideEffect == "write"
		}
	}
	if !found {
		return repo.ErrNotFound
	}
	if len(manifest.Sources) > 0 {
		// Enterprise's Source capability is uns.source; do not invent Cloud's
		// workspace or permission identifiers in this installation.
		if !contextx.HasResource(subject, "uns.source") || !contextx.HasResource(subject, "uns.read") ||
			writing && subject.APIKeyID > 0 && subject.APIKeyPermission != "data_writer" && subject.APIKeyPermission != "full_access" {
			return ErrForbidden
		}
	}
	for _, reference := range manifest.Topics {
		if reference.Readable() && !contextx.HasResource(subject, "uns.read") || reference.Writable() && !contextx.HasResource(subject, "uns.write") {
			return ErrForbidden
		}
	}
	return nil
}

func (s *Service) checkRequestBindingPublisher(ctx context.Context, r *repo.SourceStreamRepo, binding repo.OperationBinding) error {
	streamID, err := strconv.ParseInt(binding.StreamID, 10, 64)
	if err != nil || streamID <= 0 {
		return repo.ErrInvalidArgument
	}
	versionID, err := strconv.ParseInt(binding.DeploymentVersion, 10, 64)
	if err != nil || versionID <= 0 {
		return repo.ErrInvalidArgument
	}
	version, err := r.Version(ctx, streamID, versionID)
	if err != nil {
		return err
	}
	return s.checkRequestPublisher(ctx, version, binding.EntryID)
}

func checkRequestExecution(ctx context.Context) error {
	if guard, ok := ctx.Value(streamRequestAuthorityKey{}).(func(context.Context) error); ok {
		return guard(ctx)
	}
	return nil
}
