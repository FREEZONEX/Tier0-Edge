import { type ReactNode, useRef, useState } from 'react';
import { useLocation } from 'react-router';
import { useMatchRoute, useTranslate } from '@/hooks';
import type { Location } from 'react-router';
import { getRoutesDom, useLocationNavigate } from '@/routers';
import { compareLocations } from '@/utils/compare';
import { useCreation, useDeepCompareEffect, useMemoizedFn } from 'ahooks';
import { useBaseStore } from '@/stores/base';
import { useI18nStore } from '@/stores/i18n-store.ts';
import { formatShowName } from '@/utils';

export interface KeepAliveTab {
  title: ReactNode;
  fullTitle?: string;
  contextTitle?: string;
  routePath: string;
  key: string;
  tabKey: string;
  pathname: string;
  icon?: any;
  children: any;
  location: Location;
  // 是否支持多实例tab
  multiInstance?: boolean;
  // 是否允许通过 location.state.tabName 更新页签标题
  dynamicTabName?: boolean;
  // 是否允许关闭页签
  closable?: boolean;
}

export function getKeepAliveTabKey(tab: Pick<KeepAliveTab, 'multiInstance' | 'pathname' | 'routePath' | 'tabKey'>) {
  return tab.tabKey || (tab.multiInstance ? tab.pathname : tab.routePath);
}

function getKey() {
  return new Date().getTime().toString();
}

const initActive = {
  isActive: false,
  pathName: '',
};

