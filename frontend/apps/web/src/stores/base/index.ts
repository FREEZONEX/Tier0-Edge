import { isRemovedFeatureEntry } from '@/utils/removed-feature-pages';
import { capabilityAllowsPath } from '@/utils/product-capabilities';
import { isEdgeEdition, productName } from '@/utils/product-edition';
import { createWithEqualityFn, type UseBoundStoreWithEqualityFn } from 'zustand/traditional';
import type { StoreApi } from 'zustand';
import { shallow } from 'zustand/vanilla/shallow';
import type { DataItem, ResourceProps, UserInfoProps } from '@/stores/types.ts';
import { storageOpt } from '@/utils/storage';
import {
  MENU_TARGET_PATH,
  STORAGE_PATH,
  APP_LANG,
  APP_UNS_TREE,
  APP_USER_TIPS_ENABLE,
} from '@/common-types/constans.ts';
import { getPersonConfigApi } from '@/apis/core-api/uns.ts';
import { getSystemConfig } from '@/apis/core-api/system-config.ts';
import { getUserInfo } from '@/apis/core-api/auth';
import { coreResourcesForResourceKeys, toLegacyResources } from '@/apis/core-api/core-adapter';

import type { TBaseStore } from '@/stores/base/type.ts';
import { defaultLanguage, initI18n, useI18nStore } from '../i18n-store.ts';
import { getRoutesResourceApi } from '@/apis/core-api/resource.ts';
import {
  buildResourceTrees,
  type Criteria,
  filterContainerList,
  filterRouteByUserResource,
  guideConfig,
  handleButtonPermissions,
  mapResource,
  multiGroupByCondition,
} from '../utils.ts';
import { getLangListApi } from '@/apis/core-api/i18n.ts';

/**
 * 获取语言包
 */
export const getLangList = async () => {
  try {
    const langList = await getLangListApi();
    useI18nStore.setState({
      langList: langList,
    });
    return langList;
  } catch (e) {
    console.log(e);
    const langList = [
      {
        hasUsed: true,
        id: 1,
        languageCode: 'zh-CN',
        languageName: '中文（简体）',
        languageType: 1,
        label: '中文（简体）',
        value: 'zh-CN',
      },
      {
        hasUsed: true,
        id: 2,
        languageCode: 'en-US',
        languageName: 'English',
        languageType: 1,
        label: 'English',
        value: 'en-US',
      },
    ];
    useI18nStore.setState({
      langList: langList,
    });
    return langList;
  }
};

const normalizeSystemInfo = (systemInfo: any = {}) => ({
  ...(systemInfo ?? {}),
  productEdition: isEdgeEdition(systemInfo) ? 'edge' : 'enterprise',
  isEdge: isEdgeEdition(systemInfo),
  appTitle: systemInfo?.appTitle || productName(systemInfo),
  qualityName: systemInfo?.qualityName || '_quality',
  timestampName: systemInfo?.timestampName || '_timestamp',
  enableAutoCategorization: true,
});
/**
 * @description: 系统基础store 路由、用户信息、系统信息、当前菜单信息等
 *
 * currentUserInfo: 用户相关信息，包含：用户角色，用户存在的操作权限buttonList、操作资源组buttonGroup 等；
 * 导航路由信息-根据权限显示：menuTree, menuGroup
 * home页路由信息-根据权限显示：homeTree, homeGroup
 * home页Tab: homeTabGroup（暂未控制权限）
 * 原始菜单组（导航的不控制权限 含父级目录）: originMenu
 * 所有按钮组（不控制权限）: allButtonGroup
 * **/
export const initBaseContent = {
  originMenu: [],
  menuTree: [],
  homeTree: [],
  menuGroup: [],
  homeGroup: [],
  homeTabGroup: [],
  allButtonGroup: [],
  currentUserInfo: {},
  systemInfo: { appTitle: '' },
  dataBaseType: [],
  userTipsEnable: storageOpt.getOrigin(APP_USER_TIPS_ENABLE) || '',
  pluginList: [],
  buttonList: [],
  loading: true,
};

export const useBaseStore: UseBoundStoreWithEqualityFn<StoreApi<TBaseStore>> = createWithEqualityFn(
  () => initBaseContent,
  shallow
);

