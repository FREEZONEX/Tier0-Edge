import { CustomAxiosConfigEnum } from '@/utils/request';
import { coreApi } from './core-adapter';
import type { AuditLogItem, AuditLogPageResp } from './audit-log';

export type FleetNodeStatus = 'deleted' | 'offline' | 'online' | 'unjoined';

export interface FleetNode {
  fleetNodeID: string;
  name: string;
  description?: string;
  centerType: 'cloud' | 'enterprise';
  status: FleetNodeStatus;
  tunnel?: {
    status: 'online' | 'offline';
    connectedAt?: number;
    lastSeenAt?: number;
    protocolVersion?: string;
  };
  activeSessionID?: string;
  firstJoinedAt?: number;
  lastOnlineAt?: number;
  lastOfflineAt?: number;
  lastSyncAt?: number;
  containerCount?: number;
  unhealthyContainerCount?: number;
  agentVersion?: string;
  protocolVersion?: string;
  os?: string;
  arch?: string;
  assignmentRevision: number;
  credentialID?: string;
  credentialStatus?: 'active' | 'revoked';
  credentialExpiresAt?: number;
  credentialLastUsedAt?: number;
  createdBy: string;
  updatedBy: string;
  createdAt: number;
  updatedAt: number;
  deletedAt?: number;
}

export interface CreateFleetNodeReq {
  name: string;
  description?: string;
}

export interface CreateFleetNodeResp extends FleetNode {
  accessToken: string;
}

export interface UpdateFleetNodeReq {
  name: string;
  description?: string;
}

export interface FleetNodeListParams {
  keyword?: string;
  status?: FleetNodeStatus;
  os?: string;
  agentVersion?: string;
  page?: number;
  size?: number;
}

export interface FleetNodeListResp {
  list: FleetNode[];
  total: number;
  page: number;
  size: number;
  summary: {
    active: number;
    limit: number;
    online: number;
    offline: number;
    unjoined: number;
    deleted: number;
    onlineWithAbnormalContainers: number;
  };
}

export interface FleetHostSummary {
  hostname: string;
  os: string;
  arch: string;
  logicalCPU: number;
  cpu?: {
    usagePercent: number;
    available: boolean;
    errorCode?: string;
  };
  memory?: {
    totalBytes: number;
    usedBytes: number;
    available: boolean;
    errorCode?: string;
  };
  disks?: Array<{
    name: string;
    mountPoint?: string;
    totalBytes: number;
    usedBytes: number;
    available: boolean;
    errorCode?: string;
  }>;
}

export type FleetAgentAvailability = 'available' | 'error' | 'notConfigured' | 'unavailable';

export interface FleetCurrentAgentStatus {
  availability: FleetAgentAvailability;
  errorCode?: string;
  service?: string;
  version?: string;
  protocolVersion?: string;
  startedAt?: number;
  configured?: boolean;
  connectionState?: 'connected' | 'disconnected' | 'unconfigured';
  fleetNodeID?: string;
  centerURL?: string;
  installationID?: string;
  composeProjectName?: string;
  host?: FleetHostSummary;
  capabilities?: string[];
}

export interface FleetHAServiceGroupStatus {
  name: string;
  expected: string;
  running: number;
  stopped: number;
  unhealthy: number;
  failures?: string[];
  observedAt?: number;
}

export interface FleetHAStatus {
  enabled: boolean;
  memberId: string;
  role: 'active' | 'standby' | 'promoting' | 'demoting' | 'faulted' | 'unknown';
  eligible: boolean;
  eligibilityReason?: string;
  keepalivedState?: string;
  vipOwned: boolean;
  lastTransitionAt?: number;
  lastTransitionId?: string;
  lastErrorCode?: string;
  serviceGroup: FleetHAServiceGroupStatus;
  sync: {
    enabled: boolean;
    peers?: string[];
    state: 'disabled' | 'idle' | 'running' | 'succeeded' | 'error';
    lastStartedAt?: number;
    lastSucceededAt?: number;
    lastError?: string;
  };
}

export interface FleetCurrentHA {
  enabled: boolean;
  status?: FleetHAStatus;
  errorCode?: string;
}

export interface FleetCurrentNode {
  planCode: string;
  role: 'center' | 'edge';
  agent: FleetCurrentAgentStatus;
  ha?: FleetCurrentHA;
}

