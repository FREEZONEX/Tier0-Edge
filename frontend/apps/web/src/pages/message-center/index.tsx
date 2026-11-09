import {
  deleteNotifications,
  getNotificationDetail,
  getNotificationPage,
  getNotificationUnreadCount,
  isNotificationTargetPath,
  markAllNotificationsRead,
  markNotificationsRead,
  type NotificationItem,
  type NotificationPageParams,
  type NotificationReadStatus,
  type NotificationSource,
} from '@/apis/core-api/notifications';
import ComButton from '@/components/com-button';
import ComEmpty from '@/components/com-empty';
import ComLayout from '@/components/com-layout';
import ComContent from '@/components/com-layout/ComContent';
import { Checkmark, Renew, TrashCan } from '@/components/lucide-icon/carbon';
import { NotificationDetail, NotificationList, notifyNotificationChanged } from '@/components/notification-center';
import { formatNotificationSourceName } from '@/components/notification-center/notification-source-name';
import { useMediaSize, useTranslate } from '@/hooks';
import { openDeleteConfirm } from '@/utils/delete-confirm-modal';
import {
  isLaunchpadStandaloneAllowedPath,
  isLaunchpadStandaloneSite,
  withLaunchpadStandaloneQuery,
} from '@/utils/launchpad-site';
import { App, Checkbox, DatePicker, Input, Pagination, Select, Tooltip, TreeSelect } from 'antd';
import classNames from 'classnames';
import type { Dayjs } from 'dayjs';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import styles from './index.module.scss';

const defaultPageSize = 20;
const sourceTypePlatformKey = 'type:platform';
const sourceTypeAppKey = 'type:app';
const { SHOW_PARENT } = TreeSelect;
const EMPTY_SOURCE_FILTER: Pick<NotificationPageParams, 'sourceType' | 'sourceId' | 'sourceIds'> = {};

const shouldShowSourceAction = (item: NotificationItem) =>
  !isLaunchpadStandaloneSite() || Boolean(item.targetPath && isLaunchpadStandaloneAllowedPath(item.targetPath));

const sourceValueKey = (sourceId: string) => `id:${sourceId}`;

const areSourcesEqual = (left: NotificationSource[], right: NotificationSource[]) =>
  left.length === right.length &&
  left.every((source, index) => {
    const other = right[index];
    return (
      source.sourceId === other.sourceId &&
      source.sourceType === other.sourceType &&
      source.sourceName === other.sourceName
    );
  });

const resolveSourceFilter = (
  selectedKeys: string[],
  sourceList: NotificationSource[]
): Pick<NotificationPageParams, 'sourceType' | 'sourceId' | 'sourceIds'> => {
  if (!selectedKeys.length) return {};

  if (selectedKeys.length === 1 && selectedKeys[0] === sourceTypePlatformKey) {
    return { sourceType: 'platform' };
  }
  if (selectedKeys.length === 1 && selectedKeys[0] === sourceTypeAppKey) {
    return { sourceType: 'app' };
  }

  const ids = new Set<string>();
  selectedKeys.forEach((key) => {
    if (key === sourceTypePlatformKey) {
      sourceList.filter((source) => source.sourceType === 'platform').forEach((source) => ids.add(source.sourceId));
      return;
    }
    if (key === sourceTypeAppKey) {
      sourceList.filter((source) => source.sourceType === 'app').forEach((source) => ids.add(source.sourceId));
      return;
    }
    if (key.startsWith('id:')) ids.add(key.slice(3));
  });

  if (!ids.size) return {};
  if (ids.size === 1) {
    const onlyId = Array.from(ids)[0];
    return {
      sourceId: onlyId,
      sourceType: sourceList.find((source) => source.sourceId === onlyId)?.sourceType,
    };
  }
  return { sourceIds: Array.from(ids) };
};

