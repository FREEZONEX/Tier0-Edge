import { useCallback, useEffect, useState } from 'react';
import { getBusinessRoles, type BusinessRoleDirectory } from '@/apis/core-api/native-app';
import { useBaseStore } from '@/stores/base';

export function useNativeAppRoleAccess() {
  return useBaseStore(
    (s) =>
      s.systemInfo?.runtimeMode === 'native' &&
      s.systemInfo?.capabilities?.app?.enabled === true &&
      s.currentUserInfo?.resourceKeys?.includes('uns.app.manage') === true
  );
}

export function useNativeAppRoleDirectory(enabled = true) {
  const [directory, setDirectory] = useState<BusinessRoleDirectory>();
  const [loading, setLoading] = useState(enabled);
  const [error, setError] = useState(false);
  const [version, setVersion] = useState(0);
  const reload = useCallback(() => {
    if (!enabled) return;
    setLoading(true);
    setError(false);
    setVersion((value) => value + 1);
  }, [enabled]);
  useEffect(() => {
    if (!enabled) return;
    let active = true;
    getBusinessRoles()
      .then((result) => {
        if (active) setDirectory(result);
      })
      .catch(() => {
        if (active) setError(true);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [enabled, version]);
  useEffect(() => {
    window.addEventListener('native-app-roles-updated', reload);
    return () => window.removeEventListener('native-app-roles-updated', reload);
  }, [reload]);
  return { directory: enabled ? directory : undefined, loading, error, reload };
}
