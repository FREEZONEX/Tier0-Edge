import { useEffect, useLayoutEffect, useMemo, useRef } from 'react';
import { App as AntApp, Modal } from 'antd';
import { BrowserRouter, useLocation } from 'react-router';
import { getRoutesDom, RoutesElement } from '@/routers';
import CookieContext from '@/CookieContext';
import themeToken from './theme/theme-token.ts';
import 'shepherd.js/dist/css/shepherd.css';
import './App.css';
import { userLogin } from '@/apis/chat2db';
import { getSystemConfig } from '@/apis/core-api/system-config.ts';
import { UnsTreeMapProvider } from '@/UnsTreeMapContext';
import { APP_TITLE, LOGIN_URL, MENU_TARGET_PATH, OMC_MODEL, STORAGE_PATH, APP_LANG } from '@/common-types/constans.ts';
import LanguageProvider from './LanguageProvider.tsx';
import { queryChat2dbCurUser } from '@/utils/chat2db.ts';
import { checkImageExists, isInIframe } from '@/utils/url-util.ts';
import { getToken, removeToken } from '@/utils/auth.ts';
import { fetchBaseStore, fetchLaunchpadBaseStore, useBaseStore } from '@/stores/base';
import { restoreThemeRootFromStore, setThemeBySystem, ThemeType, useThemeStore } from '@/stores/theme-store.ts';
import { cleanupI18nSubscriptions, defaultLanguage, I18nEnum, initI18n } from './stores/i18n-store.ts';
import Cookies from 'js-cookie';
import { useI18nStore } from '@/stores/i18n-store';
import { CookiesProvider } from 'react-cookie';
import { isPublicAccessPath } from '@/utils/public-access';
import { storageOpt } from '@/utils/storage.ts';
import DefaultPasswordReminder from '@/components/default-password-reminder';
import GlobalLoading from '@/components/loading/global';
import {
  getLaunchpadStandaloneLoginPath,
  isMiniAppRoutePath,
  isLaunchpadExplicitLogoutInProgress,
  isLaunchpadSiteRequest,
  isLaunchpadStandaloneAllowedPath,
  isLaunchpadStandaloneListener,
} from '@/utils/launchpad-site';
import { isolatePublicPageTheme } from '@/utils/public-page-theme';

const RouteModalCleanup = () => {
  const location = useLocation();
  const mountedRef = useRef(false);

  useEffect(() => {
    if (mountedRef.current) {
      Modal.destroyAll();
    }
    mountedRef.current = true;
  }, [location.pathname]);

  return null;
};

const PublicPageThemeBoundary = () => {
  const location = useLocation();
  const theme = useThemeStore((state) => state.theme);

  useLayoutEffect(() => {
    if (!isPublicAccessPath(location.pathname) && getToken()) {
      return;
    }
    const restoreIsolatedTheme = isolatePublicPageTheme();
    return () => {
      restoreIsolatedTheme();
      restoreThemeRootFromStore();
    };
  }, [location.pathname, theme]);

  return null;
};

const normalizeSupportedLanguage = (language?: string | null) => {
  const normalized = language?.trim().replace('_', '-').toLowerCase();
  if (normalized === 'zh' || normalized === 'zh-cn') {
    return I18nEnum.ZhCN;
  }
  if (normalized === 'en' || normalized === 'en-us') {
    return I18nEnum.EnUS;
  }
  return undefined;
};

const resolvePublicBootstrapLanguage = async (preferredEnvLang?: string) => {
  try {
    const systemConfig = await getSystemConfig();
    const systemLang = normalizeSupportedLanguage(systemConfig?.lang);
    if (systemLang) {
      return systemLang;
    }
  } catch (error) {
    console.log(error);
  }

  const preferred = normalizeSupportedLanguage(preferredEnvLang);
  if (preferred) {
    return preferred;
  }

  return normalizeSupportedLanguage(storageOpt.getOrigin(APP_LANG)) || defaultLanguage;
};

