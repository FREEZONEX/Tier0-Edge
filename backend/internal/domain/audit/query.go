package audit

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"backend/internal/repo"
)

var ErrInvalidFilter = errors.New("auditLog.invalidFilter")
var ErrExportLimit = errors.New("auditLog.exportLimit")

// 顺序与 CSV 数据列一致，页面使用后端语言包翻译列名。
var csvHeaderKeys = []string{
	"auditLog.csv.createdAt", "auditLog.csv.operatorId", "auditLog.csv.operatorName", "auditLog.csv.scopeType",
	"auditLog.csv.scopeId", "auditLog.csv.scopeName", "auditLog.module", "auditLog.action",
	"auditLog.csv.resourceId", "auditLog.csv.resourceName", "auditLog.csv.resourcePath", "auditLog.result",
	"auditLog.code", "auditLog.csv.resultMessage", "auditLog.requestId", "auditLog.ip",
}

func buildFilter(q PageQuery) (repo.AuditLogFilter, error) {
	f := repo.AuditLogFilter{
		PageNo: q.PageNo, PageSize: q.PageSize, ScopeType: q.ScopeType, ScopeID: q.ScopeID,
		ScopeName: strings.TrimSpace(q.ScopeName), ResType: strings.TrimSpace(q.ResType), ResID: strings.TrimSpace(q.ResID),
		BusinessTypeCode: repo.AuditBusinessTypeCode(q.BusinessType), OperatorKeyword: strings.TrimSpace(q.OperatorKeyword),
		OperatorID: q.OperatorID, ResourceKeyword: strings.TrimSpace(q.ResourceKeyword), RequestID: strings.TrimSpace(q.RequestID),
		IP: strings.TrimSpace(q.IP), Result: strings.TrimSpace(q.Result), Code: q.Code,
		StartTime: parseTime(q.StartTime), EndTime: parseTime(q.EndTime),
	}
	if q.ScopeType < -1 || q.ScopeType > ScopeTypeProject || q.ScopeID < 0 || q.OperatorID < 0 ||
		(q.BusinessType != "" && (f.BusinessTypeCode <= 0 || f.BusinessTypeCode > repo.AuditBizView)) ||
		(q.StartTime != "" && f.StartTime <= 0) || (q.EndTime != "" && f.EndTime <= 0) ||
		(f.EndTime > 0 && f.StartTime > f.EndTime) || (f.IP != "" && net.ParseIP(f.IP) == nil) ||
		(f.Result != "" && f.Result != "success" && f.Result != "failed" && f.Result != "accepted") {
		return f, ErrInvalidFilter
	}
	for _, value := range []string{f.ScopeName, f.ResType, f.ResID, f.OperatorKeyword, f.ResourceKeyword, f.RequestID} {
		if len([]rune(value)) > 512 {
			return f, ErrInvalidFilter
		}
	}
	return f, nil
}

func (s *Service) Options(ctx context.Context, query PageQuery) ([]repo.AuditOption, error) {
	f, err := buildFilter(query)
	if err != nil {
		return nil, err
	}
	return s.repo.AuditFilterOptions(ctx, f)
}

func (s *Service) Export(ctx context.Context, query PageQuery) (map[string]any, error) {
	f, err := buildFilter(query)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.ExportAuditLogs(ctx, f)
	if err != nil {
		return nil, err
	}
	if len(items) > repo.AuditExportLimit {
		return nil, ErrExportLimit
	}
	var output bytes.Buffer
	output.WriteString("\uFEFF")
	w := csv.NewWriter(&output)
	w.UseCRLF = true
	_ = w.Write([]string{"createdAt (UTC)", "operatorId", "operatorName", "scopeType", "scopeId", "scopeName", "module", "action", "resourceId", "resourceName", "resourcePath", "result", "code", "resultMessage", "requestId", "ip"})
	for _, item := range items {
		row := []string{item.CreatedTime.UTC().Format(time.RFC3339Nano), item.OperatorID, item.OperatorName, strconv.Itoa(item.ScopeType), strconv.FormatInt(item.ScopeID, 10), item.ScopeName, item.ResType, item.BusinessType, item.ResID, item.ResName, item.ResourcePath, item.Result, strconv.FormatInt(item.Code, 10), item.ResultMsg, item.RequestID, item.IP}
		for i := range row {
			row[i] = csvCell(row[i])
		}
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return map[string]any{"content": output.String(), "headerKeys": csvHeaderKeys, "fileName": "audit-logs-" + time.Now().UTC().Format("20060102-150405") + ".csv", "total": len(items)}, nil
}

func csvCell(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n\uFEFF")
	if strings.HasPrefix(trimmed, "=") || strings.HasPrefix(trimmed, "+") || strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "@") || strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") || strings.HasPrefix(value, "\n") {
		return "'" + value
	}
	return value
}
