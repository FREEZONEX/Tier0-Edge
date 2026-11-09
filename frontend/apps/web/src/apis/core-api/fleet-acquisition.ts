import { coreApi } from './core-adapter';

export interface CollaborationNode {
  capabilities?: {
    access?: { allowSSH: boolean; allowMetadataWrite: boolean; allowControl: boolean; revision: number };
  };
  nodeId: string;
  name: string;
  description: string;
  registrationStatus: 'unregistered' | 'registered' | 'revoked';
  networkStatus: string;
  businessStatus: string;
  checkedAt: number;
  lastConnectedAt?: number;
  infrastructureStatus?: string;
  containerCount?: number;
  unhealthyContainerCount?: number;
}
export interface CollaborationConnection {
  centerUrl: string;
  nodeId: string;
  enrollmentCode: string;
  networkNodeId?: string;
}
export const listCollaborationNodes = (options?: {
  silent?: boolean;
}): Promise<{ items: CollaborationNode[]; total: number }> =>
  coreApi.post('/v2/fleet/nodes/list', { page: 1, size: 100 }, { _noMessage: options?.silent });
export const getCollaborationNode = (nodeId: string): Promise<{ node: CollaborationNode }> =>
  coreApi.post('/v2/fleet/nodes/get', { nodeId }, { _noMessage: true });
export const createCollaborationNode = (name: string): Promise<{ node: CollaborationNode }> =>
  coreApi.post('/v2/fleet/nodes/create', { name });
export const createCollaborationEnrollment = (
  nodeId: string
): Promise<{ nodeId: string; enrollmentCode: string; expiresAt: number }> =>
  coreApi.post('/v2/fleet/nodes/enrollment/create', { nodeId });
export const configureCollaboration = (data: CollaborationConnection): Promise<{ nodeId: string }> =>
  coreApi.post('/v2/fleet/connection/configure', data);

export type FleetAction =
  | 'current/get'
  | 'controls/catalog'
  | 'control-grants/catalog'
  | 'control-grants/get'
  | 'control-grants/save'
  | 'control-intents/create'
  | 'control-intents/confirm'
  | 'control-intents/list'
  | 'control-intents/get'
  | 'control-intents/query'
  | 'nodes/token/reissue'
  | 'nodes/health/check';
export function fleetAction<T = unknown>(
  action: FleetAction,
  payload: Record<string, unknown>,
  options?: { silent?: boolean }
): Promise<T> {
  return coreApi.post(`/v2/fleet/${action}`, payload, { _noMessage: options?.silent });
}
