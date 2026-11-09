import type { TopologyKind, TopologyMeta } from '@/apis/core-api/uns-topology';

export type Group = 'namespace' | 'collection' | 'source' | 'stream' | 'app';
export type RelationKind =
  'reference' | 'fields' | 'write' | 'read' | 'input' | 'reuse' | 'grant' | 'navigation' | 'viaCollection';
export const relationKinds: RelationKind[] = [
  'reference',
  'fields',
  'write',
  'read',
  'input',
  'reuse',
  'grant',
  'navigation',
  'viaCollection',
];
export const summaryOffset = (kind: RelationKind) =>
  (relationKinds.indexOf(kind) - (relationKinds.length - 1) / 2) * 16;
export const NODE_HEADER_HEIGHT = 44;
export const NODE_ROW_HEIGHT = 36;
export const NODE_CHROME_HEIGHT = NODE_HEADER_HEIGHT + 2;
export interface VisibleRow {
  key: string;
  y: number;
}
export interface GraphRow {
  key: string;
  id: string;
  name: string;
  kind: TopologyKind | 'binding';
  group: Group;
  parentKey?: string;
  meta?: TopologyMeta;
  release?: number;
  enabled?: boolean;
}
export interface Relation {
  id: string;
  from: string;
  to: string;
  kind: RelationKind;
  detail?: string;
  via?: string[];
}
export const groups: Group[] = ['namespace', 'collection', 'source', 'stream', 'app'];
export const keyOf = (kind: string, id: string | number) => `${kind}:${id}`;

export function buildGraph(items: TopologyMeta[]) {
  const rows: GraphRow[] = items.map((meta) => ({
    key: keyOf(meta.kind, meta.id),
    id: meta.id,
    name: meta.data.name || meta.id,
    kind: meta.kind,
    group: meta.kind === 'directory' || meta.kind === 'topic' ? 'namespace' : meta.kind,
    parentKey: meta.data.parentId ? keyOf('directory', meta.data.parentId) : undefined,
    meta,
  }));
  const relations: Relation[] = [];
  const add = (from: string, kind: RelationKind, toKind: string, id?: string | number, detail?: string) => {
    if (id !== undefined && id !== '')
      relations.push({ id: `${from}:${kind}:${relations.length}`, from, to: keyOf(toKind, id), kind, detail });
  };
  for (const meta of items) {
    const key = keyOf(meta.kind, meta.id);
    if (meta.kind === 'topic') {
      if (meta.data.viewMode === 'fields') {
        for (const field of meta.data.fields || [])
          add(
            key,
            'fields',
            'collection',
            field.collectionId,
            `${field.name} → ${field.recordId} / ${field.fieldPath}`
          );
      } else add(key, 'reference', 'collection', meta.data.collectionId);
    }
    if (meta.kind === 'source') {
      for (const [id, binding] of Object.entries(meta.data.config?.streams || {})) {
        const bindingKey = keyOf('binding', `${meta.id}/${id}`);
        rows.push({
          key: bindingKey,
          id,
          name: binding.name || id,
          kind: 'binding',
          group: 'source',
          parentKey: key,
          release: binding.streamRelease,
          enabled: binding.enabled,
        });
        add(
          bindingKey,
          'reuse',
          'stream',
          binding.streamId,
          binding.streamRelease === undefined ? undefined : `r${binding.streamRelease}`
        );
        add(bindingKey, 'write', 'collection', binding.target?.collectionId);
      }
    }
    if (meta.kind === 'app') {
      for (const grant of meta.data.collections || [])
        add(
          key,
          'grant',
          'collection',
          grant.collectionId,
          `${grant.alias || ''} · ${(grant.permissions || []).join(', ')}`
        );
      for (const id of meta.data.navigation?.topicIds || []) add(key, 'navigation', 'topic', id);
    }
    if (meta.kind === 'stream' && meta.published) {
      const { version, manifest } = meta.published;
      for (const source of manifest.sources) add(keyOf('source', source.id), 'input', 'stream', meta.id, `v${version}`);
      for (const reference of manifest.topics) {
        const targetKind = reference.collectionId ? 'collection' : 'topic';
        const target = reference.collectionId || reference.topicId;
        if (!reference.access || reference.access === 'read' || reference.access === 'read-write')
          add(key, 'read', targetKind, target, `v${version}`);
        if (!reference.access || reference.access === 'write' || reference.access === 'read-write')
          add(key, 'write', targetKind, target, `v${version}`);
      }
    }
  }
  return { rows, relations };
}

