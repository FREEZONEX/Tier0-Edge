package auth

import (
	"backend/internal/contextx"
	domainauth "backend/internal/domain/auth"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"
	"context"
	"fmt"
	"net/http"
	"time"
)

type MiniEntryLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMiniEntryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MiniEntryLogic {
	return &MiniEntryLogic{ctx: ctx, svcCtx: svcCtx}
}

func (l *MiniEntryLogic) check(grant domainauth.MiniEntryGrant) (time.Time, error) {
	subject, expires, err := l.svcCtx.App.Auth.ValidateMiniEntrySession(l.ctx, grant.Token)
	if err != nil {
		return time.Time{}, err
	}
	// Reuse the established resource/action mapping, including operator grants.
	path := fmt.Sprintf("/api/core/launchpad/projects/%d/apps/%d", grant.ProjectID, grant.AppID)
	allowed, _, err := l.svcCtx.App.Permission.Allow(l.ctx, subject, "api", http.MethodGet, path)
	if err != nil {
		return time.Time{}, err
	}
	if !allowed {
		return time.Time{}, respx.NewHTTPError(403, "forbidden")
	}
	ctx := contextx.WithSubject(l.ctx, subject)
	if _, err = l.svcCtx.App.Launchpad.App(ctx, subject.UserID, grant.ProjectID, grant.AppID); err != nil {
		return time.Time{}, logicx.Error(err)
	}
	return expires, nil
}
func (l *MiniEntryLogic) Ticket(req *types.MiniEntryTicketReq, token string) (*types.Envelope, error) {
	if req.ProjectId <= 0 || req.AppId <= 0 {
		return nil, respx.NewHTTPError(400, "invalid app")
	}
	grant := domainauth.MiniEntryGrant{Token: token, ProjectID: req.ProjectId, AppID: req.AppId}
	if _, err := l.check(grant); err != nil {
		return nil, err
	}
	ticket, err := l.svcCtx.App.Auth.CreateMiniEntry(l.ctx, l.svcCtx.App.Cache, grant)
	if err != nil {
		return nil, err
	}
	return respx.Envelope(map[string]any{"ticket": ticket, "expiresIn": 60}), nil
}
func (l *MiniEntryLogic) Exchange(ticket string) (domainauth.MiniEntryGrant, time.Time, error) {
	grant, err := l.svcCtx.App.Auth.TakeMiniEntry(l.ctx, l.svcCtx.App.Cache, ticket)
	if err != nil {
		return grant, time.Time{}, err
	}
	expires, err := l.check(grant)
	return grant, expires, err
}