// 设置用户tipsEnable
export const setUserTipsEnable = (value: string) => {
  storageOpt.setOrigin(APP_USER_TIPS_ENABLE, value);
  useBaseStore.setState({
    userTipsEnable: value,
  });
};

const criteria: Criteria<DataItem> = {
  buttonGroup: (item: any) => item?.uri?.includes('button:'),
};

const defaultThemeConfig = {
  general: {
    browserTitle: '',
    appTitle: '',
  },
  dark: {
    logo: '',
    loginSloganImg: '',
    loginBackgroundImg: '',
  },
  light: {
    logo: '',
    loginSloganImg: '',
    loginBackgroundImg: '',
  },
};

const loadAndApplyTheme = async () => {
  try {
    const response = await fetch(`${STORAGE_PATH}${MENU_TARGET_PATH}/theme-config.json`);
    const contentType = response.headers.get('content-type') || '';
    if (!response.ok || !contentType.includes('application/json')) {
      return defaultThemeConfig;
    }
    return await response.json();
  } catch {
    return defaultThemeConfig;
    // 可加载默认主题或保持原样
  }
};

const requestStatus = (result: PromiseSettledResult<any>) => {
  if (result.status === 'fulfilled') {
    return undefined;
  }
  return result.reason?.status || result.reason?.response?.status || result.reason?.code;
};

const resourcesFromUserKeys = (resourceKeys: string[] = []) =>
  toLegacyResources(coreResourcesForResourceKeys(resourceKeys));

const filterFeatureResources = (resources: ResourceProps[], systemInfo: any) =>
  resources.filter(
    (resource) =>
      !isRemovedFeatureEntry(resource) &&
      capabilityAllowsPath(systemInfo, resource.url) &&
      (systemInfo?.dataQualityEnabled === true ||
        !['uns.dataQuality.page', 'uns.dataQuality.read', 'uns.dataQuality.manage'].includes(
          resource.resourceKey || ''
        ))
  );

const loadRoutesResource = async (info: any, systemInfo: any) => {
  let resources: ResourceProps[];
  if (systemInfo?.authEnable === false || info?.superAdmin === true) {
    resources = await getRoutesResourceApi();
  } else {
    resources = resourcesFromUserKeys(info?.resourceKeys || []);
  }
  return filterFeatureResources(resources, systemInfo);
};

