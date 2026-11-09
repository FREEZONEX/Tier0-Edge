import { useMemo, type ReactNode } from 'react';
import { Api, CloudServices, User } from '@carbon/icons-react';
import AccountManagement from '@/pages/account-management';
import AuditLogPage from '@/pages/audit-log';
import LoginLogPage from '@/pages/login-log';
import MenuConfiguration from '@/pages/menu-configuration';
import NotificationManage from '@/pages/notification-manage';
import OAuthClients from '@/pages/oauth-clients';
import OpenData from '@/pages/open-data';
import SystemConfiguration from '@/pages/system-configuration';
import { useTranslate } from '@/hooks';
import { useBaseStore } from '@/stores/base';
import { isLaunchpadStandaloneSite, withLaunchpadStandaloneQuery } from '@/utils/launchpad-site';
import { useLocation, useNavigate } from 'react-router';
import AccountSettingsPanel from './AccountSettingsPanel';
import NoPermission from '@/pages/not-found-Page/NoPermission';
import styles from './index.module.scss';

type SettingsItem = {
  key: string;
  labelKey: string;
  labelFallback?: string;
  path: string;
  resourceKey?: string;
  superAdminOnly?: boolean;
  accessUris?: string[];
  icon?: ReactNode;
  render: (title: string) => ReactNode;
};

const hiddenSettingsItemKeys = new Set(['permission', 'routing', 'menu']);

const canAccess = (item: SettingsItem, pageUris: string[] = [], superAdmin?: boolean, authEnable?: boolean) => {
  if (!item.resourceKey || superAdmin || authEnable === false) return true;
  const allowed = new Set([item.resourceKey, item.path, ...(item.accessUris || [])].map((uri) => uri.toLowerCase()));
  return pageUris.some((uri) => allowed.has(uri.toLowerCase()));
};

