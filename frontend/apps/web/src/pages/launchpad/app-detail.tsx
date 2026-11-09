import {
  getLaunchpadAppByNameApi,
  getLaunchpadProjectByNameApi,
  recordLaunchpadAppViewApi,
} from '@/apis/core-api/launchpad';
import ComDetailHeader from '@/components/com-detail-header';
import { useActivate } from '@/contexts/tabs-lifecycle-context';
import { useTranslate } from '@/hooks';
import type { App } from '@/pages/launchpad/type';
import type { ProjectItem } from '@/pages/project/types';
import { useLocationNavigate } from '@/routers';
import { normalizeLocalAppUrl, resolveAppNotificationUrl } from '@/utils/app-url';
import { getToken } from '@/utils/auth';
import { getSearchParamsString } from '@/utils/url-util';
import { useBaseStore } from '@/stores/base';
import { withLaunchpadStandaloneQuery } from '@/utils/launchpad-site';
import { Maximize, Minimize } from '@/components/lucide-icon/carbon';
import { Button, Result, Spin } from 'antd';
import { type FC, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, type Location } from 'react-router';
import AppSwitcher from './components/AppSwitcher';
import styles from './app-detail.module.scss';

interface AppDetailPageProps {
  appOnly?: boolean;
  projectName?: string;
  appName?: string;
  location?: Location;
  onBack?: () => void;
  onAppChange?: (appName: string, app?: App) => void;
}

const APP_IFRAME_ALLOWED_FEATURES = [
  'camera',
  'microphone',
  'geolocation',
  'display-capture',
  'clipboard-read',
  'clipboard-write',
  'bluetooth',
  'serial',
  'usb',
  'hid',
  'fullscreen',
] as const;

const createAppIframeAllow = (url: string) => {
  try {
    const appOrigin = new URL(url).origin;
    return APP_IFRAME_ALLOWED_FEATURES.map((feature) => `${feature} ${appOrigin}`).join('; ');
  } catch {
    return APP_IFRAME_ALLOWED_FEATURES.join('; ');
  }
};

const decodePathSegment = (value?: string) => {
  if (!value) return '';
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
};

const getLaunchpadAppRouteParams = (pathname: string) => {
  const parts = pathname.split('/').filter(Boolean);
  if (parts[0] !== 'launchpad') {
    return { projectName: '', appName: '' };
  }
  return {
    projectName: decodePathSegment(parts[1]),
    appName: decodePathSegment(parts[2]),
  };
};

