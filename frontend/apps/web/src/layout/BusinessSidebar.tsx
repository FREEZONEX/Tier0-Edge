import { useState } from 'react';
import { PanelLeftClose, PanelLeftOpen } from 'lucide-react';
import { MenuLucideIcon } from '@/components/lucide-icon';
import { useMenuNavigate, useTranslate } from '@/hooks';
import LogoImg from '@/layout/custom-menu-header/components/LogoImg.tsx';
import { isHiddenSidebarMenuResourceKey } from '@/apis/core-api/core-adapter';
import { ButtonPermission } from '@/common-types/button-permission';
import { hasPermission } from '@/utils/auth';
import { useBaseStore } from '@/stores/base';
import { useFleetConnectionAccess } from '@/pages/fleet-management/fleet-permission';
import type { ResourceProps } from '@/stores/types';
import { ThemeType, useThemeStore } from '@/stores/theme-store.ts';
import { useLocation, useNavigate } from 'react-router';
import styles from './business-sidebar.module.scss';

const sidebarGroups = [
  {
    key: 'home',
    titleKey: '',
    match: (item: ResourceProps) => item.resourceKey === 'home.page' || item.url === '/home',
  },
  {
    key: 'uns',
    titleKey: 'sidebar.unifiedNamespace',
    match: (item: ResourceProps) =>
      ['uns.connect.ui', 'uns.page', 'uns.collections.page', 'mqtt.auth.manage'].includes(
        String(item.resourceKey || '')
      ) ||
      ['/uns', '/connect', '/collections', '/uns/data-quality-alarms', '/edge-connection'].includes(
        String(item.url || '')
      ),
  },
  {
    key: 'app',
    titleKey: 'sidebar.app',
    match: (item: ResourceProps) =>
      ['uns.app.read', 'launchpad.view'].includes(String(item.resourceKey || '')) ||
      ['/app', '/launchpad'].includes(String(item.url || '')),
  },
  {
    key: 'anchor',
    titleKey: 'sidebar.anchor',
    match: (item: ResourceProps) =>
      ['anchor.model', 'anchor.scene'].includes(String(item.resourceKey || '')) ||
      ['/anchor/model', '/anchor/scene'].includes(String(item.url || '')),
  },
  {
    key: 'analysis',
    titleKey: 'sidebar.analysis',
    match: (item: ResourceProps) => item.resourceKey === 'vision.camera.page' || item.url === '/vision',
  },
];

const flattenMenuItems = (items: ResourceProps[] = []) => {
  const out: ResourceProps[] = [];
  const walk = (nodes: ResourceProps[]) => {
    nodes
      .slice()
      .sort((a, b) => Number(a.sort || 0) - Number(b.sort || 0))
      .forEach((node) => {
        if (node.type === 2 || node.url) {
          out.push(node);
          return;
        }
        if (node.children?.length) {
          walk(node.children);
        }
      });
  };
  walk(items);
  return out;
};

const menuIdentity = (item: ResourceProps) => `${item.code || ''}|${item.resourceKey || ''}|${item.url || ''}`;

const hiddenBusinessResourceKeys = new Set([
  'iam.user.view',
  'iam.role.view',
  'iam.resource.view',
  'gateway.route.manage',
  'apikey.manage',
  'oauth.client.manage',
  'system.menu.config',
  'system.audit.log',
]);

