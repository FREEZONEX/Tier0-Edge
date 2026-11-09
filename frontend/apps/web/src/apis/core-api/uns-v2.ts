import { v4 as uuidv4 } from 'uuid';
import { ApiWrapper, CustomAxiosConfigEnum } from '@/utils/request';
import { message } from 'antd';
import { getIntl } from '@/stores/i18n-store';
import type { UnsMetadata, UnsRecord, UnsSample, Page } from './uns-metadata';

const api = new ApiWrapper('/api/core/v2/uns');
export interface DataPage<T> {
  items: T[];
  pageInfo: {
    mode: 'cursor' | 'offset';
    hasMore: boolean;
    nextCursor: string | null;
    nextPage: number | null;
    snapshotScope: 'thisRequestOnly';
  };
}

export interface ChangeResult {
  opId: string;
  action: string;
  id: string;
  kind?: string;
  collectionId?: string;
  revision?: number;
  status: string;
}
export interface BatchOutcome {
  status: 'committed' | 'validated' | 'accepted';
  jobId?: string;
  results?: ChangeResult[];
  replayed: boolean;
}
export type AppStreamPermission =
  | 'stream.requests.read'
  | 'stream.requests.invoke'
  | 'stream.requests.write'
  | 'stream.messages.publish'
  | 'stream.messages.subscribe';
export interface AppMessageTopicPermission {
  topic: string;
  publish: boolean;
  subscribe: boolean;
}
export interface AppStreamPolicy {
  appId: string;
  revision: number;
  version: 'cloud-hosted-app-stream/v1';
  status: 'active' | 'disabled';
  platformPermissions: AppStreamPermission[];
  messageTopics?: AppMessageTopicPermission[];
  authorityStatus: 'available' | 'notReady' | 'notFound' | 'unavailable';
  appName: string;
}
export interface AppStreamPolicyUpdate {
  appId: string;
  expectedRevision: number;
  status: AppStreamPolicy['status'];
  platformPermissions: AppStreamPermission[];
  messageTopics?: AppMessageTopicPermission[];
  idempotencyKey: string;
  dryRun: boolean;
}
export interface BatchJob<Result = BatchOutcome> {
  jobId: string;
  status: 'queued' | 'running' | 'succeeded' | 'failed';
  progress: { total: number; completed: number };
  result?: Result;
  error?: { code: string; opId?: string; path?: string };
}
export interface MetadataOutcome {
  status: 'committed' | 'validated' | 'partiallyCommitted' | 'failed';
  replayed: boolean;
  insertedId?: string;
  matchedCount?: number;
  deletedCount?: number;
  restoredCount?: number;
  results?: { opId: string; insertedId?: string }[];
}
export function committedMetadata<T extends MetadataOutcome>(outcome: T): T {
  if (outcome.status !== 'committed') throw new Error('UnsV2.jobFailed');
  return outcome;
}
export const unsV2Api = {
  appStreamPolicyGet: (appId: string, signal?: AbortSignal) =>
    api.post(
      '/app-stream-policy/get',
      { appId },
      { signal, [CustomAxiosConfigEnum.NoMessage]: true }
    ) as Promise<AppStreamPolicy>,
  appStreamPolicyUpdate: (request: AppStreamPolicyUpdate) =>
    api.post('/app-stream-policy/update', request, {
      [CustomAxiosConfigEnum.NoMessage]: true,
    }) as Promise<BatchOutcome>,
  streamList: (options: { size?: number; page?: number }) =>
    api.post('/streams/list', options) as Promise<{ list: UnsMetadata[] }>,
  metadataAction: <T>(action: string, payload: Record<string, unknown>, signal?: AbortSignal) =>
    api.post(`/metadata${action === 'insertOne' ? '' : ':' + action}`, payload, { signal }) as Promise<T>,
  metadataTransact: async (operations: Record<string, unknown>[]) =>
    committedMetadata(
      await unsV2Api.metadataAction<MetadataOutcome>('transact', {
        operations,
        idempotencyKey: uuidv4(),
      })
    ),
  namespaceCopy: (payload: Record<string, unknown>) =>
    api.post('/namespace:copy', {
      ...payload,
      dryRun: false,
      idempotencyKey: uuidv4(),
    }) as Promise<MetadataOutcome>,
  dataAction: <T>(
    collectionId: string,
    resource: 'records' | 'samples',
    action: string,
    payload: Record<string, unknown>,
    signal?: AbortSignal
  ) =>
    api.post(`/collections/${encodeURIComponent(collectionId)}/${resource}${action ? ':' + action : ''}`, payload, {
      signal,
    }) as Promise<T>,
  documentAction: <T>(action: string, payload: Record<string, unknown>, signal?: AbortSignal) =>
    api.post(
      `/collections/${encodeURIComponent(String(payload.collectionId))}/records${action === 'insert-one' ? '' : ':' + action.replace(/-([a-z])/g, (_, letter: string) => letter.toUpperCase())}`,
      payload,
      { signal }
    ) as Promise<T>,
  query: <T>(operation: string, queries: Record<string, unknown>[], signal?: AbortSignal) =>
    api.post(`/${operation}`, { queries }, { signal }) as Promise<{ results: T[] }>,
  resourceWrite: async (path: string, payload: Record<string, unknown>) => {
    if (!payload.items && /^\/(directories|topics|collections|sources|streams|apps)\//.test(path)) {
      const { idempotencyKey, mode, ...item } = payload;
      delete item.dryRun;
      payload = { idempotencyKey, mode, items: [item] };
    }
    const outcome = (await api.post(path, {
      ...payload,
      dryRun: false,
    })) as BatchOutcome & { result?: ChangeResult };
    return completeBatch({ ...outcome, results: outcome.results ?? (outcome.result ? [outcome.result] : []) });
  },
  jobs: <Result = BatchOutcome>(jobIds: string[], signal?: AbortSignal) =>
    api.post('/jobs/query', { queries: [{ queryId: 'jobs', jobIds }] }, { signal }) as Promise<{
      results: { list: BatchJob<Result>[] }[];
    }>,
  collectionGet: async (id: string, signal?: AbortSignal) => {
    const result = await unsV2Api.metadataAction<{ items: UnsMetadata[] }>(
      'find',
      { kind: 'collection', filter: { _id: id }, limit: 1 },
      signal
    );
    if (!result.items[0]) throw new Error('Missing collection');
    return result.items[0];
  },
  collectionList: (options: { page?: number; size?: number; search?: string }, signal?: AbortSignal) =>
    metadataPage<UnsMetadata>(['collection'], options, signal),
};

