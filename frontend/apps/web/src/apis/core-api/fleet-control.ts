import { fleetAction } from './fleet-acquisition';

export type ControlGrant = {
  allowAutomatic: boolean;
  centerId: string;
  controlCode: string;
  grantId: string;
  revision: number;
  capabilityVersion: string;
  inputSchema: Record<string, unknown>;
  maxTtlMs: number;
  minIntervalMs: number;
  enabled: boolean;
};
export type ControlCapability = {
  code: string;
  name: string;
  capabilityVersion: string;
  contractVersion: string;
  contract: { inputSchema: Record<string, unknown>; outputSchema: Record<string, unknown>; sideEffect: string };
  authorization?: ControlGrant;
};
export type ControlIntent = {
  invocationId: string;
  nodeId: string;
  controlCode: string;
  capabilityVersion: string;
  deviceId: string;
  input: Record<string, unknown>;
  state: 'pendingConfirmation' | 'queued' | 'dispatched' | 'succeeded' | 'failed' | 'unknown' | 'expired';
  revision: number;
  expiresAt: number;
  createdAt: number;
  businessCompletion: string;
  completionEvidence: Record<string, unknown>;
  lastError: string;
  result: unknown;
};
export const controlCatalog = (nodeId: string) =>
  fleetAction<{ list: ControlCapability[] }>('controls/catalog', { nodeId, page: 1, size: 100 });
export const controlIntents = (nodeId: string) =>
  fleetAction<{ items: ControlIntent[] }>('control-intents/list', { nodeId, page: 1, size: 100 });
