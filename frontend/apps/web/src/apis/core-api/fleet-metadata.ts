import { v4 as uuid } from 'uuid';
import { coreApi } from './core-adapter';
import {
  saveMetadataDocument,
  type MetaDocument,
  type MetaKind,
  type UnsMetadata,
  type UnsRecord,
  type UnsSample,
  type ResolvedField,
  type metadataApi,
} from './uns-metadata';
import {
  committedMetadata,
  latestSamples,
  recordPage,
  sampleHistory,
  type MetadataOutcome,
  type unsV2Api,
} from './uns-v2';

const normalize = (value: UnsMetadata): UnsMetadata => ({
  ...value,
  kind: (value.kind as string) === 'directory' ? 'folder' : value.kind,
});
export function fleetMetadata(nodeId: string) {
  const execute = <T>(operation: string, data: Record<string, unknown>, signal?: AbortSignal): Promise<T> =>
    coreApi.post('/v2/fleet/runtime/execute', { nodeId, operation, data }, { signal });
  const action: typeof unsV2Api.metadataAction = (name, data, signal) =>
    execute(`metadata${name[0].toUpperCase()}${name.slice(1)}`, data, signal);
  const dataAction: typeof unsV2Api.dataAction = (collectionId, resource, name, data, signal) => {
    const operations: Record<string, string> = {
      'records:aggregate': 'recordAggregatePipeline',
      'records:count': 'recordCount',
      'samples:find': 'sampleFind',
      'samples:aggregate': 'sampleAggregate',
      'samples:latest': 'sampleLatestValues',
    };
    const operation = operations[`${resource}:${name}`];
    if (!operation) throw new Error('Unsupported remote data read');
    return execute(operation, { ...data, collectionId }, signal);
  };
  const samples: typeof metadataApi.samples = async (id, params, signal) => {
    if (params.size === 1 && params.startTime === undefined && params.endTime === undefined) {
      const result = await latestSamples(id, [params.recordId], signal, dataAction);
      const sample = result[0]?.sample;
      return { list: sample ? [sample] : [], total: sample ? 1 : 0, page: 1, size: 1 };
    }
    return sampleHistory(id, params.recordId, params, signal, dataAction);
  };
  const find = async (kind: MetaKind, id: string, signal?: AbortSignal) => {
    const { items } = await action<{ items: UnsMetadata[] }>(
      'find',
      {
        kind: kind === 'folder' ? 'directory' : kind,
        filter: { _id: id },
        state: 'active',
        limit: 1,
      },
      signal
    );
    if (!items[0]) throw new Error('Missing remote metadata');
    return normalize(items[0]);
  };
  const view: typeof metadataApi.view = async (id, signal) => {
    const meta = await find('topic', id, signal);
    const records = new Map<string, UnsRecord>();
    const samples = new Map<string, UnsSample>();
    for (const collectionId of new Set((meta.data.fields || []).map((field) => field.collectionId))) {
      const bindings = (meta.data.fields || []).filter((field) => field.collectionId === collectionId);
      const recordIds = [
        ...new Set(bindings.filter((field) => field.kind === 'record').map((field) => field.recordId)),
      ];
      const sampleIds = [
        ...new Set(bindings.filter((field) => field.kind === 'sample').map((field) => field.recordId)),
      ];
      if (recordIds.length) {
        const page = await recordPage(collectionId, { recordIds, size: 200 }, signal, dataAction);
        page.list.forEach((record) => records.set(`${collectionId}/${record.id}`, record));
      }
      if (sampleIds.length) {
        const latest = await latestSamples(collectionId, sampleIds, signal, dataAction);
        latest.forEach(({ recordId, sample }) => {
          if (sample) samples.set(`${collectionId}/${recordId}`, sample);
        });
      }
    }
    const fields: ResolvedField[] = (meta.data.fields || []).map((binding) => {
      const key = `${binding.collectionId}/${binding.recordId}`;
      const record = records.get(key);
      const sample = samples.get(key);
      let value: unknown = binding.kind === 'record' ? record?.data : sample?.data;
      for (const part of binding.fieldPath.split('.')) {
        value =
          part &&
          !['__proto__', 'constructor', 'prototype'].includes(part) &&
          value !== null &&
          typeof value === 'object' &&
          Object.prototype.hasOwnProperty.call(value, part)
            ? (value as Record<string, unknown>)[part]
            : undefined;
      }
      return {
        binding,
        value,
        status: value === undefined ? 'missing' : 'available',
        time: binding.kind === 'record' ? record?.updatedAt : sample?.time,
        receivedAt: binding.kind === 'sample' ? sample?.receivedAt : undefined,
        quality: binding.kind === 'sample' ? sample?.quality : undefined,
      };
    });
    return { meta, fields };
  };
  return {
    find,
    records: (id: string, params: Parameters<typeof recordPage>[1], signal?: AbortSignal) =>
      recordPage(id, params, signal, dataAction),
    samples,
    view,
    createCollection: (parentId: string | undefined, document: MetaDocument) =>
      saveMetadataDocument(
        { kind: 'collection', expectedRevision: 0, document: { ...document, parentId } },
        find,
        action
      ),
    load: async () => {
      const items: UnsMetadata[] = [];
      for (let page = 1; ; page++) {
        const result = await action<{ items: UnsMetadata[]; pageInfo: { hasMore: boolean } }>('find', {
          kinds: ['directory', 'topic', 'collection'],
          state: 'active',
          page: { mode: 'offset', page, size: 200 },
        });
        items.push(...result.items.map(normalize));
        if (!result.pageInfo.hasMore) return items;
      }
    },
    save: (value: Parameters<typeof saveMetadataDocument>[0]) => saveMetadataDocument(value, find, action),
    remove: async (meta: UnsMetadata) =>
      committedMetadata(
        await action<MetadataOutcome>('deleteOne', {
          kind: meta.kind === 'folder' ? 'directory' : meta.kind,
          filter: { _id: meta.id },
          expectedRevision: meta.revision,
          idempotencyKey: uuid(),
        })
      ),
  };
}
