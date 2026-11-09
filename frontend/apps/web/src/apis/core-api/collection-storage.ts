import { coreApi } from './core-adapter';
export type StorageMode = 'local' | 'center' | 'both';
export interface CollectionStorage {
  policy: { mode: StorageMode; revision: number; centerId: string; updatedAt: number };
  effectiveRevision: number;
  state: string;
  connected: boolean;
  defaultPending: boolean;
  definitions: { total: number; synced: number; pending: number; failed: number };
  deliveryCenterId: string;
  historicalDelivery: { pending: number; failed: number; centers: number };
  delivery: { received: number; pending: number; committed: number; failed: number };
}
export function collectionStorageAction<T>(
  nodeId: string | undefined,
  action: 'get' | 'save',
  body: Record<string, unknown> = {}
): Promise<T> {
  return coreApi.post(
    `/v2/fleet/${nodeId ? '' : 'connection/'}storage/${action}`,
    { ...body, ...(nodeId ? { nodeId } : {}) },
    { _noMessage: true }
  );
}
