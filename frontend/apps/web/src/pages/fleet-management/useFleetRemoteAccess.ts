import { useEffect, useState } from 'react';
import { getFleetRemoteAccess, type FleetRemoteAccess } from '@/apis/core-api/fleet-connection';
export function useFleetRemoteAccess(nodeId: string, enabled = true) {
  const [access, setAccess] = useState<FleetRemoteAccess>();
  useEffect(() => {
    if (!enabled) return;
    let active = true;
    const load = async () => {
      try {
        const value = await getFleetRemoteAccess(nodeId);
        if (active) setAccess(value);
      } catch {
        if (active) setAccess(undefined);
      }
    };
    void load();
    const timer = setInterval(() => void load(), 5000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [nodeId, enabled]);
  return enabled ? access : undefined;
}
