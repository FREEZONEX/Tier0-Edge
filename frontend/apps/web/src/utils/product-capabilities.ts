import type { SystemInfoProps } from '@/stores/types';

// Support/policy comes from Backend, independently of transient runtime health.
export const capabilityEnabled = (info: SystemInfoProps, capability: string) =>
  info.capabilities?.[capability]?.enabled === true;

export const capabilityAllowsPath = (info: SystemInfoProps, path?: string) => {
  if (!path?.startsWith('/') || path.startsWith('//')) return true;
  const root = path.split(/[?#]/, 1)[0].split('/')[1]?.toLowerCase();
  return !Object.values(info.capabilities || {}).some(
    (capability) => capability.pages.includes(root) && !capability.enabled
  );
};
