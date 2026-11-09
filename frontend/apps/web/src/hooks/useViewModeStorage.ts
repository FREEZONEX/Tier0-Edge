import { useLocalStorageState } from 'ahooks';
import { useCallback, useEffect } from 'react';

export const VIEW_MODE_LIST = 'list';
export const VIEW_MODE_CARD = 'card';

export const VIEW_MODE_STORAGE_KEYS = {
  collection: 'APP_COLLECTION_VIEW_V2',
  eventFlow: 'APP_EVENTFLOW_VIEW_V2',
  flowAll: 'APP_FLOW_ALL_VIEW_V2',
  flow: 'APP_FLOW_VIEW_V2',
  app: 'APP_APP_VIEW_V2',
  plugin: 'APP_PLUGIN_VIEW_V2',
  visionAlgorithm: 'APP_VISION_ALGO_VIEW_V2',
} as const;

const VIEW_MODE_CHANGE_EVENT = 'tier0:view-mode-change';

export function useViewModeStorage(key: string) {
  const [viewMode, setStoredViewMode] = useLocalStorageState<string>(key, {
    defaultValue: VIEW_MODE_LIST,
  });

  useEffect(() => {
    const handleViewModeChange = (event: Event) => {
      const detail = (event as CustomEvent<{ key: string; value: string }>).detail;
      if (detail?.key === key && detail.value !== viewMode) {
        setStoredViewMode(detail.value);
      }
    };

    window.addEventListener(VIEW_MODE_CHANGE_EVENT, handleViewModeChange);
    return () => window.removeEventListener(VIEW_MODE_CHANGE_EVENT, handleViewModeChange);
  }, [key, setStoredViewMode, viewMode]);

  const setViewMode = useCallback(
    (value: string) => {
      setStoredViewMode(value);
      window.dispatchEvent(new CustomEvent(VIEW_MODE_CHANGE_EVENT, { detail: { key, value } }));
    },
    [key, setStoredViewMode]
  );

  return [viewMode, setViewMode] as const;
}