export interface FleetContainerPort {
  containerPort: number;
  protocol: string;
  hostIP?: string;
  hostPort?: string;
}

export interface FleetContainerSummary {
  containerName?: string;
  supersededBy?: string;
  containerID: string;
  serviceName: string;
  displayName: string;
  image: string;
  state: string;
  healthStatus: string;
  restartCount: number;
  startedAt: number;
  restartable: boolean;
  ports: FleetContainerPort[];
  composeService?: string;
  composeProject?: string;
  labels: Record<string, string>;
}

export interface FleetContainerHealthCheck {
  startedAt: number;
  finishedAt: number;
  exitCode: number;
}

export interface FleetContainerHealth {
  status: string;
  failingStreak: number;
  lastCheck?: FleetContainerHealthCheck;
}

export interface FleetContainerDetail extends FleetContainerSummary {
  imageID: string;
  createdAt: number;
  finishedAt: number;
  health: FleetContainerHealth;
}

export interface FleetContainerCollectionError {
  containerID?: string;
  code: string;
}

export interface FleetContainerSnapshot {
  snapshotID: string;
  observedAt: number;
  installationID: string;
  engine: {
    version: string;
    apiVersion: string;
    os: string;
    arch: string;
  };
  containers: FleetContainerSummary[];
  summary: {
    historical?: number;
    total: number;
    running: number;
    unhealthy: number;
  };
  partial: boolean;
  errors: FleetContainerCollectionError[];
}

export interface FleetRemoteContainerSnapshot extends FleetContainerSnapshot {
  agentVersion?: string;
  protocolVersion?: string;
  stale: boolean;
  host?: FleetHostSummary;
}

export interface FleetRemoteContainerDetail extends FleetContainerDetail {
  stale: boolean;
}

export interface FleetContainerLogsReq {
  tail?: number;
  since?: number;
  timestamps?: boolean;
  limitBytes?: number;
}

export interface FleetContainerLogsResp {
  operationID?: string;
  containerID: string;
  logs: string;
  lineCount: number;
  bytes: number;
  truncated: boolean;
  observedAt: number;
}

export interface RestartFleetContainerReq {
  requestID?: string;
}

export interface RestartFleetContainerResp {
  operationID: string;
  containerID: string;
  status: 'failed' | 'succeeded';
  startedAt: number;
  finishedAt: number;
  container: FleetContainerSummary;
}

export type FleetOperationStatus = 'canceled' | 'failed' | 'queued' | 'running' | 'succeeded' | 'timedOut';

export interface FleetOperation {
  operationID: string;
  fleetNodeID: string;
  containerID: string;
  operationType: 'container.logs' | 'container.restart' | string;
  status: FleetOperationStatus;
  actorID: string;
  request?: Record<string, unknown>;
  result?: Record<string, unknown>;
  errorCode?: string;
  startedAt?: number;
  finishedAt?: number;
  createdAt: number;
  updatedAt: number;
}

export interface FleetOperationListResp {
  list: FleetOperation[];
  total: number;
  page: number;
  size: number;
}

export interface FleetAuditLogListResp {
  list: AuditLogItem[];
  total: number;
  page: number;
  size: number;
}

export interface FleetAssignedAccount {
  centerUserID: string;
  userName: string;
  nickName?: string;
  email?: string;
  phone?: string;
  userStatus: number;
  roleCode?: string;
  assignmentRevision: number;
  assignedBy?: string;
  assignedAt?: number;
}

export interface FleetNodeAccountAssignments {
  nodeID: string;
  fleetNodeID: string;
  revision: number;
  changed?: boolean;
  centerUserIDs: string[];
  accounts: FleetAssignedAccount[];
}

export interface ReplaceFleetNodeAccountsReq {
  centerUserIDs: string[];
  expectedRevision: number;
}

const fleetNodePath = (nodeID: string) => `/fleet/nodes/${encodeURIComponent(nodeID)}`;
const currentContainerPath = (containerID: string) => `/fleet/current/containers/${encodeURIComponent(containerID)}`;
const fleetNodeContainerPath = (nodeID: string, containerID: string) =>
  `${fleetNodePath(nodeID)}/containers/${encodeURIComponent(containerID)}`;

export const createFleetNode = async (data: CreateFleetNodeReq): Promise<CreateFleetNodeResp> =>
  coreApi.post('/fleet/nodes', data, { [CustomAxiosConfigEnum.NoMessage]: true });

