// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package iam

import (
	"context"
	"strconv"
	"strings"

	auditdomain "backend/internal/domain/audit"
	domainiam "backend/internal/domain/iam"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UserUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserUpdateLogic {
	return &UserUpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserUpdateLogic) UserUpdate(req *types.UserUpdateReq) (resp *types.Envelope, err error) {
	roles := make([]domainiam.UserRoleCommand, 0, len(req.RoleList))
	for _, role := range req.RoleList {
		roles = append(roles, domainiam.UserRoleCommand{RoleID: role.RoleId, RoleName: role.RoleName})
	}
	data, err := l.svcCtx.App.IAM.UpdateUser(l.ctx, domainiam.UserUpdateCommand{
		ID:        req.Id,
		Username:  req.Username,
		FirstName: req.FirstName,
		Email:     req.Email,
		Phone:     req.Phone,
		Enabled:   req.Enabled,
		RoleList:  roles,
		UserID:    logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	if strings.TrimSpace(req.HomePage) != "" {
		// 状态变更已经提交并独立记账；后续失败仅记录整个请求失败。
		defer func() {
			state := auditdomain.StateFromContext(l.ctx)
			if err != nil && state != nil && state.Suppressed && state.Record != nil && (state.Record.BusinessType == auditdomain.BizEnable || state.Record.BusinessType == auditdomain.BizDisable) {
				state.Suppressed = false
				state.Record = nil
			}
		}()
		homePage, err := l.svcCtx.App.IAM.UpdateUserHomePage(l.ctx, req.Id, req.HomePage)
		if err != nil {
			return nil, logicx.Error(err)
		}
		data["homePage"] = homePage
	}
	if id := userIDFromResp(data); id > 0 {
		name, _ := data["userName"].(string)
		if name == "" {
			name, _ = data["nickName"].(string)
		}
		state := auditdomain.StateFromContext(l.ctx)
		if state != nil && state.Record != nil && state.Record.ResType == auditdomain.ResTypeUserManagement && state.Record.ResID == strconv.FormatInt(id, 10) && (state.Record.BusinessType == auditdomain.BizEnable || state.Record.BusinessType == auditdomain.BizDisable) {
			state.Record.ResName = name
		} else {
			l.svcCtx.App.Audit.Record(l.ctx, auditdomain.RecordInput{
				ScopeType:    auditdomain.ScopeTypePlatform,
				ResType:      auditdomain.ResTypeUserManagement,
				ResID:        strconv.FormatInt(id, 10),
				ResName:      name,
				BusinessType: auditdomain.BizUpdate,
				Detail:       map[string]any{"userId": id},
			})
		}
	}
	return respx.Envelope(data), nil
}
