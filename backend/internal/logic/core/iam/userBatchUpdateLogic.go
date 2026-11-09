// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package iam

import (
	"context"
	"strings"

	auditdomain "backend/internal/domain/audit"
	domainiam "backend/internal/domain/iam"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UserBatchUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserBatchUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserBatchUpdateLogic {
	return &UserBatchUpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserBatchUpdateLogic) UserBatchUpdate(req *types.UserBatchUpdateReq) (resp *types.Envelope, err error) {
	var homePage *string
	if value := strings.TrimSpace(req.HomePage); value != "" {
		homePage = &value
	}
	roles := make([]domainiam.UserRoleCommand, 0, len(req.RoleList))
	for _, role := range req.RoleList {
		roles = append(roles, domainiam.UserRoleCommand{RoleID: role.RoleId, RoleName: role.RoleName})
	}
	results, err := l.svcCtx.App.IAM.BatchUpdateUsers(l.ctx, domainiam.UserBatchUpdateCommand{
		UserIDs:  req.UserIds,
		HomePage: homePage,
		RoleList: roles,
		ActorID:  logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}

	users := make([]map[string]any, 0, len(results))
	for _, result := range results {
		user := map[string]any{"userId": result.UserID}
		if result.HomePage != "" {
			user["homePage"] = result.HomePage
		}
		if result.RoleID > 0 {
			user["roleList"] = []map[string]any{{"roleId": result.RoleID}}
		}
		users = append(users, user)
	}
	auditDetail := map[string]any{
		"batch":        true,
		"updatedCount": len(results),
		"users":        users,
	}
	if homePage != nil {
		auditDetail["requestedHomePage"] = *homePage
	}
	if len(roles) == 1 {
		auditDetail["requestedRoleId"] = roles[0].RoleID
	}
	l.svcCtx.App.Audit.Record(l.ctx, auditdomain.RecordInput{
		ScopeType:    auditdomain.ScopeTypePlatform,
		ResType:      auditdomain.ResTypeUserManagement,
		ResID:        "batch",
		ResName:      "batch",
		BusinessType: auditdomain.BizUpdate,
		Detail:       auditDetail,
	})

	return respx.Envelope(map[string]any{
		"updatedCount": len(results),
		"users":        users,
	}), nil
}