export const getFleetNodes = async (params?: FleetNodeListParams): Promise<FleetNodeListResp> =>
  coreApi.get('/fleet/nodes', { params });

export const getFleetNode = async (nodeID: string): Promise<FleetNode> => coreApi.get(fleetNodePath(nodeID));

export const updateFleetNode = async (nodeID: string, data: UpdateFleetNodeReq): Promise<FleetNode> =>
  coreApi.put(fleetNodePath(nodeID), data, { [CustomAxiosConfigEnum.NoMessage]: true });

export const deleteFleetNode = async (nodeID: string): Promise<{ fleetNodeID: string; deleted: boolean }> =>
  coreApi.delete(fleetNodePath(nodeID));

export const restoreFleetNode = async (nodeID: string): Promise<CreateFleetNodeResp> =>
  coreApi.post(`${fleetNodePath(nodeID)}/restore`, {});

export const getFleetNodeContainers = async (nodeID: string, refresh = false): Promise<FleetRemoteContainerSnapshot> =>
  coreApi.get(`${fleetNodePath(nodeID)}/containers`, {
    params: refresh ? { refresh: true } : undefined,
    [CustomAxiosConfigEnum.NoMessage]: true,
  });

export const getFleetNodeContainer = async (nodeID: string, containerID: string): Promise<FleetRemoteContainerDetail> =>
  coreApi.get(fleetNodeContainerPath(nodeID, containerID));

export const getFleetNodeContainerLogs = async (
  nodeID: string,
  containerID: string,
  data: FleetContainerLogsReq
): Promise<FleetContainerLogsResp> => coreApi.post(`${fleetNodeContainerPath(nodeID, containerID)}/logs`, data);

export const restartFleetNodeContainer = async (
  nodeID: string,
  containerID: string,
  data: RestartFleetContainerReq = {}
): Promise<FleetOperation> => coreApi.post(`${fleetNodeContainerPath(nodeID, containerID)}/restart`, data);

export const getFleetAuditLogs = async (
  nodeID: string,
  params?: { page?: number; size?: number }
): Promise<FleetAuditLogListResp> => {
  const result: AuditLogPageResp = await coreApi.post('/v2/fleet/nodes/audit/list', { nodeId: nodeID, ...params });
  return { list: result.data, total: result.total, page: result.pageNo, size: result.pageSize };
};

export const getFleetAuditDetail = (nodeId: string, auditId: number): Promise<AuditLogItem> =>
  coreApi.post('/v2/fleet/nodes/audit/get', { nodeId, auditId });

export const getFleetOperation = async (nodeID: string, operationID: string): Promise<FleetOperation> =>
  coreApi.get(`${fleetNodePath(nodeID)}/operations/${encodeURIComponent(operationID)}`);

export const getCurrentFleetNode = async (): Promise<FleetCurrentNode> => coreApi.get('/fleet/current');

export const configureCurrentFleetConnection = async (data: {
  accessToken: string;
}): Promise<FleetCurrentAgentStatus> => coreApi.put('/fleet/current/connection', data, { _noMessage: true });

export const getCurrentFleetContainers = async (): Promise<FleetContainerSnapshot> =>
  coreApi.get('/fleet/current/containers');

export const getCurrentFleetContainer = async (containerID: string): Promise<FleetContainerDetail> =>
  coreApi.get(currentContainerPath(containerID));

export const getCurrentFleetContainerLogs = async (
  containerID: string,
  data: FleetContainerLogsReq
): Promise<FleetContainerLogsResp> => coreApi.post(`${currentContainerPath(containerID)}/logs`, data);

export const restartCurrentFleetContainer = async (
  containerID: string,
  data: RestartFleetContainerReq = {}
): Promise<RestartFleetContainerResp> => coreApi.post(`${currentContainerPath(containerID)}/restart`, data);

export const getFleetNodeAccounts = async (nodeID: string): Promise<FleetNodeAccountAssignments> =>
  coreApi.get(`${fleetNodePath(nodeID)}/accounts`);

export const replaceFleetNodeAccounts = async (
  nodeID: string,
  data: ReplaceFleetNodeAccountsReq
): Promise<FleetNodeAccountAssignments> => coreApi.put(`${fleetNodePath(nodeID)}/accounts`, data);
