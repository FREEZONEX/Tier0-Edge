export type NotificationReadStatus = 'all' | 'unread';
export type NotificationSourceType = 'platform' | 'app';
export type NotificationRuntime = 'preview' | 'deployed';
export type NotificationEventType = 'notification' | 'alert';
export type NotificationAlertSeverity = 'critical' | 'warning';

export interface NotificationItem {
  id: string;
  messageId?: string;
  projectId?: string;
  sourceId: string;
  sourceType: NotificationSourceType;
  sourceName: string;
  sourceIconUrl?: string;
  title: string;
  content: string;
  createdAt: string;
  readAt?: string | null;
  targetAvailable?: boolean;
  targetPath?: string;
  runtime?: NotificationRuntime;
  eventType?: NotificationEventType;
  alertSeverity?: NotificationAlertSeverity;
}

export interface NotificationSource {
  sourceId: string;
  sourceType: NotificationSourceType;
  sourceName: string;
}

export interface NotificationPageParams {
  pageNo?: number;
  pageSize?: number;
  readStatus?: NotificationReadStatus;
  sourceType?: NotificationSourceType;
  sourceId?: string;
  /** Multi-source filter; preferred when more than one source is selected. */
  sourceIds?: string[];
  keyword?: string;
  startTime?: string;
  endTime?: string;
}

export type NotificationFilterParams = Pick<
  NotificationPageParams,
  'readStatus' | 'sourceType' | 'sourceId' | 'sourceIds' | 'keyword' | 'startTime' | 'endTime'
>;

export interface NotificationPageResp {
  pageNo: number;
  pageSize: number;
  total: number;
  data: NotificationItem[];
  sources: NotificationSource[];
}

export const isNotificationTargetPath = (path?: string): path is string =>
  Boolean(path?.startsWith('/') && !path.startsWith('//') && !/[\\\s\p{Cc}\p{Cf}]/u.test(path));

