import { authApi, coreApi, iamApi } from './core-adapter';
import { subscribeAuthenticatedEventStream, type EventStreamSubscription } from '@/utils/request';

export type UserGroup = {
  groupId: string;
  groupName: string;
  description: string;
  memberCount: number;
  createdTime: number;
  updatedTime: number;
};

export type UserGroupMember = {
  groupId: string;
  userId: string;
  userName: string;
  displayName: string;
  email: string;
  enabled: boolean;
  createdTime: number;
};

export type UserGroupListParams = {
  keyword?: string;
  pageNo?: number;
  pageSize?: number;
};

export type UserGroupSavePayload = {
  groupName: string;
  description?: string;
};

export type AsyncJobAccepted = {
  jobKey: string;
  estimatedCount?: number;
};

export type UserExportScope = 'selected' | 'filtered' | 'groups' | 'all';
export type UserExportSensitiveMode = 'masked' | 'full';

export type UserExportRequest = {
  scope: UserExportScope;
  userIds?: Array<string | number>;
  groupIds?: Array<string | number>;
  keyword?: string;
  statuses?: Array<'enabled' | 'disabled'>;
  roleIds?: Array<string | number>;
  sensitiveMode?: UserExportSensitiveMode;
};

export type UserExportPreview = {
  estimatedCount: number;
  sensitiveAllowed: boolean;
  scope: UserExportScope;
};

export type UserBatchUpdateRequest = {
  userIds: Array<string | number>;
  homePage?: string;
  roleList?: Array<{ roleId: string | number; roleName?: string }>;
};

export type UserBatchUpdateResponse = {
  updatedCount: number;
  users: Array<{
    userId: string;
    homePage?: string;
    roleList?: Array<{ roleId: string; roleName?: string }>;
  }>;
};

export type UserAsyncJob = {
  jobKey: string;
  jobType: string;
  status: 'pending' | 'running' | 'succeeded' | 'failed' | 'canceled';
  progress: number;
  resultJson: Record<string, any>;
  errorMessage?: string;
  startedTime?: number;
  finishedTime?: number;
};

const defaultProfileEmail = 'tier0@example.com';
const validEmailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

const normalizeProfileEmail = (email?: string, userName?: string) => {
  const value = String(email || '').trim();
  if (validEmailPattern.test(value)) return value;
  return String(userName || '').trim() === 'tier0' ? defaultProfileEmail : value;
};

const unsupported = (name: string): Promise<any> =>
  Promise.reject(new Error(`${name} is not implemented in the current backend`));

const unsupportedHandler =
  (name: string) =>
  (...args: unknown[]): Promise<any> => {
    void args;
    return unsupported(name);
  };

const mapRole = (item: any) => ({
  ...item,
  roleId: String(item.id ?? item.roleId),
  roleName: item.name ?? item.roleName ?? item.code,
  roleCode: item.code ?? item.roleCode,
  defaultHomePage: item.defaultHomePage ?? item.homePage ?? '/home',
  resourceList: item.resourceList || [],
  denyResourceList: item.denyResourceList || [],
});

const mapUser = (item: any) => ({
  ...item,
  id: String(item.userId ?? item.id),
  userId: String(item.userId ?? item.id),
  preferredUsername: item.userName ?? item.preferredUsername,
  firstName: item.nickName ?? item.firstName ?? item.userName,
  phone: item.phone ?? '',
  email: normalizeProfileEmail(item.email, item.userName ?? item.preferredUsername),
  enabled: item.enabled ?? item.status !== 0,
  roleList: item.roleList || [],
  groupList: (Array.isArray(item.groupList) ? item.groupList : []).map((group: any) => ({
    groupId: String(group?.groupId ?? group?.id ?? ''),
    groupName: String(group?.groupName ?? ''),
  })),
  lastLoginAt: Number(item.lastLogin ?? item.lastLoginAt ?? 0),
  createdAt: Number(item.createdTime ?? item.createdAt ?? 0),
});

const normalizeRoleList = (value: any) => {
  const roles = Array.isArray(value) ? value : value ? [value] : [];
  return roles
    .map((item: any) => ({
      roleId: Number(item?.roleId ?? item?.id ?? item?.value),
      roleName: item?.roleName ?? item?.name ?? item?.label,
    }))
    .filter((item: any) => Number.isFinite(item.roleId) && item.roleId > 0);
};

