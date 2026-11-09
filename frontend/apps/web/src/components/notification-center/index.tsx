import {
  getNotificationDetail,
  getNotificationPage,
  getNotificationUnreadCount,
  isNotificationTargetPath,
  markAllNotificationsRead,
  markNotificationsRead,
  type NotificationItem,
  type NotificationSourceType,
} from '@/apis/core-api/notifications';
import ComButton from '@/components/com-button';
import ComDrawer from '@/components/com-drawer';
import ComEmpty from '@/components/com-empty';
import { Notification } from '@/components/lucide-icon/carbon';
import ProModal from '@/components/pro-modal';
import { useMediaSize, useTranslate } from '@/hooks';
import {
  isLaunchpadStandaloneAllowedPath,
  isLaunchpadStandaloneSite,
  withLaunchpadStandaloneQuery,
} from '@/utils/launchpad-site';
import { App, Badge, Popover, Tooltip } from 'antd';
import classNames from 'classnames';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import MarkAllReadIcon from './MarkAllReadIcon';
import NotificationDetail from './NotificationDetail';
import NotificationList from './NotificationList';
import { NOTIFICATIONS_CHANGED_EVENT, notifyNotificationChanged } from './notification-events';
import styles from './index.module.scss';

const drawerPageSize = 20;
type NotificationDrawerSourceType = 'all' | NotificationSourceType;

const shouldShowSourceAction = (item: NotificationItem) =>
  !isLaunchpadStandaloneSite() || Boolean(item.targetPath && isLaunchpadStandaloneAllowedPath(item.targetPath));

