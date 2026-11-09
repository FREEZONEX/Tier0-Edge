import type { App, Project, ProjectDetail } from '@/pages/launchpad/type';
import { CustomAxiosConfigEnum } from '@/utils/request';
import { coreApi } from './core-adapter';

const buildQuery = (key?: string, userId?: string | number) => {
  const config = { [CustomAxiosConfigEnum.NoMessage]: true };
  const keyword = key?.trim();
  if (!keyword && !userId) {
    return config;
  }

  return { ...config, params: { keyword: keyword || undefined, userId: userId || undefined } };
};

/** 收藏项 */
export interface LaunchpadFavoriteItem {
  resType: string;
  resId: string;
  resName?: string;
}

/** 获取项目列表 */
export const getLaunchpadProjectsApi = async (key?: string, userId?: string | number): Promise<Project[]> =>
  coreApi.get('/launchpad/projects', buildQuery(key, userId));

/** 获取项目详情（包含 apps） */
export const getLaunchpadProjectByNameApi = async (
  projectName: string | number,
  key?: string
): Promise<ProjectDetail | null> => {
  return coreApi.get(`/launchpad/projects/${encodeURIComponent(String(projectName))}`, buildQuery(key));
};

/** 获取应用详情 */
export const getLaunchpadAppByNameApi = async (
  projectName: string | number,
  appName: string | number
): Promise<{ app: App; project: Project } | null> => {
  return coreApi.get(
    `/launchpad/projects/${encodeURIComponent(String(projectName))}/apps/${encodeURIComponent(String(appName))}`,
    { [CustomAxiosConfigEnum.NoMessage]: true }
  );
};

/** 获取当前用户收藏列表 */
export const getLaunchpadFavoritesApi = async (resType?: string): Promise<{ list: LaunchpadFavoriteItem[] }> => {
  const config: Record<string, any> = { [CustomAxiosConfigEnum.NoMessage]: true };
  if (resType) {
    config.params = { resType };
  }
  return coreApi.get('/launchpad/favorites', config);
};

/** 批量新增收藏 */
export const addLaunchpadFavoritesApi = async (items: LaunchpadFavoriteItem[]): Promise<void> =>
  coreApi.post('/launchpad/favorites', { items }, { [CustomAxiosConfigEnum.NoMessage]: true });

/** 批量删除收藏 */
export const removeLaunchpadFavoritesApi = async (items: LaunchpadFavoriteItem[]): Promise<void> =>
  coreApi.delete('/launchpad/favorites', { [CustomAxiosConfigEnum.NoMessage]: true }, { items });

/** 获取最近打开的应用列表 */
export const getLaunchpadRecentsApi = async (): Promise<{ list: App[] }> =>
  coreApi.get('/launchpad/recents', { [CustomAxiosConfigEnum.NoMessage]: true });

/** 记录应用被查看（用于最近打开） */
export const recordLaunchpadAppViewApi = async (appId: number | string): Promise<void> =>
  coreApi.post(`/launchpad/apps/${encodeURIComponent(String(appId))}/view`, undefined, {
    [CustomAxiosConfigEnum.NoMessage]: true,
  });

/** 兼容旧命名（历史调用里是 ById，实际按 name 查询） */
export const getLaunchpadProjectByIdApi = getLaunchpadProjectByNameApi;
export const getLaunchpadAppByIdApi = getLaunchpadAppByNameApi;
