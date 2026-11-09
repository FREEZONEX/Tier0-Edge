import { useEffect, useState } from 'react';
import { loadMetadata, type UnsMetadata } from '@/apis/core-api/uns-metadata';

export default function useWorkflowMetadata(enabled: boolean) {
  const [items, setItems] = useState<UnsMetadata[]>([]);
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    if (!enabled) return;
    let alive = true;
    setLoading(true);
    setFailed(false);
    loadMetadata()
      .then((items) => {
        if (alive) setItems(items);
      })
      .catch(() => {
        if (alive) setFailed(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [enabled, revision]);
  return {
    items,
    loading,
    failed,
    refresh: () => setRevision((v) => v + 1),
    saved: (meta: UnsMetadata) => setItems((items) => [...items.filter((item) => item.id !== meta.id), meta]),
  };
}

export function outputCollection(target: string, items: UnsMetadata[]) {
  const [kind, id] = target.split(':');
  const item = items.find((item) => item.kind === kind && item.id === id && !item.deletedAt);
  return kind === 'collection'
    ? item
    : items.find(
        (collection) =>
          collection.kind === 'collection' && collection.id === item?.data.collectionId && !collection.deletedAt
      );
}
