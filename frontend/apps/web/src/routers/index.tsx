import { isRemovedFeatureEntry } from '@/utils/removed-feature-pages';
import { capabilityAllowsPath, capabilityEnabled } from '@/utils/product-capabilities';
import { LOGIN_URL, OMC_MODEL } from '@/common-types/constans';
import LaunchpadShell from '@/layout/LaunchpadShell';
import Layout from '@/layout';
import AccountManagement from '@/pages/account-management';
import AuditLogPage from '@/pages/audit-log';
import LoginLogPage from '@/pages/login-log';
import DevPage from '@/pages/dev-page';
import GlobalLoading from '@/components/loading/global';
import DynamicIframe from '@/pages/dynamic-iframe';
import Home from '@/pages/home';
import MenuConfiguration from '@/pages/menu-configuration';
import EdgeConnectionPage from '@/pages/mqtt-auth';
import FleetRuntimePage from '@/pages/fleet-runtime';
import NotPage from '@/pages/not-found-Page';
import NoPermission from '@/pages/not-found-Page/NoPermission';
import NotFoundPage from '@/pages/not-found-Page/NotFoundPage';
import OAuthClients from '@/pages/oauth-clients';
import OpenData from '@/pages/open-data';
import PermissionManagement from '@/pages/permission-management';
import RoutingManagement from '@/pages/routing-management';
import LoginPage from '@/pages/login';
import CliAuthPage from '@/pages/cli-auth';
import McpAuthorizePage from '@/pages/mcp-authorize';
import Share from '@/pages/share';
import SettingsPage from '@/pages/settings';
import Uns from '@/pages/uns';
import Collections from '@/pages/collections';
import DataTopologyPage, { OverviewRedirect } from '@/pages/uns/components/relationship-canvas/page';
import UnsAppPage from '@/pages/uns/app-page';
import DataQualityAlarmsPage from '@/pages/uns-quality-alarms';
import { useBaseStore } from '@/stores/base';
import type { ResourceProps, SystemInfoProps, UserInfoProps } from '@/stores/types.ts';
import {
  getLaunchpadStandaloneLoginPath,
  getUserDefaultHomePath,
  isLaunchpadStandaloneAllowedPath,
  isLaunchpadStandaloneSite,
  withLaunchpadStandaloneQuery,
} from '@/utils/launchpad-site';
import Cookies from 'js-cookie';
import qs from 'qs';
import { lazy, type ReactNode, Suspense, useEffect } from 'react';
import { type Location, Navigate, type RouteObject, useNavigate, useRoutes } from 'react-router';
import DynamicMFComponent from '../components/dynamic-mf-component';

// 根路径重定向到外部login页

const getDefaultPostLoginPath = () =>
  isLaunchpadStandaloneSite() ? withLaunchpadStandaloneQuery('/launchpad') : '/home';
const LaunchpadPage = lazy(() => import('@/pages/launchpad'));
const LaunchpadProjectDetail = lazy(() => import('@/pages/launchpad/project-detail'));
const MiniAppPage = lazy(() => import('@/pages/launchpad/mini-app'));
const LaunchpadAppDetail = lazy(() => import('@/pages/launchpad/app-detail'));
const MessageCenterPage = lazy(() => import('@/pages/message-center'));
const NotificationManageRedirect = () => <Navigate to="/settings/notifications" replace />;

const AdminOnlyPage = ({ children }: { children: ReactNode }) => {
  const isAdmin = useBaseStore((state) => state.currentUserInfo?.superAdmin === true);
  return isAdmin ? children : <NoPermission />;
};

const FleetRuntimeAdminPage = () => {
  const allowed = useBaseStore((state) => capabilityEnabled(state.systemInfo, 'containerConsole'));
  return !allowed ? (
    <Navigate to="/home" replace />
  ) : (
    <AdminOnlyPage>
      <FleetRuntimePage />
    </AdminOnlyPage>
  );
};



const normalizeRedirectUri = (redirectUri?: string | string[] | null) => {
  if (typeof redirectUri !== 'string') {
    return '';
  }
  const next = redirectUri.trim();
  if (!next || !next.startsWith('/')) {
    return '';
  }
  const targetUrl = new URL(next, window.location.origin);
  const pathname = targetUrl.pathname;
  const isStandaloneContext = isLaunchpadStandaloneSite() || isLaunchpadStandaloneSite(pathname, targetUrl.search);
  if (
    next === '/' ||
    next === '/?isLogin=true' ||
    next === '/login' ||
    next === LOGIN_URL ||
    (isStandaloneContext && pathname !== '/' && !isLaunchpadStandaloneAllowedPath(pathname))
  ) {
    return '';
  }
  return isStandaloneContext ? withLaunchpadStandaloneQuery(next) : next;
};

