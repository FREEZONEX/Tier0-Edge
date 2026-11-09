import type { LoginLogItem } from '@/apis/core-api/login-log';
import { EVENT_LOGIN, EVENT_LOGOUT, LOGIN_SUCCESS } from './filters';

type Translate = (id: string, opt?: any, defaultMessage?: string, description?: string | object) => string;

export const getLoginAccountLabel = (record: Pick<LoginLogItem, 'userName'>) => record.userName?.trim() || '-';

export const getLoginIpLabel = (record: Pick<LoginLogItem, 'ipAddr'>) => record.ipAddr?.trim() || '-';

export const getLoginLocationLabel = (record: Pick<LoginLogItem, 'loginLocation'>) =>
  record.loginLocation?.trim() || '-';

export const getLoginEventLabel = (record: Pick<LoginLogItem, 'eventType'>, t: Translate) =>
  record.eventType === EVENT_LOGOUT
    ? t('loginLog.eventLogout', {}, 'Logout')
    : !record.eventType || record.eventType === EVENT_LOGIN
      ? t('loginLog.eventLogin', {}, 'Login')
      : record.eventType;

export const getLoginStatusLabel = (code: number, t: Translate) =>
  code === LOGIN_SUCCESS ? t('loginLog.statusSuccess', {}, 'Success') : t('loginLog.statusFailed', {}, 'Failed');

export const getLoginMessageLabel = (record: Pick<LoginLogItem, 'msg'>, t: Translate) => {
  const message = record.msg?.trim();
  return message === 'invalid username or password' || message === 'login.invalidCredentials'
    ? t('login.invalidCredentials')
    : message || '-';
};
