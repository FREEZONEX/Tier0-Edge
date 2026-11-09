import { useEffect, useState } from 'react';
import { sourceStreamApi, type Source, type Stream } from '@/apis/core-api/source-stream';
import { ButtonPermission } from '@/common-types/button-permission';
import { hasPermission } from '@/utils/auth';
export type ConnectionStream = Stream;
async function allPages<T>(fetch: (page: number) => Promise<{ list: T[]; total: number }>) {
  const items: T[] = [];
  for (let page = 1; ; page++) {
    const result = await fetch(page);
    items.push(...result.list);
    if (!result.list.length || items.length >= result.total) return items;
  }
}

export default function useConnections(refresh: number, enabled = true) {
  const [sources, setSources] = useState<Source[]>([]);
  const [streams, setStreams] = useState<ConnectionStream[]>([]);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const readable = hasPermission(ButtonPermission['uns.sources']) && hasPermission(ButtonPermission['uns.streams']);
  useEffect(() => {
    let alive = true;
    if (!readable || !enabled) {
      setLoading(false);
      return;
    }
    setLoading(true);
    Promise.all([
      allPages((page) => sourceStreamApi.sources({ page, size: 100 })),
      allPages((page) => sourceStreamApi.streams({ page, size: 100 })),
    ])
      .then(([nextSources, nextStreams]) => {
        if (alive) {
          setSources(nextSources);
          setStreams(nextStreams);
          setFailed(false);
        }
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
  }, [refresh, readable, enabled]);
  return { sources, streams, loading, failed, readable };
}
