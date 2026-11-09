// Package platform adapts platform OpenAPI v2 to existing domain services.
// Safe to edit. V1 handlers and domain contracts retain their existing behavior.
package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/asset"
	"backend/internal/domain/iam"
	"backend/internal/domain/notify"
	"backend/internal/domain/unsv2"
	"backend/internal/httpx"
	"backend/internal/infra/filestore"
	"backend/internal/repo"
	"backend/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
	gozerohttpx "github.com/zeromicro/go-zero/rest/httpx"
)

type apiError struct {
	status              int
	code, message, path string
}

var ErrDiscoveryRequest = invalid("", "Discovery requires an empty JSON object and no query parameters")

var ErrUnsupportedMediaType = errors.New("application/json content type required")

func (e *apiError) Error() string        { return e.message }
func invalid(path, message string) error { return &apiError{400, "INVALID_REQUEST", message, path} }
func forbidden() error {
	return &apiError{403, "FORBIDDEN", "The current identity is not authorized for this resource", ""}
}
func decode(raw []byte, target any) error {
	if len(raw) > 1<<20 {
		return &apiError{413, "REQUEST_TOO_LARGE", "JSON body exceeds 1 MiB", ""}
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return invalid("", "A JSON object is required")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	d.UseNumber()
	if err := d.Decode(target); err != nil {
		return invalid("", "Request fields do not match the operation contract: "+err.Error())
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return invalid("", "Exactly one JSON object is required")
	}
	return nil
}

// WriteError keeps all platform v2 failures in the same envelope, including
// notification domain errors which have a different legacy v1 wire shape.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	e := &apiError{500, "INTERNAL_ERROR", "Platform request failed", ""}
	var typed *apiError
	var maxBody *http.MaxBytesError
	var syntax *json.SyntaxError
	var permissionFault *unsv2.Fault
	switch {
	case errors.As(err, &typed):
		e = typed
	case errors.Is(err, ErrUnsupportedMediaType):
		e = &apiError{415, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json", ""}
	case errors.As(err, &maxBody):
		e = &apiError{413, "REQUEST_TOO_LARGE", "JSON body exceeds 1 MiB", ""}
	case errors.As(err, &syntax), errors.Is(err, io.EOF):
		e = &apiError{400, "INVALID_JSON", "A valid JSON object is required", ""}
	case errors.As(err, &permissionFault):
		switch permissionFault.Code {
		case "FORBIDDEN":
			e = &apiError{403, "FORBIDDEN", "The current identity is not authorized for this platform capability", ""}
		case "UNAUTHENTICATED":
			e = &apiError{401, "UNAUTHENTICATED", "An authenticated API key is required", ""}
		case "NOT_FOUND":
			e = &apiError{404, "NOT_FOUND", "The requested resource is unavailable", ""}
		}
	case errors.Is(err, asset.ErrInvalid), errors.Is(err, repo.ErrInvalidArgument), errors.Is(err, notify.ErrInvalidRequest), errors.Is(err, notify.ErrInvalidType):
		e = &apiError{400, "INVALID_REQUEST", "Invalid operation parameters", ""}
	case errors.Is(err, notify.ErrContentLimit):
		e = &apiError{422, "CONTENT_LIMIT", "Notification title or content exceeds the allowed length", ""}
	case errors.Is(err, asset.ErrForbidden), errors.Is(err, notify.ErrNotAllowed):
		e = &apiError{403, "FORBIDDEN", "The current identity is not authorized for this resource", ""}
	case errors.Is(err, asset.ErrNotFound), errors.Is(err, repo.ErrNotFound), errors.Is(err, notify.ErrRecipientNotFound), errors.Is(err, notify.ErrMessageNotFound):
		e = &apiError{404, "NOT_FOUND", "The requested resource is unavailable", ""}
	case errors.Is(err, notify.ErrIdempotencyConflict):
		e = &apiError{409, "IDEMPOTENCY_CONFLICT", "This idempotency key already belongs to a different notification request", "idempotencyKey"}
	case errors.Is(err, notify.ErrRateLimited):
		e = &apiError{429, "RATE_LIMITED", "Notification rate limit exceeded", ""}
	case errors.Is(err, asset.ErrPresignUnsupported), errors.Is(err, asset.ErrMultipartUnsupported), errors.Is(err, filestore.ErrMultipartUnsupported):
		e = &apiError{503, "STORAGE_NOT_CONFIGURED", "Presigned uploads require configured S3-compatible object storage", ""}
	case errors.Is(err, context.DeadlineExceeded):
		e = &apiError{504, "REQUEST_TIMEOUT", "The operation did not complete within its time budget", ""}
	}
	if httpx.IsCapabilityDiscovery(r) && e.status == 413 {
		e.message = "JSON body exceeds 4 KiB"
	}
	if httpx.WriteCapabilityDiscoveryError(w, r, e.status, e.code, e.message) {
		return
	}
	if e.status >= 500 {
		logx.WithContext(r.Context()).Errorf("platform v2 operation failed: %T", err)
	}
	body := map[string]any{"code": e.code, "retryable": e.status == 429, "details": []any{}}
	if e.path != "" {
		body["path"] = e.path
	}
	if e.status == 500 || e.status == 504 {
		body["recommendedAction"] = "Check operation status before retrying; retain the original notification idempotency key"
	}
	gozerohttpx.WriteJson(w, e.status, map[string]any{"code": e.status, "msg": e.message, "requestId": httpx.UnsV2RequestID(r.Context()), "data": nil, "error": body})
}

func execute(ctx context.Context, service *svc.ServiceContext, operation string, raw []byte) (any, error) {
	if operation == "capabilitiesDiscover" {
		return discoverCapabilities(ctx, service, raw)
	}
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || subject.UserID <= 0 || subject.APIKeyID <= 0 {
		return nil, &apiError{401, "UNAUTHENTICATED", "An authenticated API key is required", ""}
	}
	if subject.APIKeyAppID != "" && subject.APIKeyUsageType != repo.APIKeyUsageAppData {
		return nil, &apiError{403, "FORBIDDEN", "Legacy runtime App keys use OpenAPI v1; platform v2 requires a personal key or a granted UNS App data key", ""}
	}
	permissions := map[string]string{
		"identityGet": "openapi.base", "infoGet": "openapi.base", "usersList": "platform.users.read", "projectMembersList": "platform.projects.members.read",
		"notificationSend": "notifications.send", "notificationStatusGet": "notifications.read",
		"fileUploadCreate": "asset.file.upload", "fileDownloadCreate": "asset.file.download", "fileDelete": "asset.file.delete",
		"multipartCreate": "asset.file.upload", "multipartParts": "asset.file.upload", "multipartComplete": "asset.file.upload", "multipartAbort": "asset.file.upload",
	}
	permission, found := permissions[operation]
	if !found {
		return nil, &apiError{404, "NOT_FOUND", "Unknown platform operation", ""}
	}
	if err := unsv2.CheckAppPlatformPermission(ctx, permission); err != nil {
		return nil, err
	}
	if err := validatePlatformRequest(operation, raw); err != nil {
		return nil, err
	}
	switch operation {
	case "identityGet", "infoGet":
		if err := decode(raw, &struct{}{}); err != nil {
			return nil, err
		}
		capabilities := []string{}
		for _, key := range []string{"openapi.base", "platform.users.read", "platform.projects.members.read", "notifications.send", "notifications.read", "asset.file.upload", "asset.file.download", "asset.file.delete"} {
			if unsv2.CheckAppPlatformPermission(ctx, key) == nil {
				capabilities = append(capabilities, key)
			}
		}
		sort.Strings(capabilities)
		if operation == "identityGet" {
			return map[string]any{"userId": strconv.FormatInt(subject.UserID, 10), "userName": subject.UserName, "authType": subject.AuthType, "apiKeyId": strconv.FormatInt(subject.APIKeyID, 10), "appId": subject.APIKeyAppID, "capabilities": capabilities}, nil
		}
		version := strings.TrimSpace(service.Config.ProductVersion)
		if version == "" {
			version = "dev"
		}
		return map[string]any{"name": "edge-enterprise", "version": version, "apiVersion": "v2", "capabilities": capabilities}, nil
	case "usersList":
		var input struct {
			Search         string   `json:"search"`
			Page           int64    `json:"page"`
			Size           int64    `json:"size"`
			RoleKeys       []string `json:"roleKeys"`
			Statuses       []string `json:"statuses"`
			UpdatedAtStart string   `json:"updatedAtStart"`
			UpdatedAtEnd   string   `json:"updatedAtEnd"`
		}
		if err := decode(raw, &input); err != nil {
			return nil, err
		}
		if err := validatePage(input.Page, input.Size); err != nil {
			return nil, err
		}
		return service.App.IAM.PlatformMembers(ctx, iam.PlatformMembersQuery{Keyword: input.Search, Page: input.Page, Size: input.Size, Roles: input.RoleKeys, Statuses: input.Statuses, UpdatedAtStart: input.UpdatedAtStart, UpdatedAtEnd: input.UpdatedAtEnd})
	case "projectMembersList":
		return projectMembers(ctx, service, subject, raw)
	case "notificationSend":
		return sendNotification(ctx, service, subject, raw)
	case "notificationStatusGet":
		var input struct {
			MessageID string `json:"messageId"`
		}
		if err := decode(raw, &input); err != nil {
			return nil, err
		}
		return service.App.Notify.GetNotification(ctx, subject.APIKeyID, input.MessageID)
	default:
		return fileOperation(ctx, service, subject, operation, raw)
	}
}

func validatePage(page, size int64) error {
	if page < 0 || page > 1_000_000 || size < 0 || size > 100 {
		return invalid("page/size", "page must be 1..1000000 and size 1..100; omit for defaults")
	}
	return nil
}
func projectMembers(ctx context.Context, service *svc.ServiceContext, subject contextx.Subject, raw []byte) (any, error) {
	var input struct {
		ProjectID string `json:"projectId"`
		Page      int64  `json:"page"`
		Size      int64  `json:"size"`
	}
	if err := decode(raw, &input); err != nil {
		return nil, err
	}
	if err := validatePage(input.Page, input.Size); err != nil {
		return nil, err
	}
	id, err := strconv.ParseInt(input.ProjectID, 10, 64)
	if err != nil || id <= 0 {
		return nil, invalid("projectId", "A positive project ID string is required")
	}
	allowed, err := repo.NewProjectRepo(ctx).UserHasProjectAccess(ctx, subject.UserID, id)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, forbidden()
	}
	rows, err := service.App.Project.ListMembers(ctx, id)
	if err != nil {
		return nil, err
	}
	page, size := input.Page, input.Size
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	start, end := (page-1)*size, page*size
	if start > int64(len(rows)) {
		start = int64(len(rows))
	}
	if end > int64(len(rows)) {
		end = int64(len(rows))
	}
	list := []map[string]any{}
	for _, row := range rows[start:end] {
		roles := []map[string]any{}
		for _, role := range row.Roles {
			roles = append(roles, map[string]any{"roleId": strconv.FormatInt(role.ID, 10), "roleKey": role.RoleKey, "roleName": role.Name, "description": role.Description})
		}
		list = append(list, map[string]any{"memberId": strconv.FormatInt(row.ID, 10), "projectId": strconv.FormatInt(row.ProjectID, 10), "userId": strconv.FormatInt(row.UserID, 10), "userName": row.UserName, "email": row.Email, "status": row.Status, "roles": roles})
	}
	return map[string]any{"list": list, "total": len(rows), "page": page, "size": size}, nil
}

