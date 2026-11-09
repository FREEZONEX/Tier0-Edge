import {
  isNotificationExternalLink,
  isNotificationTargetPath,
  type NotificationItem,
} from '@/apis/core-api/notifications';
import ComButton from '@/components/com-button';
import ComEmpty from '@/components/com-empty';
import { ChevronLeft, Launch, Time, TrashCan } from '@/components/lucide-icon/carbon';
import { useTranslate } from '@/hooks';
import { Skeleton } from 'antd';
import classNames from 'classnames';
import { renderNotificationSourceIcon } from './notification-source-icon';
import { formatNotificationSourceName } from './notification-source-name';
import { formatNotificationDetailTime, formatNotificationPopoverTime } from './notification-time';
import styles from './index.module.scss';

interface NotificationDetailProps {
  item?: NotificationItem | null;
  loading?: boolean;
  emptyText: string;
  backLabel: string;
  sourceLabel: string;
  sourceTypeLabel: string;
  receivedAtLabel: string;
  contentLabel: string;
  previewLabel: string;
  openSourceLabel: string;
  sourceUnavailableLabel: string;
  deleteLabel?: string;
  showSourceAction?: boolean;
  /** Popover uses the compact Figma detail layout. */
  variant?: 'popover' | 'page' | 'modal';
  expandLabel?: string;
  onExpand?: () => void;
  onBack?: () => void;
  onDelete?: (item: NotificationItem) => void;
  onOpenSource?: (item: NotificationItem) => void;
}

const NotificationDetail = ({
  item,
  loading,
  emptyText,
  backLabel,
  openSourceLabel,
  sourceUnavailableLabel,
  deleteLabel,
  showSourceAction = true,
  variant = 'page',
  expandLabel,
  onExpand,
  onBack,
  onDelete,
  onOpenSource,
}: NotificationDetailProps) => {
  const formatMessage = useTranslate();
  if (loading && !item) {
    return (
      <div className={styles['detail-loading']}>
        <Skeleton active avatar paragraph={{ rows: 6 }} />
      </div>
    );
  }

  if (!item) {
    return (
      <div className={styles['empty-state']}>
        <ComEmpty description={emptyText} />
      </div>
    );
  }

  const isExternalLink = isNotificationExternalLink(item.targetPath);
  const canOpenSource = item.targetAvailable !== false && (isExternalLink || isNotificationTargetPath(item.targetPath));
  const hasSourceAction = Boolean(item.targetPath?.trim());
  const isSystem = item.sourceType === 'platform';
  const isPopover = variant === 'popover';
  const timeText = isPopover
    ? formatNotificationPopoverTime(item.createdAt)
    : formatNotificationDetailTime(item.createdAt);

  return (
    <article
      className={classNames(styles['message-detail'], {
        [styles['message-detail-popover']]: isPopover,
        [styles['message-detail-modal']]: variant === 'modal',
      })}
    >
      {onBack ? (
        <button type="button" className={styles['detail-back']} onClick={onBack}>
          <ChevronLeft size={18} />
          <span>{backLabel}</span>
        </button>
      ) : null}
      {variant !== 'modal' ? <h2>{item.title}</h2> : null}
      <div className={styles['detail-meta']}>
        <div className={styles['detail-meta-source']}>
          <div
            className={classNames(styles['detail-avatar'], {
              [styles['source-avatar-system']]: isSystem,
              [styles['source-avatar-app']]: !isSystem,
            })}
            aria-hidden
          >
            {renderNotificationSourceIcon(item, 12)}
          </div>
          <strong>
            {formatNotificationSourceName(
              item,
              formatMessage('notificationManage.sourcePlatform', undefined, 'Platform')
            )}
          </strong>
        </div>
        <div className={styles['detail-meta-time']}>
          <Time size={14} aria-hidden />
          <time dateTime={item.createdAt}>{timeText || '-'}</time>
        </div>
      </div>
      <div className={styles['detail-divider']} aria-hidden />
      <section className={styles['detail-content-section']}>
        <div className={styles['detail-content']}>{item.content}</div>
      </section>
      {onExpand || onDelete || (showSourceAction && hasSourceAction) ? (
        <div className={styles['detail-actions']}>
          {onExpand && <ComButton onClick={onExpand}>{expandLabel}</ComButton>}
          {onDelete && deleteLabel ? (
            <ComButton danger icon={<TrashCan size={16} />} onClick={() => onDelete(item)}>
              {deleteLabel}
            </ComButton>
          ) : null}
          {showSourceAction && hasSourceAction ? (
            <ComButton
              type="primary"
              disabled={!canOpenSource}
              icon={<Launch size={16} />}
              href={canOpenSource && isExternalLink ? item.targetPath : undefined}
              target={isExternalLink ? '_blank' : undefined}
              rel={isExternalLink ? 'noopener noreferrer' : undefined}
              onClick={isExternalLink ? undefined : () => onOpenSource?.(item)}
            >
              {canOpenSource ? openSourceLabel : sourceUnavailableLabel}
            </ComButton>
          ) : null}
        </div>
      ) : null}
    </article>
  );
};

export default NotificationDetail;
