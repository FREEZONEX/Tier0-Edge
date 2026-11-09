/* eslint-disable @typescript-eslint/no-unused-vars */
import type { ProjectMember, ProjectRole } from '@/pages/project/types';
import { CustomAxiosConfigEnum } from '@/utils/request';
import { coreApi, mapFlow } from './core-adapter';

export interface ProjectListParams {
  k?: string;
  pageNo?: number;
  pageSize?: number;
}

export interface ProjectPermissionResp {
  projectId: number;
  isMember: boolean;
  canView: boolean;
  canEdit: boolean;
}

export type ProjectRoleOption = {
  label: string;
  value: string;
};

export type ProjectMemberGroup = {
  projectGroupId: string;
  projectId: string;
  groupId: string;
  groupName: string;
  description: string;
  memberCount: number;
  roles: ProjectRole[];
  createdTime: number;
  updatedTime: number;
};

export interface ProjectRoleListParams {
  pageNo?: number;
  pageSize?: number;
}

const normalizeProject = (item: any) => {
  const source = item?.project || item || {};
  return {
    ...source,
    id: String(source.id || ''),
    displayName: source.displayName || source.name,
    createdAt: source.createdAt || source.createdTime,
    updatedAt: source.updatedAt || source.updatedTime,
  };
};

export const getProjectList = async (params?: ProjectListParams) => {
  const resp = await coreApi.get('/projects', { params: { keyword: params?.k } });
  return {
    data: (resp?.list || []).map(normalizeProject),
    total: resp?.total || 0,
    pageNo: params?.pageNo || 1,
    pageSize: params?.pageSize || 20,
  };
};

export const createProject = async (data?: Record<string, unknown>) =>
  normalizeProject(await coreApi.post('/projects', data));
export const getProject = async (id: string) =>
  normalizeProject(await coreApi.get(`/projects/${id}`, { [CustomAxiosConfigEnum.NoMessage]: true }));
export const updateProject = async (id: string, data?: Record<string, unknown>) =>
  normalizeProject(await coreApi.put(`/projects/${id}`, data));
export const deleteProject = async (id: string) => coreApi.delete(`/projects/${id}`);

export const updateProjectStatus = async (_projectId: string, _data?: Record<string, unknown>) => ({});

export const getProjectPermission = async (projectId: number): Promise<ProjectPermissionResp> => ({
  projectId,
  isMember: true,
  canView: true,
  canEdit: true,
});

export const getProjectApps = async (projectId: string) => coreApi.get(`/projects/${projectId}/apps`);
export const getProjectAppDetail = async (projectId: string, appId: string): Promise<ProjectAppDetailResp> =>
  coreApi.get(`/projects/${projectId}/apps/${appId}`);

export interface ImportEnvVar {
  key: string;
  value: string;
}

export type VolumePolicy = 'inherit' | 'replace';

export interface ImportVolume {
  hostPath: string;
  containerPath: string;
  readOnly?: boolean;
}

export interface ImportPreviewResp {
  appId?: string;
  projectId?: string;
  schemaDefault?: string;
  envVars?: ImportEnvVar[];
}

export interface ProjectAppDetailData {
  networkAlias?: string;
  internalUrl?: string;
  appId?: string | number;
  /** 本地应用授权主体，由服务端分配 */
  appPrincipalId?: string;
  name?: string;
  description?: string;
  status?: string;
  deployStatus?: string;
  deployError?: string;
  schemaName?: string;
  envVars?: ImportEnvVar[];
  volumes?: ImportVolume[];
}

/** 应用详情：部署轮询读取状态，替换弹窗读取 volumes 预填；部分响应包在 data 内，取值处做降级兼容 */
export interface ProjectAppDetailResp extends ProjectAppDetailData {
  data?: ProjectAppDetailData;
}

/** 导入/替换提交的「高级」区字段：env / volumes 均为整体替换语义，用户未修改时对应 policy 走 inherit */
export interface ImportAppAdvancedPayload {
  networkAlias?: string;
  envPolicy?: VolumePolicy;
  envVars?: ImportEnvVar[];
  volumePolicy?: VolumePolicy;
  volumes?: ImportVolume[];
}

/** 导入弹窗「高级」区预填：只读 zip 头部返回 schema 默认值与包内环境变量（要求 project.manage 权限） */
export const getProjectAppImportPreview = async (
  projectId: string,
  fileId: number | string
): Promise<ImportPreviewResp> => coreApi.post(`/projects/${projectId}/apps/import-preview`, { fileId });