func sendNotification(ctx context.Context, service *svc.ServiceContext, subject contextx.Subject, raw []byte) (any, error) {
	var input struct {
		RecipientUserID string   `json:"recipientUserId"`
		Type            string   `json:"type"`
		Title           string   `json:"title"`
		Content         string   `json:"content"`
		IdempotencyKey  string   `json:"idempotencyKey"`
		Mode            string   `json:"mode"`
		Link            string   `json:"link"`
		Channels        []string `json:"channels"`
	}
	if err := decode(raw, &input); err != nil {
		return nil, err
	}
	if input.Type == "" {
		input.Type = notify.TypeInbox
	}
	senderID, senderName := strconv.FormatInt(subject.UserID, 10), subject.UserName
	if subject.APIKeyAppID != "" {
		senderID = subject.APIKeyAppID
		senderName = subject.APIKeyAppID
	}
	n, err := notify.ValidateAndNormalize(input.RecipientUserID, input.Type, input.Title, input.Content, input.IdempotencyKey, "", input.Mode, input.Link, input.Channels, notify.SenderTypeOther, senderID, senderName, nil)
	if err != nil {
		return nil, err
	}
	// Sender identity is server-derived. UNS App IDs have a broader alphabet
	// than the legacy caller-supplied docker UUID validator; no input can override it.
	if subject.APIKeyAppID != "" {
		n.Sender.Type = notify.SenderTypeApp
	}
	return service.App.Notify.SendNotification(ctx, subject, n)
}

