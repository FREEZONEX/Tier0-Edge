import { useLocalStorage, useMediaSize, useMenuNavigate, useTranslate } from '@/hooks';
import { Close, Menu as MenuIcon, TreeView as TreeViewIcon, User } from '@/components/lucide-icon/carbon';
import { Divider, Drawer, Menu } from 'antd';
import { useEffect, useState } from 'react';
import { useLocation } from 'react-router';
// import HelpNav from '../components/HelpNav';
import ComGroupButton from '@/components/com-group-button';
import { MenuLucideIcon } from '@/components/lucide-icon';
import NotificationCenter from '@/components/notification-center';
import SearchSelect from '@/components/search-select';
import { useBaseStore } from '@/stores/base';
import { useUnsTreeMapContext } from '@/UnsTreeMapContext';
import { isInIframe } from '@/utils/url-util.ts';
import './index.scss';

const CustomMenuHeader = () => {
  const location = useLocation();
  const { currentMenuInfo, menuTree } = useBaseStore((state) => ({
    currentMenuInfo: state.currentMenuInfo,
    menuTree: state.menuTree,
  }));
  const isUnsPath = location.pathname.includes('/uns');
  const { isTreeMapVisible, setTreeMapVisible } = useUnsTreeMapContext();
  const handleNavigate = useMenuNavigate();
  const [drawerVisible, setDrawerVisible] = useState(false);
  const { width, isH5 } = useMediaSize();
  const formatMessage = useTranslate();
  const ignoreHeader = useLocalStorage('ignoreHeader');
  const menuLabel = (showName?: string) => formatMessage(showName || '', undefined, showName || '');

  useEffect(() => {
    // 66rem = 1056px (1rem = 16px)
    if (width && width >= 640) {
      setDrawerVisible(false);
    }
  }, [width]);

  const items = menuTree?.map?.((parent) => {
    if (parent.children?.length && parent.type !== 2) {
      return {
        icon: <MenuLucideIcon item={parent} size={24} style={{ paddingRight: 4, verticalAlign: 'middle' }} />,
        popupClassName: 'custom-menu-popover',
        key: parent.code!,
        label: <span className="menu-label">{menuLabel(parent.showName)}</span>,
        children: parent?.children?.map((child) => ({
          key: child.code!,
          icon: <MenuLucideIcon item={child} size={24} style={{ paddingRight: 4, verticalAlign: 'middle' }} />,
          onClick: () => {
            handleNavigate(child);
          },
          label: <span className="menu-label">{menuLabel(child.showName)}</span>,
        })),
      };
    } else {
      return {
        icon: <MenuLucideIcon item={parent} size={24} style={{ paddingRight: 4, verticalAlign: 'middle' }} />,
        popupClassName: 'custom-menu-popover',
        key: parent.code!,
        label: <span className="menu-label">{menuLabel(parent.showName)}</span>,
        onClick: () => {
          handleNavigate(parent);
        },
      };
    }
  });
  // const handleTodoClick = (e: any) => {
  //   navigate(e.key);
  // };
  return (
    <div
      className="custom-menu-header"
      style={{
        color: 'var(--ui-bg-color)',
        display: ignoreHeader === 'true' || window.name === 'equipment_app' ? 'none' : 'flex',
      }}
    >
      {/* 新手导航使用id */}
      <div className="custom-menu-header-left" id="custom_menu_left">
        <div className="header" style={{ color: 'var(--ui-text-color)' }}>
          <div className="menu-toggle" style={{ display: isH5 ? 'flex' : 'none' }}>
            {drawerVisible ? (
              <Close size={20} style={{ color: 'var(--ui-text-color)' }} onClick={() => setDrawerVisible(false)} />
            ) : (
              <MenuIcon size={20} style={{ color: 'var(--ui-text-color)' }} onClick={() => setDrawerVisible(true)} />
            )}
          </div>
          {/*<span className="title" title={currentMenuInfo?.showName}>*/}
          {/*  {currentMenuInfo?.showName}*/}
          {/*</span>*/}
          {isH5 ? <Divider style={{ height: 24 }} type="vertical" /> : null}
        </div>
        <div className="content" style={{ display: !isH5 ? 'flex' : 'none' }}>
          {/*渲染tabs header的div*/}
          <div className="tabs" id="custom-header-container"></div>
        </div>
      </div>
      <div className="footer" id="custom_menu_right">
        {isUnsPath && isH5 ? (
          <ComGroupButton
            options={[
              {
                label: (
                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      gap: '4px',
                    }}
                  >
                    <TreeViewIcon size={20} style={{ color: 'var(--ui-text-color)' }} />
                    <span style={{ color: 'var(--ui-text-color)' }}>{formatMessage('uns.tree')}</span>
                    {isTreeMapVisible && <Close size={20} style={{ color: 'var(--ui-text-color)' }} />}
                  </div>
                ),
                title: 'treemap',
                key: 'treemap',
                style: {
                  width: '128px',
                  ...(isTreeMapVisible && {
                    boxShadow: '-2px -2px 4px rgba(0, 0, 0, 0.1)',
                  }),
                },
                onClick: () => {
                  console.log(isTreeMapVisible);
                  setTreeMapVisible(!isTreeMapVisible);
                },
              },
              {
                label: <NotificationCenter />,
                title: formatMessage('messageCenter.title', undefined, 'Message Center'),
                key: 'notifications',
              },
            ]}
          />
        ) : (
          <ComGroupButton
            options={[
              {
                label: (
                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      height: '100%',
                      color: 'var(--ui-text-color)',
                    }}
                  >
                    <SearchSelect />
                  </div>
                ),
                noHoverStyle: true,
                title: 'search',
                key: 'search',
                style: { width: 'auto', padding: '0' },
              },
              // {
              //   label: <HelpNav />,
              //   key: 'help',
              // },
              // {
              //   label: <Task size={20} style={{ color: 'var(--ui-text-color)' }} />,
              //   title: formatMessage('common.taskCenter'),
              //   key: 'todo',
              //   onClick: handleTodoClick,
              // },
              {
                label: <NotificationCenter />,
                title: formatMessage('messageCenter.title', undefined, 'Message Center'),
                key: 'notifications',
              },
              {
                label: <User size={20} style={{ color: 'var(--ui-text-color)' }} />,
                title: 'user',
                key: 'user',
              },
              // {
              //   auth: ButtonPermission['common.routerEdit'],
              //   label: <Edit size={20} style={{ color: 'var(--ui-text-color)' }} />,
              //   title: formatMessage('common.edit', 'Edit'),
              //   key: 'edit',
              //   onClick: () => setEditOpen(true),
              // },
              // {
              //   label: (
              //     <img
              //       src={themeStore.theme.includes('dark') ? menuChangeDark : menuChange}
              //       style={{
              //         width: 20,
              //         height: 20,
              //       }}
              //     />
              //   ),
              //   key: 'change',
              //   title: formatMessage('common.change', 'change'),
              //   onClick: () => themeStore.setMenuType(MenuTypeEnum.Fixed),
              // },
            ]?.filter((i) => i.key !== 'user' || (i.key === 'user' && !isInIframe([], 'webview')))}
          />
        )}
      </div>

      <Drawer
        className="custom-menu-header-drawer"
        rootClassName="custom-menu-header-drawer-root"
        placement="left"
        mask={false}
        // autoFocus={false}
        onClose={() => setDrawerVisible(false)}
        open={drawerVisible}
        width={256}
      >
        <Menu mode="inline" items={items} selectedKeys={currentMenuInfo?.code ? [currentMenuInfo?.code] : []} />
      </Drawer>
    </div>
  );
};

export default CustomMenuHeader;