/** 元数据列表与计数分别读取当前快照；专用资源继续使用其领域列表。 */
export async function metadataPage<T>(
  kinds: string[],
  options: {
    page?: number;
    size?: number;
    search?: string;
    state?: 'active' | 'deleted' | 'all';
    filter?: Record<string, unknown>;
  } = {},
  signal?: AbortSignal
): Promise<Page<T>> {
  const page = options.page ?? 1;
  const size = options.size ?? 20;
  if (kinds.length === 1 && kinds[0] === 'source') {
    // Native Sources have their own read permission and summary contract. The
    // former metadata sources/list route was retired with CRUD consolidation.
    const result = (await api.post(
      '/sources/documents/list',
      {
        page,
        size,
        search: options.search,
        includeDocument: false,
      },
      { signal }
    )) as Page<{
      id: string | number;
      name: string;
      description?: string;
      protocol: string;
      revision: number;
      createdAt: number;
      updatedAt: number;
    }>;
    return {
      ...result,
      list: result.list.map((source) => ({
        id: String(source.id),
        kind: 'source',
        revision: source.revision,
        createdAt: source.createdAt,
        updatedAt: source.updatedAt,
        data: {
          name: source.name,
          description: source.description,
          adapter: source.protocol,
        },
      })) as T[],
    };
  }
  if (kinds.length === 1 && ['stream', 'app'].includes(kinds[0])) {
    return api.post(
      `/${kinds[0]}s/list`,
      {
        page,
        size,
        search: options.search,
        archived: options.state === 'deleted',
      },
      { signal }
    ) as Promise<Page<T>>;
  }
  const filter = {
    ...options.filter,
    ...(options.search
      ? {
          name: { $regex: options.search.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), $options: 'i' },
        }
      : {}),
  };
  const scope = { kinds, state: options.state ?? 'active', filter };
  const [result, count] = await Promise.all([
    unsV2Api.metadataAction<DataPage<T>>('find', { ...scope, page: { mode: 'offset', page, size } }, signal),
    unsV2Api.metadataAction<{ count: number }>('count', scope, signal),
  ]);
  return { list: result.items, total: count.count, page, size };
}

