package permission

import (
	"backend/internal/runtimeenv"
	"context"
	"net/http"

	"backend/internal/contextx"
	domainlicense "backend/internal/domain/license"
	"backend/internal/repo"
)

type Evaluator struct {
	iam *repo.IAMRepo
}

func New(ctx context.Context) *Evaluator {
	return &Evaluator{iam: repo.NewIAMRepo(ctx)}
}

func (e *Evaluator) Allow(ctx context.Context, subject contextx.Subject, actionType, method, path string) (bool, string, error) {
	if e == nil || e.iam == nil {
		return false, "", nil
	}
	matches, err := e.iam.MatchAction(ctx, actionType, method, path)
	if err != nil {
		return false, "", err
	}
	if len(matches) == 0 {
		return false, "", nil
	}
	for _, match := range matches {
		// On native Edge these two entry points admit workspace members. The
		// catalog and reverse proxy both enforce each App's business roles.
		if runtimeenv.IsNative() && match.ResourceKey == "uns.app.read" && subject.AuthType == "session" && !subject.MCPRestricted &&
			(match.ActionValue == "/apps/**" || match.ActionValue == "/api/core/app-runtime/apps") {
			if !domainlicense.Default().CapabilityAllowed("app") {
				return false, match.ResourceKey, nil
			}
			member, err := repo.IsActiveWorkspaceMember(ctx, subject.UserID)
			return member, match.ResourceKey, err
		}
		if contextx.HasResource(subject, match.ResourceKey) && licenseAllowsResource(match.ResourceKey) {
			return true, match.ResourceKey, nil
		}
		// Cloud request management includes discovery, but never invocation or
		// write effects. Keep the route's real seed match and authenticated
		// role/key intersection; no synthetic read scope or path bypass.
		if match.ResourceKey == "stream.requests.read" && contextx.HasResource(subject, "stream.requests.manage") && licenseAllowsResource("stream.requests.manage") {
			return true, "stream.requests.manage", nil
		}
	}
	return false, matches[0].ResourceKey, nil
}

func (e *Evaluator) IsAdmin(ctx context.Context, subject contextx.Subject) (bool, error) {
	if e == nil || e.iam == nil {
		return false, nil
	}
	return e.iam.IsAdmin(ctx, subject.UserID)
}

func licenseAllowsResource(resourceKey string) bool {
	licenseSvc := domainlicense.Default()
	return licenseSvc.AllowResource(resourceKey)
}

func IsReadMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
