import { ApiWrapper, CustomAxiosConfigEnum } from '@/utils/request';

const api = new ApiWrapper('/api/core/v2/uns');
const requests = new ApiWrapper('/api/core/v2/stream-requests');
const quiet = { [CustomAxiosConfigEnum.NoMessage]: true };
export const requestGrantActions = ['stream.requests.read', 'stream.requests.invoke', 'stream.requests.write'] as const;
export type RequestGrantAction = (typeof requestGrantActions)[number];
export interface AppRequestGrant {
  code: string;
  principalKind: 'app';
  principalID: string;
  status: 'active' | 'inactive';
  bindingDigest: string;
  snapshotToken: string;
  recipientEligible: 1 | 2;
  recipientActions: RequestGrantAction[];
  activationSupported: 1 | 2;
  registered: 1 | 2;
  revision: number;
  active: 1 | 2;
  actions: RequestGrantAction[];
  targets: {
    digest: string;
    action: string;
    revision: number;
    active: 1 | 2;
    current: 1 | 2;
    kind?: string;
    reference?: string;
  }[];
}
export interface AppRequestEntry {
  code: string;
  streamId: string;
  streamName: string;
  entryName: string;
  configuredActive: 1 | 2;
  state: 'available' | 'blocked';
}
export interface RequestCandidate {
  code: string;
  name: string;
  status: string;
}
export interface AppRequestGrantUpdate {
  code: string;
  principalKind: 'app';
  principalID: string;
  active: 1 | 2;
  actions: RequestGrantAction[];
  expectedSnapshotToken: string;
}

export function requestGrantUpdate(
  snapshot: AppRequestGrant,
  active: 1 | 2,
  actions: RequestGrantAction[]
): AppRequestGrantUpdate {
  return {
    code: snapshot.code,
    principalKind: 'app',
    principalID: snapshot.principalID,
    active,
    actions: active === 1 ? actions : [],
    expectedSnapshotToken: snapshot.snapshotToken,
  };
}

function checked(value: AppRequestGrant, appId: string, code: string) {
  if (
    !value ||
    value.principalKind !== 'app' ||
    value.principalID !== appId ||
    value.code !== code ||
    !/^cgs1\.[a-f0-9]{64}\.[a-f0-9]{64}$/.test(value.snapshotToken) ||
    !Number.isSafeInteger(value.revision) ||
    !Array.isArray(value.actions) ||
    !Array.isArray(value.recipientActions) ||
    !Array.isArray(value.targets)
  )
    throw new Error('Invalid Request grant response');
  return value;
}

export const appRequestGrantsApi = {
  list: async (appId: string, signal?: AbortSignal) => {
    const entries: AppRequestEntry[] = [];
    for (let page = 1; ; page++) {
      const result = (await api.post(
        '/app-stream-access/list',
        { appId, page, size: 100, includeInactive: 1 },
        { ...quiet, signal }
      )) as { items: AppRequestEntry[]; total: number };
      entries.push(...result.items);
      if (entries.length >= result.total || !result.items.length) return entries;
    }
  },
  candidates: async (signal?: AbortSignal) => {
    const entries: RequestCandidate[] = [];
    for (let page = 1; ; page++) {
      const result = (await requests.post(
        '/discover',
        { page, size: 100 },
        { ...quiet, signal, [CustomAxiosConfigEnum.NoCode]: true }
      )) as { list: RequestCandidate[]; total: number };
      if (!Array.isArray(result.list)) throw new Error('Invalid Request discovery response');
      entries.push(...result.list);
      if (entries.length >= result.total || !result.list.length) return entries;
    }
  },
  get: async (appId: string, code: string, signal?: AbortSignal) =>
    checked(
      (await api.post(
        '/stream-requests/grants/get',
        { code, principalKind: 'app', principalID: appId },
        { ...quiet, signal }
      )) as AppRequestGrant,
      appId,
      code
    ),
  set: async (request: AppRequestGrantUpdate, signal?: AbortSignal) =>
    checked(
      (await api.post('/stream-requests/grants/set', request, { ...quiet, signal })) as AppRequestGrant,
      request.principalID,
      request.code
    ),
};