/** 普通查询返回资源对象，聚合返回 pipeline 定义的文档。 */
export async function recordPage(
  collectionId: string,
  options: {
    search?: string;
    recordIds?: string[];
    page?: number;
    size?: number;
    filter?: Record<string, unknown>;
    sort?: { field: string; direction: 1 | -1 };
  } = {},
  signal?: AbortSignal,
  dataAction = unsV2Api.dataAction
): Promise<Page<UnsRecord>> {
  const page = options.page ?? 1;
  const size = options.size ?? 20;
  const filter = {
    ...options.filter,
    ...(options.recordIds ? { _id: { $in: options.recordIds } } : {}),
    ...(options.search ? { _id: options.search } : {}),
  };
  const sort = options.sort ?? { field: '_id', direction: 1 };
  const order = [[sort.field, sort.direction]];
  if (sort.field !== '_id') order.push(['_id', 1]);
  const [result, total] = await Promise.all([
    dataAction<DataPage<Record<string, unknown> & { _id: string; _meta: Omit<UnsRecord, 'id' | 'data'> }>>(
      collectionId,
      'records',
      'aggregate',
      { pipeline: [{ $match: filter }, { $sort: order }], page: { mode: 'offset', page, size } },
      signal
    ),
    dataAction<{ count: number }>(collectionId, 'records', 'count', { filter }, signal),
  ]);
  return {
    list: result.items.map(({ _id, _meta, ...data }) => ({ ..._meta, id: _id, data })),
    total: total.count,
    page,
    size,
  };
}

/** 历史查询固定到指定 Record；分页和计数分别使用独立请求快照。 */
export async function sampleHistory(
  collectionId: string,
  recordId: string,
  options: { startTime?: number; endTime?: number; page?: number; size?: number } = {},
  signal?: AbortSignal,
  dataAction = unsV2Api.dataAction
): Promise<Page<UnsSample>> {
  const page = options.page ?? 1;
  const size = options.size ?? 20;
  if (!recordId.trim()) return { list: [], total: 0, page, size };
  const scope = { recordIds: [recordId], from: options.startTime ?? 0, to: options.endTime ?? Date.now() + 1 };
  const [result, count] = await Promise.all([
    dataAction<DataPage<UnsSample>>(
      collectionId,
      'samples',
      'find',
      {
        ...scope,
        sort: [
          ['_meta.time', -1],
          ['_id', -1],
        ],
        page: { mode: 'offset', page, size },
      },
      signal
    ),
    dataAction<DataPage<{ total: number }>>(
      collectionId,
      'samples',
      'aggregate',
      { ...scope, pipeline: [{ $count: 'total' }] },
      signal
    ),
  ]);
  return {
    list: result.items,
    total: count.items[0]?.total ?? 0,
    page,
    size,
  };
}

export async function latestSamples(
  collectionId: string,
  recordIds: string[],
  signal?: AbortSignal,
  dataAction = unsV2Api.dataAction
) {
  const result: { recordId: string; sample: UnsSample | null }[] = [];
  for (let offset = 0; offset < recordIds.length; offset += 20) {
    const response = await dataAction<{ series: { recordId: string; sample: UnsSample | null }[] }>(
      collectionId,
      'samples',
      'latest',
      { recordIds: recordIds.slice(offset, offset + 20) },
      signal
    );
    result.push(...response.series);
  }
  return result;
}

const resourcePaths = {
  directory: 'directories',
  topic: 'topics',
  collection: 'collections',
  source: 'sources',
  stream: 'streams',
  app: 'apps',
} as const;
type ConfigurationKind = keyof typeof resourcePaths;

