/* eslint-disable @typescript-eslint/no-unused-vars */
import { coreApi } from './core-adapter';
import { CustomAxiosConfigEnum } from '@/utils/request';

export type RuntimeMetricItem = {
  available?: boolean;
  usagePercent?: number;
  usedBytes?: number;
  totalBytes?: number;
  logicalCores?: number;
  scope?: string;
  source?: string;
  path?: string;
};

export type RuntimeMetrics = {
  cpu?: RuntimeMetricItem;
  memory?: RuntimeMetricItem;
  hostCpu?: RuntimeMetricItem;
  hostMemory?: RuntimeMetricItem;
  disk?: RuntimeMetricItem;
  collectedAt?: number;
};

// 系统配置
export const getSystemConfig = async () => coreApi.get('/system/config');

// 聊天入口的状态检查需及时结束，错误由入口统一展示。
export const getAiChatConfigured = async (signal: AbortSignal): Promise<boolean | null> => {
  const config = await coreApi.get('/system/config', {
    signal,
    timeout: 10000,
    [CustomAxiosConfigEnum.NoMessage]: true,
  });
  return typeof config?.aiChatConfigured === 'boolean' ? config.aiChatConfigured : null;
};

export const getRuntimeMetrics = async (): Promise<RuntimeMetrics> => coreApi.get('/system/runtime-metrics');

//获取主题配置

export const getAllThemeConfig = async (_params?: any) => ({});

// 登录有效期设置(全局系统配置,仅对新登录会话生效)
export type LoginSessionDurationValue = '12h' | '1d' | '7d' | '30d' | 'permanent';

export const DEFAULT_LOGIN_SESSION_DURATION: LoginSessionDurationValue = '12h';

export const getLoginSessionDuration = async (): Promise<LoginSessionDurationValue> => {
  const data: any = await coreApi.get('/system/login-session-duration');
  return (data?.duration as LoginSessionDurationValue) || DEFAULT_LOGIN_SESSION_DURATION;
};

export const updateLoginSessionDuration = async (duration: LoginSessionDurationValue) =>
  coreApi.put('/system/login-session-duration', { duration });