const BusinessSidebar = () => {
  const navigate = useNavigate();
  const { allowed: edgeAllowed } = useFleetConnectionAccess();
  const location = useLocation();
  const [collapsed, setCollapsed] = useState(false);
  const { menuTree, currentMenuInfo } = useBaseStore((state) => ({
    menuTree: state.menuTree,
    currentMenuInfo: state.currentMenuInfo,
  }));
  const theme = useThemeStore((state) => state.theme);
  const formatMessage = useTranslate();
  useBaseStore((state) => state.buttonList);
  useBaseStore((state) => state.currentUserInfo);
  const handleNavigate = useMenuNavigate();
  const menuLabel = (showName?: string) => formatMessage(showName || '', undefined, showName || '');
  const edgeMenu = flattenMenuItems(menuTree).find((item) => item.url === '/edge-connection');
  const menuItems = flattenMenuItems(menuTree)
    .map((item) => ({ ...item }))
    .filter(
      (item) =>
        !hiddenBusinessResourceKeys.has(String(item.resourceKey || '')) &&
        item.url !== '/edge-connection' &&
        item.url !== '/uns/topology' &&
        item.resourceKey !== 'uns.topology.ui' &&
        !isHiddenSidebarMenuResourceKey(item.resourceKey)
    );
  // 复用已有 UNS 页面权限；派生视图不创建新的后端授权资源。
  const namespace = menuItems.find((item) => item.resourceKey === 'uns.page' || item.url === '/uns');
  if (namespace) namespace.showName = 'UnsMeta.namespace';
  const connectBase = namespace || (edgeAllowed ? edgeMenu : undefined);
  if (connectBase) {
    if (
      edgeAllowed ||
      hasPermission(ButtonPermission['uns.sources']) ||
      hasPermission(ButtonPermission['uns.streams'])
    ) {
      const connect: ResourceProps = {
        ...connectBase,
        id: `${connectBase.id}:connect`,
        code: 'connect',
        resourceKey: 'uns.connect.ui',
        url: '/connect',
        showName: 'UnsConnect.connect',
        icon: undefined,
        children: [],
        isFrontend: true,
        urlType: 1,
      };
      menuItems.splice(namespace ? menuItems.indexOf(namespace) : 0, 0, connect);
    }
  }
  const groupedItems = sidebarGroups.map((group) => ({
    ...group,
    items: menuItems.filter(group.match),
  }));
  const groupedKeys = new Set(groupedItems.flatMap((group) => group.items.map(menuIdentity)));
  const otherItems = menuItems.filter((item) => !groupedKeys.has(menuIdentity(item)));

  const isSelected = (item: ResourceProps) =>
    location.pathname === '/connect'
      ? item.url === '/connect'
      : location.pathname === '/uns'
        ? item.url === '/uns'
        : currentMenuInfo?.code === item.code || item.children?.some((child) => child.code === currentMenuInfo?.code);

  const renderMenuEntry = (item: ResourceProps) => {
    return (
      <button
        type="button"
        key={item.code}
        className={isSelected(item) ? `${styles['menu-item']} ${styles.active}` : styles['menu-item']}
        title={menuLabel(item.showName)}
        onClick={() => handleNavigate(item)}
      >
        <MenuLucideIcon item={item} size={18} className={styles['menu-icon']} />
        <span>{menuLabel(item.showName)}</span>
      </button>
    );
  };

  const renderGroup = (group: { key: string; titleKey: string; items: ResourceProps[] }) => {
    if (!group.items.length) return null;
    return (
      <div className={styles['menu-group']} key={group.key}>
        {!collapsed && group.titleKey ? (
          <div className={styles['group-title']}>{formatMessage(group.titleKey)}</div>
        ) : null}
        <div className={styles['group-items']}>{group.items.map(renderMenuEntry)}</div>
      </div>
    );
  };

  return (
    <aside className={collapsed ? `${styles['business-sidebar']} ${styles.collapsed}` : styles['business-sidebar']}>
      <div className={styles['sidebar-logo']}>
        <LogoImg
          isDark={theme === ThemeType.Dark}
          onClick={() => {
            navigate('/home');
          }}
          width={collapsed ? 28 : undefined}
        />
        <button
          type="button"
          className={styles['collapse-trigger']}
          onClick={() => setCollapsed((value) => !value)}
          aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        >
          {collapsed ? (
            <PanelLeftOpen size={16} strokeWidth={1.75} aria-hidden />
          ) : (
            <PanelLeftClose size={16} strokeWidth={1.75} aria-hidden />
          )}
        </button>
      </div>
      <nav className={styles['business-menu']}>
        {groupedItems.map(renderGroup)}
        {otherItems.length ? renderGroup({ key: 'other', titleKey: 'sidebar.other', items: otherItems }) : null}
      </nav>
    </aside>
  );
};

export default BusinessSidebar;
