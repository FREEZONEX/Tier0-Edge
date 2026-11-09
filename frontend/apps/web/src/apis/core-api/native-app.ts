import { CustomAxiosConfigEnum } from '@/utils/request';
import { unsV2Api } from './uns-v2';
import { coreApi } from './core-adapter';

export interface NativeAppBundle {
  name: string;
  description?: string;
  version?: string;
  sourceAppId?: string;
  packageName?: string;
  existingApp?: { appId: string; name: string; version: string; samePackage: boolean };
  runtime: { entry?: string; startScript?: string; buildScript?: string; packageManager?: string };
  ignoredResources: string[];
  invalidRoles?: boolean;
  rolePlan?: {
    digest: string;
    items: {
      role_key: string;
      name: string;
      description?: string;
      existingName?: string;
      existingDescription?: string;
    }[];
  };
  metadata?: { schemaVersion: number; namespace: string };
  metadataPlan?: {
    kind: 'collection' | 'source' | 'stream';
    key: string;
    resourceId?: string;
    alias: string;
    collectionId: string;
    name: string;
    action: 'create' | 'reuse';
    permissions: string[];
  }[];
}
export interface NativeAppGrant {
  alias: string;
  collectionId: string;
  permissions: string[];
}
export interface NativeAppInstallation {
  description: string;
  definitionRevision: number;
  roles?: { id: string; name: string }[];
  canOpen: boolean;
  appId: string;
  name: string;
  version: string;
  status: string;
  desiredState: string;
  stage: string;
  lastError: string;
  entryPath: string;
}
export interface NativeAppJob {
  jobId: string;
  appId: string;
  status: 'pending' | 'running' | 'succeeded' | 'failed' | 'canceled';
  stage: string;
  error?: string;
}
const quiet = { [CustomAxiosConfigEnum.NoMessage]: true };
export const listNativeApps = (archived = false, launchable = false) =>
  coreApi.get('/app-runtime/apps', { ...quiet, params: { archived, launchable } }) as Promise<{
    items: NativeAppInstallation[];
    total: number;
  }>;
export const previewNativeApp = (fileId: string) =>
  coreApi.post('/app-runtime/preview', { fileId }, quiet) as Promise<NativeAppBundle>;
export const installNativeApp = (data: {
  fileId: string;
  appId?: string;
  idempotencyKey: string;
  collections: NativeAppGrant[];
  skipInvalidRoles?: boolean;
  rolePlanDigest?: string;
  renameRoleKeys?: string[];
}) => coreApi.post('/app-runtime/install', data, quiet) as Promise<NativeAppJob>;
export const getNativeAppJob = (jobId: string) =>
  coreApi.get(`/app-runtime/jobs/${encodeURIComponent(jobId)}`, quiet) as Promise<NativeAppJob>;
export type NativeAppAction = 'start' | 'stop' | 'restart' | 'uninstall' | 'refreshCredentials';
export const actOnNativeApp = (appId: string, action: NativeAppAction) =>
  coreApi.post(`/app-runtime/apps/${encodeURIComponent(appId)}/actions`, { action }, { ...quiet, timeout: 180000 });
export const getNativeAppLogs = (appId: string) =>
  coreApi.get(`/app-runtime/apps/${encodeURIComponent(appId)}/logs`, quiet) as Promise<{ text: string }>;

// 本地文件上传不依赖 S3/分片直传；使用精确的字符串 filePath，避免雪花 ID 的 JS 精度损失。
export const uploadNativeApp = async (file: File, signal: AbortSignal, onProgress: (percent: number) => void) => {
  const result = (await coreApi.uploads('/assets', [{ name: 'file', value: file, fileName: file.name }], {
    ...quiet,
    method: 'post',
    timeout: 0,
    signal,
    onUploadProgress: (event) => {
      if (event.total) onProgress(Math.min(100, Math.round((event.loaded / event.total) * 100)));
    },
  })) as { list: { filePath: string }[] };
  return result.list[0];
};

export type WorkspaceRole = {
  key: string;
  name: string;
  description: string;
  revision: number;
  canDelete: boolean;
  userIds?: string[];
  groupIds?: string[];
};
type DirectoryEntry = { id: string; name: string; memberIds?: string[] };
export interface BusinessRoleDirectory {
  roles: WorkspaceRole[];
  users: DirectoryEntry[];
  groups: DirectoryEntry[];
  bindings: (DirectoryEntry & { key: string; trashed: boolean })[];
}
export const getBusinessRoles = () =>
  coreApi.get('/app-runtime/business-roles', quiet) as Promise<BusinessRoleDirectory>;
const rolesUpdated = <T>(result: T): T => {
  window.dispatchEvent(new Event('native-app-roles-updated'));
  return result;
};
export type RoleAssignmentTarget = { kind: 'user' | 'group' | 'role'; id: string; name: string };
export const replaceBusinessRoleAssignments = (
  target: RoleAssignmentTarget,
  roleKeys: string[],
  expectedRoleKeys: string[]
) =>
  coreApi
    .post(
      '/app-runtime/business-role-assignments/update',
      { targetType: target.kind, targetId: target.id, roleKeys, expectedRoleKeys },
      quiet
    )
    .then(rolesUpdated);
export const getEffectiveRoleMembers = (directory: BusinessRoleDirectory, role: WorkspaceRole) =>
  directory.users.filter(
    (user) =>
      role.userIds?.includes(user.id) ||
      directory.groups.some((group) => role.groupIds?.includes(group.id) && group.memberIds?.includes(user.id))
  );
export const saveBusinessRole = (role: WorkspaceRole) => {
  const { revision, ...fields } = role;
  return coreApi
    .post(
      '/app-runtime/business-roles/' + (revision ? 'update' : 'create'),
      { ...fields, expectedRevision: revision },
      quiet
    )
    .then(rolesUpdated);
};
export const deleteBusinessRole = (role: WorkspaceRole) =>
  coreApi
    .post('/app-runtime/business-roles/delete', { key: role.key, expectedRevision: role.revision }, quiet)
    .then(rolesUpdated);
export const trashNativeApp = (app: NativeAppInstallation, trashed: boolean) =>
  coreApi.post(
    `/app-runtime/apps/${encodeURIComponent(app.appId)}/trash`,
    { expectedRevision: app.definitionRevision, trashed },
    quiet
  );
export const saveAppDetails = (
  id: string,
  name: string,
  description: string,
  idempotencyKey: string,
  app?: NativeAppInstallation
) =>
  unsV2Api.resourceWrite(app ? '/apps/update' : '/apps/create', {
    id,
    name,
    description,
    idempotencyKey,
    ...(app ? { expectedRevision: app.definitionRevision } : { entry: { basePath: '/apps/' + id, path: '/' } }),
  });
