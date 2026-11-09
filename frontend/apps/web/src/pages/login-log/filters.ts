import type { LoginLogPageParams } from '@/apis/core-api/login-log';

export const LOGIN_SUCCESS = 200;
export const LOGIN_FAILED = 401;
export const EVENT_LOGIN = 'login';
export const EVENT_LOGOUT = 'logout';

export type LoginLogFilters = Pick<LoginLogPageParams, 'keyword' | 'eventType' | 'code' | 'startTime' | 'endTime'>;

export const cleanLoginFilters = (value: LoginLogFilters): LoginLogFilters => ({
  keyword: value.keyword?.trim() || undefined,
  eventType: value.eventType || undefined,
  code: value.code,
  startTime: value.startTime,
  endTime: value.endTime,
});

export const hasLoginFilters = (value: LoginLogFilters) =>
  Object.values(value).some((item) => item !== undefined && item !== '');