function App() {
  const currentPath = window.location.pathname;
  const isMiniApp = isMiniAppRoutePath(currentPath);
  const isLaunchpadAllowedPath = isLaunchpadStandaloneAllowedPath(currentPath);
  const isLaunchpadSite = isLaunchpadSiteRequest(currentPath);
  const { systemInfo, loading, routesStatus, currentUserInfo, menuGroup } = useBaseStore((state) => ({
    systemInfo: state.systemInfo,
    loading: state.loading,
    routesStatus: state.routesStatus,
    menuGroup: state.menuGroup,
    currentUserInfo: state.currentUserInfo,
  }));
  const _theme = useThemeStore((state) => state._theme);
  const lang = useI18nStore((state) => state.lang);

  useEffect(() => {
    const isOmc = isInIframe([], 'webview');
    const skipBootstrap = isPublicAccessPath(currentPath);
    const shouldBootstrapBaseStore =
      !skipBootstrap || ((currentPath === '/403' || currentPath === '/404') && !!getToken());
    const preferredEnvLang =
      import.meta.env.VITE_LANGUAGE ||
      import.meta.env.VITE_OS_LANG ||
      import.meta.env.VITE_APP_LANG ||
      import.meta.env.REACT_APP_LOCAL_LANG ||
      import.meta.env.REACT_APP_OS_LANG;
    if (isOmc) {
      Cookies.set(OMC_MODEL, '1', {
        expires: 365,
      });
    } else {
      Cookies.remove(OMC_MODEL, { path: '/' });
    }
    const init = async () => {
      if (
        isLaunchpadStandaloneListener() &&
        currentPath !== '/' &&
        !isMiniApp &&
        !isLaunchpadAllowedPath &&
        !isPublicAccessPath(currentPath)
      ) {
        window.location.replace('/launchpad');
        return;
      }
      if (!shouldBootstrapBaseStore) {
        const fallbackLang = await resolvePublicBootstrapLanguage(preferredEnvLang);
        try {
          document.title = APP_TITLE;
          await initI18n(fallbackLang);
        } catch (error) {
          console.log(error);
          await initI18n(fallbackLang);
        } finally {
          useBaseStore.setState({
            loading: false,
          });
        }
        return;
      }
      if (isLaunchpadSite || isMiniApp) {
        fetchLaunchpadBaseStore();
        return;
      }
      fetchBaseStore(true);
    };

    init();
    return () => {
      cleanupI18nSubscriptions();
    };
  }, []);

  useEffect(() => {
    if (systemInfo?.containerMap?.chat2db) {
      // chat2db鐧诲綍閫昏緫
      try {
        queryChat2dbCurUser?.()?.then(async (res) => {
          if (!res) {
            // 閲嶆柊鐧诲綍
            await userLogin?.();
            await queryChat2dbCurUser?.();
          }
        });
      } catch (e) {
        console.log(e);
      }
    }
  }, [systemInfo?.containerMap]);

  useEffect(() => {
    if (!systemInfo.appTitle) return;
    const loadFavicon = async () => {
      // 娴忚鍣ㄦ爣棰?
      document.title = `${systemInfo.themeConfig?.general?.browserTitle || systemInfo.appTitle}`;
      const browserIco = systemInfo?.themeConfig?.general?.browserIco;
      const baseUrl = browserIco ? `${STORAGE_PATH}${MENU_TARGET_PATH}/${browserIco}` : '';
      const themeExists = browserIco ? await checkImageExists(baseUrl) : false;

      // 缁熶竴澶勭悊鏂囦欢绫诲瀷鍜岃矾寰?
      const [type, path] = themeExists ? ['image/svg+xml', baseUrl] : ['image/x-icon', '/favicon.ico'];

      // 缁熶竴澶勭悊鏃堕棿鎴?
      const href = `${path}?v=${Date.now()}`;

      // 鏌ユ壘鎴栧垱寤?link 鍏冪礌
      let link = document.querySelector<HTMLLinkElement>("link[rel~='icon']");
      if (!link) {
        link = document.createElement('link');
        link.rel = 'icon';
        document.head.append(link);
      }

      // 缁熶竴璁剧疆灞炴€?
      Object.assign(link, { type, href });
    };

    loadFavicon();
  }, [systemInfo, lang]);

  useEffect(() => {
    if (_theme === ThemeType.System) {
      const mediaChange = (event: any) => {
        setThemeBySystem(event.matches);
      };
      const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
      mediaQuery.addEventListener('change', mediaChange);
      return () => {
        mediaQuery.removeEventListener('change', mediaChange);
      };
    }
  }, [_theme]);

  useEffect(() => {
    if (routesStatus !== 401) {
      return;
    }
    removeToken();
    const loginPath = systemInfo?.loginPath || LOGIN_URL;
    if (isLaunchpadExplicitLogoutInProgress()) {
      window.location.replace(loginPath);
      return;
    }
    const currentPath = `${window.location.pathname}${window.location.search}`;
    const redirectUri =
      currentPath && currentPath !== '/' && currentPath !== loginPath
        ? `?redirectUri=${encodeURIComponent(currentPath)}`
        : '';
    window.location.replace(getLaunchpadStandaloneLoginPath(`${loginPath}${redirectUri}`));
  }, [routesStatus, systemInfo?.loginPath]);

  const routeDom = useMemo(() => {
    return getRoutesDom({ menuGroup, systemInfo, currentUserInfo });
  }, [menuGroup, systemInfo, currentUserInfo, lang]);

  if (loading && !isLaunchpadSite) {
    return <GlobalLoading />;
  }

  if (routesStatus === 401) {
    return null;
  }

  return (
    <CookiesProvider defaultSetOptions={{ path: '/' }}>
      <CookieContext />
      <LanguageProvider config={{ theme: themeToken, modal: { centered: true } }}>
        {/*antd缁勪欢搴撶殑涓婚*/}
        <UnsTreeMapProvider>
          <AntApp>
            <BrowserRouter>
              <PublicPageThemeBoundary />
              <RouteModalCleanup />
              <DefaultPasswordReminder>
                <RoutesElement routeDom={routeDom} />
              </DefaultPasswordReminder>
            </BrowserRouter>
          </AntApp>
        </UnsTreeMapProvider>
      </LanguageProvider>
    </CookiesProvider>
  );
}

export default App;
