import type { UnsMetadata } from '@/apis/core-api/uns-metadata';
import type { UnsTreeNode } from '../../types';

export function namespaceTree(items: UnsMetadata[], search = ''): UnsTreeNode[] {
  // 与 Fleet 一致：集合和引用是不同资源，按各自 ID 和 parentId 展示。
  const namespace = items;
  const visible = new Set(
    namespace
      .filter((item) => !search || item.data.name.toLowerCase().includes(search.toLowerCase()))
      .map((item) => item.id)
  );
  namespace.forEach((item) => {
    if (!visible.has(item.id)) return;
    let parent = item.data.parentId;
    const seen = new Set<string>();
    while (parent && !seen.has(parent)) {
      seen.add(parent);
      visible.add(parent);
      parent = items.find((entry) => entry.id === parent)?.data.parentId;
    }
  });
  const build = (parentId?: string): UnsTreeNode[] =>
    namespace
      .filter((item) => (item.data.parentId || '') === (parentId || '') && visible.has(item.id))
      .map((item) => ({
        key: item.id,
        title: item.data.name,
        pathType: item.kind === 'folder' ? 0 : 2,
        isLeaf: item.kind !== 'folder',
        children: item.kind === 'folder' ? build(item.id) : undefined,
      }));
  return build();
}
