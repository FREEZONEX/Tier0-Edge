import type { MetaKind, UnsMetadata, MetadataPurgeResult } from '@/apis/core-api/uns-metadata';
import type { BatchJob } from '@/apis/core-api/uns-v2';

export function trashRoots(items: UnsMetadata[]) {
  return items.filter((item) => !item.data.directoryTrashId || item.data.directoryTrashId === item.id);
}

export function trashBatches(items: UnsMetadata[], inventory = items, purge = false) {
  const batches: { kind: MetaKind; items: UnsMetadata[] }[] = [];
  const roots = trashRoots(items);
  const selectedFolders = new Set(roots.filter((item) => item.kind === 'folder').map((item) => item.id));
  const ancestors = (item: UnsMetadata) => {
    const ids = new Set<string>();
    let parent = item.data.parentId;
    while (parent && !ids.has(parent)) {
      ids.add(parent);
      parent = inventory.find((entry) => entry.id === parent)?.data.parentId;
    }
    return [...ids];
  };
  for (const kind of ['folder', 'collection', 'topic'] as const) {
    const group = roots
      .filter((item) => item.kind === kind && !item.mountId && !item.data.purgeJobId)
      .filter((item) => !purge || !ancestors(item).some((id) => selectedFolders.has(id)))
      .sort((a, b) => ancestors(a).length - ancestors(b).length);
    for (let offset = 0; offset < group.length; offset += 200) {
      batches.push({ kind, items: group.slice(offset, offset + 200) });
    }
  }
  return batches;
}

export function purgeJobState(job: BatchJob<MetadataPurgeResult>) {
  if (job.status === 'queued' || job.status === 'running') return 'pending';
  return job.status === 'succeeded' && job.result?.status === 'committed' ? 'succeeded' : 'failed';
}