const RootRedirect = () => {
  const { currentUserInfo, systemInfo } = useBaseStore((state) => ({
    currentUserInfo: state.currentUserInfo,
    systemInfo: state.systemInfo,
  }));
  const params = qs.parse(window.location.search, { ignoreQueryPrefix: true });
  useEffect(() => {
    const handleRedirect = async () => {
      if (params?.isLogin) {
        window.location.replace(
          normalizeRedirectUri(getUserDefaultHomePath(currentUserInfo, '')) || getDefaultPostLoginPath()
        );
        return;
      }

      if (currentUserInfo?.homePage) {
        window.location.replace(
          normalizeRedirectUri(getUserDefaultHomePath(currentUserInfo, '')) || getDefaultPostLoginPath()
        );
        return;
      }

      if (Cookies.get(OMC_MODEL)) {
        console.warn('omc——cookie失效');
        window.location.replace('/403');
        return;
      }


      window.location.replace(getLaunchpadStandaloneLoginPath(systemInfo?.loginPath || LOGIN_URL));
    };

    handleRedirect();
  }, [params?.isLogin, currentUserInfo?.homePage, systemInfo?.loginPath]);
  return null;
};

export const childrenRoutes = [
  {
    path: '/uns/topology',
    Component: DataTopologyPage,
    handle: { parentPath: '/uns', code: 'UnsGraph.title', showName: 'UnsGraph.title' },
  },
  { path: '/collections', Component: Collections },
  {
    path: '/uns/apps/:appId',
    Component: UnsAppPage,
    handle: { parentPath: '/uns', multiInstance: true, dynamicTabName: true, showName: 'AppEntry.open' },
  },
  {
    path: '/collections/:collectionId',
    Component: Collections,
    handle: { parentPath: '/collections', code: 'Collections.title' },
  },
  {
    path: '/home',
    Component: Home,
  },
  {
    path: '/runtime',
    Component: FleetRuntimeAdminPage,
    handle: {
      parentPath: '/home',
      code: 'fleet.runtime.title',
    },
  },
  {
    path: '/connect',
    Component: Uns,
    handle: {
      parentPath: '/_common',
      type: 'all',
      code: 'UnsConnect.connect',
      showName: 'UnsConnect.connect',
      dynamicTabName: true,
    },
  },
  {
    path: '/uns',
    Component: Uns,
    handle: { code: 'UnsMeta.namespace', showName: 'UnsMeta.namespace', dynamicTabName: true },
  },
  {
    path: '/uns/data-quality-alarms',
    Component: DataQualityAlarmsPage,
    handle: {
      parentPath: '/uns',
      code: 'dataQuality.alarmWorkbench',
    },
  },
  // {
  //   path: '/app-display',
  //   Component: AppDisplay,
  // },
  // {
  //   path: '/app-iframe',
  //   Component: AppIframe,
  //   handle: {
  //     parentPath: '/app-display',
  //     code: 'route.appIframe',
  //   },
  // },
  // {
  //   path: '/app-space',
  //   Component: AppSpace,
  // },
  // {
  //   path: '/app-gui',
  //   Component: AppGUI,
  //   handle: {
  //     parentPath: '/app-space',
  //     code: 'route.appGUI',
  //   },
  // },
  // {
  //   path: '/app-preview',
  //   Component: AppPreview,
  //   handle: {
  //     parentPath: '/app-space',
  //     code: 'route.appPreview',
  //   },
  // },
  {
    path: '/account-management',
    Component: AccountManagement,
  },
  {
    path: '/settings',
    Component: SettingsPage,
    handle: {
      parentPath: '/_common',
      code: 'common.settings',
      type: 'all',
    },
  },
  {
    path: '/settings/:section',
    Component: SettingsPage,
    handle: {
      parentPath: '/settings',
      code: 'common.settings',
      type: 'all',
    },
  },
  {
    path: '/login-log',
    Component: LoginLogPage,
    handle: { parentPath: '/_common', code: 'route.loginLog', type: 'all' },
  },
  {
    path: '/audit-log',
    Component: AuditLogPage,
    handle: {
      parentPath: '/_common',
      code: 'route.auditLog',
      type: 'all',
    },
  },
  {
    path: '/message-center',
    Component: MessageCenterPage,
    handle: {
      parentPath: '/_common',
      code: 'messageCenter.title',
      type: 'all',
    },
  },
  {
    path: '/notification-manage',
    Component: NotificationManageRedirect,
    handle: {
      parentPath: '/_common',
      code: 'route.notificationManage',
      type: 'all',
    },
  },
  {
    path: '/MenuConfiguration',
    Component: MenuConfiguration,
  },
  {
    path: '/PermissionManagement',
    Component: PermissionManagement,
  },
  {
    path: '/dev-page',
    Component: DevPage,
    handle: {
      showName: 'devPage',
      type: 'all',
    },
  },
  // 插件移到主项目 数据开放
  {
    path: '/OpenData',
    Component: OpenData,
  },
  {
    path: '/oauth-clients',
    Component: OAuthClients,
  },
  {
    path: '/routing-management',
    Component: RoutingManagement,
    handle: {
      parentPath: '/_common',
      code: 'route.routingManagement',
    },
  },
  {
    path: '/403',
    Component: NoPermission,
    handle: {
      parentPath: '/_common',
      showName: '403',
      type: 'all',
    },
  },
  {
    path: '/404',
    element: <NotFoundPage />,
    handle: {
      parentPath: '/_common',
      showName: '404',
      type: 'all',
    },
  },
  {
    path: '/app',
    Component: lazy(() => import('@/pages/native-app')),
    handle: { code: 'nativeApp.title', showName: 'nativeApp.title' },
  },
  {
    path: '/edge-connection',
    Component: EdgeConnectionPage,
    handle: {
      parentPath: '/_common',
      code: 'menu.mqttAuth',
      type: 'all',
      multiInstance: true,
    },
  },
];

