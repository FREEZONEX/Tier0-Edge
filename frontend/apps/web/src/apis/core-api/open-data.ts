import { ApiWrapper } from '@/utils';

const apiKeyApi = new ApiWrapper('/api/core/common');
const iamApi = new ApiWrapper('/api/core/iam');
const aiConfigApi = new ApiWrapper('/api/core/ai');

export interface ApiKeyListParams {
  keyword?: string;
  page?: number;
  size?: number;
  keyType?: string;
}

export interface ApiKeyCreatePayload {
  name: string;
  permission?: string;
  usageType?: string;
  keyType?: string;
  resourceKeys?: string[];
}

export interface ApiKeyUpdatePayload {
  name?: string;
  permission?: string;
}

export const querySecretKeyList = async (params: ApiKeyListParams = {}) => apiKeyApi.get('/api-keys', { params });

export const createSecretKey = async (data: ApiKeyCreatePayload) => apiKeyApi.post('/api-keys', data);

export const deleteSecretKey = async (id: number) => apiKeyApi.delete(`/api-keys/${id}`);

export const updateSecretKey = async (id: number, data: ApiKeyUpdatePayload) => apiKeyApi.put(`/api-keys/${id}`, data);

export interface OAuthClientPayload {
  clientName: string;
  redirectUris: string[];
  allowedOrigins?: string[];
}

export const queryOAuthClientList = async () => iamApi.get('/oauth-clients');

export const createOAuthClient = async (data: OAuthClientPayload) => iamApi.post('/oauth-clients', data);

export const updateOAuthClient = async (id: number, data: OAuthClientPayload) =>
  iamApi.put(`/oauth-clients/${id}`, data);

export const deleteOAuthClient = async (id: number) => iamApi.delete(`/oauth-clients/${id}`);

export type AIProviderType = 'openai_compatible' | 'azure_openai' | 'anthropic';

export type AIEmbeddingMode = 'inherit' | 'custom';

export interface AIProviderConfig {
  id: number;
  workspaceId: number;
  name: string;
  provider: AIProviderType;
  baseUrl: string;
  model?: string;
  keySuffix?: string;
  apiKeySet: boolean;
  embeddingMode?: AIEmbeddingMode;
  embeddingBaseUrl?: string;
  embeddingModel?: string;
  embeddingKeySuffix?: string;
  embeddingApiKeySet?: boolean;
  isDefault: boolean;
  enabled: boolean;
  createdTime?: string;
  updatedTime?: string;
}

export interface AIProviderConfigPayload {
  workspaceId: number;
  name: string;
  provider: AIProviderType;
  baseUrl: string;
  model?: string;
  apiKey?: string;
  embeddingMode?: AIEmbeddingMode;
  embeddingBaseUrl?: string;
  embeddingApiKey?: string;
  embeddingModel?: string;
  isDefault?: boolean;
  enabled?: boolean;
}

export type AIEmbeddingTestStatus = 'ok' | 'unsupported' | 'unauthorized' | 'model_not_found' | 'failed';

export interface AIEmbeddingTestResult {
  status: AIEmbeddingTestStatus;
  httpStatus: number;
  model: string;
  dimensions: number;
  expectedDimensions: number;
  dimensionsMatch: boolean;
  latencyMs: number;
}

export interface AITrustedHosts {
  hosts: string[];
  customized: boolean;
}

export const queryAIProviderConfigs = async (workspaceId = 1) =>
  aiConfigApi.get('/configs', { params: { workspaceId } });

export const createAIProviderConfig = async (data: AIProviderConfigPayload) => aiConfigApi.post('/configs', data);

export const testAIProviderConnection = async (data: AIProviderConfigPayload) =>
  aiConfigApi.post('/configs/test', data);

export const testAIEmbeddingConnection = async (data: AIProviderConfigPayload): Promise<AIEmbeddingTestResult> =>
  aiConfigApi.post('/configs/test-embedding', data);

export const updateAIProviderConfig = async (id: number, data: AIProviderConfigPayload) =>
  aiConfigApi.put(`/configs/${id}`, data);

export const deleteAIProviderConfig = async (id: number, workspaceId = 1) =>
  aiConfigApi.delete(`/configs/${id}`, { params: { workspaceId } });

export const queryAITrustedHosts = async (): Promise<AITrustedHosts> => aiConfigApi.get('/trusted-hosts');

export const saveAITrustedHosts = async (hosts: string[]): Promise<AITrustedHosts> =>
  aiConfigApi.put('/trusted-hosts', { hosts });