type uploadInput struct {
	FileName    string `json:"fileName"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
	Visibility  string `json:"visibility"`
	SessionID   string `json:"sessionId"`
}

func normalizeUpload(input *uploadInput) error {
	if input.Visibility == "" {
		input.Visibility = asset.VisibilityPrivate
	}
	if input.Visibility != asset.VisibilityPrivate && input.Visibility != asset.VisibilityPublic {
		return invalid("visibility", "Expected private or public")
	}
	return nil
}
func authorizedFile(ctx context.Context, service *svc.ServiceContext, subject contextx.Subject, path string) (repo.AssetFile, error) {
	file, err := service.App.Asset.ResolveOpenapiFile(ctx, path, subject.UserID)
	if err != nil {
		return repo.AssetFile{}, err
	}
	if subject.APIKeyAppID != "" && file.AppInstanceID != subject.APIKeyAppID {
		return repo.AssetFile{}, &apiError{404, "NOT_FOUND", "The requested file is unavailable", ""}
	}
	return file, nil
}

func fileOperation(ctx context.Context, service *svc.ServiceContext, subject contextx.Subject, operation string, raw []byte) (any, error) {
	assets := service.App.Asset
	if operation == "fileUploadCreate" {
		var input uploadInput
		if err := decode(raw, &input); err != nil {
			return nil, err
		}
		if err := normalizeUpload(&input); err != nil {
			return nil, err
		}
		value, err := assets.CreatePresignedUpload(ctx, asset.OpenapiUploadCommand{FileName: input.FileName, ContentType: input.ContentType, Size: input.SizeBytes, Visibility: input.Visibility, SessionID: input.SessionID, AppInstanceID: subject.APIKeyAppID, UserID: subject.UserID})
		if err != nil {
			return nil, err
		}
		expiresAt := value.ExpiresAt
		if expiresAt == 0 {
			expiresAt = time.Now().Add(time.Hour).UnixMilli()
		}
		return map[string]any{"fileId": strconv.FormatInt(value.FileID, 10), "filePath": value.FilePath, "upload": map[string]any{"method": "POST", "url": value.PostURL, "fields": value.PostFields, "fileField": "file"}, "sizeBytes": input.SizeBytes, "visibility": input.Visibility, "expiresAt": expiresAt}, nil
	}
	if operation == "multipartCreate" {
		var input struct {
			uploadInput
			PartSizeBytes int64 `json:"partSizeBytes"`
		}
		if err := decode(raw, &input); err != nil {
			return nil, err
		}
		if err := normalizeUpload(&input.uploadInput); err != nil {
			return nil, err
		}
		if input.PartSizeBytes < 0 || input.PartSizeBytes > 1<<30 {
			return nil, invalid("partSizeBytes", "partSizeBytes must be 0..1073741824")
		}
		value, err := assets.CreateMultipartUpload(ctx, asset.OpenapiMultipartInitCommand{FileName: input.FileName, ContentType: input.ContentType, Size: input.SizeBytes, PartSize: input.PartSizeBytes, Visibility: input.Visibility, SessionID: input.SessionID, AppInstanceID: subject.APIKeyAppID, UserID: subject.UserID})
		if err != nil {
			return nil, err
		}
		return map[string]any{"filePath": value.FilePath, "uploadId": value.UploadID, "partSizeBytes": value.PartSize, "partCount": value.PartCount, "expiresAt": value.ExpiresAt}, nil
	}
	if operation == "fileDownloadCreate" {
		var input struct {
			FilePath         string `json:"filePath"`
			ExpiresInSeconds int64  `json:"expiresInSeconds"`
		}
		if err := decode(raw, &input); err != nil {
			return nil, err
		}
		if input.ExpiresInSeconds < 0 || input.ExpiresInSeconds > asset.MaxDownloadExpireSec {
			return nil, invalid("expiresInSeconds", "Download expiry must be 0..604800 seconds")
		}
		file, err := authorizedFile(ctx, service, subject, input.FilePath)
		if err != nil {
			return nil, err
		}
		value, err := assets.OpenapiFileURL(ctx, file, input.ExpiresInSeconds)
		if err != nil {
			return nil, err
		}
		return map[string]any{"filePath": file.StorageKey, "url": value.FileURL, "expiresAt": value.ExpiresAt}, nil
	}
	if operation == "fileDelete" {
		var input struct {
			FilePath string `json:"filePath"`
		}
		if err := decode(raw, &input); err != nil {
			return nil, err
		}
		file, err := authorizedFile(ctx, service, subject, input.FilePath)
		if err != nil {
			return nil, err
		}
		if err = assets.DeleteOpenapiFile(ctx, file); err != nil {
			return nil, err
		}
		return map[string]any{"filePath": file.StorageKey, "deleted": true}, nil
	}
	var input struct {
		FilePath    string  `json:"filePath"`
		UploadID    string  `json:"uploadId"`
		PartNumbers []int32 `json:"partNumbers"`
		Parts       []struct {
			PartNumber int32  `json:"partNumber"`
			ETag       string `json:"etag"`
		} `json:"parts"`
	}
	if err := decode(raw, &input); err != nil {
		return nil, err
	}
	file, err := authorizedFile(ctx, service, subject, input.FilePath)
	if err != nil {
		return nil, err
	}
	if _, err = assets.ResolveMultipartTemp(ctx, file.FileKey, input.UploadID); err != nil {
		return nil, err
	}
	switch operation {
	case "multipartParts":
		if len(input.Parts) > 0 || len(input.PartNumbers) < 1 || len(input.PartNumbers) > 1000 {
			return nil, invalid("partNumbers", "Supply 1..1000 part numbers and no completion parts")
		}
		seen := map[int32]bool{}
		for _, n := range input.PartNumbers {
			if n < 1 || n > 10000 || seen[n] {
				return nil, invalid("partNumbers", "Part numbers must be distinct integers from 1 to 10000")
			}
			seen[n] = true
		}
		values, err := assets.MultipartPartURLs(ctx, file.FileKey, input.UploadID, input.PartNumbers)
		if err != nil {
			return nil, err
		}
		list := []map[string]any{}
		for _, v := range values {
			list = append(list, map[string]any{"partNumber": v.PartNumber, "url": v.URL, "expiresAt": v.ExpiresAt})
		}
		return map[string]any{"list": list}, nil
	case "multipartComplete":
		if len(input.PartNumbers) > 0 || len(input.Parts) < 1 || len(input.Parts) > 10000 {
			return nil, invalid("parts", "Supply 1..10000 completed parts and no partNumbers")
		}
		parts := make([]filestore.CompletePart, len(input.Parts))
		seen := map[int32]bool{}
		for i, p := range input.Parts {
			if p.PartNumber < 1 || p.PartNumber > 10000 || seen[p.PartNumber] || strings.TrimSpace(p.ETag) == "" {
				return nil, invalid("parts", "Distinct valid part numbers and returned ETags are required")
			}
			seen[p.PartNumber] = true
			parts[i] = filestore.CompletePart{PartNumber: p.PartNumber, ETag: p.ETag}
		}
		value, err := assets.MultipartComplete(ctx, file.FileKey, input.UploadID, parts)
		if err != nil {
			return nil, err
		}
		return map[string]any{"fileId": strconv.FormatInt(value.FileID, 10), "filePath": value.FilePath, "url": value.FileURL, "sizeBytes": value.SizeBytes, "expiresAt": value.ExpiresAt, "status": "completed"}, nil
	case "multipartAbort":
		if len(input.PartNumbers) > 0 || len(input.Parts) > 0 {
			return nil, invalid("parts", "Abort accepts only filePath and uploadId")
		}
		aborted, err := assets.MultipartAbort(ctx, file.FileKey, input.UploadID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"filePath": file.StorageKey, "aborted": aborted}, nil
	}
	return nil, &apiError{404, "NOT_FOUND", "Unknown file operation", ""}
}