const AppDetailPage: FC<AppDetailPageProps> = ({
  appOnly = false,
  projectName: propProjectName,
  appName: propAppName,
  location: keepAliveLocation,
  onBack,
  onAppChange,
}) => {
  const navigate = useLocationNavigate();
  const routerNavigate = useNavigate();
  const routerLocation = useLocation();
  const location = keepAliveLocation || routerLocation;
  const routeParams = useMemo(() => getLaunchpadAppRouteParams(location.pathname), [location.pathname]);
  const projectName = propProjectName || routeParams.projectName;
  const appName = propAppName || routeParams.appName;
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const isMountedRef = useRef(true);
  const appDetailRequestIdRef = useRef(0);
  const [app, setApp] = useState<App | null>(null);
  const [appNotFound, setAppNotFound] = useState(false);
  const [noPermission, setNoPermission] = useState(false);
  const [apps, setApps] = useState<App[]>([]);
  const [projectInfo, setProjectInfo] = useState<ProjectItem | null>(null);
  const [loading, setLoading] = useState(!!appName);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const formatMessage = useTranslate();
  // Builder（auth/me.roleCode=admin/builder → superAdmin）版异常页多一个 Go to Project，跳转项目 Members tab；Operator 仅 Back to Launchpad
  const isBuilder = useBaseStore((state) => state.currentUserInfo?.superAdmin === true);

  // background=true：tab 重新激活时的静默刷新，保留已渲染内容（iframe 不卸载），失败时维持现状
  const fetchAppDetail = useCallback(
    (options?: { background?: boolean }) => {
      const background = options?.background === true;
      if (!projectName || !appName) return;
      const requestId = ++appDetailRequestIdRef.current;
      if (!background) {
        setLoading(true);
        setAppNotFound(false);
        setNoPermission(false);
      }
      getLaunchpadAppByNameApi(projectName, appName)
        .then((data) => {
          if (!isMountedRef.current || requestId !== appDetailRequestIdRef.current) return;
          setApp(data?.app || null);
          setAppNotFound(false);
          setNoPermission(false);
        })
        .catch((error) => {
          if (!isMountedRef.current || requestId !== appDetailRequestIdRef.current) return;
          // 403/404 是后端对权限与存在性的确定性结果，后台刷新同样要落错误态；
          // 仅瞬时错误（网络抖动等）保留当前视图，避免打断正在使用的页面
          if (background && error?.code !== 403 && error?.code !== 404) {
            console.warn('launchpad app background refresh failed:', error);
            return;
          }
          setApp(null);
          // 403 = App 存在但成员缺 App Role → App access required；
          // 其余（含 404 不存在/已删除/非成员，不披露存在性）→ App not found
          if (error?.code === 403) {
            setNoPermission(true);
            setAppNotFound(false);
          } else {
            setAppNotFound(true);
            setNoPermission(false);
          }
        })
        .finally(() => {
          if (isMountedRef.current && requestId === appDetailRequestIdRef.current) setLoading(false);
        });
    },
    [projectName, appName]
  );

  const fetchProjectApps = useCallback(() => {
    if (appOnly || !projectName) return;
    getLaunchpadProjectByNameApi(projectName)
      .then((data) => {
        if (!isMountedRef.current) return;
        setApps(data?.apps || []);
        setProjectInfo(data?.project || null);
      })
      .catch(() => {
        if (!isMountedRef.current) return;
        setApps([]);
        setProjectInfo(null);
      });
  }, [appOnly, projectName]);

  const recordAppView = useCallback((appId?: string | number) => {
    if (!appId) return;
    void recordLaunchpadAppViewApi(appId);
  }, []);

  useEffect(() => {
    isMountedRef.current = true;
    return () => {
      isMountedRef.current = false;
    };
  }, []);

  useEffect(() => {
    fetchAppDetail();
  }, [fetchAppDetail]);

  useEffect(() => {
    recordAppView(app?.appId);
  }, [app?.appId, recordAppView]);

  useEffect(() => {
    if (appOnly || !app) return;
    if (routerLocation.pathname !== location.pathname || routerLocation.search !== location.search) return;
    const tabName = app.displayName || app.appName || appName;
    const tabContextName = location.state?.tabContextName || app.projectDisplayName || app.projectName || projectName;
    if (!tabName || (location.state?.tabName === tabName && location.state?.tabContextName === tabContextName)) return;
    routerNavigate(location.pathname + location.search, {
      replace: true,
      state: {
        ...(location.state || {}),
        tabName,
        tabContextName,
      },
    });
  }, [
    appOnly,
    app,
    appName,
    location.pathname,
    location.search,
    location.state,
    projectName,
    routerLocation.pathname,
    routerLocation.search,
    routerNavigate,
  ]);

  // 只在 projectName 变化时重新获取 apps 列表，切换 app 不重新请求
  useEffect(() => {
    fetchProjectApps();
  }, [fetchProjectApps]);

  useActivate(() => {
    fetchAppDetail({ background: true });
    fetchProjectApps();
  });

  useEffect(() => {
    const handleFullscreenChange = () => {
      setIsFullscreen(!!document.fullscreenElement);
    };
    document.addEventListener('fullscreenchange', handleFullscreenChange);
    return () => document.removeEventListener('fullscreenchange', handleFullscreenChange);
  }, []);

  const handleBack = () => {
    if (onBack) {
      onBack();
      return;
    }
    if (appNotFound) {
      routerNavigate(withLaunchpadStandaloneQuery('/launchpad'), {
        replace: true,
        state: { tabName: formatMessage('Launchpad.title') },
      });
      return;
    }
    navigate({
      pathname: `/launchpad/${encodeURIComponent(projectName || '')}`,
      state: { tabName: projectName || formatMessage('Launchpad.title') },
    });
  };

  const handleGoToProject = () => {
    const projectId = projectInfo?.id;
    if (!projectId) return;
    routerNavigate(`/project/${projectId}?${getSearchParamsString({ from: '/launchpad', tab: 'members' })}`, {
      state: { tabName: projectInfo?.displayName || projectInfo?.name || projectName },
    });
  };

  const handleAppChange = (newAppName: string, nextApp?: App) => {
    if (onAppChange) {
      onAppChange(newAppName, nextApp);
    } else {
      routerNavigate(
        withLaunchpadStandaloneQuery(
          `/launchpad/${encodeURIComponent(projectName || '')}/${encodeURIComponent(newAppName)}`
        ),
        {
          replace: true,
          state: {
            tabName: nextApp?.displayName || nextApp?.appName || newAppName,
            tabContextName:
              location.state?.tabContextName || nextApp?.projectDisplayName || nextApp?.projectName || projectName,
            tabParentPathname: location.state?.tabParentPathname,
            replaceCurrentTab: true,
          },
        }
      );
    }
  };

  const toggleFullscreen = () => {
    if (!isFullscreen) {
      const elem = iframeRef.current?.parentElement;
      if (elem?.requestFullscreen) elem.requestFullscreen();
      setIsFullscreen(true);
    } else {
      document.exitFullscreen?.();
      setIsFullscreen(false);
    }
  };

  // 头部始终渲染，title 优先用已有 app state，loading 时用 appName 作为占位
  const title = app?.displayName || app?.appName || appName || '';
  const rawAppUrl = app?.url;
  const notificationPath = new URLSearchParams(location.search).get('notificationPath');

  const appUrl = useMemo(() => {
    if (!rawAppUrl) return '';
    const shouldRewritePrivateHost = app?.siteType === 'dynamic' && !app?.manual && app?.appType !== 'manual';
    const normalizedUrl = normalizeLocalAppUrl(rawAppUrl, { allowPrivateHostRewrite: shouldRewritePrivateHost });
    const targetUrl = resolveAppNotificationUrl(normalizedUrl, notificationPath);
    if (!targetUrl) return '';
    const token = getToken();
    if (!token) return targetUrl;
    const url = new URL(targetUrl, window.location.origin);
    url.searchParams.set('_token', token);
    return url.toString();
  }, [app?.appType, app?.manual, app?.siteType, rawAppUrl, notificationPath]);
  const appIframeAllow = useMemo(() => createAppIframeAllow(appUrl), [appUrl]);

  // App not found：不存在 / 已删除 / 非 Workspace Member（不披露存在性）
  if (appNotFound) {
    return (
      <div className={styles.appDetailPage}>
        <Result
          status="404"
          title={formatMessage('Launchpad.appNotFound', {}, 'App not found')}
          subTitle={formatMessage(
            'Launchpad.appNotFoundDesc',
            {},
            'This app is unavailable. It may have been removed, or the link is no longer valid.'
          )}
          extra={
            !appOnly && (
              <Button
                type="primary"
                onClick={() => routerNavigate(withLaunchpadStandaloneQuery('/launchpad'), { replace: true })}
              >
                {formatMessage('Launchpad.backToLaunchpad', {}, 'Back to Launchpad')}
              </Button>
            )
          }
        />
      </div>
    );
  }

  // App access required：成员但缺 App 绑定的 Business Role（Builder 版多 Go to Project 跳转项目 Members tab 引导配置角色，Operator 仅 Back）
  if (noPermission) {
    return (
      <div className={styles.appDetailPage}>
        <Result
          status="403"
          title={formatMessage('Launchpad.appAccessTitle', {}, 'App access required')}
          subTitle={formatMessage(
            isBuilder ? 'Launchpad.appAccessDescBuilder' : 'Launchpad.appAccessDescOperator',
            {},
            isBuilder
              ? 'You do not have the Business Role required to open this app. Configure role permissions in the project, or contact the project owner.'
              : 'You have not been assigned the role required to access this app. Please contact the project owner or administrator to configure role permissions.'
          )}
          extra={
            !appOnly && (
              <>
                {isBuilder && (
                  <Button type="primary" onClick={handleGoToProject}>
                    {formatMessage('Launchpad.goToProject', {}, 'Go to Project')}
                  </Button>
                )}
                <Button
                  type={isBuilder ? 'default' : 'primary'}
                  onClick={() => routerNavigate(withLaunchpadStandaloneQuery('/launchpad'), { replace: true })}
                >
                  {formatMessage('Launchpad.backToLaunchpad', {}, 'Back to Launchpad')}
                </Button>
              </>
            )
          }
        />
      </div>
    );
  }

  return (
    <div className={styles.appDetailPage}>
      {!appOnly && (
        <ComDetailHeader
          title={title}
          onBack={handleBack}
          showBack
          showDesc={false}
          rightExtra={
            <>
              <AppSwitcher apps={apps} currentAppName={appName} onAppChange={handleAppChange} />
              <Button
                type="text"
                icon={isFullscreen ? <Minimize size={16} /> : <Maximize size={16} />}
                onClick={toggleFullscreen}
                className={styles.fullscreenButton}
              />
            </>
          }
          style={{
            border: '1px solid var(--ui-menu-hover-color)',
            alignItems: 'center',
          }}
        />
      )}
      <div className={styles.iframeContainer}>
        {loading ? (
          <div className={styles.loading}>
            <Spin size="large" />
          </div>
        ) : !app ? (
          <div className={styles.error}>{formatMessage('Launchpad.appNotFound')}</div>
        ) : !appUrl ? (
          <div className={styles.loading}>{formatMessage('project.appNotReady')}</div>
        ) : (
          <iframe
            ref={iframeRef}
            src={appUrl}
            className={styles.iframe}
            title={app.displayName || app.appName}
            allow={appIframeAllow}
          />
        )}
      </div>
    </div>
  );
};

export default AppDetailPage;
