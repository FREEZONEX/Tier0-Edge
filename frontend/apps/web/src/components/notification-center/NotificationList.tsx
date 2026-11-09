import type { NotificationItem } from '@/apis/core-api/notifications';
import ComButton from '@/components/com-button';
import ComEmpty from '@/components/com-empty';
import { TrashCan } from '@/components/lucide-icon/carbon';
import { useTranslate } from '@/hooks';
import { Checkbox, Skeleton, Tooltip } from 'antd';
import classNames from 'classnames';
import { groupNotificationItems } from './notification-groups';
import { renderNotificationSourceIcon } from './notification-source-icon';
import { formatNotificationSourceName } from './notification-source-name';
import { formatNotificationPopoverTime } from './notification-time';
import styles from './index.module.scss';

export interface NotificationListProps {
  items: NotificationItem[];
  loading?: boolean;
  selectedId?: string;
  checkedIds?: Set<string>;
  selectable?: boolean;
  /** Popover uses the compact Figma layout; page keeps selectable/delete affordances. */
  variant?: 'popover' | 'page';
  emptyText: string;
  unreadLabel: string;
  selectLabel?: string;
  deleteLabel?: string;
  onOpen: (item: NotificationItem) => void;
  onCheck?: (item: NotificationItem, checked: boolean) => void;
  onDelete?: (item: NotificationItem) => void;
}

const NotificationList = ({
  items,
  loading,
  selectedId,
  checkedIds,
  selectable,
  variant = 'page',
  emptyText,
  unreadLabel,
  selectLabel,
  deleteLabel,
  onOpen,
  onCheck,
  onDelete,
}: NotificationListProps) => {
  const formatMessage = useTranslate();
  const isPopover = variant === 'popover';

  if (loading && !items.length) {
    return (
      <div className={styles['list-loading']}>
        {[0, 1, 2, 3].map((key) => (
          <Skeleton key={key} active avatar paragraph={{ rows: 2 }} title={{ width: '52%' }} />
        ))}
      </div>
    );
  }

  if (!items.length) {
    return (
      <div className={styles['empty-state']}>
        <ComEmpty description={emptyText} />
      </div>
    );
  }

  const renderRow = (item: NotificationItem) => {
    const isUnread = !item.readAt;
    const isSystem = item.sourceType === 'platform';
    const sourceName = formatNotificationSourceName(
      item,
      formatMessage('notificationManage.sourcePlatform', undefined, 'Platform')
    );
    const isInteractiveTarget = (target: EventTarget | null) =>
      target instanceof Element &&
      Boolean(
        target.closest(`.${styles['row-checkbox']}, .${styles['row-delete-slot']}, .${styles['row-delete-btn']}`)
      );

    return (
      <div
        key={item.id}
        className={classNames(styles['message-row'], styles['message-row-clean'], {
          [styles['message-row-popover']]: isPopover,
          [styles.selected]: selectedId === item.id,
        })}
        role="button"
        tabIndex={0}
        aria-label={`${isUnread ? `${unreadLabel}. ` : ''}${sourceName}. ${item.title}`}
        onClick={(event) => {
          if (isInteractiveTarget(event.target)) return;
          onOpen(item);
        }}
        onKeyDown={(event) => {
          if (event.target !== event.currentTarget) return;
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            onOpen(item);
          }
        }}
      >
        {selectable ? (
          <span
            className={styles['row-checkbox']}
            onClick={(event) => event.stopPropagation()}
            onMouseDown={(event) => event.stopPropagation()}
          >
            <Checkbox
              checked={checkedIds?.has(item.id)}
              aria-label={selectLabel ? `${selectLabel}: ${item.title}` : item.title}
              onChange={(event) => onCheck?.(item, event.target.checked)}
            />
          </span>
        ) : null}
        {isPopover ? (
          <div
            className={classNames(styles['source-avatar'], {
              [styles['source-avatar-system']]: isSystem,
              [styles['source-avatar-app']]: !isSystem,
            })}
            aria-hidden
          >
            {renderNotificationSourceIcon(item, 14)}
          </div>
        ) : null}
        <div className={styles['row-main']}>
          <div className={styles['row-meta']}>
            <div className={styles['row-source']}>
              {!isPopover ? (
                <div
                  className={classNames(styles['source-avatar'], styles['source-avatar-inline'], {
                    [styles['source-avatar-system']]: isSystem,
                    [styles['source-avatar-app']]: !isSystem,
                  })}
                  aria-hidden
                >
                  {renderNotificationSourceIcon(item, 12)}
                </div>
              ) : null}
              <span className={styles['source-name']}>{sourceName}</span>
            </div>
            <span className={styles['message-time']}>{formatNotificationPopoverTime(item.createdAt) || '-'}</span>
          </div>
          <div className={styles['row-title-wrap']}>
            <span className={styles['row-title']}>{item.title}</span>
            {isUnread ? (
              <span className={classNames(styles['unread-dot'], styles['unread-dot-popover'])} aria-hidden />
            ) : null}
          </div>
          <div className={styles['row-content']}>{item.content}</div>
        </div>
        {onDelete && deleteLabel ? (
          <div className={styles['row-delete-slot']}>
            <Tooltip title={deleteLabel}>
              <ComButton
                className={styles['row-delete-btn']}
                type="text"
                danger
                aria-label={deleteLabel}
                icon={<TrashCan size={14} />}
                onClick={(event) => {
                  event.stopPropagation();
                  onDelete(item);
                }}
              />
            </Tooltip>
          </div>
        ) : null}
      </div>
    );
  };

  const groupedItems = groupNotificationItems(items);

  return (
    <div
      className={classNames(styles['message-list'], styles['message-list-clean'], {
        [styles['message-list-popover']]: isPopover,
      })}
    >
      {groupedItems.map((group) => (
        <section key={group.id} className={styles['list-group']}>
          <header className={styles['list-group-header']}>
            {formatMessage(`messageCenter.${group.labelKey}`, undefined, group.labelFallback)}
          </header>
          {group.items.map((item) => renderRow(item))}
        </section>
      ))}
    </div>
  );
};

export default NotificationList;
