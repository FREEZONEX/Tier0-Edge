import { formatTimestamp } from '@/utils/format';

export function formatRegistryTime(value: number) {
  if (!value) return '—';
  // Source / Stream 的现有投影记录包含秒和毫秒时间戳，日期组件统一使用毫秒。
  const milliseconds = value < 1e12 ? value * 1000 : value;
  return formatTimestamp(milliseconds);
}
