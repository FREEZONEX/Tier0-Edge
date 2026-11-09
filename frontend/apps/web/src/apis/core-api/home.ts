import { CustomAxiosConfigEnum } from '@/utils/request';
import { coreApi } from './core-adapter';
export type HomeRecentItem = {
  key: string;
  resourceKey: string;
  resourceName: string;
  resourceType: string;
  businessType?: string;
  routePath: string;
  icon?: string;
  operatedAt?: number;
};

const silentConfig = {
  [CustomAxiosConfigEnum.NoMessage]: true,
};

export const getHomeRecents = async (): Promise<HomeRecentItem[]> => {
  try {
    const resp = await coreApi.get('/home/recents', {
      params: { limit: 5, isShowInRecent: 1 },
      ...silentConfig,
    });
    return Array.isArray(resp?.list) ? resp.list : [];
  } catch {
    return [];
  }
};
