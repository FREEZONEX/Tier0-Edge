package repo

import (
	"context"
	"strings"
)

// AuditExportLimit 限制同步导出的内存和响应体；超限必须缩小查询范围。
const AuditExportLimit = 10000

type AuditOption struct {
	ResType          string `json:"resType"`
	BusinessTypeCode int64  `json:"-"`
	BusinessType     string `json:"businessType"`
}

func (r *AuditRepo) AuditFilterOptions(ctx context.Context, filter AuditLogFilter) ([]AuditOption, error) {
	filter.ResType, filter.BusinessTypeCode = "", 0
	ids, err := r.userIDsMatchingEmail(ctx, filter.OperatorKeyword)
	if err != nil {
		return nil, err
	}
	filter.OperatorUserIDs = ids
	var options []AuditOption
	err = applyAuditFilter(r.db.WithContext(ctx).Model(&AuditLog{}), filter).
		Select("res_type, business_type AS business_type_code").Distinct().
		Order("res_type, business_type_code").Find(&options).Error
	for i := range options {
		options[i].BusinessType = AuditBusinessTypeName(options[i].BusinessTypeCode)
	}
	return options, err
}

// ExportAuditLogs 用单条有界 SELECT 获取一致的结果集合，不用 offset 分页拼接移动中的日志。
func (r *AuditRepo) ExportAuditLogs(ctx context.Context, filter AuditLogFilter) ([]AuditLog, error) {
	ids, err := r.userIDsMatchingEmail(ctx, filter.OperatorKeyword)
	if err != nil {
		return nil, err
	}
	filter.OperatorUserIDs = ids
	var items []AuditLog
	err = applyAuditFilter(r.db.WithContext(ctx).Model(&AuditLog{}), filter).
		Select("id, user_id, operator_name, res_type, res_id, res_name, resource_path, business_type, code, result, result_msg, scope_type, scope_id, scope_name, request_id, ip, created_time").
		Order("created_time DESC, id DESC").Limit(AuditExportLimit + 1).Find(&items).Error
	if err != nil {
		return nil, err
	}
	normalizeAuditLogs(items)
	return items, nil
}

func auditLike(value string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.TrimSpace(value)) + "%"
}
