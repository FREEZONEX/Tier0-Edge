import type { ConnectResource } from '@/apis/core-api/connect-workspace';
import { resourceKey, type ConnectTarget } from './connect-model';

export type ConnectNamespaceNode = {
  key: string;
  target: ConnectTarget;
  children: ConnectNamespaceNode[];
};

// 只补所选资源的目录祖先；Collection 与旧 Topic 不按显示名称猜测 Namespace 归属。
export function buildConnectNamespace(targets: ConnectTarget[], resources: ConnectResource[]) {
  const available = new Map(
    resources.map((resource) => [resourceKey('metadata', resource.kind, resource.id), resource])
  );
  const included = new Map(targets.map((target) => [target.key, target]));
  for (const target of targets) {
    if (target.space !== 'metadata') continue;
    let parentId = target.parentId;
    const seen = new Set<string>();
    while (parentId && !seen.has(parentId)) {
      seen.add(parentId);
      const key = resourceKey('metadata', 'directory', parentId);
      const parent = available.get(key);
      if (!parent) break;
      included.set(key, { ...parent, key, space: 'metadata', missing: false });
      parentId = parent.parentId;
    }
  }
  const parentByKey = new Map<string, string>();
  for (const target of included.values()) {
    if (target.space !== 'metadata' || !target.parentId) continue;
    const parent = resourceKey('metadata', 'directory', target.parentId);
    if (included.has(parent)) parentByKey.set(target.key, parent);
  }
  // 不完整分页／异常父引用仍可查看；循环成员成为根，不能让树丢失或无限递归。
  for (const key of included.keys()) {
    const path: string[] = [];
    const seen = new Set<string>();
    let cursor: string | undefined = key;
    while (cursor && !seen.has(cursor)) {
      seen.add(cursor);
      path.push(cursor);
      cursor = parentByKey.get(cursor);
    }
    if (cursor) for (const member of path.slice(path.indexOf(cursor))) parentByKey.delete(member);
  }
  const nodes = new Map<string, ConnectNamespaceNode>();
  for (const target of included.values()) nodes.set(target.key, { key: target.key, target, children: [] });
  const roots: ConnectNamespaceNode[] = [];
  for (const node of nodes.values()) {
    const parent = nodes.get(parentByKey.get(node.key) ?? '');
    if (parent) parent.children.push(node);
    else roots.push(node);
  }
  const compare = (a: ConnectNamespaceNode, b: ConnectNamespaceNode) =>
    Number(b.target.kind === 'directory') - Number(a.target.kind === 'directory') ||
    a.target.name.localeCompare(b.target.name) ||
    a.key.localeCompare(b.key);
  roots.sort(compare);
  for (const node of nodes.values()) node.children.sort(compare);
  return { roots, nodes, parentByKey };
}

export function visibleConnectAncestor(
  key: string,
  parentByKey: ReadonlyMap<string, string>,
  visible: ReadonlySet<string>
) {
  const seen = new Set<string>();
  let cursor: string | undefined = key;
  while (cursor && !seen.has(cursor)) {
    if (visible.has(cursor)) return cursor;
    seen.add(cursor);
    cursor = parentByKey.get(cursor);
  }
  return undefined;
}
