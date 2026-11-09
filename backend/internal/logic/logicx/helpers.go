package logicx

import (
	"context"
	"errors"

	"backend/internal/contextx"
	"backend/internal/domain/aigateway"
	"backend/internal/domain/apikey"
	"backend/internal/domain/asset"
	"backend/internal/domain/audit"
	"backend/internal/domain/cluster"
	"backend/internal/domain/dataquality"
	"backend/internal/domain/iam"
	"backend/internal/domain/launchpad"
	"backend/internal/domain/mqtt"
	"backend/internal/domain/oauth"
	"backend/internal/domain/project"
	"backend/internal/domain/uns"
	respx "backend/internal/httpx"
	"backend/internal/repo"
)

func UserID(ctx context.Context) int64 {
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok {
		return 0
	}
	return subject.UserID
}

func Error(err error) error {
	var fleetAgentErr *cluster.FleetAgentResponseError
	if errors.As(err, &fleetAgentErr) {
		statusCode := fleetAgentErr.StatusCode
		if statusCode < 400 || statusCode > 599 {
			statusCode = 502
		}
		return respx.NewHTTPError(statusCode, fleetAgentErr.Error())
	}
	// 消息通知域（TASK-030）：codedError 自带 HTTP 状态映射（3005001~3005040）
	var statusCoder interface{ HTTPStatus() int }
	if errors.As(err, &statusCoder) {
		return respx.WrapHTTPError(statusCoder.HTTPStatus(), err.Error(), err)
	}
	status, msg := errorStatus(err)
	if status > 0 {
		return respx.WrapHTTPError(status, msg, err)
	}
	return err
}

// errorStatus 返回 err 对应的 HTTP 状态码与错误消息；0 表示未识别，原样透传。
func errorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, dataquality.ErrInvalidPolicy):
		return 400, err.Error()
	case errors.Is(err, dataquality.ErrFeatureDisabled):
		return 503, err.Error()
	case errors.Is(err, uns.ErrInvalid), errors.Is(err, uns.ErrInvalidJSON), errors.Is(err, repo.ErrInvalidArgument), errors.Is(err, repo.ErrFlowFolderCannotDeploy), errors.Is(err, asset.ErrInvalid), errors.Is(err, aigateway.ErrInvalid), errors.Is(err, aigateway.ErrTrustedHostsInvalid), errors.Is(err, apikey.ErrInvalid), errors.Is(err, oauth.ErrInvalid), errors.Is(err, oauth.ErrInvalidRedirect), errors.Is(err, project.ErrInvalid), errors.Is(err, project.ErrUnsupportedPackageFormat), errors.Is(err, project.ErrProjectHasApps), errors.Is(err, project.ErrAppIdentityUnverifiable), errors.Is(err, project.ErrAppIdentityNotMatch), errors.Is(err, project.ErrAppReplaceInvalidTarget), errors.Is(err, project.ErrAppReplaceInProgress), errors.Is(err, project.ErrAppNameTooLong), errors.Is(err, project.ErrAppDescTooLong), errors.Is(err, cluster.ErrInvalid), errors.Is(err, cluster.ErrInvalidJSON), errors.Is(err, mqtt.ErrInvalid), errors.Is(err, iam.ErrInvalidRole), errors.Is(err, iam.ErrInvalidUser), errors.Is(err, iam.ErrInvalidPassword):
		return 400, err.Error()
	case errors.Is(err, aigateway.ErrUpstreamNotAllowed), errors.Is(err, aigateway.ErrUpstreamRestricted):
		return 400, err.Error()
	case errors.Is(err, aigateway.ErrCredentialKeyMissing), errors.Is(err, aigateway.ErrCredentialInvalid), errors.Is(err, aigateway.ErrDisabled):
		return 503, err.Error()
	case errors.Is(err, aigateway.ErrConnectionFailed):
		return 502, err.Error()
	case errors.Is(err, cluster.ErrUnauthorized):
		return 401, err.Error()
	case errors.Is(err, cluster.ErrFleetCenterType):
		return 422, err.Error()
	case errors.Is(err, cluster.ErrHostCannotConnect):
		return 403, err.Error()
	case errors.Is(err, cluster.ErrFleetAgentNotConfigured), errors.Is(err, cluster.ErrFleetAgentUnavailable):
		return 503, err.Error()
	case errors.Is(err, iam.ErrLicenseResourceDenied):
		return 403, err.Error()
	case errors.Is(err, project.ErrForbidden), errors.Is(err, project.ErrImportAssetDenied), errors.Is(err, asset.ErrForbidden), errors.Is(err, iam.ErrForbidden):
		return 403, err.Error()
	case errors.Is(err, asset.ErrPresignUnsupported), errors.Is(err, asset.ErrMultipartUnsupported):
		return 501, err.Error()
	case errors.Is(err, repo.ErrSystemReadonly):
		return 403, err.Error()
	case errors.Is(err, repo.ErrDuplicate), errors.Is(err, repo.ErrConflict), errors.Is(err, repo.ErrUserAccountDuplicate), errors.Is(err, repo.ErrUserEmailDuplicate), errors.Is(err, project.ErrDuplicate), errors.Is(err, project.ErrAppNameDuplicate), errors.Is(err, project.ErrAppAlreadyExists), errors.Is(err, dataquality.ErrNodeLimit), errors.Is(err, dataquality.ErrDeepUnavailable):
		return 409, err.Error()
	case errors.Is(err, cluster.ErrScopeRevisionConflict):
		return 409, err.Error()
	case errors.Is(err, aigateway.ErrAlreadyConfigured):
		return 409, err.Error()
	case errors.Is(err, cluster.ErrAccountRevisionConflict):
		return 409, err.Error()
	case errors.Is(err, cluster.ErrFleetNodeQuotaExceeded):
		return 409, err.Error()
	case errors.Is(err, cluster.ErrFleetNodeOnline):
		return 409, err.Error()
	case errors.Is(err, cluster.ErrAccountNotAssignable):
		return 400, err.Error()
	case errors.Is(err, uns.ErrRestoreConflict):
		return 409, err.Error()
	case errors.Is(err, launchpad.ErrAppRoleRequired):
		// App 存在但成员缺角色 → 403，前端据此展示 App access required（区别于 404 的 App not found）
		return 403, err.Error()
	case errors.Is(err, repo.ErrNotFound), errors.Is(err, uns.ErrNotFound), errors.Is(err, uns.ErrNotInRecycle), errors.Is(err, asset.ErrNotFound), errors.Is(err, aigateway.ErrNotFound), errors.Is(err, apikey.ErrNotFound), errors.Is(err, oauth.ErrNotFound), errors.Is(err, oauth.ErrInvalidCode), errors.Is(err, project.ErrNotFound), errors.Is(err, project.ErrAppNotFound), errors.Is(err, audit.ErrNotFound), errors.Is(err, launchpad.ErrNotFound), errors.Is(err, mqtt.ErrNotFound):
		return 404, err.Error()
		return 503, err.Error()
	default:
		return 0, ""
	}
}
