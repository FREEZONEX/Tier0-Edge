import { latestSamples, recordPage, unsV2Api } from './uns-v2';
import { metadataApi, type Page, type UnsRecord, type UnsSample } from './uns-metadata';

export const graphKinds = [
  'directory',
  'topic',
  'collection',
  'source',
  'stream',
  'source_state',
  'app',
  'task',
  'source_plan',
  'source_version',
  'subscription',
] as const;
export type GraphKind = (typeof graphKinds)[number];
export interface GraphMeta {
  key: string;
  kind: GraphKind;
  id: string;
  name: string;
  revision: number;
  archived: boolean;
  data: Record<string, unknown>;
}
export interface GraphEdge {
  key: string;
  source: string;
  target: string;
  relation: string;
}
export interface GraphResult {
  nodes: GraphMeta[];
  edges: GraphEdge[];
  total: number;
  page: number;
  size: number;
  collections: (GraphMeta & { collectionId: string })[];
  unloadedRelations: number;
  unloadedByNode?: { nodeKey: string; count: number }[];
}
export interface GraphQuery {
  mode: 'overview' | 'focus';
  targets?: { kind: GraphKind; id: string }[];
  kinds?: GraphKind[];
  search?: string;
  includeArchived?: boolean;
  page: number;
  size?: number;
}
export interface GraphRecordPage extends Page<UnsRecord> {
  latestSamples: { recordId: string; sample: UnsSample | null }[];
  historyFailed?: boolean;
  samplesEnabled?: boolean;
}
const collectionSamplesEnabled = async (collectionId: string, signal?: AbortSignal) => {
  const result = await unsV2Api.collectionGet(collectionId, signal);
  if (!result) throw new Error('Collection definition is unavailable');
  return result.data.sampleSchema != null;
};
export const unsGraphApi = {
  graph: async (query: GraphQuery, signal?: AbortSignal) => {
    const result = await unsV2Api.query<GraphResult>(
      'graph/query',
      [{ queryId: 'graph', ...query, size: Math.min(query.size || 100, 100) }],
      signal
    );
    return result.results[0];
  },
  records: async (
    collectionId: string,
    page: number,
    signal?: AbortSignal,
    recordIds?: string[]
  ): Promise<GraphRecordPage> => {
    if (recordIds && !recordIds.length) return { list: [], total: 0, page, size: 50, latestSamples: [] };
    const records = await recordPage(collectionId, { page, size: 50, recordIds }, signal);
    if (!records.list.length) return { ...records, latestSamples: [] };
    let samplesEnabled: boolean | undefined;
    try {
      samplesEnabled = await collectionSamplesEnabled(collectionId, signal);
      if (!samplesEnabled)
        return {
          ...records,
          samplesEnabled: false,
          latestSamples: records.list.map((record) => ({ recordId: record.id, sample: null })),
        };
      const latest = await latestSamples(
        collectionId,
        records.list.map((record) => record.id),
        signal
      );
      return { ...records, samplesEnabled: true, latestSamples: latest };
    } catch (error) {
      if (signal?.aborted) throw error;
      return { ...records, samplesEnabled, latestSamples: [], historyFailed: true };
    }
  },
  record: async (collectionId: string, recordId: string, signal?: AbortSignal) => {
    const result = await unsV2Api.dataAction<{ items: UnsRecord[] }>(
      collectionId,
      'records',
      'find',
      { filter: { _id: recordId }, limit: 1 },
      signal
    );
    return result.items[0];
  },
  samples: metadataApi.samples,
  recordHistory: async (
    collectionId: string,
    recordId: string,
    options: { startTime?: number; endTime?: number; page: number },
    signal?: AbortSignal
  ) => {
    const [record, samplesEnabled] = await Promise.all([
      unsGraphApi.record(collectionId, recordId, signal),
      collectionSamplesEnabled(collectionId, signal),
    ]);
    if (!samplesEnabled)
      return {
        record,
        samplesEnabled: false,
        history: { list: [] as UnsSample[], total: 0, page: options.page, size: 50 },
        hasHistory: false,
      };
    const [history, latest] = await Promise.all([
      metadataApi.samples(collectionId, { recordId, ...options, size: 50 }, signal),
      metadataApi.samples(collectionId, { recordId, size: 1 }, signal),
    ]);
    return { record, samplesEnabled: true, history, hasHistory: latest.total > 0 };
  },
};
