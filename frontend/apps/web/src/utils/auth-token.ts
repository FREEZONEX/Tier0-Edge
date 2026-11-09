import Cookies from 'js-cookie';
import { APP_COMMUNITY_TOKEN, SYS_COMMUNITY_TOKEN } from '@/common-types/constans.ts';

// 请求层依赖此叶子模块，不能在这里引入 Store 或 API 客户端。
export const isDevScopedTokenEnabled = () => {
  if (!import.meta.env.DEV || typeof window === 'undefined') {
    return false;
  }
  const hostname = window.location.hostname;
  return (
    Boolean(window.location.port) || hostname === 'localhost' || hostname === '127.0.0.1' || hostname === '0.0.0.0'
  );
};

const devScopedTokenKey = () => `${APP_COMMUNITY_TOKEN}:${window.location.host}`;

const getDevScopedToken = () => {
  if (!isDevScopedTokenEnabled()) {
    return undefined;
  }
  try {
    return window.sessionStorage.getItem(devScopedTokenKey()) || undefined;
  } catch {
    return undefined;
  }
};

export const setDevScopedToken = (token: string) => {
  if (!isDevScopedTokenEnabled()) {
    return;
  }
  try {
    window.sessionStorage.setItem(devScopedTokenKey(), token);
  } catch {
    // sessionStorage can be unavailable in strict browser privacy modes.
  }
};

export const removeDevScopedToken = () => {
  if (!isDevScopedTokenEnabled()) {
    return;
  }
  try {
    window.sessionStorage.removeItem(devScopedTokenKey());
  } catch {
    // ignore storage cleanup failures
  }
};

export function getToken() {
  if (isDevScopedTokenEnabled()) {
    return getDevScopedToken();
  }
  return Cookies.get(APP_COMMUNITY_TOKEN) || Cookies.get(SYS_COMMUNITY_TOKEN);
}