// edge版本 用户默认支持所有的权限和菜单
// 更新路由基础方法 (私有)
const updateBaseStore = async (isFirst: boolean = false) => {
  if (isFirst) {
    try {
      // 首次需要先拿到用户信息，再按用户角色决定是否需要完整资源树。
      const [infoResult, systemInfoResult] = await Promise.allSettled([
        getUserInfo(),
        getSystemConfig(),
      ]);
      const info: any = infoResult.status === 'fulfilled' ? infoResult.value : undefined;
      const systemInfo: any = systemInfoResult.status === 'fulfilled' ? systemInfoResult.value : {};
      const resource: any = await loadRoutesResource(info, systemInfo);
      const infoStatus = requestStatus(infoResult);
      const routesStatus = infoStatus === 401 ? 401 : infoStatus;
      systemInfo.themeConfig = await loadAndApplyTheme();
      // 国际化语言包list
      await getLangList();

      // 通过用户的资源池  拿到 - 菜单资源 和 操作资源
      const { buttonGroup: legacyButtonGroup, others } = multiGroupByCondition(info?.resourceList || [], criteria);
      const buttonPermissionList =
        Array.isArray(info?.buttonList) && info.buttonList.length > 0
          ? info.buttonList
          : legacyButtonGroup?.map((i: any) => i.uri) || [];
      const buttonGroup = buttonPermissionList.map((uri: string) => ({ uri }));
      // 整合出用户路由资源组
      const userRoutesResourceList = others || [];
      // 过滤后的路由组,含home\home_tab\menu及目录 去除操作权限
      const allRoutes = filterRouteByUserResource(
        mapResource(resource?.filter((r: ResourceProps) => r.type !== 3)),
        userRoutesResourceList,
        systemInfo?.authEnable && !info?.superAdmin
      );
      // 剔除未启用的路由
      const enableRoutes = allRoutes?.filter((f) => f.enable);
      // 获取终极菜单
      const { homeTree, homeTabGroup, homeGroup, menuGroup, menuTree } = buildResourceTrees(enableRoutes);
      const allButtonGroup = resource?.filter((r: ResourceProps) => r.type === 3);
      const _buttonList =
        systemInfo?.authEnable === false || info?.superAdmin === true
          ? handleButtonPermissions(['button:*'], allButtonGroup) || []
          : handleButtonPermissions(buttonGroup?.map((i: any) => i.uri) || [], allButtonGroup) || [];
      // 储存用户信息
      storageOpt.set('personInfo', {
        username: info?.preferredUsername,
      });
      const containerList = filterContainerList(systemInfo?.containerMap || {});
      // 个人用户设置
      const currentUserInfo = {
        ...info,
        roleList: info?.roleList || [],
        roleString: info?.roleList?.map((i: any) => i.roleName)?.join('/') || '',
        buttonList: buttonGroup?.map((i: any) => i.uri) || [],
        pageList: userRoutesResourceList || [],
        superAdmin: info?.superAdmin,
        buttonGroup,
      };
      const normalizedSystemInfo = normalizeSystemInfo(systemInfo);
      useBaseStore.setState({
        ...initBaseContent,
        homeTree,
        homeTabGroup,
        homeGroup,
        menuGroup,
        menuTree,
        originMenu: resource,
        allButtonGroup,
        // pluginList,
        routesStatus,
        currentUserInfo,
        systemInfo: normalizedSystemInfo,
        containerList,
        buttonList: _buttonList,
        dataBaseType: normalizedSystemInfo?.containerMap?.tdengine?.envMap?.service_is_show
          ? ['tdengine']
          : ['timescale'],
        mqttBrokeType: normalizedSystemInfo?.containerMap?.emqx?.name,
      });
      // 设置新手引导
      guideConfig({ systemInfo: normalizedSystemInfo, menuGroup, info });

      // 设置unsTree信息
      const unsTreeInfo = storageOpt.get(APP_UNS_TREE);
      if (unsTreeInfo) {
        storageOpt.set(APP_UNS_TREE, { ...unsTreeInfo, state: { lazyTree: normalizedSystemInfo?.lazyTree } });
      } else {
        storageOpt.set(APP_UNS_TREE, { state: { lazyTree: normalizedSystemInfo?.lazyTree }, version: 0 });
      }
      // 请求国际化语言
      const _lang = await fetchUserLanguage({
        userId: currentUserInfo?.sub,
        lang: normalizedSystemInfo?.lang,
      });
      // 首次需要初始化语言包
      return await initI18n(_lang);
    } catch (_) {
      console.log(_);
      // 首次需要初始化语言包
      return await initI18n(storageOpt.getOrigin(APP_LANG) || defaultLanguage);
    }
  } else {
    const baseState = useBaseStore.getState();
    // 重新获取菜单
    return loadRoutesResource(baseState?.currentUserInfo, baseState?.systemInfo).then((resource: ResourceProps[]) => {
      const allRoutes = filterRouteByUserResource(
        mapResource(resource.filter((r: ResourceProps) => r.type !== 3)),
        baseState?.currentUserInfo?.pageList,
        baseState.systemInfo?.authEnable && !baseState.currentUserInfo?.superAdmin
      );
      const enableRoutes = allRoutes?.filter((f) => f.enable);
      const { homeTree, homeTabGroup, homeGroup, menuGroup, menuTree } = buildResourceTrees(enableRoutes);
      const allButtonGroup = resource?.filter((r: ResourceProps) => r.type === 3);
      const _buttonList =
        baseState?.systemInfo?.authEnable === false || baseState?.currentUserInfo?.superAdmin === true
          ? handleButtonPermissions(['button:*'], allButtonGroup) || []
          : handleButtonPermissions(
              baseState?.currentUserInfo?.buttonGroup?.map((i: any) => i.uri) || [],
              allButtonGroup
            ) || [];
      useBaseStore.setState({
        homeTree,
        homeTabGroup,
        homeGroup,
        menuGroup,
        menuTree,
        originMenu: resource,
        allButtonGroup,
        buttonList: _buttonList,
      });
      return allRoutes;
    });
  }
};

// 初始化获取baseStore
export const fetchBaseStore = async (isFirst: boolean = false): Promise<any> => {
  return updateBaseStore(isFirst).finally(() => {
    useBaseStore.setState({
      loading: false,
    });
  });
};