const SettingsPage = () => {
  const formatMessage = useTranslate();
  const navigate = useNavigate();
  const location = useLocation();
  const { currentUserInfo, systemInfo } = useBaseStore((state) => ({
    currentUserInfo: state.currentUserInfo,
    systemInfo: state.systemInfo,
  }));
  const pageUris = currentUserInfo?.pageList?.map((item) => (item as any).uri || item.url || '') || [];
  const groups = useMemo(
    () => [
      {
        key: 'user',
        titleKey: 'settings.user',
        icon: <User size={16} />,
        items: [
          {
            key: 'profile',
            labelKey: 'account.profile',
            path: '/settings/profile',
            render: () => <AccountSettingsPanel activeTab="profile" />,
          },
          {
            key: 'preferences',
            labelKey: 'settings.preferences',
            path: '/settings/preferences',
            render: () => <AccountSettingsPanel activeTab="preferences" />,
          },
          {
            key: 'security',
            labelKey: 'settings.security',
            path: '/settings/security',
            render: () => <AccountSettingsPanel activeTab="security" />,
          },
        ] as SettingsItem[],
      },
      {
        key: 'service',
        titleKey: 'settings.service',
        icon: <Api size={16} />,
        items: [
          {
            key: 'api-keys',
            labelKey: 'menu.apiKey',
            path: '/settings/api-keys',
            resourceKey: 'apikey.manage',
            accessUris: ['/OpenData'],
            render: () => <OpenData />,
          },
        ] as SettingsItem[],
      },
      {
        key: 'platform',
        titleKey: 'settings.platform',
        icon: <CloudServices size={16} />,
        items: [
          {
            key: 'system-configuration',
            labelKey: 'settings.systemConfig',
            path: '/settings/system-configuration',
            superAdminOnly: true,
            render: (title) => <SystemConfiguration title={title} />,
          },
          {
            key: 'users',
            labelKey: 'UserManagement',
            path: '/settings/users',
            resourceKey: 'iam.user.view',
            accessUris: ['/account-management'],
            render: (title) => <AccountManagement title={title} />,
          },
          {
            key: 'permission',
            labelKey: 'PermissionManagement.title',
            path: '/settings/permissions',
            resourceKey: 'iam.resource.view',
            accessUris: ['/PermissionManagement'],
            render: () => null,
          },
          {
            key: 'menu',
            labelKey: 'MenuConfiguration.menu',
            path: '/settings/menu',
            resourceKey: 'system.menu.config',
            accessUris: ['/MenuConfiguration'],
            render: (title) => <MenuConfiguration title={title} />,
          },
          {
            key: 'oauth',
            labelKey: 'menu.oauthClient',
            path: '/settings/oauth-clients',
            resourceKey: 'oauth.client.manage',
            accessUris: ['/oauth-clients'],
            render: () => <OAuthClients />,
          },
          {
            key: 'audit',
            labelKey: 'route.auditLog',
            path: '/settings/audit-log',
            resourceKey: 'system.audit.log',
            accessUris: ['/audit-log'],
            render: () => <AuditLogPage />,
          },
          {
            key: 'notifications',
            labelKey: 'route.notificationManage',
            path: '/settings/notifications',
            resourceKey: 'notify.manage',
            accessUris: ['/notification-manage'],
            render: () => (
              <div className={styles['settings-notifications-page']}>
                <NotificationManage />
              </div>
            ),
          },
          {
            key: 'loginLog',
            labelKey: 'route.loginLog',
            path: '/settings/login-log',
            resourceKey: 'system.login.log',
            accessUris: ['/login-log'],
            render: () => <LoginLogPage />,
          },
          {
            key: 'routing',
            labelKey: 'route.routingManagement',
            path: '/settings/routing',
            resourceKey: 'gateway.route.manage',
            accessUris: ['/routing-management'],
            render: () => null,
          },
        ] as SettingsItem[],
      },
    ],
    []
  );
  const isLaunchpadStandalone = isLaunchpadStandaloneSite(location.pathname, location.search);
  const sourceGroups = isLaunchpadStandalone ? groups.filter((group) => group.key === 'user') : groups;
  const canAccessItem = (item: SettingsItem) =>
    (!item.superAdminOnly || currentUserInfo?.superAdmin) &&
    canAccess(item, pageUris, currentUserInfo?.superAdmin, systemInfo?.authEnable);
  const requestedItem = sourceGroups
    .flatMap((group) => group.items)
    .find(
      (item) =>
        location.pathname.toLowerCase() === item.path.toLowerCase() ||
        location.pathname.toLowerCase().startsWith(`${item.path.toLowerCase()}/`)
    );
  if (requestedItem && !canAccessItem(requestedItem)) {
    return <NoPermission />;
  }
  const visibleGroups = sourceGroups
    .map((group) => ({
      ...group,
      items: group.items.filter((item) => !hiddenSettingsItemKeys.has(item.key) && canAccessItem(item)),
    }))
    .filter((group) => group.items.length);
  const allItems = visibleGroups.flatMap((group) => group.items);
  const activeItem =
    allItems.find((item) => location.pathname.toLowerCase() === item.path.toLowerCase()) ||
    allItems.find((item) => location.pathname.toLowerCase().startsWith(`${item.path.toLowerCase()}/`)) ||
    allItems[0];
  const platformItemKeys = new Set(groups.find((group) => group.key === 'platform')?.items.map((item) => item.key));
  const isPlatformItem = activeItem?.key ? platformItemKeys.has(activeItem.key) : false;
  const pageClassName = isLaunchpadStandalone
    ? `${styles['settings-page']} ${styles['settings-page-standalone']}`
    : styles['settings-page'];

  return (
    <div className={pageClassName}>
      <aside className={styles['settings-nav']}>
        {visibleGroups.map((group) => (
          <div className={styles['nav-group']} key={group.key}>
            <div className={styles['group-title']}>
              {group.icon}
              <span>{formatMessage(group.titleKey)}</span>
            </div>
            {group.items.map((item) => (
              <button
                type="button"
                key={item.key}
                className={item.key === activeItem?.key ? `${styles['nav-item']} ${styles.active}` : styles['nav-item']}
                onClick={() => navigate(withLaunchpadStandaloneQuery(item.path))}
              >
                {formatMessage(item.labelKey, {}, item.labelFallback)}
              </button>
            ))}
          </div>
        ))}
      </aside>
      <main className={styles['settings-content']}>
        <div className={styles['content-inner']}>
          <div
            className={
              activeItem?.resourceKey || isPlatformItem
                ? `${styles['embedded-page']} ${isPlatformItem ? styles['platform-embedded-page'] : ''}`
                : `${styles['embedded-page']} ${styles['settings-account-page']}`
            }
          >
            {activeItem?.render(formatMessage(activeItem.labelKey, {}, activeItem.labelFallback))}
          </div>
        </div>
      </main>
    </div>
  );
};

export default SettingsPage;
