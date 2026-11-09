import { useState } from 'react';
import { Flex } from 'antd';
import { ChevronDown, User } from '@/components/lucide-icon/carbon';
import { productLogo, productName } from '@/utils/product-edition';
import SideNavList from './components/SideNavList';
import SideMenuList from './components/SideMenuList';
import menuChange from '@/assets/icons/menu-change.svg';
import menuChangeDark from '@/assets/icons/menu-change-dark.svg';
import menuDown from '@/assets/icons/menu-down.svg';
import menuLightUp from '@/assets/icons/menu-light-up.svg';
import upDark from '@/assets/icons/up-dark.svg';
import downDark from '@/assets/icons/down-dark.svg';
import HelpNav from '../components/HelpNav';
import './index.scss';
import UserPopover from '@/components/com-group-button/UserPopover';
import DraggableContainer from '@/components/draggable-container';
import SearchSelect from '@/components/search-select';
import { useTranslate } from '@/hooks';
import { useBaseStore } from '@/stores/base';
import { MenuTypeEnum, setMenuType, ThemeType, useThemeStore } from '@/stores/theme-store';

const Module = () => {
  const systemInfo = useBaseStore((state) => state.systemInfo);
  const { menuTree, currentMenuInfo } = useBaseStore((state) => ({
    menuTree: state.menuTree,
    currentMenuInfo: state.currentMenuInfo,
  }));
  const theme = useThemeStore((state) => state.theme);
  const isDark = theme === ThemeType.Dark;
  const [openHoverNav, setOpenHoverNav] = useState(false);
  const [showAllNav, setShowAllNav] = useState(false);
  const [searchOpen, setSearchOpen] = useState(true);
  const formatMessage = useTranslate();
  const currentMenuName = formatMessage(currentMenuInfo?.showName || '', undefined, currentMenuInfo?.showName || '');

  return (
    <DraggableContainer>
      {/*  <div className={`navWrapFixed navWrapLight ${isdark != 'light' ? 'navWrapDark' : 'navWrapLight'}`}> */}
      <div className={`navWrapFixed navWrapLight`}>
        <div
          className="navWrap"
          style={{
            height: showAllNav ? 'calc(100vh - 20px)' : '46px',
            width: searchOpen ? '280px' : '400px',
          }}
        >
          <div className="navTop">
            <div
              className="imgWrap"
              style={{
                opacity: openHoverNav ? 0.2 : 1,
                '--color-font': 'var(--ui-text-color)',
              }}
              onClick={() => {
                setOpenHoverNav((pre) => !pre);
                setShowAllNav(false);
                setSearchOpen(true);
              }}
            >
              <img src={productLogo(systemInfo, isDark)} alt={productName(systemInfo)} />
              <span style={{ margin: '0 5px' }} title={currentMenuName}>
                {currentMenuName}
              </span>
              <ChevronDown />
            </div>
            <Flex style={{ height: '100%' }}>
              <div
                className="navTopIcon"
                onClick={() => {
                  setShowAllNav((pre) => !pre);
                  setOpenHoverNav(false);
                  setSearchOpen(true);
                }}
              >
                {isDark ? (
                  <img src={showAllNav ? upDark : downDark} />
                ) : (
                  <img src={showAllNav ? menuLightUp : menuDown} />
                )}
              </div>
              <div style={{ cursor: 'pointer' }}>
                <SearchSelect value={searchOpen} onChange={setSearchOpen} selectStyle={{ height: 44 }} />
              </div>
              <div className="navTopIcon" onClick={() => setMenuType(MenuTypeEnum.Top)}>
                <img
                  src={isDark ? menuChangeDark : menuChange}
                  style={{
                    width: 20,
                    height: 20,
                  }}
                />
              </div>
            </Flex>
          </div>
          <div className="navContent">
            <SideNavList navList={menuTree} selectedKeys={currentMenuInfo?.code ? [currentMenuInfo?.code] : []} />
          </div>
          <div className="navBottom">
            <div className="iconWrap">
              {/*<div*/}
              {/*  className="iconWrapper"*/}
              {/*  onClick={() => {*/}
              {/*    goPath('/setting');*/}
              {/*  }}*/}
              {/*>*/}
              {/*  <Settings size={18} />*/}
              {/*</div>*/}
              <div className="iconWrapper" style={{ padding: 0 }}>
                <HelpNav />
              </div>
              <UserPopover zIndex={10000} placement={'top'}>
                <div className="iconWrapper">
                  <User size={18} />
                </div>
              </UserPopover>
            </div>
          </div>
        </div>
        {!showAllNav && openHoverNav && (
          <div className="navHoverContent">
            <SideMenuList
              openHoverNav={openHoverNav}
              navList={menuTree}
              selectedKeys={currentMenuInfo?.code ? [currentMenuInfo?.code] : []}
              setOpenHoverNav={setOpenHoverNav}
            />
          </div>
        )}
      </div>
    </DraggableContainer>
  );
};
export default Module;