export const importProjectApp = async (projectId: string, data?: Record<string, unknown> & ImportAppAdvancedPayload) =>
  coreApi.post(`/projects/${projectId}/apps`, data);
export const createManualProjectApp = async (projectId: string, data?: Record<string, unknown>) =>
  coreApi.post(`/projects/${projectId}/apps/manual`, data);
export const updateProjectApp = async (projectId: string, appId: string, data?: Record<string, unknown>) =>
  coreApi.put(`/projects/${projectId}/apps/${appId}`, data);
export const replaceProjectApp = async (
  projectId: string,
  appId: string,
  data?: Record<string, unknown> & ImportAppAdvancedPayload
) => coreApi.post(`/projects/${projectId}/apps/${appId}/replace`, data);
export const deleteProjectApp = async (projectId: string, appId: string) =>
  coreApi.delete(`/projects/${projectId}/apps/${appId}`);
export const updateProjectAppStatus = async (projectId: string, appId: string, data?: Record<string, unknown>) =>
  coreApi.put(`/projects/${projectId}/apps/${appId}/status`, data);
export const getProjectFlows = async (projectId: string) => {
  const resp = await coreApi.get(`/projects/${projectId}/flows`);
  return (resp?.list || []).map(mapFlow);
};
export const getProjectMembers = async (projectId: number): Promise<ProjectMember[]> => {
  const resp = await coreApi.get(`/projects/${projectId}/members`);
  return (resp?.list || []).map((item: any) => ({
    ...item,
    id: String(item.id || item.memberId || ''),
    memberId: String(item.memberId || item.id || ''),
    userId: String(item.userId || ''),
    updatedAt: item.updatedAt || item.updatedTime,
  }));
};
export const addProjectMember = async (projectId: number | string, data?: Record<string, unknown>) =>
  coreApi.post(`/projects/${projectId}/members`, data);
export const updateProjectMember = async (
  projectId: number | string,
  memberId: number | string,
  data?: Record<string, unknown>
) => coreApi.put(`/projects/${projectId}/members/${memberId}`, data);
export const deleteProjectMember = async (projectId: number | string, memberId: string) =>
  coreApi.delete(`/projects/${projectId}/members/${memberId}`);
export const getProjectMemberGroups = async (projectId: number | string): Promise<ProjectMemberGroup[]> => {
  const response = await coreApi.get(`/projects/${projectId}/member-groups`);
  return (Array.isArray(response?.list) ? response.list : []).map((item: any) => ({
    ...item,
    projectGroupId: String(item?.projectGroupId ?? item?.id ?? ''),
    projectId: String(item?.projectId ?? projectId),
    groupId: String(item?.groupId ?? ''),
    groupName: String(item?.groupName ?? ''),
    description: String(item?.description ?? ''),
    memberCount: Number(item?.memberCount ?? 0),
    roles: Array.isArray(item?.roles) ? item.roles : [],
    createdTime: Number(item?.createdTime ?? 0),
    updatedTime: Number(item?.updatedTime ?? 0),
  }));
};
export const addProjectMemberGroup = async (
  projectId: number | string,
  data: { groupId: string | number; roleIds?: Array<string | number> }
) =>
  coreApi.post(`/projects/${projectId}/member-groups`, {
    groupId: Number(data.groupId),
    roleIds: (data.roleIds || []).map(Number),
  });
export const updateProjectMemberGroup = async (
  projectId: number | string,
  projectGroupId: number | string,
  data: { roleIds?: Array<string | number> }
) =>
  coreApi.put(`/projects/${projectId}/member-groups/${projectGroupId}`, {
    roleIds: (data.roleIds || []).map(Number),
  });
export const deleteProjectMemberGroup = async (projectId: number | string, projectGroupId: number | string) =>
  coreApi.delete(`/projects/${projectId}/member-groups/${projectGroupId}`);
export const getProjectRoles = async (projectId: number, _params?: ProjectRoleListParams): Promise<ProjectRole[]> => {
  const resp = await coreApi.get(`/projects/${projectId}/roles`);
  return resp?.list || [];
};
export const getProjectRoleOptions = async (projectId: number): Promise<ProjectRoleOption[]> =>
  (await getProjectRoles(projectId)).map((role) => ({
    label: role.roleName || role.roleKey,
    value: String(role.roleId),
  }));
