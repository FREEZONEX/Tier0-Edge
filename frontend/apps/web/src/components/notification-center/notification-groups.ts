import type { NotificationItem } from '@/apis/core-api/notifications';
import dayjs from 'dayjs';

export type NotificationGroupKey = 'today' | 'yesterday' | 'last7Days' | 'older';

export type NotificationListGroup = {
  id: NotificationGroupKey;
  labelKey: NotificationGroupKey;
  labelFallback: string;
  items: NotificationItem[];
};

const GROUP_ORDER: NotificationGroupKey[] = ['today', 'yesterday', 'last7Days', 'older'];

const GROUP_FALLBACK: Record<NotificationGroupKey, string> = {
  today: 'Today',
  yesterday: 'Yesterday',
  last7Days: 'Last 7 days',
  older: 'Older',
};

const resolveGroupKey = (createdAt: string): NotificationGroupKey => {
  const date = dayjs(createdAt);
  if (!date.isValid()) return 'older';

  const now = dayjs().startOf('day');
  const day = date.startOf('day');
  const daysAgo = now.diff(day, 'day');

  if (daysAgo <= 0) return 'today';
  if (daysAgo === 1) return 'yesterday';
  if (daysAgo <= 7) return 'last7Days';
  return 'older';
};

export const groupNotificationItems = (items: NotificationItem[]): NotificationListGroup[] => {
  const buckets = new Map<NotificationGroupKey, NotificationListGroup>();

  for (const key of GROUP_ORDER) {
    buckets.set(key, {
      id: key,
      labelKey: key,
      labelFallback: GROUP_FALLBACK[key],
      items: [],
    });
  }

  for (const item of items) {
    const key = resolveGroupKey(item.createdAt);
    buckets.get(key)!.items.push(item);
  }

  return GROUP_ORDER.map((key) => buckets.get(key)!).filter((group) => group.items.length > 0);
};