export const isNotificationExternalLink = (link?: string): link is string => {
  if (!link || !/^https?:\/\//.test(link) || /[\\\s\p{Cc}\p{Cf}]/u.test(link)) return false;
  if (/^https?:\/\/[^/?#]*@/.test(link)) return false;
  try {
    const url = new URL(link);
    return ['http:', 'https:'].includes(url.protocol) && Boolean(url.hostname) && !url.username && !url.password;
  } catch {
    return false;
  }
};

type NotificationItemPayload = Partial<NotificationItem> & {
  id: string;
  appId?: string;
  appName?: string;
  appIconUrl?: string;
  appAvailable?: boolean;
  appPath?: string;
};

type NotificationSourcePayload = Partial<NotificationSource> & {
  appId?: string;
  appName?: string;
};

type NotificationPagePayload = Omit<Partial<NotificationPageResp>, 'data' | 'sources'> & {
  data?: NotificationItemPayload[];
  list?: NotificationItemPayload[];
  items?: NotificationItemPayload[];
  sources?: NotificationSourcePayload[];
};

const silentRequest = { _noMessage: true } as const;
const getCoreApi = async () => (await import('./core-adapter')).coreApi;

const normalizeTargetPath = (item: NotificationItemPayload): string | undefined => {
  const targetPath = item.targetPath || item.appPath;
  const sourceType = item.sourceType || (item.appId ? 'app' : 'platform');
  if (sourceType !== 'platform' || !item.title?.startsWith('数据质量异常：') || !isNotificationTargetPath(targetPath)) {
    return targetPath;
  }

  const targetUrl = new URL(targetPath, 'http://tier0.local');
  if (targetUrl.pathname !== '/uns' || !targetUrl.searchParams.has('namespaceId')) {
    return targetPath;
  }

  // Historical data-quality notifications only stored the UNS node link. Enrich
  // them while reading so existing messages get the same focused navigation as
  // newly generated notifications without rewriting persisted message data.
  targetUrl.searchParams.set('entry', 'notification');
  targetUrl.searchParams.set('focus', 'dataQuality');
  return `${targetUrl.pathname}?${targetUrl.searchParams.toString()}${targetUrl.hash}`;
};

const normalizeItem = (item: NotificationItemPayload): NotificationItem => ({
  ...item,
  sourceId: item.sourceId || item.appId || item.id,
  sourceType: item.sourceType || (item.appId ? 'app' : 'platform'),
  sourceName: item.sourceName || item.appName || '-',
  sourceIconUrl: item.sourceIconUrl || item.appIconUrl,
  title: item.title || '',
  content: item.content || '',
  createdAt: item.createdAt || '',
  targetAvailable: item.targetAvailable ?? item.appAvailable,
  targetPath: normalizeTargetPath(item),
});

const normalizeSource = (source: NotificationSourcePayload): NotificationSource | null => {
  const sourceId = source.sourceId || source.appId;
  const sourceName = source.sourceName || source.appName;
  if (!sourceId || !sourceName) return null;
  return {
    sourceId,
    sourceName,
    sourceType: source.sourceType || (source.appId ? 'app' : 'platform'),
  };
};

const normalizePage = (
  payload: NotificationPagePayload | NotificationItemPayload[] | undefined,
  params: NotificationPageParams
): NotificationPageResp => {
  if (Array.isArray(payload)) {
    return {
      pageNo: params.pageNo || 1,
      pageSize: params.pageSize || 20,
      total: payload.length,
      data: payload.map(normalizeItem),
      sources: [],
    };
  }

  const data = (payload?.data || payload?.list || payload?.items || []).map(normalizeItem);
  return {
    pageNo: payload?.pageNo || params.pageNo || 1,
    pageSize: payload?.pageSize || params.pageSize || 20,
    total: payload?.total ?? data.length,
    data,
    sources: (payload?.sources || [])
      .map(normalizeSource)
      .filter((source): source is NotificationSource => Boolean(source)),
  };
};

export const getNotificationPage = async (params: NotificationPageParams = {}): Promise<NotificationPageResp> => {
  const coreApi = await getCoreApi();
  const payload = await coreApi.get('/notifications', { params, ...silentRequest });
  return normalizePage(payload, params);
};

export const getNotificationUnreadCount = async (): Promise<number> => {
  const coreApi = await getCoreApi();
  const payload = await coreApi.get('/notifications/unread-count', silentRequest);
  if (typeof payload === 'number') return Number.isFinite(payload) ? Math.max(0, payload) : 0;
  const count = Number(payload?.unreadCount ?? payload?.count ?? 0);
  return Number.isFinite(count) ? Math.max(0, count) : 0;
};

export const getNotificationDetail = async (id: string): Promise<NotificationItem> => {
  const coreApi = await getCoreApi();
  const payload = await coreApi.get(`/notifications/${encodeURIComponent(id)}`, silentRequest);
  return normalizeItem(payload);
};

export const markNotificationsRead = async (ids: string[]): Promise<void> => {
  const coreApi = await getCoreApi();
  return coreApi.post('/notifications/read', { ids }, silentRequest);
};

const normalizeAffectedCount = (payload: unknown, fallbackCount: number) => {
  if (typeof payload === 'number') return Math.max(0, payload);
  if (!payload || typeof payload !== 'object') return fallbackCount;
  const result = payload as { affectedCount?: unknown; count?: unknown; updatedCount?: unknown };
  const count = Number(result.affectedCount ?? result.updatedCount ?? result.count);
  return Number.isFinite(count) ? Math.max(0, count) : fallbackCount;
};

export const markAllNotificationsRead = async (
  filters: NotificationFilterParams = {},
  fallbackCount = 0
): Promise<number> => {
  const coreApi = await getCoreApi();
  const payload = await coreApi.post('/notifications/read-all', filters, silentRequest);
  return normalizeAffectedCount(payload, fallbackCount);
};

export const deleteNotifications = async (ids: string[]): Promise<void> => {
  const coreApi = await getCoreApi();
  return coreApi.post('/notifications/delete', { ids }, silentRequest);
};
