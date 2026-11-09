// Code scaffolded by goctl. Safe to edit.
package mcp

import (
	"context"

	"backend/internal/httpx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type McpInfoLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 当前实例 MCP 接入信息
func NewMcpInfoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *McpInfoLogic {
	return &McpInfoLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *McpInfoLogic) McpInfo() (resp *types.Envelope, err error) {
	return httpx.Envelope(l.svcCtx.App.MCP.Info(l.ctx)), nil
}
