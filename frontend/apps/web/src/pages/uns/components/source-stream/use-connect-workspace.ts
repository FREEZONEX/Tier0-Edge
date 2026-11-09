import { useEffect, useState } from 'react';
import { loadConnectWorkspace, type ConnectWorkspaceData } from '@/apis/core-api/connect-workspace';

const emptyData: ConnectWorkspaceData = { sources: [], streams: [], resources: [], unavailable: [], truncated: [] };

export function useConnectWorkspace(
  sourceAllowed: boolean,
  streamAllowed: boolean,
  metadataAllowed: boolean,
  refresh: number
) {
  const [data, setData] = useState(emptyData);
  const [loading, setLoading] = useState(true);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    loadConnectWorkspace(
      { sources: sourceAllowed, streams: streamAllowed, resources: metadataAllowed },
      controller.signal
    )
      .then((result) => {
        if (!controller.signal.aborted) setData(result);
      })
      .catch(() => {
        if (!controller.signal.aborted) setData({ ...emptyData, unavailable: ['sources', 'streams', 'resources'] });
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [sourceAllowed, streamAllowed, metadataAllowed, refresh]);
  return { data, loading };
}
