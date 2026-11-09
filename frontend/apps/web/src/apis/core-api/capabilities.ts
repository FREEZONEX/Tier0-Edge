import { ApiWrapper, CustomAxiosConfigEnum } from '@/utils/request';
import {
  validateCapabilityDiscovery,
  type CapabilityDiscoverResponse,
} from '../../../../../../packages/app-data-client/src/capabilities';
export type {
  CapabilityOperation,
  PublicCapability,
} from '../../../../../../packages/app-data-client/src/capabilities';
const api = new ApiWrapper('/api/core/v2/capabilities');
// Normal session bridge; the browser never creates or stores an API key.
type SessionCapabilityResponse = Omit<CapabilityDiscoverResponse, 'product'> & {
  product: CapabilityDiscoverResponse['product'] | 'edge';
};
export async function discoverCapabilities(): Promise<SessionCapabilityResponse> {
  const raw = await api.post(
    '/discover',
    {},
    { [CustomAxiosConfigEnum.NoCode]: true, [CustomAxiosConfigEnum.NoMessage]: true }
  );
  // Native Edge has the Enterprise operation contract, with its own session product identity.
  if (raw?.product === 'edge') {
    return { ...validateCapabilityDiscovery({ ...raw, product: 'enterprise' }), product: 'edge' };
  }
  return validateCapabilityDiscovery(raw);
}