const MessageCenterPage = () => {
  const formatMessage = useTranslate();
  const navigate = useNavigate();
  const { message, modal } = App.useApp();
  const { isH5: isCompact } = useMediaSize({ media: 840 });
  const [items, setItems] = useState<NotificationItem[]>([]);
  const [sources, setSources] = useState<NotificationSource[]>([]);
  const [total, setTotal] = useState(0);
  const [unreadCount, setUnreadCount] = useState(0);
  const [pageNo, setPageNo] = useState(1);
  const [pageSize, setPageSize] = useState(defaultPageSize);
  const [readStatus, setReadStatus] = useState<NotificationReadStatus>('all');
  const [selectedSourceKeys, setSelectedSourceKeys] = useState<string[]>([]);
  const [searchDraft, setSearchDraft] = useState('');
  const [keyword, setKeyword] = useState('');
  const [timeRange, setTimeRange] = useState<[Dayjs, Dayjs] | null>(null);
  const [checkedIds, setCheckedIds] = useState<Set<string>>(new Set());
  const [selectedItem, setSelectedItem] = useState<NotificationItem | null>(null);
  const [loading, setLoading] = useState(false);
  const [detailLoading, setDetailLoading] = useState(false);
  const [loadFailed, setLoadFailed] = useState(false);
  const t = useCallback(
    (key: string, fallback: string, values?: Record<string, unknown>) =>
      formatMessage(`messageCenter.${key}`, values, fallback),
    [formatMessage]
  );
  const sourceFilter = useMemo(() => {
    if (!selectedSourceKeys.length) return EMPTY_SOURCE_FILTER;
    return resolveSourceFilter(selectedSourceKeys, sources);
  }, [selectedSourceKeys, sources]);
  const currentFilters = useMemo(
    () => ({
      readStatus,
      ...sourceFilter,
      keyword: keyword || undefined,
      startTime: timeRange?.[0].startOf('day').toISOString(),
      endTime: timeRange?.[1].endOf('day').toISOString(),
    }),
    [keyword, readStatus, sourceFilter, timeRange]
  );
  const requestKey = useMemo(
    () => JSON.stringify({ pageNo, pageSize, ...currentFilters }),
    [currentFilters, pageNo, pageSize]
  );
  const lastRequestKeyRef = useRef<string>();

  const fetchMessages = useCallback(async () => {
    const shouldResetSelection = lastRequestKeyRef.current !== requestKey;
    lastRequestKeyRef.current = requestKey;
    setLoading(true);
    setLoadFailed(false);
    try {
      const [response, nextUnreadCount] = await Promise.all([
        getNotificationPage({
          pageNo,
          pageSize,
          ...currentFilters,
        }),
        getNotificationUnreadCount(),
      ]);
      setItems(response.data);
      setTotal(response.total);
      setUnreadCount(nextUnreadCount);
      if (shouldResetSelection) {
        setCheckedIds(new Set());
        setSelectedItem(null);
      }
      const nextSources = [...response.sources].sort((left, right) => left.sourceName.localeCompare(right.sourceName));
      setSources((current) => (areSourcesEqual(current, nextSources) ? current : nextSources));
    } catch {
      setLoadFailed(true);
    } finally {
      setLoading(false);
    }
  }, [currentFilters, pageNo, pageSize, requestKey]);

  useEffect(() => {
    const timer = window.setTimeout(() => void fetchMessages(), 0);
    return () => window.clearTimeout(timer);
  }, [fetchMessages]);

  const openDetail = async (item: NotificationItem) => {
    let nextItem = item;
    setSelectedItem(item);
    if (!item.readAt) {
      try {
        await markNotificationsRead([item.id]);
        nextItem = { ...item, readAt: new Date().toISOString() };
        if (readStatus === 'unread') {
          setItems((current) => current.filter((entry) => entry.id !== item.id));
          setTotal((current) => Math.max(0, current - 1));
        } else {
          setItems((current) => current.map((entry) => (entry.id === item.id ? nextItem : entry)));
        }
        setSelectedItem(nextItem);
        setUnreadCount((current) => Math.max(0, current - 1));
        notifyNotificationChanged();
      } catch {
        message.error(t('readFailed', 'Failed to mark the message as read'));
      }
    }

    setDetailLoading(true);
    try {
      const detail = await getNotificationDetail(item.id);
      setSelectedItem({ ...detail, readAt: detail.readAt || nextItem.readAt });
    } catch {
      message.error(t('detailFailed', 'Failed to load message details'));
    } finally {
      setDetailLoading(false);
    }
  };

  const markSelectedRead = async () => {
    const ids = Array.from(checkedIds);
    if (!ids.length) return;
    const unreadSelectedCount = items.filter((item) => checkedIds.has(item.id) && !item.readAt).length;
    try {
      await markNotificationsRead(ids);
      const readAt = new Date().toISOString();
      if (readStatus === 'unread') {
        setItems((current) => current.filter((item) => !checkedIds.has(item.id)));
        setTotal((current) => Math.max(0, current - ids.length));
      } else {
        setItems((current) =>
          current.map((item) => (checkedIds.has(item.id) ? { ...item, readAt: item.readAt || readAt } : item))
        );
      }
      setSelectedItem((current) =>
        current && checkedIds.has(current.id) ? { ...current, readAt: current.readAt || readAt } : current
      );
      setCheckedIds(new Set());
      setUnreadCount((current) => Math.max(0, current - unreadSelectedCount));
      notifyNotificationChanged();
      message.success(t('readCountSuccess', 'Marked {count} messages as read', { count: ids.length }));
    } catch {
      message.error(t('readFailed', 'Failed to mark messages as read'));
    }
  };

  const markAllRead = async () => {
    try {
      const unreadOnPage = items.filter((item) => !item.readAt).length;
      const fallbackCount = readStatus === 'unread' ? total : unreadOnPage;
      const affectedCount = await markAllNotificationsRead(currentFilters, fallbackCount);
      const readAt = new Date().toISOString();
      if (readStatus === 'unread') {
        setItems([]);
        setTotal((current) => Math.max(0, current - affectedCount));
        setSelectedItem(null);
      } else {
        setItems((current) => current.map((item) => ({ ...item, readAt: item.readAt || readAt })));
        setSelectedItem((current) => (current ? { ...current, readAt: current.readAt || readAt } : current));
      }
      setCheckedIds(new Set());
      setUnreadCount((current) => Math.max(0, current - affectedCount));
      notifyNotificationChanged();
      message.success(t('readCountSuccess', 'Marked {count} messages as read', { count: affectedCount }));
    } catch {
      message.error(t('readAllFailed', 'Failed to mark all messages as read'));
    }
  };

  const confirmDelete = (ids: string[]) => {
    if (!ids.length) return;
    const targetIds = new Set(ids);
    const unreadDeletedCount = items.filter((item) => targetIds.has(item.id) && !item.readAt).length;
    openDeleteConfirm(modal, {
      title: t('deleteConfirmTitle', 'Delete notifications'),
      content: t('deleteConfirmBody', 'Delete the selected {count} notification(s)? This action cannot be undone.', {
        count: ids.length,
      }),
      okText: t('confirmDelete', 'Delete'),
      cancelText: t('cancel', 'Cancel'),
      onOk: async () => {
        try {
          await deleteNotifications(ids);
          setItems((current) => current.filter((item) => !targetIds.has(item.id)));
          setTotal((current) => Math.max(0, current - ids.length));
          setSelectedItem((current) => (current && targetIds.has(current.id) ? null : current));
          setCheckedIds((current) => new Set(Array.from(current).filter((id) => !targetIds.has(id))));
          setUnreadCount((current) => Math.max(0, current - unreadDeletedCount));
          notifyNotificationChanged();
          message.success(t('deleteCountSuccess', 'Deleted {count} messages', { count: ids.length }));
        } catch {
          message.error(t('deleteFailed', 'Failed to delete messages'));
          throw new Error('notification-delete-failed');
        }
      },
    });
  };

  const allOnPageChecked = Boolean(items.length) && items.every((item) => checkedIds.has(item.id));
  const partiallyChecked = checkedIds.size > 0 && !allOnPageChecked;
  const sourceTreeData = useMemo(() => {
    const platformChildren = sources
      .filter((source) => source.sourceType === 'platform')
      .map((source) => ({
        title: formatNotificationSourceName(
          source,
          formatMessage('notificationManage.sourcePlatform', undefined, 'Platform')
        ),
        value: sourceValueKey(source.sourceId),
        key: sourceValueKey(source.sourceId),
      }));
    const appChildren = sources
      .filter((source) => source.sourceType === 'app')
      .map((source) => ({
        title: source.sourceName,
        value: sourceValueKey(source.sourceId),
        key: sourceValueKey(source.sourceId),
      }));
    return [
      {
        title: t('filterSystem', 'System'),
        value: sourceTypePlatformKey,
        key: sourceTypePlatformKey,
        children: platformChildren,
      },
      {
        title: t('filterApp', 'App'),
        value: sourceTypeAppKey,
        key: sourceTypeAppKey,
        children: appChildren,
      },
    ];
  }, [sources, t, formatMessage]);
  const hasActiveFilter = Boolean(readStatus !== 'all' || selectedSourceKeys.length || keyword || timeRange);

  return (
    <ComLayout loading={false}>
      <ComContent
        className={styles['message-center-content']}
        title={t('title', 'Notifications')}
        hasBack={false}
        style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden' }}
      >
        <div className={styles['page-shell']}>
          <section className={styles['control-panel']} aria-label={t('filters', 'Message filters')}>
            <div className={styles['filter-bar']}>
              <div className={styles['filter-left']}>
                <TreeSelect
                  className={styles['source-filter']}
                  treeCheckable
                  allowClear
                  showCheckedStrategy={SHOW_PARENT}
                  treeDefaultExpandAll
                  maxTagCount="responsive"
                  placeholder={t('allSources', 'Any source')}
                  aria-label={t('source', 'Source')}
                  treeData={sourceTreeData}
                  value={selectedSourceKeys}
                  popupMatchSelectWidth={260}
                  onChange={(value) => {
                    setSelectedSourceKeys((value as string[]) || []);
                    setPageNo(1);
                  }}
                />
                <Select
                  className={styles['status-filter']}
                  value={readStatus}
                  options={[
                    { value: 'all', label: t('allMessages', 'All messages') },
                    { value: 'unread', label: t('unread', 'Unread') },
                  ]}
                  aria-label={t('status', 'Status')}
                  onChange={(value) => {
                    setReadStatus(value as NotificationReadStatus);
                    setPageNo(1);
                  }}
                />
                <DatePicker.RangePicker
                  className={styles['time-filter']}
                  value={timeRange}
                  aria-label={t('timeRange', 'Time range')}
                  onChange={(value) => {
                    setTimeRange(value ? (value as [Dayjs, Dayjs]) : null);
                    setPageNo(1);
                  }}
                />
              </div>
              <div className={styles['filter-right']}>
                <Input.Search
                  className={styles['message-search']}
                  allowClear
                  value={searchDraft}
                  placeholder={t('searchPlaceholder', 'Search by title, content, or source')}
                  onChange={(event) => setSearchDraft(event.target.value)}
                  onSearch={(value) => {
                    setKeyword(value.trim());
                    setPageNo(1);
                  }}
                />
                <Tooltip title={t('refresh', 'Refresh')}>
                  <ComButton
                    className={styles['icon-button']}
                    aria-label={t('refresh', 'Refresh')}
                    icon={<Renew size={16} />}
                    onClick={fetchMessages}
                  />
                </Tooltip>
              </div>
            </div>
          </section>

          <div className={classNames(styles.workspace, { [styles['has-detail']]: Boolean(selectedItem) })}>
            <section className={styles['list-pane']} aria-label={t('messageList', 'Message list')}>
              <div className={styles['list-toolbar']}>
                <div className={styles['selection-summary']}>
                  <Checkbox
                    checked={allOnPageChecked}
                    indeterminate={partiallyChecked}
                    aria-label={t('selectPage', 'Select this page')}
                    onChange={(event) => {
                      setCheckedIds(event.target.checked ? new Set(items.map((item) => item.id)) : new Set());
                    }}
                  />
                  <span>
                    {checkedIds.size
                      ? t('selectedCount', '{count} selected', { count: checkedIds.size })
                      : t('messageCount', '{count} messages', { count: total })}
                  </span>
                </div>
                <div className={styles['batch-actions']}>
                  {checkedIds.size ? (
                    <>
                      <ComButton type="text" icon={<Checkmark size={16} />} onClick={markSelectedRead}>
                        {t('markRead', 'Mark as read')}
                      </ComButton>
                      <ComButton
                        type="text"
                        danger
                        icon={<TrashCan size={16} />}
                        onClick={() => confirmDelete(Array.from(checkedIds))}
                      >
                        {t('delete', 'Delete')}
                      </ComButton>
                    </>
                  ) : null}
                  <ComButton
                    className={styles['mark-all-read-btn']}
                    type="link"
                    disabled={!unreadCount}
                    onClick={markAllRead}
                  >
                    {t('markAllRead', 'Mark all as read')}
                  </ComButton>
                </div>
              </div>
              <div className={styles['list-scroll']}>
                {loadFailed ? (
                  <div className={styles['page-error']}>
                    <ComEmpty description={t('loadFailed', 'Failed to load messages')} />
                    <ComButton onClick={fetchMessages}>{t('retry', 'Retry')}</ComButton>
                  </div>
                ) : (
                  <NotificationList
                    items={items}
                    loading={loading}
                    selectedId={selectedItem?.id}
                    selectable
                    checkedIds={checkedIds}
                    emptyText={
                      hasActiveFilter
                        ? t('emptyFiltered', 'No messages match the current filters')
                        : readStatus === 'unread'
                          ? t('emptyUnread', 'No unread messages')
                          : t('emptyAll', 'No messages yet')
                    }
                    unreadLabel={t('unread', 'Unread')}
                    selectLabel={t('selectMessage', 'Select message')}
                    onOpen={(item) => void openDetail(item)}
                    deleteLabel={t('delete', 'Delete')}
                    onDelete={(item) => confirmDelete([item.id])}
                    onCheck={(item, checked) => {
                      setCheckedIds((current) => {
                        const next = new Set(current);
                        if (checked) next.add(item.id);
                        else next.delete(item.id);
                        return next;
                      });
                    }}
                  />
                )}
              </div>
              <div className={styles['pagination-bar']}>
                <Pagination
                  align="end"
                  current={pageNo}
                  pageSize={pageSize}
                  total={total}
                  showSizeChanger
                  hideOnSinglePage={false}
                  onChange={(nextPage, nextPageSize) => {
                    setPageNo(nextPageSize === pageSize ? nextPage : 1);
                    setPageSize(nextPageSize);
                  }}
                />
              </div>
            </section>
            <section className={styles['detail-pane']} aria-label={t('messageDetail', 'Message detail')}>
              <NotificationDetail
                item={selectedItem}
                loading={detailLoading}
                emptyText={t('detailEmpty', 'Select a message to view its details')}
                backLabel={t('backToList', 'Back to messages')}
                sourceLabel={t('source', 'Source')}
                sourceTypeLabel={
                  selectedItem?.sourceType === 'platform'
                    ? t('systemNotification', 'System')
                    : t('appNotification', 'App')
                }
                receivedAtLabel={t('receivedAt', 'Received')}
                contentLabel={t('messageContent', 'Message')}
                previewLabel={t('preview', 'Preview')}
                openSourceLabel={t('viewSource', 'Open')}
                sourceUnavailableLabel={t('sourceUnavailable', 'Source unavailable')}
                showSourceAction={selectedItem ? shouldShowSourceAction(selectedItem) : true}
                onBack={isCompact ? () => setSelectedItem(null) : undefined}
                onOpenSource={(item) => {
                  if (isNotificationTargetPath(item.targetPath))
                    navigate(withLaunchpadStandaloneQuery(item.targetPath));
                }}
              />
            </section>
          </div>
        </div>
      </ComContent>
    </ComLayout>
  );
};

export default MessageCenterPage;
