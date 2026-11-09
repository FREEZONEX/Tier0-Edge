import { coreApi } from './core-adapter';
export function collectionSyncAction<T>(action: 'catalog', body: Record<string, unknown>): Promise<T> {
 return coreApi.post(`/v2/fleet/collection-sync/${action}`, body, { _noMessage: true });
}
