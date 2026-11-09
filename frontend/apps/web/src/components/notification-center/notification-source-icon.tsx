import { BellDot, Box, type LucideIcon } from 'lucide-react';
import { useState, type FC, type ReactNode } from 'react';
import type { NotificationItem, NotificationSourceType } from '@/apis/core-api/notifications';

/** 与 Cloud NoticeRowIcon 一致：系统消息用 BellDot，应用消息用应用图标或 Box。 */
export const resolveNotificationSourceIcon = (item: {
  sourceId?: string;
  sourceName?: string;
  sourceType?: NotificationSourceType;
  targetPath?: string;
}): LucideIcon => {
  return item.sourceType === 'app' ? Box : BellDot;
};

/**
 * 发信应用的图标：优先用后端给的地址，取不到就回落到通用图形。
 *
 * 与 Launchpad 的 AppIcon 同一口径——地址指向 /api/core/launchpad/apps/{id}/icon，
 * 应用删掉或没权限时会 404/401，没有兜底就会在面板里留一个碎图占位。
 */
const NotificationSourceImage: FC<{ url: string; fallback: ReactNode }> = ({ url, fallback }) => {
  // 记录加载失败的具体 url：url 变化时天然重置失败态，无需 effect
  const [failedUrl, setFailedUrl] = useState<string>();
  if (failedUrl === url) return <>{fallback}</>;
  return <img src={url} alt="" onError={() => setFailedUrl(url)} />;
};

export const renderNotificationSourceIcon = (
  item: Pick<NotificationItem, 'sourceId' | 'sourceName' | 'sourceType' | 'sourceIconUrl' | 'targetPath'>,
  size = 14
): ReactNode => {
  const Icon = resolveNotificationSourceIcon(item);
  const fallback = <Icon size={size} aria-hidden />;
  if (item.sourceType === 'app' && item.sourceIconUrl) {
    return <NotificationSourceImage url={item.sourceIconUrl} fallback={fallback} />;
  }
  return fallback;
};
