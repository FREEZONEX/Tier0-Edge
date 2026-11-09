import {
  addLaunchpadFavoritesApi,
  getLaunchpadFavoritesApi,
  removeLaunchpadFavoritesApi,
} from '@/apis/core-api/launchpad';
import { PinnedResType } from './pinned-res-type';
import { useCallback, useEffect, useRef, useState } from 'react';

/**
 * Launchpad 置顶（Pinned）状态。
 * 应用与项目置顶都以后端为唯一事实源；请求失败时回滚，本地存储只作显示缓存。
 * 项目置顶原来只写本地，导致同一账号换浏览器或换到移动端就看不到，已改为一并同步。
 *
 * 与云端 saas 统一为 Pinned 语义（resType 见 pinned-res-type.ts）。存量的
 * res_type='app' 记录由后端一次性迁移复制为置顶记录，前端不做兼容读。
 */
const STORAGE_KEY = 'launchpad:pinned';
// 改名前的本地存储键；只读一次用于承接旧数据，读完即按新键写回。
const LEGACY_STORAGE_KEY = 'launchpad:favorites';

interface PinnedState {
  apps: string[];
  projects: string[];
}

const readPinned = (): PinnedState => {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY) ?? window.localStorage.getItem(LEGACY_STORAGE_KEY);
    if (!raw) return { apps: [], projects: [] };
    const parsed = JSON.parse(raw);
    return {
      apps: Array.isArray(parsed?.apps) ? parsed.apps.map(String) : [],
      projects: Array.isArray(parsed?.projects) ? parsed.projects.map(String) : [],
    };
  } catch {
    return { apps: [], projects: [] };
  }
};

const writePinned = (state: PinnedState) => {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
  } catch {
    /* localStorage 不可用时静默降级为内存态 */
  }
};

export interface LaunchpadPinned {
  isAppPinned: (id: string | number) => boolean;
  isProjectPinned: (id: string | number) => boolean;
  pinnedAppIds: string[];
  toggleAppPin: (id: string | number) => void;
  toggleProjectPin: (id: string | number) => void;
  /** 重新拉取后端置顶状态；tab 保活场景下由页面在 useActivate 中调用，保持多 tab 一致 */
  refreshPinned: () => Promise<void>;
}

export function useLaunchpadPinned(): LaunchpadPinned {
  const [state, setState] = useState<PinnedState>(readPinned);
  // setState 的 updater 会被 React 急切求值 + 渲染期重放（可能多次执行），
  // 因此判定和 localStorage 写入等副作用一律放在 updater 外，这里用 ref 镜像当前状态。
  const stateRef = useRef(state);

  const applyState = useCallback((next: PinnedState) => {
    stateRef.current = next;
    writePinned(next);
    setState(next);
  }, []);

  // 拉取后端置顶（服务端为唯一事实源）；请求失败时保留当前本地状态作为显示缓存。
  // 挂载时执行一次；tab 保活场景下页面在 useActivate 中再次调用保持多 tab 一致。
  const refreshPinned = useCallback(async () => {
    try {
      const [appRes, projectRes] = await Promise.all([
        getLaunchpadFavoritesApi(PinnedResType.App),
        getLaunchpadFavoritesApi(PinnedResType.Project),
      ]);
      applyState({
        apps: (appRes?.list || []).map((item) => String(item.resId)),
        projects: (projectRes?.list || []).map((item) => String(item.resId)),
      });
    } catch {
      // 拉取失败保留现状，等下次激活重试
    }
  }, [applyState]);

  useEffect(() => {
    void refreshPinned();
  }, [refreshPinned]);

  const isAppPinned = useCallback((id: string | number) => state.apps.includes(String(id)), [state.apps]);
  const isProjectPinned = useCallback((id: string | number) => state.projects.includes(String(id)), [state.projects]);

  const toggleAppPin = useCallback(
    async (id: string | number) => {
      const idStr = String(id);
      const previousApps = stateRef.current.apps;
      const isAdding = !previousApps.includes(idStr);
      const nextApps = isAdding ? [...previousApps, idStr] : previousApps.filter((item) => item !== idStr);
      applyState({ ...stateRef.current, apps: nextApps });

      // 置顶状态以后端为准：始终同步后端，失败回滚本地，避免"只写本地"的假置顶
      try {
        const item = { resType: PinnedResType.App, resId: idStr };
        if (isAdding) {
          await addLaunchpadFavoritesApi([item]);
        } else {
          await removeLaunchpadFavoritesApi([item]);
        }
      } catch {
        // 后端同步失败时回滚本地状态
        applyState({ ...stateRef.current, apps: previousApps });
      }
    },
    [applyState]
  );

  const toggleProjectPin = useCallback(
    async (id: string | number) => {
      const idStr = String(id);
      const previousProjects = stateRef.current.projects;
      const isAdding = !previousProjects.includes(idStr);
      const nextProjects = isAdding ? [...previousProjects, idStr] : previousProjects.filter((item) => item !== idStr);
      applyState({ ...stateRef.current, projects: nextProjects });

      // 与应用置顶同一口径：始终同步后端，失败回滚本地
      try {
        const item = { resType: PinnedResType.Project, resId: idStr };
        if (isAdding) {
          await addLaunchpadFavoritesApi([item]);
        } else {
          await removeLaunchpadFavoritesApi([item]);
        }
      } catch {
        applyState({ ...stateRef.current, projects: previousProjects });
      }
    },
    [applyState]
  );

  return {
    isAppPinned,
    isProjectPinned,
    pinnedAppIds: state.apps,
    toggleAppPin,
    toggleProjectPin,
    refreshPinned,
  };
}