export const launchpadChildRoutes = [
  {
    path: '/launchpad',
    Component: LaunchpadPage,
    handle: {
      parentPath: '/_common',
      code: 'Launchpad.title',
      type: 'all',
    },
  },
  {
    path: '/launchpad/:projectName',
    Component: LaunchpadProjectDetail,
    handle: {
      parentPath: '/launchpad',
      code: 'Launchpad.title',
      showName: 'Launchpad.title',
      type: 'all',
      multiInstance: true,
    },
  },
  {
    path: '/launchpad/:projectName/:appName',
    Component: LaunchpadAppDetail,
    handle: {
      parentPath: '/launchpad',
      code: 'Launchpad.title',
      showName: 'Launchpad.title',
      type: 'all',
      multiInstance: true,
    },
  },
];

const launchpadSettingsChildRoutes = [
  {
    path: '/message-center',
    Component: MessageCenterPage,
    handle: {
      parentPath: '/_common',
      code: 'messageCenter.title',
      type: 'all',
    },
  },
  {
    path: '/settings',
    Component: SettingsPage,
    handle: {
      parentPath: '/_common',
      code: 'common.settings',
      type: 'all',
    },
  },
  {
    path: '/settings/:section',
    Component: SettingsPage,
    handle: {
      parentPath: '/settings',
      code: 'common.settings',
      type: 'all',
    },
  },
];

const getStandaloneLaunchpadChildRoutes = () =>
  launchpadChildRoutes.map((route) => {
    if (route.path !== '/launchpad') {
      return route;
    }
    return {
      ...route,
      handle: {
        ...route.handle,
        closable: false,
      },
    };
  });

const getLaunchpadRoutes = () =>
  isLaunchpadStandaloneSite()
    ? [
        {
          path: '/',
          element: <LaunchpadShell />,
          handle: {
            layout: 'launchpad',
          },
          children: [...getStandaloneLaunchpadChildRoutes(), ...launchpadSettingsChildRoutes],
        },
      ]
    : [];

const getMainChildrenRoutes = () => [...childrenRoutes, ...(!isLaunchpadStandaloneSite() ? launchpadChildRoutes : [])];

// 前端路由路径
export const frontendPathList = [
  '/uns/overview',
  '/mini-app/:projectName/:appName',
  ...childrenRoutes.map((item) => item.path),
  ...launchpadChildRoutes.map((item) => item.path),
  ...launchpadSettingsChildRoutes.map((item) => item.path),
];

