package unsv2

import (
	"context"
	"errors"
	"strconv"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
)

// Inject the existing authentication service without coupling the UNS domain to HTTP adapters.
type AppSessionVerifier interface {
	ValidateMiniEntrySession(context.Context, string) (contextx.Subject, time.Time, error)
	CurrentUser(context.Context, int64) (repo.User, []string, []string, bool, error)
}

type AppSessionUser struct {
	UserID   string `json:"userId"`
	UserName string `json:"userName"`
	NickName string `json:"nickName"`
}
type AppSessionIdentity struct {
	AppID           string         `json:"appId"`
	User            AppSessionUser `json:"user"`
	RoleIDs         []string       `json:"roleIds"`
	Roles           []Role         `json:"roles"`
	SessionExpireAt string         `json:"sessionExpireAt"`
}

// The API key proves which App is calling; the independently verified session
// proves which human is using it. The key's owner is never the App's actor.
func (s *Service) appSessionInspect(ctx context.Context, appID, token string) (any, error) {
	caller, ok := contextx.SubjectFrom(ctx)
	if !ok || caller.MCPRestricted {
		return nil, problem("FORBIDDEN", "App session inspection requires a direct caller")
	}
	if caller.AuthType == "apiKey" {
		if caller.APIKeyUsageType != repo.APIKeyUsageAppData || caller.APIKeyAppID != appID {
			return nil, problem("FORBIDDEN", "An App-bound key for the requested App is required")
		}
	} else if caller.AuthType != "session" {
		return nil, problem("FORBIDDEN", "A platform session or App-bound key is required")
	}
	if s.sessionAuth == nil {
		return nil, problem("INTERNAL_ERROR", "Session identity service is unavailable")
	}
	human, expires, err := s.sessionAuth.ValidateMiniEntrySession(ctx, token)
	if err != nil {
		return nil, problem("UNAUTHENTICATED", "The platform session is invalid or expired")
	}
	if caller.AuthType == "session" && caller.UserID != human.UserID {
		return nil, problem("FORBIDDEN", "Internal callers may inspect only their own session")
	}
	_, app, err := appDefinition(ctx, appID)
	if err != nil {
		return nil, err
	}
	id := strconv.FormatInt(human.UserID, 10)
	roles := []Role{}
	if _, nativeErr := repo.NewAppInstallationRepo(ctx).Get(ctx, appID); nativeErr == nil {
		roles, err = ResolveNativeAppRoles(ctx, appID, human.UserID)
		if errors.Is(err, ErrNativeAppAccess) {
			return nil, problem("FORBIDDEN", err.Error())
		}
		if err != nil {
			return nil, err
		}
	} else if !errors.Is(nativeErr, repo.ErrNotFound) {
		return nil, nativeErr
	} else {
		for _, member := range app.Members {
			if member.UserID != id {
				continue
			}
			for _, role := range app.Roles {
				if contains(member.RoleIDs, role.ID) {
					roles = append(roles, role)
				}
			}
			break
		}
		if len(roles) == 0 {
			return nil, problem("FORBIDDEN", "The user is not an authorized member of this App")
		}
	}
	roleIDs := make([]string, len(roles))
	for i, r := range roles {
		roleIDs[i] = r.ID
	}
	user, _, _, _, err := s.sessionAuth.CurrentUser(ctx, human.UserID)
	if err != nil {
		return nil, problem("UNAUTHENTICATED", "The platform account is unavailable")
	}
	return AppSessionIdentity{AppID: appID, User: AppSessionUser{UserID: id, UserName: user.UserName, NickName: user.NickName}, RoleIDs: roleIDs, Roles: roles, SessionExpireAt: expires.UTC().Format(time.RFC3339)}, nil
}
