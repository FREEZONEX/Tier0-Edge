import { coreApi } from './core-adapter';
import { getIntl } from '@/stores/i18n-store';

export interface AuditLogPageParams {
  pageNo?: number;
  pageSize?: number;
  scopeType?: number;
  scopeId?: number;
  resType?: string;
  businessType?: string;
  operatorKeyword?: string;
  code?: number;
  startTime?: string;
  endTime?: string;
  scopeName?: string;
  operatorId?: number;
  resourceKeyword?: string;
  resId?: string;
  requestId?: string;
  ip?: string;
  result?: string;
}

export interface AuditLogItem {
  id: number;
  requestId?: string;
  operatorId?: string;
  operatorName?: string;
  operatorEmail?: string;
  scopeType: number;
  scopeId?: number;
  scopeName?: string;
  resType: string;
  resId?: string;
  resName?: string;
  businessType: string;
  code: number;
  resultMsg?: string;
  detailJson?: string;
  ip?: string;
  userAgent?: string;
  createdAt: number;
  resourcePath?: string;
  result?: string;
  detailValid?: boolean;
}

export interface AuditLogPageResp {
  pageNo: number;
  pageSize: number;
  total: number;
  data: AuditLogItem[];
}

export const getAuditLogPage = async (data?: AuditLogPageParams): Promise<AuditLogPageResp> =>
  coreApi.post('/audit-logs/page', data);
export const getAuditLogDetail = async (id: number | string): Promise<AuditLogItem> => coreApi.get(`/audit-logs/${id}`);

export interface AuditFilterOption {
  resType: string;
  businessType: string;
}
export const getAuditLogOptions = (data?: AuditLogPageParams): Promise<AuditFilterOption[]> =>
  coreApi.post('/audit-logs/filter-options', data);
export const exportAuditLogs = async (
  data?: AuditLogPageParams
): Promise<{ content: string; fileName: string; total: number }> => {
  const result: { content: string; headerKeys?: string[]; fileName: string; total: number } = await coreApi.post(
    '/audit-logs/export',
    data
  );
  const headerEnd = result.content.indexOf('\r\n');
  if (result.headerKeys?.length && headerEnd >= 0) {
    const header = result.headerKeys.map((key) => `"${getIntl(key).replaceAll('"', '""')}"`).join(',');
    result.content = `\uFEFF${header}${result.content.slice(headerEnd)}`;
  }
  return result;
};
