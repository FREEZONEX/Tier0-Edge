package iam

import (
	"bytes"
	"context"
	"fmt"

	domainasset "backend/internal/domain/asset"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

const userImportTemplateFileName = "user-import-template.xlsx"

type UserImportTemplateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserImportTemplateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserImportTemplateLogic {
	return &UserImportTemplateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *UserImportTemplateLogic) UserImportTemplate() (*types.Envelope, error) {
	raw, err := l.svcCtx.App.IAM.BuildUserImportTemplate()
	if err != nil {
		return nil, logicx.Error(err)
	}
	userID := logicx.UserID(l.ctx)
	created, err := l.svcCtx.App.Asset.UploadContent(l.ctx, domainasset.UploadContentCommand{
		FileName: userImportTemplateFileName, ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		Size: int64(len(raw)), Reader: bytes.NewReader(raw), UserID: userID,
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	fileID := domainasset.ParseOwnerID(fmt.Sprint(created["fileId"]))
	if fileID <= 0 {
		return nil, logicx.Error(domainasset.ErrInvalid)
	}
	if _, err := l.svcCtx.App.Asset.Bind(l.ctx, domainasset.BindCommand{
		FileID: fileID, OwnerType: "iamImportTemplate", OwnerID: userID, UserID: userID,
	}); err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"fileId": fileID, "fileName": userImportTemplateFileName}), nil
}
