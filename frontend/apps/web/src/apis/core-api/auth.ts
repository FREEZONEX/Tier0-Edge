import {
  authApi,
  buttonUrisForResourceKeys,
  coreResourcesForResourceKeys,
  iamApi,
  routeUrisForResourceKeys,
  type CoreResource,
} from './core-adapter';
import { removeToken, setToken } from '@/utils/auth';

const isSuperAdmin = (user?: any) => {
  const roleCode = String(user?.roleCode || '')
    .trim()
    .toLowerCase();
  if (roleCode === 'admin' || roleCode === 'builder') {
    return true;
  }
  return ['tier0', 'admin'].includes(String(user?.userName || '').trim());
};
const defaultHomePage = '/home';
const defaultProfileEmail = 'tier0@example.com';
const validEmailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

// cookie 寿命跟随登录有效期设置(后端按设置签发 JWT 并返回 sessionExpireAt);
// 旧后端未返回该字段时回退原 0.5 天。
// 注意:Chrome 104+ 将 cookie 显式有效期钳制在 400 天内,permanent(100 年)实际
// 按 400 天写入;真正的"永不掉线"需要滑动续期机制,另行跟进。
const DEFAULT_TOKEN_COOKIE_EXPIRES_DAYS = 0.5;
const MAX_TOKEN_COOKIE_EXPIRES_DAYS = 400;
const tokenCookieExpires = (sessionExpireAt?: string) => {
  if (!sessionExpireAt) return DEFAULT_TOKEN_COOKIE_EXPIRES_DAYS;
  const expireAt = new Date(sessionExpireAt);
  if (Number.isNaN(expireAt.getTime())) return DEFAULT_TOKEN_COOKIE_EXPIRES_DAYS;
  const maxExpires = new Date(Date.now() + MAX_TOKEN_COOKIE_EXPIRES_DAYS * 24 * 60 * 60 * 1000);
  return expireAt > maxExpires ? maxExpires : expireAt;
};

const normalizeProfileEmail = (email?: string, user?: any) => {
  const value = String(email || '').trim();
  if (validEmailPattern.test(value)) return value;
  return isSuperAdmin(user) && String(user?.userName || '').trim() === 'tier0' ? defaultProfileEmail : value;
};

const normalizeHomePage = (homePage?: string | null) => {
  const value = String(homePage || '').trim();
  return value.startsWith('/') && !value.startsWith('//') ? value : '';
};

