import type { GraphRow, Relation, RelationKind } from './model';

export interface NamespaceLink {
  row: GraphRow;
  paths: { kind: RelationKind; keys: string[] }[];
}

/** 从目录中的 Collection 和独立引用出发，不沿其他 App 扩散到无关集合。 */
export function namespaceRelations(row: GraphRow, rows: GraphRow[], relations: Relation[]) {
  const byKey = new Map(rows.map((item) => [item.key, item]));
  const scope = new Set([row.key]);
  if (row.kind === 'directory') {
    for (const parent of scope)
      for (const item of rows)
        if (['directory', 'topic', 'collection'].includes(item.kind) && item.parentKey === parent) scope.add(item.key);
  }
  const topics = new Set([...scope].filter((key) => byKey.get(key)?.kind === 'topic'));
  const result = new Map<string, NamespaceLink>();
  const add = (key: string, kind: RelationKind, keys: string[]) => {
    const target = byKey.get(key);
    if (!target) return;
    let link = result.get(key);
    if (!link) result.set(key, (link = { row: target, paths: [] }));
    if (!link.paths.some((path) => path.kind === kind && path.keys.join('\0') === keys.join('\0')))
      link.paths.push({ kind, keys });
  };
  const collectionPaths = new Map<string, { from: string }[]>();
  for (const key of scope) {
    if (byKey.get(key)?.kind !== 'collection') continue;
    collectionPaths.set(key, [{ from: key }]);
    if (key !== row.key) add(key, 'viaCollection', [row.key]);
  }
  for (const relation of relations) {
    if (!topics.has(relation.from) || !['reference', 'fields'].includes(relation.kind)) continue;
    if (byKey.get(relation.to)?.kind !== 'collection') continue;
    collectionPaths.set(relation.to, [...(collectionPaths.get(relation.to) || []), relation]);
    add(relation.to, relation.kind, [relation.from]);
  }
  const bindingPaths = new Map<string, { collection: string; topic: string }[]>();
  const streamPaths = new Map<string, string[]>();
  for (const relation of relations) {
    if (relation.kind === 'navigation' && topics.has(relation.to)) add(relation.from, relation.kind, [relation.to]);
    const paths = collectionPaths.get(relation.to);
    if (!paths) continue;
    if (relation.kind === 'grant') {
      for (const path of paths) add(relation.from, relation.kind, [relation.to, path.from]);
    }
    if (['read', 'write'].includes(relation.kind) && byKey.get(relation.from)?.kind === 'stream') {
      for (const path of paths) {
        const keys = [...new Set([relation.to, path.from])];
        add(relation.from, relation.kind, keys);
        streamPaths.set(relation.from, [...new Set([...(streamPaths.get(relation.from) || []), ...keys])]);
      }
    }
    if (relation.kind === 'write') {
      const binding = byKey.get(relation.from);
      if (binding?.kind !== 'binding' || !binding.parentKey) continue;
      for (const path of paths) {
        add(binding.parentKey, relation.kind, [binding.key, relation.to, path.from]);
        bindingPaths.set(binding.key, [
          ...(bindingPaths.get(binding.key) || []),
          { collection: relation.to, topic: path.from },
        ]);
      }
    }
  }
  for (const relation of relations) {
    if (relation.kind === 'input' && streamPaths.has(relation.to))
      add(relation.from, relation.kind, [relation.to, ...streamPaths.get(relation.to)!]);
    if (relation.kind !== 'reuse') continue;
    const binding = byKey.get(relation.from);
    for (const path of bindingPaths.get(relation.from) || [])
      add(
        relation.to,
        relation.kind,
        [binding?.parentKey, relation.from, path.collection, path.topic].filter((key): key is string => !!key)
      );
  }
  return result;
}
