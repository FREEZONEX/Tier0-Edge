import { v4 as uuidv4 } from 'uuid';
import {
  committedMetadata,
  latestSamples,
  metadataPage,
  recordPage,
  sampleHistory,
  unsV2Api,
  type MetadataOutcome,
} from './uns-v2';

export type MetaKind = 'folder' | 'topic' | 'collection';
export interface MetaSchema {
  type: string;
  format?: 'int32' | 'float' | 'double' | 'date-time';
  title?: string;
  description?: string;
  unit?: string;
  properties?: Record<string, MetaSchema>;
  required?: string[];
  additionalProperties?: boolean;
  items?: MetaSchema;
  enum?: unknown[];
  minimum?: number;
  maximum?: number;
}
export interface FieldBinding {
  name: string;
  displayName?: string;
  kind: 'record' | 'sample';
  collectionId: string;
  recordId: string;
  fieldPath: string;
}
export interface MetaDocument {
  schemaVersion?: number;
  viewMode?: 'records' | 'fields';
  name: string;
  description?: string;
  parentId?: string;
  collectionId?: string;
  fields?: FieldBinding[];
  recordSchema?: MetaSchema;
  sampleSchema?: MetaSchema;
  directoryTrashId?: string;
  purgeJobId?: string;
}
export interface UnsMetadata {
  originNodeId?: string;
  id: string;
  mountId?: string;
  kind: MetaKind;
  data: MetaDocument;
  revision: number;
  createdAt: number;
  updatedAt: number;
  deletedAt: number;
}
export interface UnsRecord {
  sourceId: string | null;
  sourceTime: number | null;
  lock?: { label: string; expiresAt: number; fence: number; since: number };
  collectionId: string;
  id: string;
  data: Record<string, unknown>;
  revision: number;
  createdAt: number;
  updatedAt: number;
}
export interface UnsSample {
  sourceId: string | null;
  streamId?: string;
  bindingId?: string;
  collectionId: string;
  recordId: string;
  id: string;
  time: number;
  receivedAt: number;
  quality: string;
  data: Record<string, unknown>;
}
export interface ResolvedField {
  binding: FieldBinding;
  status: 'available' | 'missing';
  value: unknown;
  time?: number;
  receivedAt?: number;
  quality?: string;
}
export interface Page<T> {
  list: T[];
  total: number;
  page: number;
  size: number;
}
export interface MetadataPurgeResult {
  status: 'committed' | 'partiallyCommitted';
  results?: { id: string; kind: string; status: string }[];
  summary?: { total: number; succeeded: number; failed: number; notExecuted: number };
}
const canonicalKind = (kind: string) => (kind === 'folder' ? 'directory' : kind);
const legacyMeta = (meta: UnsMetadata): UnsMetadata => ({
  ...meta,
  kind: (meta.kind as string) === 'directory' ? 'folder' : meta.kind,
});
const documentV2 = (kind: MetaKind, doc: MetaDocument): MetaDocument => {
  const fields = doc.fields || [];
  if (kind === 'topic' && fields.length)
    return {
      schemaVersion: 1,
      name: doc.name,
      description: doc.description,
      parentId: doc.parentId,
      viewMode: 'fields',
      fields,
    };
  return { ...doc, schemaVersion: 1, ...(kind === 'topic' ? { viewMode: 'records', fields: undefined } : {}) };
};
const oneQuery = async <T>(operation: string, query: Record<string, unknown>, signal?: AbortSignal) => {
  const response = await unsV2Api.query<T>(operation, [{ queryId: 'query', ...query }], signal);
  return response.results[0];
};
const readMeta = async (kind: MetaKind, id: string) => {
  const result = await unsV2Api.metadataAction<{ items: UnsMetadata[] }>('find', {
    kind: canonicalKind(kind),
    filter: { _id: id },
    limit: 1,
  });
  if (!result.items[0]) throw new Error('Missing metadata');
  return legacyMeta(result.items[0]);
};

export const saveMetadataDocument = (
  value: { id?: string; kind: MetaKind; expectedRevision: number; document: MetaDocument },
  read: typeof readMeta = readMeta,
  write: typeof unsV2Api.metadataAction = unsV2Api.metadataAction
) =>
  (async () => {
    const id = value.id || uuidv4();
    const document = documentV2(value.kind, value.document);
    if (value.expectedRevision) {
      const current = await read(value.kind, id);
      // 表单只拥有这些字段；完整替换须保留未展示的索引、语义等配置。
      const replacement: Record<string, unknown> = { ...current.data };
      const editableFields = [
        'name',
        'description',
        'parentId',
        ...(value.kind === 'collection'
          ? ['recordSchema', 'sampleSchema']
          : value.kind === 'topic'
            ? ['collectionId', 'fields', 'viewMode']
            : []),
      ];
      for (const field of editableFields) delete replacement[field];
      Object.assign(replacement, document);
      committedMetadata(
        await write<MetadataOutcome>('replaceOne', {
          kind: canonicalKind(value.kind),
          filter: { _id: id },
          expectedRevision: value.expectedRevision,
          replacement,
          idempotencyKey: uuidv4(),
        })
      );
    } else {
      committedMetadata(
        await write<MetadataOutcome>('insertOne', {
          kind: canonicalKind(value.kind),
          id,
          data: document,
          idempotencyKey: uuidv4(),
        })
      );
    }
    return read(value.kind, id);
  })();

