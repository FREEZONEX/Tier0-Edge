import { isRemovedFeaturePath } from './removed-feature-pages';
import { LOGIN_URL, OPERATOR_ROLE_CODE } from '@/common-types/constans';

const isBrowser = () => typeof window !== 'undefined';
let isExplicitLogoutInProgress = false;

export const LAUNCHPAD_STANDALONE_QUERY_KEY = 'launchpadStandalone';

export const beginLaunchpadExplicitLogout = () => {
  isExplicitLogoutInProgress = true;
};

export const isLaunchpadExplicitLogoutInProgress = () => isExplicitLogoutInProgress;

const normalizePort = (value?: string) => String(value || '').trim();

const getRuntimeLaunchpadPort = () => {
  if (!isBrowser()) {
    return '';
  }
  return normalizePort(window.__TIER0_RUNTIME_CONFIG__?.launchpadPort);
};

const isRuntimeLaunchpadStandalone = () => Boolean(isBrowser() && window.__TIER0_RUNTIME_CONFIG__?.launchpadStandalone);

const getProcessLaunchpadPort = () => {
  if (typeof process === 'undefined') {
    return '';
  }
  return normalizePort(process.env?.LAUNCHPAD_PORT);
};

export const getLaunchpadStandalonePort = () =>
  getRuntimeLaunchpadPort() || normalizePort(import.meta.env.VITE_LAUNCHPAD_PORT) || getProcessLaunchpadPort();

// Content-only entry for the mini program; distinct from the standalone portal.
export const isMiniAppRoutePath = (pathname: string) => /^\/mini-app\/\d+\/\d+\/?$/.test(pathname);

export const isLaunchpadRoutePath = (pathname: string) =>
  pathname === '/launchpad' || pathname.startsWith('/launchpad/');

export const isLaunchpadUserSettingsPath = (pathname: string) =>
  pathname === '/settings' ||
  pathname === '/settings/profile' ||
  pathname === '/settings/preferences' ||
  pathname === '/settings/security';

export const isLaunchpadStandaloneListener = () => {
  if (isRuntimeLaunchpadStandalone()) {
    return true;
  }
  const standalonePort = getLaunchpadStandalonePort();
  return Boolean(standalonePort && isBrowser() && window.location.port === standalonePort);
};

export const isLaunchpadStandaloneAllowedPath = (pathname: string) =>
  isLaunchpadRoutePath(pathname) || isLaunchpadUserSettingsPath(pathname) || pathname === '/message-center';

export const isLaunchpadStandaloneQueryEnabled = (search?: string) => {
  const currentSearch = search ?? (isBrowser() ? window.location.search : '');
  return new URLSearchParams(currentSearch).get(LAUNCHPAD_STANDALONE_QUERY_KEY) === 'true';
};

const isLaunchpadStandaloneQueryContext = (pathname: string, search?: string) =>
  isLaunchpadStandaloneQueryEnabled(search) &&
  (pathname === '/' || pathname === LOGIN_URL || isLaunchpadStandaloneAllowedPath(pathname));

export const isLaunchpadStandaloneSite = (
  pathname = isBrowser() ? window.location.pathname : '',
  search = isBrowser() ? window.location.search : ''
) => isLaunchpadStandaloneListener() || isLaunchpadStandaloneQueryContext(pathname, search);

export const isLaunchpadSiteRequest = (pathname: string) =>
  isLaunchpadStandaloneSite(pathname) && isLaunchpadStandaloneAllowedPath(pathname);

export const withLaunchpadStandaloneQuery = (path: string) => {
  if (
    !isBrowser() ||
    !isLaunchpadStandaloneQueryContext(window.location.pathname, window.location.search) ||
    !path.startsWith('/') ||
    path.startsWith('//')
  ) {
    return path;
  }

  const url = new URL(path, window.location.origin);
  if (url.origin !== window.location.origin || !isLaunchpadStandaloneAllowedPath(url.pathname)) {
    return path;
  }
  url.searchParams.set(LAUNCHPAD_STANDALONE_QUERY_KEY, 'true');
  return `${url.pathname}${url.search}${url.hash}`;
};

export const withOperatorLaunchpadStandaloneQuery = (homePage: string, roleCode?: string, superAdmin = false) => {
  if (!isBrowser() || !homePage.startsWith('/') || homePage.startsWith('//')) {
    return homePage;
  }

  const url = new URL(homePage, window.location.origin);
  if (url.origin !== window.location.origin) {
    return homePage;
  }

  url.searchParams.delete(LAUNCHPAD_STANDALONE_QUERY_KEY);
  if (
    superAdmin ||
    String(roleCode || '')
      .trim()
      .toLowerCase() !== OPERATOR_ROLE_CODE ||
    url.pathname.replace(/\/+$/, '').toLowerCase() !== '/launchpad'
  ) {
    return `${url.pathname}${url.search}${url.hash}`;
  }

  url.searchParams.set(LAUNCHPAD_STANDALONE_QUERY_KEY, 'true');
  return `${url.pathname}${url.search}${url.hash}`;
};

type LaunchpadUserContext = {
  homePage?: string;
  roleCode?: string;
  superAdmin?: boolean;
};

export const getUserDefaultHomePath = (userInfo?: LaunchpadUserContext, fallbackPath = '/uns') =>
  withOperatorLaunchpadStandaloneQuery(
    isRemovedFeaturePath(userInfo?.homePage) ? fallbackPath : userInfo?.homePage || fallbackPath,
    userInfo?.roleCode,
    userInfo?.superAdmin
  );

export const getLaunchpadStandaloneLoginPath = (loginPath: string) => {
  if (
    !isBrowser() ||
    isExplicitLogoutInProgress ||
    !isLaunchpadStandaloneQueryContext(window.location.pathname, window.location.search) ||
    !loginPath
  ) {
    return loginPath;
  }

  const loginUrl = new URL(loginPath, window.location.origin);
  if (loginUrl.origin !== window.location.origin) {
    return loginPath;
  }

  const currentPath =
    window.location.pathname === '/'
      ? withLaunchpadStandaloneQuery('/launchpad')
      : `${window.location.pathname}${window.location.search}${window.location.hash}`;
  loginUrl.searchParams.set(LAUNCHPAD_STANDALONE_QUERY_KEY, 'true');
  loginUrl.searchParams.set('redirectUri', currentPath);

  return loginPath.startsWith('http://') || loginPath.startsWith('https://')
    ? loginUrl.toString()
    : `${loginUrl.pathname}${loginUrl.search}${loginUrl.hash}`;
};