// 仅投影已知的显式路径，不能把间接关联变成 App 的 Stream 授权。
// 集合可见时保留原始两段关系；隐藏集合时才显示可追溯的虚线。
export function displayRelations(rows: GraphRow[], relations: Relation[], shown: Group[]): Relation[] {
  if (shown.includes('collection') || !shown.includes('app') || !shown.includes('stream')) return relations;
  const byKey = new Map(rows.map((row) => [row.key, row]));
  const topicCollections = new Map<string, Set<string>>();
  const streamCollections = new Map<string, Set<string>>();
  const add = (map: Map<string, Set<string>>, key: string, collection: string) => {
    const values = map.get(key) || new Set<string>();
    values.add(collection);
    map.set(key, values);
  };
  for (const relation of relations)
    if (
      ['reference', 'fields'].includes(relation.kind) &&
      byKey.get(relation.from)?.kind === 'topic' &&
      byKey.get(relation.to)?.kind === 'collection'
    )
      add(topicCollections, relation.from, relation.to);
  for (const relation of relations) {
    if (!['read', 'write'].includes(relation.kind) || byKey.get(relation.from)?.kind !== 'stream') continue;
    if (byKey.get(relation.to)?.kind === 'collection') add(streamCollections, relation.from, relation.to);
    for (const collection of topicCollections.get(relation.to) || []) add(streamCollections, relation.from, collection);
  }
  // 兼容 Source 的显式 Stream 使用实例，仍只沿 reuse + write 引用。
  const bindingStreams = new Map<string, Set<string>>();
  for (const relation of relations)
    if (relation.kind === 'reuse' && byKey.get(relation.to)?.kind === 'stream')
      add(bindingStreams, relation.from, relation.to);
  for (const relation of relations)
    if (relation.kind === 'write' && byKey.get(relation.to)?.kind === 'collection')
      for (const stream of bindingStreams.get(relation.from) || []) add(streamCollections, stream, relation.to);
  const collectionStreams = new Map<string, Set<string>>();
  for (const [stream, collections] of streamCollections)
    for (const collection of collections) add(collectionStreams, collection, stream);
  const projected = new Map<string, Relation & { via: string[] }>();
  for (const relation of relations) {
    if (relation.kind !== 'grant' || byKey.get(relation.from)?.kind !== 'app') continue;
    for (const stream of collectionStreams.get(relation.to) || []) {
      const id = `viaCollection:${relation.from}:${stream}`;
      const path = projected.get(id) || {
        id,
        from: relation.from,
        to: stream,
        kind: 'viaCollection' as const,
        via: [],
      };
      if (!path.via.includes(relation.to)) path.via.push(relation.to);
      projected.set(id, path);
    }
  }
  return [...relations, ...projected.values()];
}