// 设置当前菜单信息
export const setCurrentMenuInfo = (data: ResourceProps) => {
  useBaseStore.setState({
    currentMenuInfo: data,
  });
};

// 手动更新用户信息
export const updateForUserInfo = (info: UserInfoProps) => {
  useBaseStore.setState({
    currentUserInfo: {
      ...useBaseStore.getState().currentUserInfo,
      ...info,
    },
  });
};

export const setPluginList = (pluginList: any[]) => {
  useBaseStore.setState({
    pluginList,
  });
};

const getPreferredEnvLanguage = () =>
  import.meta.env.VITE_LANGUAGE ||
  import.meta.env.VITE_OS_LANG ||
  import.meta.env.VITE_APP_LANG ||
  import.meta.env.REACT_APP_LOCAL_LANG ||
  import.meta.env.REACT_APP_OS_LANG;

const fetchUserLanguage = async (info: { userId?: string; lang?: string }) => {
  const { lang, userId } = info;
  try {
    if (!userId) {
      return getPreferredEnvLanguage() || lang || storageOpt.getOrigin(APP_LANG) || defaultLanguage;
    } else {
      const response = await getPersonConfigApi(userId);
      return (
        response.mainLanguage || getPreferredEnvLanguage() || lang || storageOpt.getOrigin(APP_LANG) || defaultLanguage
      );
    }
  } catch (error) {
    console.error('配置请求失败', error);
    return getPreferredEnvLanguage() || lang || storageOpt.getOrigin(APP_LANG) || defaultLanguage;
  }
};

export const fetchLaunchpadBaseStore = async (): Promise<any> => {
  useBaseStore.setState({
    loading: true,
  });

  try {
    const [infoResult, systemInfoResult] = await Promise.allSettled([getUserInfo(), getSystemConfig()]);
    const info: any = infoResult.status === 'fulfilled' ? infoResult.value : undefined;
    const systemInfo: any = systemInfoResult.status === 'fulfilled' ? systemInfoResult.value : {};
    const infoStatus = requestStatus(infoResult);
    const routesStatus = infoStatus === 401 ? 401 : infoStatus;
    const normalizedSystemInfo = normalizeSystemInfo(systemInfo);
    normalizedSystemInfo.themeConfig = await loadAndApplyTheme();

    if (routesStatus === 401) {
      useBaseStore.setState({
        ...initBaseContent,
        routesStatus,
        systemInfo: normalizedSystemInfo,
        loading: false,
      });
      return;
    }
    await getLangList();

    const buttonList = Array.isArray(info?.buttonList) ? info.buttonList : [];
    const currentUserInfo = {
      ...info,
      roleList: info?.roleList || [],
      roleString: info?.roleList?.map((i: any) => i.roleName)?.join('/') || '',
      buttonList,
      pageList: info?.resourceList || [],
      superAdmin: info?.superAdmin,
      buttonGroup: buttonList.map((uri: string) => ({ uri })),
    };
    const lang = await fetchUserLanguage({
      userId: currentUserInfo?.sub,
      lang: normalizedSystemInfo?.lang,
    });
    await initI18n(lang);

    useBaseStore.setState({
      ...initBaseContent,
      routesStatus,
      currentUserInfo,
      systemInfo: normalizedSystemInfo,
      containerList: filterContainerList(normalizedSystemInfo?.containerMap || {}),
      buttonList,
      loading: false,
    });
  } catch (error) {
    console.log(error);
    await initI18n(storageOpt.getOrigin(APP_LANG) || defaultLanguage);
    useBaseStore.setState({
      loading: false,
    });
  }
};

export const fetchSystemInfo = async (fetchRoute?: boolean): Promise<any> => {
  return await getSystemConfig().then((systemInfo) => {
    const normalizedSystemInfo = normalizeSystemInfo(systemInfo);
    const containerList = filterContainerList(systemInfo?.containerMap || {});
    useBaseStore.setState({
      systemInfo: normalizedSystemInfo,
      containerList,
      dataBaseType: normalizedSystemInfo?.containerMap?.tdengine?.envMap?.service_is_show
        ? ['tdengine']
        : ['timescale'],
      mqttBrokeType: normalizedSystemInfo?.containerMap?.emqx?.name,
    });
    if (fetchRoute) {
      return fetchBaseStore?.();
    }
  });
};
