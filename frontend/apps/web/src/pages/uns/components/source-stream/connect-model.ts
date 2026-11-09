import type {
  ConnectCategory,
  ConnectResource,
  ConnectSource,
  ConnectStream,
  ConnectWorkspaceData,
} from '@/apis/core-api/connect-workspace';

export type ConnectBasis = 'active' | 'draft';
export type ConnectTarget = ConnectResource & { key: string; space: 'metadata' | 'legacy'; missing: boolean };
export type ConnectRelation = { id: string; from: string; to: string; kind: 'dependency' | 'read' | 'write' };
export const connectCategories: ConnectCategory[] = ['industrial', 'api', 'business'];
export const sourceKey = (id: string | number) => `source:${id}`;
export const streamKey = (id: string | number) => `stream:${id}`;
export const resourceKey = (space: 'metadata' | 'legacy', kind: ConnectResource['kind'], id: string) =>
  `${space}:${kind}:${id}`;

export function buildConnectModel(data: ConnectWorkspaceData, basis: ConnectBasis, search = '') {
  const targets = new Map<string, ConnectTarget>();
  const sources = new Map(data.sources.map((source) => [sourceKey(source.id), source]));
  const streams = new Map(data.streams.map((stream) => [streamKey(stream.id), stream]));
  const missingSources = new Set<string>();
  const relations = new Map<string, ConnectRelation>();
  for (const resource of data.resources) {
    const key = resourceKey('metadata', resource.kind, resource.id);
    targets.set(key, { ...resource, key, space: 'metadata', missing: false });
  }
  const add = (from: string, to: string, kind: ConnectRelation['kind']) => {
    const id = JSON.stringify([from, to, kind]);
    relations.set(id, { id, from, to, kind });
  };
  for (const stream of data.streams) {
    const key = streamKey(stream.id);
    const manifest = stream[basis];
    if (!manifest) continue;
    for (const reference of manifest.sources) {
      const from = sourceKey(reference.id);
      if (!sources.has(from)) missingSources.add(reference.id);
      add(from, key, 'dependency');
    }
    for (const reference of manifest.topics) {
      const id = reference.collectionId || reference.topicId;
      if (!id) continue;
      const kind = reference.collectionId ? 'collection' : 'topic';
      // 复用 SDK 已校验的缺省空间约定：纯数字为旧 Topic，其余为 Meta；显式空间优先。
      const space = reference.space || (reference.collectionId || !/^\d+$/.test(id) ? 'metadata' : 'legacy');
      const targetKey = resourceKey(space, kind, id);
      if (!targets.has(targetKey))
        targets.set(targetKey, {
          key: targetKey,
          id,
          kind,
          space,
          name: id,
          missing: true,
        });
      const access = reference.access || 'read-write';
      if (access === 'read' || access === 'read-write') add(targetKey, key, 'read');
      if (access === 'write' || access === 'read-write') add(key, targetKey, 'write');
    }
  }
  // 搜索以匹配对象为种子，仅展开它直接所属的 Stream 及其依赖和输出，不做无限遍历。
  const query = search.trim().toLocaleLowerCase();
  const matches = (value: { id: string | number; name: string }) =>
    !query || `${value.id} ${value.name}`.toLocaleLowerCase().includes(query);
  const selected = new Set([
    ...data.sources.filter(matches).map((source) => sourceKey(source.id)),
    ...data.streams.filter(matches).map((stream) => streamKey(stream.id)),
    ...[...targets.values()].filter(matches).map((target) => target.key),
    ...[...missingSources].filter((id) => !query || id.toLowerCase().includes(query)).map(sourceKey),
  ]);
  const selectedStreams = new Set([...selected].filter((key) => streams.has(key)));
  for (const relation of relations.values()) {
    if (selected.has(relation.from) && streams.has(relation.to)) selectedStreams.add(relation.to);
    if (selected.has(relation.to) && streams.has(relation.from)) selectedStreams.add(relation.from);
  }
  for (const key of selectedStreams) selected.add(key);
  for (const relation of relations.values()) {
    if (selectedStreams.has(relation.from)) selected.add(relation.to);
    if (selectedStreams.has(relation.to)) selected.add(relation.from);
  }
  return {
    sources: data.sources.filter((source) => selected.has(sourceKey(source.id))),
    streams: data.streams.filter((stream) => selected.has(streamKey(stream.id))),
    targets: [...targets.values()].filter((target) => selected.has(target.key)),
    missingSources: [...missingSources].filter((id) => selected.has(sourceKey(id))),
    relations: [...relations.values()].filter((relation) => selected.has(relation.from) && selected.has(relation.to)),
  };
}

export type ConnectSourceCard = Pick<ConnectSource, 'id' | 'name' | 'protocol' | 'role' | 'health'>;
export type ConnectStreamCard = ConnectStream;
