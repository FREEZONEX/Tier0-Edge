import type { MetaSchema } from '@/apis/core-api/uns-metadata';
import type { WorkflowTrace, WorkflowValue } from './program-workflow';
import { outputAt } from './workflow-data';

const safe = (key: string) => !['__proto__', 'prototype', 'constructor'].includes(key) && !key.includes('.');
export const isRecord = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value);
export function recordLists(
  value: unknown,
  prefix = '',
  depth = 0
): { path: string; records: Record<string, unknown>[] }[] {
  if (Array.isArray(value)) return value.every(isRecord) ? [{ path: prefix, records: value }] : [];
  if (!isRecord(value) || depth >= 8) return [];
  return Object.entries(value)
    .filter(([key]) => safe(key))
    .flatMap(([key, child]) => recordLists(child, prefix ? `${prefix}.${key}` : key, depth + 1));
}
export function bindingValue(value: WorkflowValue, results: WorkflowTrace[]): unknown {
  if (value.kind === 'field') return outputAt(results.find((trace) => trace.id === value.step)?.output, value.path);
  if (value.kind === 'text') return value.value;
  try {
    return JSON.parse(value.value);
  } catch {
    return undefined;
  }
}
export const recordFields = (records: Record<string, unknown>[]) =>
  [...new Set(records.flatMap((record) => Object.keys(record)))].filter(safe);
export function identityFields(records: Record<string, unknown>[]) {
  return recordFields(records)
    .filter((key) => {
      const values = records.map((row) => row[key]);
      return (
        values.every(
          (value) =>
            (typeof value === 'string' && !!value.trim()) || (typeof value === 'number' && Number.isSafeInteger(value))
        ) && new Set(values.map(String)).size === values.length
      );
    })
    .sort((a, b) => Number(/(^id$|_id$|Id$)/.test(b)) - Number(/(^id$|_id$|Id$)/.test(a)));
}
export function inferRecordSchema(records: Record<string, unknown>[]): MetaSchema {
  const infer = (values: unknown[], depth: number): MetaSchema | undefined => {
    const typeOf = (v: unknown) =>
      v === null ? 'null' : Array.isArray(v) ? 'array' : typeof v === 'number' ? 'number' : typeof v;
    const types = [...new Set(values.map(typeOf))];
    // 当前平台不支持联合类型；不为 null、混合类型或未知数组元素伪造约束。
    if (types.length !== 1 || types[0] === 'null' || depth > 7) return;
    const schema: MetaSchema = { type: types[0] };
    if (types.includes('object') && depth < 8) {
      const rows = values.filter(isRecord);
      schema.properties = Object.fromEntries(
        recordFields(rows)
          .filter((key) => /^[A-Za-z0-9][A-Za-z0-9_:-]{0,127}$/.test(key))
          .slice(0, 128)
          .flatMap((key) => {
            const field = infer(
              rows.filter((row) => Object.hasOwn(row, key)).map((row) => row[key]),
              depth + 1
            );
            return field ? [[key, field]] : [];
          })
      );
      schema.additionalProperties = true;
    }
    if (types.includes('array') && depth < 8) {
      const children = values.filter(Array.isArray).flat();
      schema.items = infer(children, depth + 1);
      if (!schema.items) return;
    }
    return schema;
  };
  return infer(records, 0) || { type: 'object', properties: {}, additionalProperties: true };
}