const routeUri = (item: any) => String(item?.uri || '').trim();
const isLaunchpadAppHomePage = (value: string) => /^\/launchpad\/[^/?#]+\/[^/?#]+\/?$/i.test(value);

export const resolveAllowedHomePage = (homePage?: string | null, resourceList: any[] = [], superAdmin = false) => {
  const normalized = normalizeHomePage(homePage);
  if (superAdmin) {
    return normalized || defaultHomePage;
  }
  const allowedRoutes = resourceList.map(routeUri).filter((uri) => uri.startsWith('/'));
  if (!allowedRoutes.length) {
    return defaultHomePage;
  }
  const permissionRoute = isLaunchpadAppHomePage(normalized) ? '/launchpad' : normalized;
  if (permissionRoute && allowedRoutes.some((uri) => uri.toLowerCase() === permissionRoute.toLowerCase())) {
    return normalized;
  }
  return allowedRoutes.find((uri) => uri.toLowerCase() === defaultHomePage) || allowedRoutes[0] || defaultHomePage;
};

const getCurrentUserConfig = async () => {
  try {
    return await authApi.get('/config');
  } catch {
    return { homePage: defaultHomePage };
  }
};

const getPermissionResources = async (user: any = {}) => {
  const resourceKeys = user?.resourceKeys || [];
  if (user?.superAdmin !== true) {
    return coreResourcesForResourceKeys(resourceKeys);
  }
  try {
    const resources = (await iamApi.get('/resources')) as CoreResource[];
    return Array.isArray(resources) ? resources : [];
  } catch {
    return coreResourcesForResourceKeys(resourceKeys);
  }
};

const buildUserPermissions = (resources: CoreResource[] = [], user?: any, config?: any) => {
  const resourceList = routeUrisForResourceKeys(resources, user?.resourceKeys || []);
  const uiButtons = (user?.uiActions || []).filter(
    (item: string) => item === 'button:*' || String(item || '').startsWith('button:')
  );
  const buttonList = uiButtons.length > 0 ? uiButtons : buttonUrisForResourceKeys(resources, user?.resourceKeys || []);
  const superAdmin = isSuperAdmin(user);

  return {
    resourceList,
    buttonList: superAdmin ? ['button:*'] : buttonList,
    homePage: resolveAllowedHomePage(config?.homePage, resourceList, superAdmin),
    superAdmin,
  };
};

const completeLogin = async (result: any) => {
  if (result?.token) {
    setToken(result.token, { expires: tokenCookieExpires(result?.sessionExpireAt), path: '/' });
  }
  const [resources, config] = await Promise.all([getPermissionResources(result), getCurrentUserConfig()]);
  const permissions = buildUserPermissions(resources as CoreResource[], result, config);
  return {
    ...result,
    sub: String(result?.userId || ''),
    preferredUsername: result?.userName,
    firstName: result?.nickName || result?.userName,
    forceChangePassword: Boolean(result?.forceChangePassword),
    phone: result?.phone,
    email: normalizeProfileEmail(result?.email, result),
    ...permissions,
  };
};

export const loginApi = async (data?: Record<string, unknown>) => {
  const result = await authApi.post('/login', data);
  return completeLogin(result);
};

export type ExternalSSOPending = {
  displayName?: string;
  email?: string;
  suggestedUsername?: string;
  mfaAuthenticated?: boolean;
  redirectUri?: string;
};

export const getExternalSSOPending = (provider: string, ticket: string): Promise<ExternalSSOPending> =>
  authApi.get(`/sso/${encodeURIComponent(provider)}/pending`, { params: { ticket }, _noMessage: true });

export const checkExternalSSOUsername = (
  provider: string,
  ticket: string,
  username: string
): Promise<{ available: boolean }> =>
  authApi.get(`/sso/${encodeURIComponent(provider)}/username-availability`, {
    params: { ticket, username },
    _noMessage: true,
  });

export const completeExternalSSO = async (provider: string, ticket: string, username: string) => {
  const result = await authApi.post(
    `/sso/${encodeURIComponent(provider)}/complete`,
    { ticket, username },
    { _noMessage: true }
  );
  return completeLogin(result);
};

export const linkExternalSSO = async (provider: string, ticket: string, username: string, password: string) => {
  const result = await authApi.post(
    `/sso/${encodeURIComponent(provider)}/link`,
    { ticket, username, password },
    { _noMessage: true }
  );
  return completeLogin(result);
};

export const externalSSOStartURL = (provider: string, redirectUri?: string) => {
  const query = redirectUri ? `?redirectUri=${encodeURIComponent(redirectUri)}` : '';
  return `/api/core/auth/sso/${encodeURIComponent(provider)}/start${query}`;
};
// 获取用户信息
export const getUserInfo = async () => {
  const user = await authApi.get('/me');
  const [resources, config] = await Promise.all([getPermissionResources(user), getCurrentUserConfig()]);
  const coreResources = resources as CoreResource[];
  const permissions = buildUserPermissions(coreResources, user, config);
  return {
    firstTimeLogin: user?.isRandomPwd ? 1 : 0,
    forceChangePassword: Boolean(user?.forceChangePassword),
    tipsEnable: 0,
    sub: String(user?.userId || ''),
    preferredUsername: user?.userName,
    roleCode: user?.roleCode,
    firstName: user?.nickName || user?.userName,
    email: normalizeProfileEmail(user?.email, user),
    phone: user?.phone,
    enabled: true,
    roleList: [],
    resourceKeys: user?.resourceKeys || [],
    ...permissions,
    denyResourceList: [],
  };
};

export const logoutApi = async () => {
  try {
    await authApi.post('/logout');
  } finally {
    removeToken();
  }
};