const NotificationCenter = () => {
  const formatMessage = useTranslate();
  const navigate = useNavigate();
  const { message } = App.useApp();
  const { isH5 } = useMediaSize();
  const [view, setView] = useState<'closed' | 'inbox' | 'expanded'>('closed');
  const open = view === 'inbox';
  const readerOpen = view !== 'closed';
  const [unreadCount, setUnreadCount] = useState(0);
  const [sourceType, setSourceType] = useState<NotificationDrawerSourceType>('all');
  const [items, setItems] = useState<NotificationItem[]>([]);
  const [appUnreadCount, setAppUnreadCount] = useState(0);
  const [systemUnreadCount, setSystemUnreadCount] = useState(0);
  const unreadCountRequest = useRef(0);
  const categoryCountRequest = useRef(0);
  const [selectedItem, setSelectedItem] = useState<NotificationItem | null>(null);
  const [loading, setLoading] = useState(false);
  const [detailLoading, setDetailLoading] = useState(false);
  const [loadFailed, setLoadFailed] = useState(false);
  const t = useCallback(
    (key: string, fallback: string, values?: Record<string, unknown>) =>
      formatMessage(`messageCenter.${key}`, values, fallback),
    [formatMessage]
  );

  const refreshUnreadCount = useCallback(async () => {
    const requestId = ++unreadCountRequest.current;
    try {
      const count = await getNotificationUnreadCount();
      if (requestId === unreadCountRequest.current) setUnreadCount(count);
    } catch {
      // The global entry stays quiet when the notification service is temporarily unavailable.
    }
  }, []);

  const refreshCategoryCounts = useCallback(async () => {
    const requestId = ++categoryCountRequest.current;
    try {
      const [appPage, systemPage] = await Promise.all([
        getNotificationPage({ pageNo: 1, pageSize: 1, sourceType: 'app', readStatus: 'unread' }),
        getNotificationPage({ pageNo: 1, pageSize: 1, sourceType: 'platform', readStatus: 'unread' }),
      ]);
      // 已读操作会触发新的查询，旧请求不得覆盖更新后的数量。
      if (requestId !== categoryCountRequest.current) return;
      setAppUnreadCount(appPage.total);
      setSystemUnreadCount(systemPage.total);
    } catch {
      // Counts are decorative; list load errors surface separately.
    }
  }, []);

  const fetchMessages = useCallback(async () => {
    setLoading(true);
    setLoadFailed(false);
    try {
      const response = await getNotificationPage({
        pageNo: 1,
        pageSize: drawerPageSize,
        sourceType: sourceType === 'all' ? undefined : sourceType,
      });
      setItems(response.data);
    } catch {
      setLoadFailed(true);
    } finally {
      setLoading(false);
    }
  }, [sourceType]);

  useEffect(() => {
    const refreshCounts = () => {
      void refreshUnreadCount();
      if (readerOpen) void refreshCategoryCounts();
    };
    const initialTimer = window.setTimeout(refreshCounts, 0);
    const interval = window.setInterval(refreshCounts, 30_000);
    const handleVisibility = () => {
      if (document.visibilityState === 'visible') refreshCounts();
    };
    window.addEventListener('focus', refreshCounts);
    window.addEventListener(NOTIFICATIONS_CHANGED_EVENT, refreshCounts);
    document.addEventListener('visibilitychange', handleVisibility);
    return () => {
      window.clearTimeout(initialTimer);
      window.clearInterval(interval);
      window.removeEventListener('focus', refreshCounts);
      window.removeEventListener(NOTIFICATIONS_CHANGED_EVENT, refreshCounts);
      document.removeEventListener('visibilitychange', handleVisibility);
    };
  }, [readerOpen, refreshCategoryCounts, refreshUnreadCount]);

  useEffect(() => {
    if (!open || selectedItem) return undefined;
    const timer = window.setTimeout(() => {
      void fetchMessages();
    }, 0);
    return () => window.clearTimeout(timer);
  }, [fetchMessages, open, selectedItem]);

  const openDetail = async (item: NotificationItem) => {
    let nextItem = item;
    setSelectedItem(item);
    setView('expanded');
    if (!item.readAt) {
      try {
        await markNotificationsRead([item.id]);
        nextItem = { ...item, readAt: new Date().toISOString() };
        setItems((current) => current.map((entry) => (entry.id === item.id ? nextItem : entry)));
        setSelectedItem((current) => (current?.id === item.id ? nextItem : current));
        notifyNotificationChanged();
      } catch {
        message.error(t('readFailed', 'Failed to mark the message as read'));
      }
    }

    setDetailLoading(true);
    try {
      const detail = await getNotificationDetail(item.id);
      setSelectedItem((current) =>
        current?.id === item.id ? { ...detail, readAt: detail.readAt || nextItem.readAt } : current
      );
    } catch {
      message.error(t('detailFailed', 'Failed to load message details'));
    } finally {
      setDetailLoading(false);
    }
  };

  const selectedUnreadCount =
    sourceType === 'all' ? unreadCount : sourceType === 'app' ? appUnreadCount : systemUnreadCount;

  const markAllRead = async () => {
    try {
      const affectedCount = await markAllNotificationsRead(
        {
          sourceType: sourceType === 'all' ? undefined : sourceType,
        },
        selectedUnreadCount
      );
      const readAt = new Date().toISOString();
      setItems((current) => current.map((item) => ({ ...item, readAt: item.readAt || readAt })));
      setSelectedItem((current) => (current ? { ...current, readAt: current.readAt || readAt } : current));
      setUnreadCount((current) => Math.max(0, current - affectedCount));
      notifyNotificationChanged();
      message.success(t('readCountSuccess', 'Marked {count} messages as read', { count: affectedCount }));
    } catch {
      message.error(t('readAllFailed', 'Failed to mark all messages as read'));
    }
  };

  const goToMessageCenter = () => {
    setView('closed');
    setSelectedItem(null);
    navigate(withLaunchpadStandaloneQuery('/message-center'));
  };
  const hasUnread = selectedUnreadCount > 0 || items.some((item) => !item.readAt);

  const closeInbox = () => {
    setView('closed');
    setSelectedItem(null);
  };

  const categoryTabs: Array<{ value: NotificationDrawerSourceType; label: string; count?: number }> = [
    { value: 'all', label: t('all', 'All') },
    { value: 'app', label: t('filterApp', 'App'), count: appUnreadCount },
    { value: 'platform', label: t('filterSystem', 'System'), count: systemUnreadCount },
  ];

  const inboxHeaderActions = (
    <div className={styles['inbox-header-actions']}>
      <Tooltip title={t('readAll', 'Read All')}>
        <ComButton
          type="text"
          className={styles['mark-all-btn']}
          disabled={!hasUnread}
          aria-label={t('readAll', 'Read All')}
          icon={<MarkAllReadIcon />}
          onClick={() => void markAllRead()}
        />
      </Tooltip>
      <ComButton className={styles['view-all-btn']} onClick={goToMessageCenter}>
        {t('viewAll', 'View All')}
      </ComButton>
    </div>
  );

  const inboxHeader = (
    <header className={styles['inbox-header']}>
      <strong>{t('title', 'Notifications')}</strong>
      {inboxHeaderActions}
    </header>
  );

  const renderDetail = () =>
    selectedItem ? (
      <NotificationDetail
        item={selectedItem}
        loading={detailLoading}
        emptyText={t('detailEmpty', 'Select a message to view its details')}
        backLabel={t('backToList', 'Back to messages')}
        sourceLabel={t('source', 'Source')}
        sourceTypeLabel={
          selectedItem.sourceType === 'platform' ? t('systemNotification', 'System') : t('appNotification', 'App')
        }
        receivedAtLabel={t('receivedAt', 'Received')}
        contentLabel={t('messageContent', 'Message')}
        previewLabel={t('preview', 'Preview')}
        openSourceLabel={t('viewSource', 'Open')}
        sourceUnavailableLabel={t('sourceUnavailable', 'Source unavailable')}
        showSourceAction={shouldShowSourceAction(selectedItem)}
        variant="modal"
        onOpenSource={(item) => {
          if (isNotificationTargetPath(item.targetPath)) {
            closeInbox();
            navigate(withLaunchpadStandaloneQuery(item.targetPath));
          }
        }}
      />
    ) : null;

  const inboxBody = (
    <div className={styles['drawer-shell']}>
      <>
        <div className={styles['drawer-toolbar']}>
          <div className={styles['category-tabs']} role="tablist" aria-label={t('filters', 'Message filters')}>
            {categoryTabs.map((tab) => (
              <button
                key={tab.value}
                type="button"
                role="tab"
                aria-selected={sourceType === tab.value}
                className={classNames(styles['category-tab'], {
                  [styles['category-tab-active']]: sourceType === tab.value,
                })}
                onClick={() => setSourceType(tab.value)}
              >
                <span>{tab.label}</span>
                {typeof tab.count === 'number' ? (
                  <span className={styles['category-tab-count']}>{tab.count > 99 ? '99+' : tab.count}</span>
                ) : null}
              </button>
            ))}
          </div>
        </div>
        <div className={styles['drawer-list']}>
          {loadFailed ? (
            <div className={styles['error-state']}>
              <ComEmpty description={t('loadFailed', 'Failed to load messages')} />
              <ComButton onClick={fetchMessages}>{t('retry', 'Retry')}</ComButton>
            </div>
          ) : (
            <NotificationList
              items={items}
              loading={loading}
              variant="popover"
              emptyText={t('emptyAll', 'No messages yet')}
              unreadLabel={t('unread', 'Unread')}
              onOpen={(item) => void openDetail(item)}
            />
          )}
        </div>
      </>
    </div>
  );

  const inboxTrigger = (
    <span
      className={styles['header-trigger']}
      role="button"
      tabIndex={0}
      aria-label={t('title', 'Notifications')}
      onClick={(event) => {
        event.stopPropagation();
        if (isH5) {
          if (open) closeInbox();
          else setView('inbox');
        }
      }}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          if (open) closeInbox();
          else setView('inbox');
        }
      }}
    >
      <Badge className={styles['header-badge']} count={unreadCount} overflowCount={99}>
        <Notification size={20} />
      </Badge>
    </span>
  );

  return (
    <>
      {isH5 ? (
        <>
          {inboxTrigger}
          <ComDrawer
            rootClassName={styles['notification-drawer']}
            title={t('title', 'Notifications')}
            placement="right"
            width="100%"
            open={open}
            onClose={closeInbox}
            extra={inboxHeaderActions}
          >
            {inboxBody}
          </ComDrawer>
        </>
      ) : (
        <Popover
          rootClassName={styles['inbox-popover']}
          placement="bottomRight"
          trigger="click"
          arrow={false}
          open={open}
          onOpenChange={(nextOpen) => {
            // The expanded reader owns the selection while the popover is hidden.
            if (view === 'expanded') return;
            if (nextOpen) setView('inbox');
            else closeInbox();
          }}
          content={
            <section className={styles['inbox-panel']} aria-label={t('title', 'Notifications')}>
              {inboxHeader}
              {inboxBody}
            </section>
          }
        >
          {inboxTrigger}
        </Popover>
      )}
      <ProModal
        className={styles['notification-reader-modal']}
        title={<h2 className={styles['reader-title']}>{selectedItem?.title}</h2>}
        width={680}
        open={view === 'expanded' && Boolean(selectedItem)}
        onCancel={() => {
          setSelectedItem(null);
          setView('inbox');
        }}
        draggable={false}
        destroyOnHidden
      >
        {renderDetail()}
      </ProModal>
    </>
  );
};

export { NotificationDetail, NotificationList, notifyNotificationChanged };
export default NotificationCenter;
