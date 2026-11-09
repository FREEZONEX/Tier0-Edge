// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package iam

import (
	"context"
	"strconv"

	auditdomain "backend/internal/domain/audit"
	domainiam "backend/internal/domain/iam"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UserCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserCreateLogic {
	return &UserCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserCreateLogic) UserCreate(req *types.UserSaveReq) (resp *types.Envelope, err error) {
	roles := make([]domainiam.UserRoleCommand, 0, len(req.RoleList))
	for _, role := range req.RoleList {
		roles = append(roles, domainiam.UserRoleCommand{RoleID: role.RoleId, RoleName: role.RoleName})
	}
	data, err := l.svcCtx.App.IAM.CreateUser(l.ctx, domainiam.UserSaveCommand{
		Username:    req.Username,
		Password:    req.Password,
		FirstName:   req.FirstName,
		Email:       req.Email,
		Phone:       req.Phone,
		Enabled:     req.Enabled,
		IsRandomPwd: req.ForceChangePassword,
		RoleList:    roles,
		UserID:      logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	if id := userIDFromResp(data); id > 0 {
		name, _ := data["userName"].(string)
		if name == "" {
			name, _ = data["nickName"].(string)
		}
		l.svcCtx.App.Audit.Record(l.ctx, auditdomain.RecordInput{
			ScopeType:    auditdomain.ScopeTypePlatform,
			ResType:      auditdomain.ResTypeUserManagement,
			ResID:        strconv.FormatInt(id, 10),
			ResName:      name,
			BusinessType: auditdomain.BizCreate,
			Detail:       map[string]any{"userId": id},
		})
	}
	return respx.Envelope(data), nil
}

func userIDFromResp(data map[string]any) int64 {
	if id, ok := data["userId"].(int64); ok {
		return id
	}
	if id, ok := data["userId"].(float64); ok {
		return int64(id)
	}
	if id, ok := data["userId"].(int); ok {
		return int64(id)
	}
	if id, ok := data["userId"].(string); ok {
		v, _ := strconv.ParseInt(id, 10, 64)
		return v
	}
	return 0
}