const getRoutes = () => [
  { path: '/uns/overview', Component: OverviewRedirect },
  {
    path: '/',
    element: <RootRedirect />,
  },
  {
    path: '/mini-app/:projectName/:appName',
    // 独立路由不经过 TabWrapper，需要自行承接异步页面加载。
    element: (
      <Suspense fallback={<GlobalLoading />}>
        <MiniAppPage />
      </Suspense>
    ),
  },
  ...getLaunchpadRoutes(),
  {
    path: '/',
    element: <Layout />,
    handle: {
      layout: 'main',
    },
    children: getMainChildrenRoutes(),
  },
  {
    path: LOGIN_URL,
    Component: LoginPage,
  },
  {
    path: '/cli-auth',
    Component: CliAuthPage,
  },
  {
    path: '/mcp-authorize',
    Component: McpAuthorizePage,
  },
  {
    path: '/share',
    Component: Share,
  },
  {
    path: '*',
    element: <NotPage knownPaths={frontendPathList} />,
  },
];

export const getRoutesDom = ({
  menuGroup,
  systemInfo,
  currentUserInfo,
}: {
  menuGroup: ResourceProps[];
  systemInfo?: SystemInfoProps;
  currentUserInfo?: UserInfoProps;
}) => {
  return getRoutes().map((route) => {
    if ((route.handle as any)?.layout === 'launchpad') {
      return route;
    }
    if (route.children) {
      return {
        ...route,
        children: [
          // 前端路由
          ...((route.children ?? [])
            ?.map((child) => {
              if (!capabilityAllowsPath(systemInfo || {}, child.path)) return null;
              const info = menuGroup?.find((f) => f.isFrontend && child.path === f?.url);
              if (info) {
                return {
                  ...child,
                  handle: {
                    ...child.handle,
                    path: child.path,
                    // code: info?.code,
                    showName: child.path === '/uns' ? 'UnsMeta.namespace' : info?.showName,
                    icon: info?.icon,
                  },
                };
              } else if (child.handle?.parentPath === '/_common') {
                // 开发环境打开方便调试
                if (import.meta.env.DEV) return child;
                if (child.handle?.type === 'all') {
                  return child;
                }
                // 没有正真父级菜单情况
                if (
                  systemInfo?.authEnable &&
                  !currentUserInfo?.pageList?.some((s: any) => s.uri?.toLowerCase?.() === child.path?.toLowerCase?.())
                ) {
                  return null;
                }
                return {
                  ...child,
                  handle: {
                    ...child.handle,
                    path: child.path,
                    code: child.handle?.code ?? child.path,
                  },
                };
              } else if (child.handle?.parentPath) {
                // 没有暴露出去的路由
                return {
                  ...child,
                  handle: {
                    ...child.handle,
                    path: child.path,
                    // multiInstance 路由不设置 code，因为它们的标题来自 location.state.tabName
                    code: (child.handle as any)?.multiInstance ? undefined : (child.handle?.code ?? child.path),
                  },
                };
              } else {
                // 开发环境打开方便调试
                if (import.meta.env.DEV) return child;
                return null;
              }
            })
            ?.filter((f) => f) || []),
          // 后端路由（及前端模块联邦路由）
          ...(menuGroup
            ?.filter(
              (item) =>
                !item.isFrontend && !isRemovedFeatureEntry(item) && capabilityAllowsPath(systemInfo || {}, item.url)
            )
            ?.map((d) => {
              if (!d) return null;
              // 模块联邦-插件及插件子路由
              if (d.isRemote) {
                const path = d?.remoteModelName ? `/${d?.parentCode}/${d?.remoteModelName}` : '/' + d?.code;
                return {
                  path,
                  Component: DynamicMFComponent,
                  handle: {
                    key: d?.code,
                    code: d?.code,
                    showName: d?.showName,
                    icon: d?.icon,
                    path,
                    // 模块联邦子模块
                    moduleName: d?.remoteModelName,
                    parentPath: '/' + d?.parentCode,
                  },
                };
              }
              return {
                path: '/' + d?.code,
                element: <DynamicIframe url={d?.url} name={d?.showName} code={d?.code} />,
                handle: {
                  openType: d?.openType,
                  key: d?.code,
                  code: d?.code,
                  showName: d?.showName,
                  icon: d?.icon,
                  path: '/' + d?.code,
                },
              };
            })
            ?.filter((f) => f) || []),
        ],
      };
    } else {
      return route;
    }
  }) as RouteObject[];
};

export const RoutesElement = ({ routeDom }: { routeDom: RouteObject[] }) => {
  return useRoutes(routeDom);
};

export const useLocationNavigate = () => {
  const navigate = useNavigate();
  return (location: Partial<Location>) => {
    const { pathname, search, state } = location;
    navigate(withLaunchpadStandaloneQuery(pathname + (search ?? '')), { state });
  };
};
