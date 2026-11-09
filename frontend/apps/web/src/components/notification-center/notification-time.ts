import dayjs from 'dayjs';
import { formatTimestamp } from '@/utils/format';

export type NotificationTimeTranslator = (
  key: 'justNow' | 'minutesAgo' | 'todayAt' | 'yesterdayAt',
  fallback: string,
  values?: Record<string, unknown>
) => string;

export const formatNotificationListTime = (value: string, translate: NotificationTimeTranslator) => {
  const date = dayjs(value);
  if (!date.isValid()) return '';

  const now = dayjs();
  const diffMinutes = now.diff(date, 'minute');
  if (diffMinutes < 1) return translate('justNow', 'Just now');
  if (diffMinutes < 60) {
    return translate('minutesAgo', '{count} min ago', { count: diffMinutes });
  }

  const time = date.format('HH:mm');
  if (date.isSame(now, 'day')) return translate('todayAt', 'Today {time}', { time });
  if (date.isSame(now.subtract(1, 'day'), 'day')) {
    return translate('yesterdayAt', 'Yesterday {time}', { time });
  }
  if (date.isSame(now, 'year')) return date.format('MM-DD HH:mm');
  return date.format('YYYY-MM-DD HH:mm');
};

export const formatNotificationDetailTime = (value: string) => {
  const date = dayjs(value);
  return date.isValid() ? formatTimestamp(value) : '';
};

/** 通知列表、快捷详情和完整详情沿用项目统一的本地化日期格式。 */
export const formatNotificationPopoverTime = formatNotificationDetailTime;