export function useTabs() {
  const navigate = useLocationNavigate();
  const { menuGroup, systemInfo, currentUserInfo } = useBaseStore((state) => ({
    menuGroup: state.menuGroup,
    systemInfo: state.systemInfo,
    currentUserInfo: state.currentUserInfo,
  }));
  const lang = useI18nStore((state) => state.lang);
  // 存放页面记录
  const [keepAliveTabs, setKeepAliveTabs] = useState<KeepAliveTab[]>([]);
  // 当前激活的tab
  const [activeTabRoutePath, setActiveTabRoutePath] = useState<string>('');
  // 主动操作
  const isActiveOpt = useRef(initActive);
  const formatMessage = useTranslate();
  const location = useLocation();

  const matchRoute = useMatchRoute();

  useDeepCompareEffect(() => {
    if (!matchRoute) return;
    const matchTabKey = matchRoute.tabKey || matchRoute.routePath;
    if (
      !isActiveOpt.current?.isActive &&
      (!isActiveOpt.current?.pathName || isActiveOpt.current?.pathName !== matchTabKey)
    ) {
      const existKeepAliveTab = keepAliveTabs.find((o) => getKeepAliveTabKey(o) === matchTabKey);
      if (!existKeepAliveTab) {
        const activeTabIndex = keepAliveTabs.findIndex((o) => getKeepAliveTabKey(o) === activeTabRoutePath);
        const activeTab = keepAliveTabs[activeTabIndex];
        const shouldReplaceActiveTab = Boolean(
          matchRoute.location.state?.replaceCurrentTab && activeTabIndex >= 0 && activeTab?.multiInstance
        );
        if (shouldReplaceActiveTab) {
          setKeepAliveTabs((prev) =>
            prev.map((tab, index) =>
              index === activeTabIndex
                ? {
                    ...matchRoute,
                    key: getKey(),
                    location: {
                      ...matchRoute.location,
                      state: {
                        ...matchRoute.location.state,
                        replaceCurrentTab: undefined,
                      },
                    },
                  }
                : tab
            )
          );
        } else {
          // 带父级定位的页面紧跟父标签插入，用轻量顺序表达页面归属关系。
          setKeepAliveTabs((prev) => {
            const nextTab = {
              ...matchRoute,
              key: getKey(),
            };
            const parentTabPathname = matchRoute.location.state?.tabParentPathname;
            if (!parentTabPathname) return [...prev, nextTab];

            const parentIndex = prev.findIndex((tab) => tab.pathname === parentTabPathname);
            if (parentIndex < 0) return [...prev, nextTab];

            return [...prev.slice(0, parentIndex + 1), nextTab, ...prev.slice(parentIndex + 1)];
          });
        }
      } else if (!existKeepAliveTab.children) {
        // 如果tab相同，但是children为空，说明重缓存中加载的数据，我们只需要刷新当前页签并且把children设置为新的children
        setKeepAliveTabs((prev) => {
          const index = (prev || []).findIndex((tab) => getKeepAliveTabKey(tab) === matchTabKey);
          if (index >= 0 && prev) {
            return prev.map((tab, i) => (i === index ? { ...tab, key: getKey(), children: matchRoute.children } : tab));
          }
          return [...(prev || [])];
        });
      } else if (existKeepAliveTab && !compareLocations(matchRoute.location, existKeepAliveTab.location, ['key'])) {
        // 处理location - 创建新对象以确保 React 检测到变化
        // 保留已有的 tabName，避免返回导航时丢失标签名称
        setKeepAliveTabs((prev) => {
          const index = (prev || []).findIndex((tab) => getKeepAliveTabKey(tab) === matchTabKey);
          if (index >= 0 && prev) {
            const existingTab = prev[index];
            // 单 tab 路由在参数变化时会复用同一个缓存实例。
            // 这里同步 children；当 pathname 变化时重置 key，确保路由节点重挂载。
            const shouldResetKey = existingTab.location?.pathname !== matchRoute.location.pathname;
            const shouldKeepExistingTabName =
              matchRoute.dynamicTabName && (!shouldResetKey || matchRoute.multiInstance);
            const mergedLocation = {
              ...matchRoute.location,
              state: {
                ...matchRoute.location.state,
                tabName:
                  matchRoute.location.state?.tabName ||
                  (shouldKeepExistingTabName ? existingTab.location?.state?.tabName : undefined),
                tabContextName:
                  matchRoute.location.state?.tabContextName ||
                  (shouldKeepExistingTabName ? existingTab.location?.state?.tabContextName : undefined),
                tabParentPathname:
                  matchRoute.location.state?.tabParentPathname || existingTab.location?.state?.tabParentPathname,
              },
            };
            const updatedTabs = prev.map((tab, i) =>
              i === index
                ? {
                    ...tab,
                    ...matchRoute,
                    location: mergedLocation,
                    children: matchRoute.children,
                    tabKey: matchTabKey,
                    key: shouldResetKey ? getKey() : tab.key,
                  }
                : tab
            );
            const parentTabPathname = mergedLocation.state.tabParentPathname;
            if (!parentTabPathname) return updatedTabs;

            const updatedTab = updatedTabs[index];
            const tabsWithoutUpdated = updatedTabs.filter((_, i) => i !== index);
            const parentIndex = tabsWithoutUpdated.findIndex((tab) => tab.pathname === parentTabPathname);
            if (parentIndex < 0) return updatedTabs;

            return [
              ...tabsWithoutUpdated.slice(0, parentIndex + 1),
              updatedTab,
              ...tabsWithoutUpdated.slice(parentIndex + 1),
            ];
          }
          return [...(prev || [])];
        });
      }
    }
    setActiveTabRoutePath(matchTabKey);
    isActiveOpt.current = initActive;
  }, [matchRoute, location.pathname, location.search, location.key]);

  // 关闭tab
  const onCloseTab = useMemoizedFn((routePath: string = activeTabRoutePath || '') => {
    if (!keepAliveTabs?.length) {
      return;
    }
    const index = (keepAliveTabs || []).findIndex(
      (o) => getKeepAliveTabKey(o) === routePath || o.routePath === routePath
    );
    if (index === -1) return;
    if (keepAliveTabs[index]?.closable === false) return;

    const tabKey = getKeepAliveTabKey(keepAliveTabs[index]);
    let _location: any;
    if (tabKey === activeTabRoutePath && keepAliveTabs.length > 1) {
      let nextTab: KeepAliveTab;
      if (index > 0) {
        nextTab = keepAliveTabs[index - 1];
      } else {
        nextTab = keepAliveTabs[index + 1];
      }
      _location = nextTab.location;
      navigate(_location);
      isActiveOpt.current = {
        isActive: true,
        pathName: getKeepAliveTabKey(nextTab),
      };
    }
    keepAliveTabs.splice(index, 1);
    setKeepAliveTabs([...keepAliveTabs]);
  });

  const onRemoveTab = useMemoizedFn((routePath: string = activeTabRoutePath || '') => {
    if (!keepAliveTabs?.length) {
      return;
    }
    const index = (keepAliveTabs || []).findIndex(
      (o) => getKeepAliveTabKey(o) === routePath || o.routePath === routePath
    );
    if (index === -1) return;

    keepAliveTabs.splice(index, 1);
    setKeepAliveTabs([...keepAliveTabs]);
  });

  // 刷新tab
  const onRefreshTab = useMemoizedFn((routePath: string = activeTabRoutePath || '') => {
    setKeepAliveTabs((prev) => {
      const index = (prev || []).findIndex(
        (tab) => getKeepAliveTabKey(tab) === routePath || tab.routePath === routePath
      );
      if (index >= 0 && prev) {
        prev[index].key = getKey();
      }
      return [...(prev || [])];
    });
  });

  // 关闭除了自己其它tab
  const onCloseOtherTab = useMemoizedFn((routePath: string = activeTabRoutePath || '') => {
    if (!keepAliveTabs?.length) {
      return;
    }
    const tab = keepAliveTabs.find((o) => getKeepAliveTabKey(o) === routePath || o.routePath === routePath);
    const fixedTabs = keepAliveTabs.filter((o) => o.closable === false && getKeepAliveTabKey(o) !== routePath);
    if (!tab && !fixedTabs.length) return;
    const toCloseTabs = tab ? [...fixedTabs, tab] : fixedTabs;

    setKeepAliveTabs(toCloseTabs);
    const { location } = tab || fixedTabs[0] || {};
    if (location) {
      navigate(location);
    } else {
      navigate({ pathname: tab?.pathname || routePath });
    }
    isActiveOpt.current = {
      isActive: true,
      pathName: tab ? getKeepAliveTabKey(tab) : routePath,
    };
  });
  const tabs = useCreation(() => {
    const childrenRoutes = getRoutesDom({ menuGroup, systemInfo, currentUserInfo }).flatMap(
      (route) => route.children || []
    );
    return keepAliveTabs?.map((o) => {
      const info = childrenRoutes?.find((f) => f.path === o.routePath);

      if (o.dynamicTabName && o.location?.state?.tabName) {
        const tabName = String(o.location.state.tabName);
        const contextTitle = o.location.state.tabContextName ? String(o.location.state.tabContextName) : undefined;
        const tabNameFull = String(
          o.location.state.tabNameFull || (contextTitle ? `${contextTitle} / ${tabName}` : tabName)
        );
        return {
          ...o,
          title: tabName,
          fullTitle: tabNameFull,
          contextTitle,
        };
      }

      return {
        ...o,
        title: info
          ? formatShowName({
              code: (info?.handle as any)?.code,
              showName: (info?.handle as any)?.showName,
              formatMessage,
              finallyShowName: typeof o.title === 'string' ? o.title : undefined,
            })
          : o.title,
      };
    });
  }, [keepAliveTabs, lang, menuGroup, systemInfo, currentUserInfo]);
  return {
    tabs,
    setTabs: setKeepAliveTabs,
    activeTabRoutePath,
    onCloseTab,
    onRemoveTab,
    onRefreshTab,
    onCloseOtherTab,
  };
}
