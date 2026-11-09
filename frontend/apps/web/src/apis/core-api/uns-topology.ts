import { ApiWrapper } from '@/utils/request';
import type { MetaSchema, Page, UnsRecord, UnsSample } from './uns-metadata';
import type { ProgramManifest } from './source-stream';
import { latestSamples, metadataPage, recordPage, sampleHistory, unsV2Api } from './uns-v2';

export type TopologyKind = 'directory' | 'topic' | 'collection' | 'source' | 'stream' | 'app';
export interface TopologyMeta {
  id: string;
  kind: TopologyKind;
  revision: number;
  launch?: { status: string; url?: string };
  published?: {
    version: number;
    manifest: { sources: Pick<ProgramManifest['sources'][number], 'id'>[]; topics: ProgramManifest['topics'] };
  };
  data: {
    name: string;
    entry?: { environment?: string; basePath?: string; path: string };
    parentId?: string;
    collectionId?: string;
    viewMode?: string;
    fields?: { name: string; kind: string; collectionId: string; recordId: string; fieldPath: string }[];
    recordSchema?: MetaSchema;
    sampleSchema?: MetaSchema;
    adapter?: string;
    activeVersion?: number;
    config?: {
      streams?: Record<
        string,
        {
          name?: string;
          streamId?: string | number;
          streamRelease?: number;
          enabled?: boolean;
          collectionId?: string;
          deletedAt?: number;
          target?: { collectionId?: string };
        }
      >;
    };
    collections?: { collectionId: string; alias?: string; permissions?: string[] }[];
    navigation?: { topicIds?: string[] };
  };
}
export interface TopologyRecords extends Page<UnsRecord> {
  latestSamples?: { recordId: string; sample: UnsSample | null }[];
}
export interface SampleTrendPoint {
  time: number;
  bucket: number;
  value: number | null;
  min: number | null;
  max: number | null;
  count: number;
  validCount: number;
}
export interface SampleTrend {
  recordId: string;
  bucketMs: number;
  aggregation: 'mean-min-max';
  timeRange: { from: number; to: number };
  points: SampleTrendPoint[];
}

const api = new ApiWrapper('/api/core/v2/uns');
export async function loadTopologyApp(id: string, signal: AbortSignal): Promise<TopologyMeta> {
  return project((await api.post('/apps/get', { id }, { signal, _noMessage: true })) as TopologyMeta);
}
export const topologyKinds: TopologyKind[] = ['directory', 'topic', 'collection', 'source', 'stream', 'app'];

// 只保留画布需要的字段，连接配置、凭据、成员信息不进入组件状态或详情面板。
function project(row: TopologyMeta): TopologyMeta {
  const { data, ...identity } = row;
  return {
    ...identity,
    data: {
      name: data.name,
      entry: data.entry,
      parentId: data.parentId,
      collectionId: data.collectionId,
      viewMode: data.viewMode,
      fields: data.fields,
      recordSchema: data.recordSchema,
      sampleSchema: data.sampleSchema,
      adapter: data.adapter,
      activeVersion: data.activeVersion,
      collections: data.collections,
      navigation: data.navigation,
      config: data.config?.streams
        ? {
            streams: Object.fromEntries(
              Object.entries(data.config.streams)
                .filter(([, binding]) => !binding.deletedAt)
                .map(([id, binding]) => [
                  id,
                  {
                    name: binding.name,
                    streamId: binding.streamId,
                    streamRelease: binding.streamRelease,
                    enabled: binding.enabled,
                    // 兼容当前采集配置的显式 collectionId；不解析脚本或按名称推断目标。
                    target: binding.target
                      ? { collectionId: binding.target.collectionId }
                      : binding.collectionId
                        ? { collectionId: binding.collectionId }
                        : undefined,
                  },
                ])
            ),
          }
        : undefined,
    },
  };
}

async function publishedPrograms(signal: AbortSignal) {
  // 复用现有采集 API，延迟加载以避免画布与认证初始化形成模块循环。
  const { sourceStreamApi } = await import('./source-stream');
  const programs = new Map<string, NonNullable<TopologyMeta['published']>>();
  let count = 0;
  for (let page = 1; ; page += 1) {
    const result = await sourceStreamApi.streams({ model: 'program', page, size: 100 }, { signal, _noMessage: true });
    for (const stream of result.list) {
      // manifest 由后端从 activeVersion 读取；草稿和 pendingManifest 不参与已发布拓扑。
      if (stream.activeVersion > 0 && stream.manifest) {
        programs.set(String(stream.id), {
          version: stream.activeVersion,
          manifest: {
            sources: stream.manifest.sources.map(({ id }) => ({ id })),
            topics: stream.manifest.topics.map((reference) =>
              reference.collectionId
                ? { collectionId: reference.collectionId, access: reference.access }
                : { topicId: reference.topicId!, access: reference.access }
            ),
          },
        });
      }
    }
    count += result.list.length;
    if (count >= result.total) return programs;
    if (!result.list.length) throw new Error('Incomplete program page');
  }
}

