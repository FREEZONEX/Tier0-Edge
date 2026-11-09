import type { SystemInfoProps } from '@/stores/types';
import logoLight from '@/assets/custom-nav/logo-light.svg';
import logoDark from '@/assets/custom-nav/logo-dark.svg';
import edgeLogoLight from '@/assets/custom-nav/logo-edge-light.svg';
import edgeLogoDark from '@/assets/custom-nav/logo-edge-dark.svg';

// Always derive display flags from the server's build edition, never localStorage,
// installation mode, URL parameters or the currently activated license plan.
export const isEdgeEdition = (info?: Pick<SystemInfoProps, 'productEdition'>) => info?.productEdition === 'edge';
export const productName = (info?: Pick<SystemInfoProps, 'productEdition'>) =>
  isEdgeEdition(info) ? 'Tier0 Edge' : 'Tier0 enterprise';
export const productLogo = (info: Pick<SystemInfoProps, 'productEdition'> | undefined, dark = false) =>
  isEdgeEdition(info) ? (dark ? edgeLogoDark : edgeLogoLight) : dark ? logoDark : logoLight;
