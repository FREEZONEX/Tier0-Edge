import type { NotificationSource } from '@/apis/core-api/notifications';

/** 仅翻译后端的平台默认名称，保留应用名和具体系统模块名称。 */
export const formatNotificationSourceName = (
  source: Pick<NotificationSource, 'sourceType' | 'sourceName'>,
  platformLabel: string
) => (source.sourceType === 'platform' && source.sourceName === 'Platform' ? platformLabel : source.sourceName);