// 单部分修改使用专用接口；完整编辑涉及多个部分时保持原来的原子提交。
export async function saveConfiguration(
  kind: ConfigurationKind,
  id: string,
  expectedRevision: number,
  before: Record<string, unknown>,
  after: Record<string, unknown>
) {
  if (['directory', 'topic', 'collection'].includes(kind)) {
    return committedMetadata(
      await unsV2Api.metadataAction<MetadataOutcome>('replaceOne', {
        kind,
        filter: { _id: id },
        expectedRevision,
        replacement: after,
        idempotencyKey: uuidv4(),
      })
    );
  }
  const changed = Object.keys({ ...before, ...after }).filter(
    (key) => key !== 'schemaVersion' && JSON.stringify(before[key]) !== JSON.stringify(after[key])
  );
  const metadata = ['name', 'description', ...(kind === 'app' ? ['status'] : [])];
  let action = '';
  let fields: Record<string, unknown> = {};
  if (changed.every((key) => metadata.includes(key))) {
    action = 'update';
    fields = Object.fromEntries(
      changed.map((key) => [key, after[key] ?? (key === 'parentId' ? null : key === 'description' ? '' : undefined)])
    );
    if (!changed.length) fields.name = after.name;
  } else if (kind === 'stream' && changed.every((key) => key === 'draft')) {
    action = 'draft/update';
    fields = { draft: after.draft };
  } else if (
    kind === 'app' &&
    changed.length === 1 &&
    ['collections', 'navigation', 'members', 'roles'].includes(changed[0])
  ) {
    const field = changed[0];
    action = `${field === 'collections' ? 'grants' : field}/update`;
    fields = { [field]: after[field] };
  } else if (kind === 'source' && changed.length === 1 && changed[0] === 'config') {
    const oldConfig = before.config as Record<string, unknown> | undefined;
    const newConfig = after.config as Record<string, unknown> | undefined;
    if (
      newConfig &&
      Object.keys(newConfig).every((key) => ['connection', 'streams'].includes(key)) &&
      JSON.stringify(oldConfig?.streams) === JSON.stringify(newConfig.streams)
    ) {
      action = 'connection/update';
      fields = { connection: newConfig.connection };
    }
  }
  // 必填字段删除、未知字段及不可分割的编辑交给完整领域校验，不能静默忽略。
  if (action && after.schemaVersion === 1 && Object.values(fields).every((value) => value !== undefined)) {
    return unsV2Api.resourceWrite(`/${resourcePaths[kind]}/${action}`, {
      id,
      expectedRevision,
      ...fields,
    });
  }
  return committedMetadata(
    (await api.post(`/${resourcePaths[kind]}:replace`, {
      id,
      expectedRevision,
      replacement: after,
      dryRun: false,
      idempotencyKey: uuidv4(),
    })) as MetadataOutcome
  );
}

// Return a typed pending state; callers decide whether to poll or show a job link.
export class PendingUnsJob extends Error {
  constructor(public jobId: string) {
    super('UnsV2.jobPending');
  }
}
export async function completeBatch(outcome: BatchOutcome): Promise<ChangeResult[]> {
  if (outcome.status !== 'accepted') return outcome.results || [];
  const jobId = outcome.jobId!;
  for (let attempt = 0; attempt < 120; attempt += 1) {
    const response = await unsV2Api.jobs([jobId]);
    const job = response.results[0].list[0];
    if (job.status === 'succeeded') return job.result?.results || [];
    if (job.status === 'failed') {
      message.error(getIntl('UnsV2.jobFailed'));
      throw new Error(job.error?.code || 'UnsV2.jobFailed');
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  message.info(`${getIntl('UnsV2.jobPending')} · ${jobId}`);
  throw new PendingUnsJob(jobId);
}

export interface SourceCredentialReference {
  credentialRef: string;
  kind: string;
  revision: number;
  updatedAt: number;
}
export interface SourceCredentialCatalog {
  protocol: string;
  sourceId: string;
  sourceRevision: number;
  selectedRef: string;
  items: SourceCredentialReference[];
}
export const sourceCredentialApi = {
  list: (id: string) => api.post('/sources/credentials/list', { id }) as Promise<SourceCredentialCatalog>,
  save: (payload: Record<string, unknown>) =>
    api.post('/sources/credentials/save', { ...payload, dryRun: false }) as Promise<{
      reference: SourceCredentialReference;
    }>,
};