// 有界展开：先找到核心对象对应的集合，再展示这些集合的直接关系。
// 不穿过另一 App 的授权，把该 App 的其他集合错误地扩展成核心 App 的权限。
export function relatedRows(rows: GraphRow[], relations: Relation[], selected?: string) {
  if (!selected) return new Set<string>();
  const byKey = new Map(rows.map((row) => [row.key, row]));
  const seed = byKey.get(selected);
  const active = new Set([selected]);
  if (seed?.kind === 'source' || seed?.kind === 'directory') {
    for (const key of active) for (const row of rows) if (row.parentKey === key) active.add(row.key);
  }
  if (seed?.kind === 'stream')
    for (const relation of relations)
      if (relation.kind === 'reuse' && relation.to === selected) active.add(relation.from);
  if (seed?.kind === 'source')
    for (const relation of relations)
      if (relation.kind === 'input' && relation.from === selected) active.add(relation.to);
  // Program 可以显式读写 Topic；先保留这些 Topic，再沿其引用找到集合。
  for (const relation of relations)
    if (
      active.has(relation.from) &&
      byKey.get(relation.from)?.kind === 'stream' &&
      byKey.get(relation.to)?.kind === 'topic'
    )
      active.add(relation.to);
  const collections = new Set<string>();
  if (seed?.kind === 'collection') collections.add(selected);
  for (const relation of relations)
    if (active.has(relation.from) && byKey.get(relation.to)?.kind === 'collection') collections.add(relation.to);
  for (const key of collections) active.add(key);
  for (const relation of relations)
    if (collections.has(relation.to) && byKey.get(relation.from)?.kind === 'topic') active.add(relation.from);
  for (const relation of relations) {
    if (
      collections.has(relation.to) &&
      (seed?.kind !== 'app' || byKey.get(relation.from)?.kind !== 'app' || relation.from === selected)
    )
      active.add(relation.from);
    if (relation.kind === 'navigation' && relation.from === selected) active.add(relation.to);
  }
  for (const relation of relations)
    if (
      active.has(relation.to) &&
      byKey.get(relation.to)?.kind === 'topic' &&
      byKey.get(relation.from)?.kind === 'stream'
    )
      active.add(relation.from);
  for (const relation of relations) if (relation.kind === 'reuse' && active.has(relation.from)) active.add(relation.to);
  for (const relation of relations) if (relation.kind === 'input' && active.has(relation.to)) active.add(relation.from);
  for (const key of active) {
    const parent = byKey.get(key)?.parentKey;
    if (parent && byKey.has(parent)) active.add(parent);
  }
  return active;
}

export function filterRows(rows: GraphRow[], query: string) {
  const search = query.trim().toLocaleLowerCase();
  if (!search) return rows;
  const byKey = new Map(rows.map((row) => [row.key, row]));
  const visible = new Set(
    rows.filter((row) => `${row.name} ${row.id}`.toLocaleLowerCase().includes(search)).map((row) => row.key)
  );
  const matched = new Set(visible);
  // 匹配目录/来源时展示其子项；匹配叶子时补足祖先，保持层次可理解。
  for (const key of matched) {
    if (byKey.get(key)?.kind !== 'directory' && byKey.get(key)?.kind !== 'source') continue;
    const descendants = new Set([key]);
    for (const parent of descendants)
      for (const row of rows)
        if (row.parentKey === parent) {
          visible.add(row.key);
          descendants.add(row.key);
        }
  }
  for (const key of visible) {
    const parent = byKey.get(key)?.parentKey;
    if (parent && byKey.has(parent)) visible.add(parent);
  }
  return rows.filter((row) => visible.has(row.key));
}

export function treeRows(rows: GraphRow[], collapsed: Set<string>) {
  const byKey = new Map(rows.map((row) => [row.key, row]));
  const children = new Map<string, GraphRow[]>();
  for (const row of rows) {
    const parent = row.parentKey && byKey.has(row.parentKey) ? row.parentKey : '';
    children.set(parent, [...(children.get(parent) || []), row]);
  }
  const result: { row: GraphRow; depth: number; hasChildren: boolean }[] = [];
  const seen = new Set<string>();
  const visit = (row: GraphRow, depth: number) => {
    if (seen.has(row.key)) return;
    seen.add(row.key);
    result.push({ row, depth, hasChildren: !!children.get(row.key)?.length });
    if (!collapsed.has(row.key)) for (const child of children.get(row.key) || []) visit(child, depth + 1);
  };
  for (const row of children.get('') || []) visit(row, 0);
  // 缺失父节点与异常循环不能让对象无声消失；循环中尚未访问的节点作为根展示。
  for (const row of rows) {
    let parent = row.parentKey;
    const ancestors = new Set<string>();
    let hidden = false;
    while (parent && !ancestors.has(parent)) {
      if (collapsed.has(parent)) {
        hidden = true;
        break;
      }
      ancestors.add(parent);
      parent = byKey.get(parent)?.parentKey;
    }
    if (!hidden && !seen.has(row.key)) visit(row, 0);
  }
  return result;
}