export async function loadTopology(signal: AbortSignal) {
  // 分 kind 查询，某种对象无权限或加载失败时，不抹掉其他已成功加载的对象。
  const [metadata, [programs]] = await Promise.all([
    Promise.allSettled(
      topologyKinds.map(async (kind) => {
        const rows: TopologyMeta[] = [];
        for (let page = 1; ; page += 1) {
          const result = await metadataPage<TopologyMeta>([kind], { page, size: 200 }, signal);
          rows.push(...result.list.map(project));
          if (rows.length >= result.total) break;
          if (!result.list.length) throw new Error('Incomplete metadata page');
        }
        return rows;
      })
    ),
    Promise.allSettled([publishedPrograms(signal)]),
  ]);
  const unavailable = topologyKinds.filter((_, index) => metadata[index].status === 'rejected');
  if (programs.status === 'rejected' && !unavailable.includes('stream')) unavailable.push('stream');
  return {
    items: metadata
      .flatMap((result) => (result.status === 'fulfilled' ? result.value : []))
      .map((row) => {
        const published =
          row.kind === 'stream' && programs.status === 'fulfilled' ? programs.value.get(row.id) : undefined;
        // 并发刷新时若已发布版本变了，等待下一次刷新，不把两个版本拼成一条关系。
        return published && published.version === row.data.activeVersion ? { ...row, published } : row;
      }),
    unavailable,
  };
}

/** 业务行只在选中集合后分页查询，最新采样只扩展当前页的 Record。 */
export async function topologyRecords(collection: TopologyMeta, page: number, search: string, signal: AbortSignal) {
  const records = await recordPage(collection.id, { page, size: 20, search }, signal);
  const latest = collection.data.sampleSchema
    ? await latestSamples(
        collection.id,
        records.list.map((record) => record.id),
        signal
      )
    : [];
  return { ...records, latestSamples: latest } satisfies TopologyRecords;
}

export async function topologyLatestSample(collectionId: string, recordId: string, signal: AbortSignal) {
  return (await latestSamples(collectionId, [recordId], signal))[0]?.sample ?? null;
}

export async function topologyTrend(
  collectionId: string,
  recordId: string,
  fieldPath: string[],
  range: { from: number; to: number },
  maxPoints: number,
  signal: AbortSignal
) {
  const limit = maxPoints || 400;
  if (limit < 32 || limit > 1000 || !Number.isInteger(limit)) throw new Error('maxPoints must be 32..1000');
  if (
    !Number.isSafeInteger(range.from) ||
    !Number.isSafeInteger(range.to) ||
    range.from < 0 ||
    range.to <= range.from ||
    range.to - range.from > 31 * 24 * 60 * 60 * 1000
  )
    throw new Error('trend requires [from,to) within 31 days');
  if (fieldPath.length < 1 || fieldPath.length > 8 || fieldPath.some((part) => !part))
    throw new Error('trend requires 1..8 declared field segments');
  const bucketMs = Math.ceil((range.to - range.from) / limit);
  const field = `$${fieldPath.map((part) => part.replaceAll('\\', '\\\\').replaceAll('.', '\\.')).join('.')}`;
  const valid = {
    $and: [{ $eq: [{ $toLower: '$_meta.quality' }, 'good'] }, { $ifNull: [{ $eq: [field, field] }, false] }],
  };
  // 保留原趋势的等宽毫秒桶、空桶不补零及 bad-quality 不参与统计语义。
  const pipeline = [
    {
      $group: {
        _id: { $toLong: { $divide: [{ $subtract: ['$_meta.time', range.from] }, bucketMs] } },
        time: { $min: '$_meta.time' },
        value: { $avg: { $cond: [valid, field, null] } },
        min: { $min: { $cond: [valid, field, null] } },
        max: { $max: { $cond: [valid, field, null] } },
        count: { $count: {} },
        validCount: { $sum: { $cond: [valid, 1, 0] } },
      },
    },
    { $sort: [['_id', 1]] },
  ];
  const results = await Promise.all(
    Array.from({ length: Math.ceil(limit / 200) }, (_, index) =>
      unsV2Api.dataAction<{ items: (Omit<SampleTrendPoint, 'bucket'> & { _id: number })[] }>(
        collectionId,
        'samples',
        'aggregate',
        {
          recordIds: [recordId],
          from: range.from,
          to: range.to,
          pipeline,
          page: { mode: 'offset', page: index + 1, size: 200 },
        },
        signal
      )
    )
  );
  return {
    recordId,
    bucketMs,
    aggregation: 'mean-min-max',
    timeRange: range,
    points: results.flatMap((result) => result.items.map(({ _id, ...point }) => ({ ...point, bucket: _id }))),
  } satisfies SampleTrend;
}

export async function topologyHistory(
  collectionId: string,
  recordId: string,
  range: { from: number; to: number },
  page: number,
  signal: AbortSignal
) {
  return sampleHistory(collectionId, recordId, { startTime: range.from, endTime: range.to, page, size: 20 }, signal);
}
