import { isProgramDocument, sourceStreamApi, type ProgramManifest, type Source, type Stream } from './source-stream';
import { metadataPage } from './uns-v2';
import type { UnsMetadata } from './uns-metadata';

export type ConnectCategory = 'industrial' | 'api' | 'business';
export type ConnectSource = Pick<Source, 'id' | 'name' | 'protocol' | 'health'> & {
  category: ConnectCategory;
  role: 'client' | 'server';
};
export type ConnectStream = Pick<Stream, 'id' | 'name' | 'status' | 'executionState' | 'activeVersion'> & {
  editable: boolean;
  active?: ProgramManifest;
  draft?: ProgramManifest;
};
export type ConnectResource = {
  id: string;
  kind: 'directory' | 'topic' | 'collection';
  name: string;
  parentId?: string;
};
export type ConnectWorkspaceData = {
  sources: ConnectSource[];
  streams: ConnectStream[];
  resources: ConnectResource[];
  unavailable: ('sources' | 'streams' | 'resources')[];
  truncated: ('sources' | 'streams' | 'resources')[];
};

const PAGE_SIZE = 100;
const MAX_ITEMS = 2000;

// 有界分页，遇到空页/重复页明确失败，不能把未加载对象解释为无依赖。
export async function loadConnectPages<T extends { id: string | number }>(
  fetchPage: (page: number, size: number) => Promise<{ list: T[]; total: number }>,
  signal: AbortSignal,
  keyOf: (row: T) => string = (row) => String(row.id)
) {
  const rows = new Map<string, T>();
  for (let page = 1; ; page++) {
    signal.throwIfAborted();
    const result = await fetchPage(page, PAGE_SIZE);
    signal.throwIfAborted();
    const previous = rows.size;
    for (const row of result.list) rows.set(keyOf(row), row);
    if (rows.size >= result.total) return { list: [...rows.values()], truncated: false };
    if (rows.size === previous) throw new Error('Incomplete Connect workspace page');
    if (rows.size >= MAX_ITEMS) return { list: [...rows.values()], truncated: true };
  }
}

export function projectConnectSource(source: Source): ConnectSource {
  return {
    id: source.id,
    name: source.name,
    protocol: source.protocol,
    health: source.health,
    role: source.document.role || 'client',
    category: source.document.sourceType
      ? 'business'
      : ['http-api', 'oauth2'].includes(source.protocol)
        ? 'api'
        : 'industrial',
  };
}

export function projectConnectStream(stream: Stream): ConnectStream {
  const project = (manifest?: ProgramManifest) =>
    manifest && {
      sources: manifest.sources.map(({ id }) => ({ id, resources: [] })),
      topics: manifest.topics.map((reference) => ({ ...reference })),
      entries: [],
    };
  return {
    id: stream.id,
    name: stream.name,
    status: stream.status,
    executionState: stream.executionState,
    activeVersion: stream.activeVersion,
    editable: isProgramDocument(stream.document),
    active: stream.activeVersion > 0 ? project(stream.manifest) : undefined,
    draft: project(stream.draftManifest),
  };
}

export async function loadConnectWorkspace(
  allowed: { sources: boolean; streams: boolean; resources: boolean },
  signal: AbortSignal
): Promise<ConnectWorkspaceData> {
  const kinds = ['sources', 'streams', 'resources'] as const;
  const results = await Promise.allSettled([
    allowed.sources
      ? loadConnectPages((page, size) => sourceStreamApi.sources({ page, size }, signal), signal).then((result) => ({
          ...result,
          list: result.list.map(projectConnectSource),
        }))
      : Promise.resolve({ list: [], truncated: false }),
    allowed.streams
      ? loadConnectPages((page, size) => sourceStreamApi.streams({ page, size }, { signal }), signal).then(
          (result) => ({
            ...result,
            list: result.list.map(projectConnectStream),
          })
        )
      : Promise.resolve({ list: [], truncated: false }),
    allowed.resources
      ? loadConnectPages(
          async (page, size) => {
            const result = await metadataPage<UnsMetadata>(
              ['directory', 'topic', 'collection'],
              { page, size },
              signal
            );
            return {
              total: result.total,
              list: result.list.map((row) => ({
                id: row.id,
                kind: row.kind as ConnectResource['kind'],
                name: row.data.name || row.id,
                parentId: row.data.parentId,
              })),
            };
          },
          signal,
          (row) => JSON.stringify([row.kind, row.id])
        )
      : Promise.resolve({ list: [], truncated: false }),
  ]);
  signal.throwIfAborted();
  return {
    sources: results[0].status === 'fulfilled' ? (results[0].value.list as ConnectSource[]) : [],
    streams: results[1].status === 'fulfilled' ? (results[1].value.list as ConnectStream[]) : [],
    resources: results[2].status === 'fulfilled' ? (results[2].value.list as ConnectResource[]) : [],
    unavailable: kinds.filter((kind, index) => !allowed[kind] || results[index].status === 'rejected'),
    truncated: kinds.filter((_, index) => results[index].status === 'fulfilled' && results[index].value.truncated),
  };
}