const normalizeUserPayload = (data?: Record<string, any>) => ({
  username: data?.username ?? data?.userName ?? data?.preferredUsername,
  password: data?.password,
  firstName: data?.firstName ?? data?.nickName,
  email: data?.email,
  phone: data?.phone,
  enabled: data?.enabled !== false,
  homePage: data?.homePage,
  roleList: normalizeRoleList(data?.roleList),
});

const normalizeCurrentUserProfilePayload = (data?: Record<string, any>) => ({
  firstName: data?.firstName ?? data?.nickName,
  email: data?.email,
  phone: data?.phone,
});

// 获取用户信息
export const getUserManageList = async (data?: Record<string, unknown>) => {
  const resp = await iamApi.get('/users', { params: data });
  const list = Array.isArray(resp?.list) ? resp.list : [];
  return {
    data: list.map(mapUser),
    total: Number(resp?.total ?? list.length),
    pageNo: Number(data?.pageNo ?? 1),
    pageSize: Number(data?.pageSize ?? Math.max(list.length, 20)),
  };
};

// 获取用户信息 - select使用
export const searchUserManageList = async (data?: Record<string, unknown>) =>
  getUserManageList(data).then((resp: any) =>
    (resp?.data || []).map((item: any) => ({
      label: item.preferredUsername,
      value: item.id,
    }))
  );

// 更新用户
export const updateUser = async (data?: Record<string, any>) => {
  const userId = data?.userId ?? data?.id;
  return mapUser(await iamApi.put(`/users/${userId}`, normalizeUserPayload(data)));
};

export const batchUpdateUsers = async (payload: UserBatchUpdateRequest): Promise<UserBatchUpdateResponse> => {
  const response = await iamApi.put('/users/batch', {
    ...payload,
    userIds: payload.userIds.map(Number),
    roleList: payload.roleList ? normalizeRoleList(payload.roleList) : undefined,
  });
  return {
    updatedCount: Number(response?.updatedCount ?? 0),
    users: (Array.isArray(response?.users) ? response.users : []).map((user: any) => ({
      userId: String(user?.userId ?? ''),
      homePage: user?.homePage ? String(user.homePage) : undefined,
      roleList: Array.isArray(user?.roleList)
        ? user.roleList.map((role: any) => ({
            roleId: String(role?.roleId ?? ''),
            roleName: role?.roleName ? String(role.roleName) : undefined,
          }))
        : undefined,
    })),
  };
};

// 更新当前登录用户基础信息
export const updateCurrentUserProfile = async (data?: Record<string, any>) =>
  mapUser(await authApi.put('/profile', normalizeCurrentUserProfilePayload(data)));

// 更新手机号
export const updatePhone = async (data?: Record<string, any>) => updateCurrentUserProfile(data);

// 更新邮箱
export const updateEmail = async (data?: Record<string, any>) => updateCurrentUserProfile(data);

// 删除用户
export const deleteUser = async (id: string | number) => iamApi.delete(`/users/${id}`);

// 重置密码
export const resetPwd = async (data?: Record<string, any>) => {
  const userId = data?.userId ?? data?.id;
  return iamApi.post(`/users/${userId}/password/reset`, { password: data?.password });
};

// 用户重置密码
export const userResetPwd = async (data?: Record<string, any>) =>
  authApi.put('/password', {
    oldPassword: data?.oldPassword ?? data?.password,
    password: data?.password,
    newPassword: data?.newPassword,
  });

// 更新用户tips开关启用状态
export const updateTipsEnable = unsupportedHandler('updateTipsEnable');

// 创建用户
export const createUser = async (data?: Record<string, any>) =>
  mapUser(
    await iamApi.post('/users', {
      ...normalizeUserPayload(data),
      forceChangePassword: data?.forceChangePassword === true,
    })
  );

// 获取角色列表
export const getRoleList = async () => {
  const resp = await iamApi.get('/roles');
  const list = Array.isArray(resp?.list) ? resp.list : [];
  return list.map(mapRole);
};

