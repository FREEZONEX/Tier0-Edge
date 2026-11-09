import type { AxiosWrapperRequestConfig } from '@/utils/request';
import { coreApi } from './core-adapter';

export interface LoginLogPageParams {
  pageNo?: number;
  pageSize?: number;
  keyword?: string;
  userName?: string;
  ipAddr?: string;
  eventType?: string;
  code?: number;
  startTime?: string;
  endTime?: string;
}

export interface LoginLogItem {
  id: number;
  userId: number;
  userName: string;
  ipAddr: string;
  loginLocation: string;
  browser: string;
  os: string;
  eventType: string;
  code: number;
  msg: string;
  createdAt: number;
}

export interface LoginLogPageResp {
  pageNo: number;
  pageSize: number;
  total: number;
  data: LoginLogItem[];
}

export const getLoginLogPage = async (
  data?: LoginLogPageParams,
  config?: AxiosWrapperRequestConfig
): Promise<LoginLogPageResp> => coreApi.post('/login-logs/page', data, config);