export const metadataApi = {
  find: (kind: MetaKind, id: string, archived = false, signal?: AbortSignal) =>
    unsV2Api
      .metadataAction<{ items: UnsMetadata[] }>(
        'find',
        {
          kind: canonicalKind(kind),
          filter: { _id: id },
          limit: 1,
          state: archived ? 'deleted' : 'active',
        },
        signal
      )
      .then(({ items }) => (items[0] ? legacyMeta(items[0]) : undefined)),
  namespaceAncestors: (parentId: string, signal?: AbortSignal) =>
    oneQuery<Page<UnsMetadata> & { ancestors: UnsMetadata[] }>(
      'namespace/query',
      {
        parentId,
        includeAncestors: true,
        size: 1,
      },
      signal
    ).then((result) => (result.ancestors ?? []).map(legacyMeta)),
  createCollection: (parentId: string | undefined, document: MetaDocument) =>
    saveMetadataDocument({ kind: 'collection', expectedRevision: 0, document: { ...document, parentId } }),
  list: (params: { kind?: string; deleted?: boolean; page?: number; size?: number }, signal?: AbortSignal) =>
    metadataPage<UnsMetadata>(
      params.kind === 'namespace'
        ? ['directory', 'topic', 'collection']
        : params.kind
          ? [canonicalKind(params.kind)]
          : ['directory', 'topic', 'collection'],
      {
        state: params.deleted ? 'deleted' : 'active',
        page: params.page,
        size: Math.min(params.size || 20, 200),
      },
      signal
    ).then((page) => ({ ...page, list: page.list.map(legacyMeta) })),
  save: (value: Parameters<typeof saveMetadataDocument>[0]) => saveMetadataDocument(value),
  restore: (kind: MetaKind, items: UnsMetadata[]) =>
    unsV2Api
      .metadataAction<MetadataOutcome>('restore', {
        kind: canonicalKind(kind),
        items: items.map((item) => ({ id: item.id, expectedRevision: item.revision })),
        idempotencyKey: uuidv4(),
      })
      .then(committedMetadata),
  purge: (kind: MetaKind, ids: string[], idempotencyKey: string) =>
    unsV2Api.metadataAction<{ status: 'accepted'; jobId: string }>('purge', {
      kind: canonicalKind(kind),
      ids,
      idempotencyKey,
    }),
  purgeJobs: (jobIds: string[], signal?: AbortSignal) =>
    unsV2Api.jobs<MetadataPurgeResult>(jobIds, signal).then((response) => response.results[0]?.list ?? []),
  copy: (meta: UnsMetadata, target: { parentId?: string; name?: string; description?: string }) =>
    (async () => {
      const result = committedMetadata(
        await unsV2Api.namespaceCopy({
          kind: canonicalKind(meta.kind),
          id: meta.id,
          expectedRevision: meta.revision,
          data: target,
        })
      );
      if (!result.insertedId) throw new Error('Missing copied metadata');
      return readMeta(meta.kind, result.insertedId);
    })(),
  state: (meta: UnsMetadata, restore: boolean) =>
    (async () => {
      committedMetadata(
        await unsV2Api.metadataAction<MetadataOutcome>(restore ? 'restore' : 'deleteOne', {
          kind: canonicalKind(meta.kind),
          idempotencyKey: uuidv4(),
          ...(restore
            ? { items: [{ id: meta.id, expectedRevision: meta.revision }] }
            : {
                filter: { _id: meta.id },
                expectedRevision: meta.revision,
              }),
        })
      );
      const result = await metadataApi.find(meta.kind, meta.id, !restore);
      if (!result) throw new Error('Missing metadata');
      return result;
    })(),
  view: (id: string, signal?: AbortSignal) =>
    oneQuery<{ meta: UnsMetadata; fields: ResolvedField[] }>('topics/view', { topicId: id }, signal).then((view) => ({
      ...view,
      meta: legacyMeta(view.meta),
    })),
  records: (id: string, params: { search?: string; page?: number; size?: number }, signal?: AbortSignal) =>
    recordPage(id, params, signal),
  saveRecord: (collectionId: string, value: { id: string; expectedRevision: number; data: Record<string, unknown> }) =>
    (async () => {
      const { expectedRevision, ...fields } = value;
      const outcome = await unsV2Api.documentAction<{ deliveryState?: string }>(
        expectedRevision ? 'replace-one' : 'insert-one',
        {
          collectionId,
          ...(expectedRevision ? { filter: { _id: fields.id }, replacement: fields.data, expectedRevision } : fields),
        }
      );
      if (outcome.deliveryState === 'pendingUpload') return { deliveryState: 'pendingUpload' as const };
      const result = await unsV2Api.dataAction<{ items: UnsRecord[] }>(collectionId, 'records', 'find', {
        filter: { _id: value.id },
        limit: 1,
      });
      if (!result.items[0]) throw new Error('Missing saved record');
      return result.items[0];
    })(),
  deleteRecord: (record: UnsRecord) =>
    unsV2Api.documentAction('delete-one', {
      collectionId: record.collectionId,
      filter: { _id: record.id },
      expectedRevision: record.revision,
    }),
  samples: (
    id: string,
    params: { recordId: string; startTime?: number; endTime?: number; page?: number; size?: number },
    signal?: AbortSignal
  ) => {
    if (params.size === 1 && params.startTime === undefined && params.endTime === undefined)
      return latestSamples(id, [params.recordId], signal).then((res) => {
        const sample = res[0]?.sample;
        return { list: sample ? [sample] : [], total: sample ? 1 : 0, page: 1, size: 1 };
      });
    return sampleHistory(
      id,
      params.recordId,
      { ...params, startTime: params.startTime ?? 0, endTime: params.endTime ?? Date.now() + 1 },
      signal
    );
  },
};

export async function loadMetadata(deleted = false, signal?: AbortSignal) {
  const result: UnsMetadata[] = [];
  let page = 1;
  while (true) {
    const current = await metadataApi.list({ deleted, page, size: 200 }, signal);
    result.push(...current.list);
    if (result.length >= current.total || !current.list.length) return result;
    page += 1;
  }
}
