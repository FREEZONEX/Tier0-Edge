import { coreApi } from './core-adapter';

// 消息通知 API（TASK-030）：用户端消息中心 + 管理端通知管理
// 后端契约见 enterprise-new/backend/http/core/notify.api

export interface UserMessageItem {
  id: number;
  messageId: number;
  type: string; // inbox/banner/news
  group: string;
  title: string;
  content: string;
  link?: string;
  isRead: number;
  isHandled: number;
  senderType?: string;
  senderId?: string;
  senderName?: string;
  createdTime: number;
}

export interface UserMessageListResp {
  list: UserMessageItem[];
  total: number;
}

export interface NotificationItem {
  id: number;
  type: string;
  title: string;
  content: string;
  link: string;
  asModal: number;
  audienceType: number;
  audienceUserIds: string;
  scheduleTime: number;
  status: number; // 1草稿 2已规划 3运行中 4已发送 5已撤回
  sendStatistics: string;
  createdBy: number;
  createdTime: number;
  updatedTime: number;
}

export interface NotificationListResp {
  list: NotificationItem[];
  total: number;
}

export interface MessageHistorySource {
  sourceType: 'platform' | 'app';
  sourceId: string;
  sourceName: string;
}

export interface MessageHistoryItem {
  messageId: number;
  type: string;
  title: string;
  content: string;
  link?: string;
  origin: 'admin' | 'openapi' | 'system' | string;
  mode?: string;
  sourceType: 'platform' | 'app';
  sourceId: string;
  sourceName: string;
  senderDetail?: string;
  recipientCount: number;
  readCount: number;
  notifyRequestId?: number;
  notificationId?: number;
  status: 'sent' | 'revoked';
  createdTime: number;
}

export interface MessageHistoryListParams {
  page?: number;
  size?: number;
  keyword?: string;
  sourceType?: 'platform' | 'app';
  sourceId?: string;
  type?: string;
  status?: 'sent' | 'revoked';
  recipientUserId?: number;
  startTime?: number;
  endTime?: number;
}

export interface MessageHistoryListResp {
  list: MessageHistoryItem[];
  total: number;
  sources: MessageHistorySource[];
}

export interface NotificationRecipientUser {
  userId: number;
  userName: string;
  displayName: string;
}

export interface NotificationRecipientGroup {
  groupId: number;
  groupName: string;
  description: string;
  memberCount: number;
  userIds: number[];
}

export interface NotificationRecipientOptionsResp {
  users: NotificationRecipientUser[];
  groups: NotificationRecipientGroup[];
}

// ---------- 用户端 ----------

export const getUserMessages = async (params: {
  page?: number;
  pageSize?: number;
  group?: string;
  unreadOnly?: number;
}): Promise<UserMessageListResp> => coreApi.get('/notify/user-message/list', { params });

export const getUnreadCount = async (): Promise<{ count: number }> => coreApi.get('/notify/user-message/unread-count');

export const readMessages = async (ids?: number[]): Promise<void> => {
  await coreApi.post('/notify/user-message/read', { ids: ids ?? [] });
};

export const readAllMessages = async (): Promise<void> => {
  await coreApi.post('/notify/user-message/read-all', {});
};

export const handleMessage = async (id: number): Promise<void> => {
  await coreApi.post('/notify/user-message/handle', { id });
};

export const deleteMessages = async (ids: number[]): Promise<void> => {
  await coreApi.post('/notify/user-message/delete', { ids });
};

// ---------- 管理端 ----------

export const getNotificationList = async (params: {
  page?: number;
  pageSize?: number;
  type?: string;
  status?: number;
}): Promise<NotificationListResp> => coreApi.get('/notify/notification/list', { params });

export const getMessageHistory = async (params: MessageHistoryListParams): Promise<MessageHistoryListResp> =>
  coreApi.get('/notify/message-history/list', { params });

export const getNotificationRecipientOptions = async (): Promise<NotificationRecipientOptionsResp> =>
  coreApi.get('/notify/recipient-options');

export const getNotificationDetail = async (id: number, silent = false): Promise<NotificationItem> =>
  coreApi.get('/notify/notification/detail', { params: { id }, _noMessage: silent });

export const saveNotification = async (data: {
  id?: number;
  type: string;
  title: string;
  content: string;
  link?: string;
  asModal?: number;
}): Promise<{ id: number }> => coreApi.post('/notify/notification/save', data);

export const sendNotification = async (
  data: {
    id: number;
    audienceType: number; // 1=全部 2=指定
    userIds?: number[];
    scheduleTime?: number;
  },
  silent = false
): Promise<void> => {
  await coreApi.post('/notify/notification/send', data, { _noMessage: silent });
};

export const revokeNotification = async (id: number): Promise<void> => {
  await coreApi.post('/notify/notification/revoke', { id });
};

export const deleteNotification = async (id: number): Promise<void> => {
  await coreApi.delete('/notify/notification', undefined, { id });
};

export const copyNotification = async (id: number): Promise<{ id: number }> =>
  coreApi.post('/notify/notification/copy', { id });

export const estimateUsers = async (data: { audienceType: number; userIds?: number[] }): Promise<{ count: number }> =>
  coreApi.post('/notify/notification/estimate-users', data);
