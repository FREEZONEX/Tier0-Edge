import { ApiWrapper, CustomAxiosConfigEnum } from '@/utils/request';

const api = new ApiWrapper('/api/core');

export interface McpInfo {
  enabled: boolean;
  transport: string;
  endpointPath: string;
  publicUrl: string;
  explicitPublicUrl: boolean;
  productVersion: string;
  contractVersion: string;
  readOnly: boolean;
  requiredScopes: string[];
  optionalScopes: string[];
  availableScopes: string[];
  defaultAuthentication: 'oauth';
  oauth: { available: boolean; reason?: string; resource?: string; scopes?: string[]; localClientsOnly?: boolean };
  toolCount: number;
}

export const getMcpInfo = (signal?: AbortSignal): Promise<McpInfo> =>
  api.get('/mcp/info', { signal, [CustomAxiosConfigEnum.NoMessage]: true });

// 浏览器知道实际 TLS/Host；路径由实例的受信代理配置提供。
export const resolveMcpUrl = (info: McpInfo): string =>
  info.explicitPublicUrl ? info.publicUrl : new URL(info.endpointPath, window.location.origin).href;

const oauthApi = new ApiWrapper('/api/core/mcp/oauth');
const quiet = { [CustomAxiosConfigEnum.NoMessage]: true };
export interface McpOAuthRequest {
  requestId: string;
  clientName: string;
  clientVerified: boolean;
  resource: string;
  redirectUri: string;
  userName: string;
  requestedScopes: string[];
  availableScopes: string[];
  expiresAt: number;
  readOnly: boolean;
}
export interface McpOAuthGrant {
  grantId: string;
  clientName: string;
  resource: string;
  scopes: string[];
  createdAt: number;
  expiresAt: number;
  revokedAt: number;
  expired: boolean;
}
export const getMcpOAuthRequest = (requestId: string, signal?: AbortSignal): Promise<McpOAuthRequest> =>
  oauthApi.get('/request', { ...quiet, params: { requestId }, signal });
export const decideMcpOAuth = (requestId: string, allow: boolean, scopes: string[]): Promise<{ redirectUri: string }> =>
  oauthApi.post('/decision', { requestId, allow, scopes }, quiet);
export const getMcpOAuthGrants = (signal?: AbortSignal): Promise<{ list: McpOAuthGrant[] }> =>
  oauthApi.get('/grants', { ...quiet, signal });
export const revokeMcpOAuthGrant = (grantId: string): Promise<{ revoked: boolean }> =>
  oauthApi.post('/grants/revoke', { grantId }, quiet);
