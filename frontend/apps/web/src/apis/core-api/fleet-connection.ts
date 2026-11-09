import { coreApi } from './core-adapter';

export interface FleetConnectionState {
  tokenConfigured?: boolean;
  switch?: {
    state: 'network' | 'registration' | 'verification' | 'ready' | 'cancelled';
    centerUrl: string;
    nodeId: string;
    errorCode?: string;
    causeCode?: string;
    networkMode?: 'direct' | 'tailscale';
    updatedAt: number;
  };
  network?: {
    mode?: 'direct' | 'tailscale';
    state: string;
    nodeId?: string;
    loginServer?: string;
    addresses?: string[];
    errorCode?: string;
    checkedAt?: number;
  };
  requestedConnection?: { centerUrl: string; nodeId: string; state: string };
  connectionReason?: string;
  access: FleetRemoteAccess;
  collectionScope: FleetCollectionScope;
  enabled: boolean;
  registrationStatus: string;
  businessStatus?: 'available' | 'unavailable';
  checkedAt?: number;
  connection?: {
    centerType?: 'enterprise' | 'cloud';
    networkMode?: 'direct' | 'tailscale';
    centerUrl: string;
    centerId: string;
    nodeId: string;
    registeredAt: number;
  };
}
export interface FleetSyncedUser {
  userId: number | string;
  userName: string;
  nickName: string;
  email: string;
  status: number;
  deletedTime: number;
  roleList: { roleId: number | string; roleName: string; roleCode: string }[];
}
export const getFleetConnection = (): Promise<FleetConnectionState> =>
  coreApi.post('/v2/fleet/current/get', {}, { _noMessage: true });
export const getFleetSyncedUsers = (): Promise<{ list: FleetSyncedUser[]; total: number }> =>
  coreApi.post('/v2/fleet/connection/users', {}, { _noMessage: true });

export interface FleetRemoteAccess {
  allowSSH: boolean;
  allowMetadataWrite: boolean;
  allowControl: boolean;
  revision: number;
}
export const getFleetRemoteAccess = async (nodeId: string): Promise<FleetRemoteAccess> => {
  const result: { access: FleetRemoteAccess } = await coreApi.post(
    '/v2/fleet/nodes/capabilities/get',
    { nodeId },
    { _noMessage: true }
  );
  return result.access;
};
export const saveFleetRemoteAccess = (value: {
  allowControl?: boolean;
  allowSSH: boolean;
  allowMetadataWrite: boolean;
  expectedRevision: number;
}): Promise<FleetRemoteAccess> => coreApi.post('/v2/fleet/connection/access/save', value, { _noMessage: true });

export interface FleetCollectionScope {
  mode: 'all' | 'selected';
  collectionIds: string[];
  revision: number;
}
export const saveFleetCollectionScope = (value: {
  mode: 'all' | 'selected';
  collectionIds: string[];
  expectedRevision: number;
}): Promise<FleetCollectionScope> => coreApi.post('/v2/fleet/connection/collections/save', value, { _noMessage: true });
