package nativeapp

import (
	"context"
	"errors"
	"net/http"

	"backend/internal/domain/appinstall"
	domainlicense "backend/internal/domain/license"
	"backend/internal/domain/unsv2"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/types"
)

func execute(ctx context.Context, svcCtx *svc.ServiceContext, operation string, request any) (*types.Envelope, error) {
	if c := domainlicense.Default().Capabilities()["app"]; !c.Enabled {
		return nil, respx.NewHTTPError(http.StatusForbidden, c.ReasonCode)
	}
	if svcCtx.App.NativeAppManagement == nil {
		return nil, respx.NewHTTPError(http.StatusServiceUnavailable, "Native App runtime is not configured")
	}
	management := svcCtx.App.NativeAppManagement
	var data any
	var err error
	switch operation {
	case "NativeAppList":
		input := request.(*types.NativeAppListReq)
		data, err = management.Catalog(ctx, input.Archived, input.Launchable)
	case "NativeAppTrash":
		input := request.(*types.NativeAppTrashReq)
		err = management.SetTrashed(ctx, input.AppId, input.ExpectedRevision, input.Trashed)
	case "NativeAppRoleOptions", "NativeAppRoles":
		data, err = appinstall.BusinessRoles(ctx, operation == "NativeAppRoles")
	case "NativeAppRoleCreate", "NativeAppRoleUpdate":
		input := request.(*types.NativeAppRoleReq)
		if (operation == "NativeAppRoleCreate") != (input.ExpectedRevision == 0) {
			return nil, respx.NewHTTPError(http.StatusBadRequest, "Invalid role revision")
		}
		err = appinstall.SaveBusinessRole(ctx, repo.BusinessRole{Key: input.Key, Name: input.Name, Description: input.Description, UserIDs: input.UserIds, GroupIDs: input.GroupIds, Revision: input.ExpectedRevision})
	case "NativeAppRoleAssignment":
		input := request.(*types.NativeAppRoleAssignmentReq)
		err = appinstall.ReplaceBusinessRoleAssignments(ctx, input.TargetType, input.TargetId, input.RoleKeys, input.ExpectedRoleKeys)
	case "NativeAppRoleDelete":
		input := request.(*types.NativeAppRoleDeleteReq)
		err = appinstall.DeleteBusinessRole(ctx, input.Key, input.ExpectedRevision)
	case "NativeAppPreview":
		data, err = management.Preview(ctx, request.(*types.NativeAppPreviewReq).FileId)
	case "NativeAppInstall":
		input := request.(*types.NativeAppInstallReq)
		grants := make([]unsv2.Grant, 0, len(input.Collections))
		for _, g := range input.Collections {
			grants = append(grants, unsv2.Grant{Alias: g.Alias, CollectionID: g.CollectionId, Permissions: g.Permissions})
		}
		data, err = management.Install(ctx, appinstall.InstallRequest{FileID: input.FileId, AppID: input.AppId, IdempotencyKey: input.IdempotencyKey, Collections: grants, SkipInvalidRoles: input.SkipInvalidRoles, RolePlanDigest: input.RolePlanDigest, RenameRoleKeys: input.RenameRoleKeys})
	case "NativeAppAction":
		input := request.(*types.NativeAppActionReq)
		err = management.Action(ctx, input.AppId, input.Action)
		data = map[string]any{"appId": input.AppId, "action": input.Action}
	case "NativeAppJob":
		data, err = management.Job(ctx, request.(*types.NativeAppJobReq).JobId)
	case "NativeAppLogs":
		var text string
		text, err = svcCtx.App.NativeApps.Logs(ctx, request.(*types.NativeAppIdReq).AppId)
		data = map[string]any{"text": text}
	default:
		return nil, respx.NewHTTPError(http.StatusNotFound, "Unknown native App operation")
	}
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, respx.NewHTTPError(http.StatusNotFound, "App resource not found")
		}
		if errors.Is(err, repo.ErrConflict) {
			return nil, respx.NewHTTPError(http.StatusConflict, "App operation conflicts with an existing request")
		}
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}
