import { coreApi } from './core-adapter';
export type CloudSyncLogItem = {
  id: number;
  syncType?: string;
  direction?: string;
  status?: string;
  syncMode?: string;
  connectClientKey?: string;
  nodeID?: string;
  nodeName?: string;
  snapshotVersion?: number;
  snapshotHash?: string;
  resultCode?: string;
  summary?: string;
  errorMessage?: string;
  environment?: string;
  details?: string;
  createdTime?: number;
  updatedTime?: number;
};

export type ClusterSyncLogListParams = {
  page?: number;
  size?: number;
  syncType?: string;
  direction?: string;
  status?: string;
  connectClientKey?: string;
  nodeID?: string;
};

export const getClusterSyncLogs = async (
  params: ClusterSyncLogListParams
): Promise<{ list: CloudSyncLogItem[]; total: number; page?: number; size?: number }> =>
  coreApi.get('/cluster/sync-logs', { params });
