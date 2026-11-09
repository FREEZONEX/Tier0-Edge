import type { ContainerItemProps, ResourceProps, SystemInfoProps, UserInfoProps } from '@/stores/types.ts';

export type TBaseStore = {
  // 菜单树
  menuTree: ResourceProps[];
  // home页树
  homeTree: ResourceProps[];
  // 菜单分组
  menuGroup: ResourceProps[];
  // home页分组
  homeGroup: ResourceProps[];
  // home页tab分组
  homeTabGroup: ResourceProps[];
  // 原始菜单
  originMenu: ResourceProps[];
  // 按钮信息
  allButtonGroup: ResourceProps[];
  // 路由状态，401控制
  routesStatus?: number;
  // 用户信息集合
  currentUserInfo: UserInfoProps;
  // 系统信息集合
  systemInfo: SystemInfoProps;
  // 关于我们
  containerList?: {
    aboutUs?: ContainerItemProps[];
  };
  // 数据库类型 TimescaleDB tdEngine
  dataBaseType: string[];
  // mqtt broke类型 emqx gmqtt
  mqttBrokeType?: string;
  // 当前菜单信息
  currentMenuInfo?: ResourceProps;
  // 用户是否启用tips
  userTipsEnable: string;
  loading: boolean;
  // 插件列表
  pluginList: any[];
  // 按钮权限列表
  buttonList: string[];
  // license 授权的模块开关；接口失败或字段缺失时为 undefined，按「不限制」处理
};
