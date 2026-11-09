import { coreApi } from './core-adapter';
export interface SSHProfile {
  nodeId: string;
  host: string;
  port: number;
  username: string;
  authType: 'key' | 'password' | 'certificate' | 'ephemeralCertificate';
  credentialRef: string;
  hostKeyFingerprint: string;
  enabled: boolean;
  revision: number;
}
export interface SSHExecution {
  id: string;
  status: string;
  exitCode?: number;
  truncated: boolean;
}
export interface SSHResult {
  execution: SSHExecution;
  output: string;
  cursor: number;
}
export const sshProfileGet = (nodeId: string): Promise<SSHProfile> =>
  coreApi.post('/v2/fleet/ssh/profile/get', { nodeId }, { _noMessage: true });
export const sshProfileSave = (nodeId: string, profile: SSHProfile, expectedRevision: number): Promise<SSHProfile> =>
  coreApi.post('/v2/fleet/ssh/profile/save', { nodeId, profile, expectedRevision });
export const sshProfileTest = (nodeId: string) => coreApi.post('/v2/fleet/ssh/profile/test', { nodeId });
export const sshExecute = (nodeId: string, executionId: string, script: string): Promise<SSHExecution> =>
  coreApi.post('/v2/fleet/ssh/executions/create', { nodeId, executionId, script, timeoutSeconds: 300 });
export const sshOutput = (nodeId: string, executionId: string): Promise<SSHResult> =>
  coreApi.post('/v2/fleet/ssh/executions/get', { nodeId, executionId, cursor: 0 });
export const sshCancel = (nodeId: string, executionId: string) =>
  coreApi.post('/v2/fleet/ssh/executions/cancel', { nodeId, executionId });

export const sshHistory = (nodeId: string): Promise<{ items: SSHExecution[] }> =>
  coreApi.post('/v2/fleet/ssh/executions/list', { nodeId });

export interface SSHTerminalResult {
  username?: string;
  sessionId: string;
  state: string;
  errorCode?: string;
  data: string;
  cursor: number;
  reset: boolean;
  sequence: number;
  expiresAt: number;
}
export const sshTerminal = <T = SSHTerminalResult>(
  action: 'create' | 'read' | 'write' | 'resize' | 'close',
  payload: Record<string, unknown>
): Promise<T> => coreApi.post('/v2/fleet/ssh/terminal/' + action, payload, { _noMessage: true });

export type SSHCredentials = { password?: string; privateKey?: string; passphrase?: string; certificate?: string };
export interface SSHSetup {
  connectionMode: 'automatic' | 'custom';
  authType: SSHProfile['authType'];
  credentialConfigured: boolean;
  defaultUsername?: string;
  defaultPort?: number;

  nodeId: string;
  host?: string;
  port?: number;
  username?: string;
  hostKeyFingerprint?: string;
  enabled: boolean;
  revision: number;
  setupState: 'initializing' | 'ready' | 'failed' | 'disabled';
  setupError: string;
  lastCheckedAt?: number;
}
export const sshSetup = (
  action: 'get' | 'retry' | 'update',
  data: {
    nodeId: string;
    expectedRevision?: number;
    username?: string;
    port?: number;
    enabled?: boolean;
    connectionMode?: 'automatic' | 'custom';
    authType?: SSHProfile['authType'];
    reset?: boolean;
    credentials?: SSHCredentials;
  }
): Promise<SSHSetup> => coreApi.post(`/v2/fleet/ssh/setup/${action}`, data, { _noMessage: true, timeout: 30000 });

// Unknown server codes retain a useful translated fallback.
export const sshFailureKey = (code?: string): string => {
  const known = new Set([
    'SSH_WAITING_FOR_AGENT',
    'SSH_INSTALLATION_REQUIRED',
    'SSH_INSTALLATION_INVALID',
    'SSH_NETWORK_UNAVAILABLE',
    'SSH_HOST_KEY_CHANGED',
    'SSH_CONNECTION_FAILED',
    'EDGE_SSH_DISABLED',
    'SSH_PORT_BLOCKED',
    'SSH_POLICY_UNAVAILABLE',
    'SSH_TIMEOUT',
    'SSH_PORT_REFUSED',
    'SSH_AUTH_REJECTED',
    'SSH_HANDSHAKE_FAILED',
    'SSH_CREDENTIAL_INVALID',
    'SSH_PROXY_UNAVAILABLE',
    'SSH_ACCESS_UNAVAILABLE',
    'SSH_CA_PENDING',
  ]);
  return 'fleet.ssh.reason.' + (known.has(code || '') ? code : 'SSH_CONNECTION_FAILED');
};