const mapUserGroup = (item: any): UserGroup => ({
  groupId: String(item?.groupId ?? item?.id ?? ''),
  groupName: String(item?.groupName ?? ''),
  description: String(item?.description ?? ''),
  memberCount: Number(item?.memberCount ?? 0),
  createdTime: Number(item?.createdTime ?? 0),
  updatedTime: Number(item?.updatedTime ?? 0),
});

const mapUserGroupMember = (item: any): UserGroupMember => ({
  groupId: String(item?.groupId ?? ''),
  userId: String(item?.userId ?? ''),
  userName: String(item?.userName ?? ''),
  displayName: String(item?.displayName ?? item?.userName ?? ''),
  email: String(item?.email ?? ''),
  enabled: Boolean(item?.enabled),
  createdTime: Number(item?.createdTime ?? 0),
});

export const getUserGroups = async (params: UserGroupListParams = {}) => {
  const response = await iamApi.get('/user-groups', { params });
  const list = Array.isArray(response?.list) ? response.list.map(mapUserGroup) : [];
  return {
    list,
    total: Number(response?.total ?? list.length),
    page: Number(response?.page ?? params.pageNo ?? 1),
    size: Number(response?.size ?? params.pageSize ?? 20),
  };
};

export const createUserGroup = async (payload: UserGroupSavePayload): Promise<UserGroup> => {
  const response = await iamApi.post('/user-groups', payload);
  return mapUserGroup(response?.group ?? response);
};

export const updateUserGroup = async (groupId: string | number, payload: UserGroupSavePayload): Promise<UserGroup> => {
  const response = await iamApi.put(`/user-groups/${groupId}`, payload);
  return mapUserGroup(response?.group ?? response);
};

export const deleteUserGroup = async (groupId: string | number) => iamApi.delete(`/user-groups/${groupId}`);

export const getUserGroupMembers = async (groupId: string | number) => {
  const response = await iamApi.get(`/user-groups/${groupId}/members`);
  const list = Array.isArray(response?.list) ? response.list.map(mapUserGroupMember) : [];
  return { list, total: Number(response?.total ?? list.length) };
};

export const replaceUserGroupMembers = async (groupId: string | number, userIds: Array<string | number>) =>
  iamApi.put(`/user-groups/${groupId}/members`, { userIds: userIds.map(Number) });

export const uploadUserImportFile = async (file: File): Promise<number> => {
  const response = await coreApi.uploads('/assets', [{ name: 'files', value: file, fileName: file.name }], {
    method: 'post',
  });
  const list = response?.list ?? response?.data?.list;
  const fileId = Number(Array.isArray(list) ? list[0]?.fileId : 0);
  if (!fileId) {
    throw new Error('account.bulk.uploadFailed');
  }
  return fileId;
};

export const createUserImportJob = async (sourceFileId: number): Promise<AsyncJobAccepted> =>
  iamApi.post('/users/import', { sourceFileId });

const normalizeUserExportPayload = (payload: UserExportRequest) => ({
  ...payload,
  userIds: payload.userIds?.map(Number),
  groupIds: payload.groupIds?.map(Number),
  roleIds: payload.roleIds?.map(Number),
});

export const createUserImportTemplate = async (): Promise<{ fileId: number; fileName: string }> =>
  iamApi.post('/users/import-template', {});

export const previewUserExport = async (payload: UserExportRequest): Promise<UserExportPreview> =>
  iamApi.post('/users/export/preview', normalizeUserExportPayload(payload));

export const createUserExportJob = async (payload: UserExportRequest): Promise<AsyncJobAccepted> =>
  iamApi.post('/users/export', normalizeUserExportPayload(payload));

export const getUserJob = async (jobKey: string): Promise<UserAsyncJob> => iamApi.get(`/user-jobs/${jobKey}`);

export const subscribeUserJobEvents = (
  jobKey: string,
  callbacks: {
    onMessage: (job: UserAsyncJob) => void;
    onError: (error: unknown) => void;
    onClose: () => void;
  }
): EventStreamSubscription =>
  subscribeAuthenticatedEventStream<UserAsyncJob>(
    `/api/core/iam/user-jobs/${encodeURIComponent(jobKey)}?stream=true`,
    callbacks
  );

// 更新个人首页
export const setHomePageApi = async (_data?: Record<string, unknown>) => authApi.put('/config', _data || {});
