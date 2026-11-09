import dayjs from 'dayjs';
import type { AuditLogPageParams } from '@/apis/core-api/audit-log';

export type AuditFilters = Omit<AuditLogPageParams, 'pageNo' | 'pageSize'>;
export const filterKeys = [
  'startTime',
  'endTime',
  'operatorKeyword',
  'operatorId',
  'resType',
  'businessType',
  'result',
  'resourceKeyword',
  'resId',
  'scopeType',
  'scopeId',
  'scopeName',
  'requestId',
  'ip',
] as const;

export const defaultFilters = (): AuditFilters => ({ startTime: dayjs().subtract(7, 'day').toISOString() });
export const cleanFilters = (filters: AuditFilters): AuditFilters =>
  Object.fromEntries(
    Object.entries(filters)
      .map(([key, value]) => [key, typeof value === 'string' ? value.trim() : value])
      .filter(([, value]) => value !== undefined && value !== '')
  );

export const readFilters = (params: URLSearchParams): AuditFilters => {
  if (!params.has('auditQuery')) return defaultFilters();
  const result: Record<string, string | number> = {};
  for (const key of filterKeys) {
    const value = params.get(key);
    if (!value) continue;
    if (['scopeType', 'scopeId', 'operatorId'].includes(key)) {
      if (Number.isSafeInteger(Number(value))) result[key] = Number(value);
    } else if (key === 'startTime' || key === 'endTime') {
      const date = dayjs(/^\d+$/.test(value) ? Number(value) : value);
      if (date.isValid()) result[key] = date.toISOString();
    } else result[key] = value;
  }
  return result;
};

export const writeFilters = (params: URLSearchParams, filters: AuditFilters, page: number, size: number) => {
  const next = new URLSearchParams(params);
  filterKeys.forEach((key) => next.delete(key));
  Object.entries(cleanFilters(filters)).forEach(([key, value]) => next.set(key, String(value)));
  next.set('auditQuery', '1');
  next.set('page', String(page));
  next.set('size', String(size));
  return next;
};
