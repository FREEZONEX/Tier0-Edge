import { isRemovedFeatureEntry } from '@/utils/removed-feature-pages';
import { ApiWrapper } from '@/utils/request';
import type { ResourceProps } from '@/stores/types';
import { ButtonPermission } from '@/common-types/button-permission';

export const coreApi = new ApiWrapper('/api/core');
export const authApi = new ApiWrapper('/api/core/auth');
export const iamApi = new ApiWrapper('/api/core/iam');

export type CoreResource = {
  id: number | string;
  parentId?: number | string;
  resourceKey: string;
  name: string;
  type?: number;
  routePath?: string;
  urlType?: number;
  openType?: number;
  icon?: string;
  sort?: number;
  systemGenerated?: number;
  actions?: {
    id: number | string;
    actionType: string;
    actionValue: string;
    methods?: string;
    enabled?: number;
  }[];
  children?: CoreResource[];
};

const resourceNodeId = (resourceKey: string) => `resource:${resourceKey}`;

const routeOverrides: Record<string, string> = {
  'uns.app.read': '/app',
  'iam.user.view': '/settings/users',
  'iam.role.view': '/settings/users',
  'iam.role.delete': '/settings/users',
  'iam.resource.view': '/settings/permissions',
  'gateway.route.manage': '/settings/routing',
  'gateway.route.delete': '/settings/routing',
  'gateway.route.test': '/settings/routing',
  'apikey.manage': '/settings/api-keys',
  'oauth.client.manage': '/settings/oauth-clients',
  'cluster.edge.manage': '/edge-connection',
  'home.page': '/home',
  'uns.page': '/uns',
  'uns.overview.page': '/uns/overview',
  'uns.collections.page': '/collections',
  'uns.dataQuality.page': '/uns/data-quality-alarms',
  'uns.read': '/uns',
  'uns.write': '/uns',
  'uns.delete': '/uns',
  'flow.runtime.view': '/flow',
  'flow.runtime.edit': '/flow',
  'flow.collection.page': '/flow',
  'flow.read': '/flow',
  'flow.manage': '/flow',
  'flow.event.page': '/flow',
  'notebook.view': '/notebook',
  'mqtt.auth.manage': '/edge-connection',
  'mqtt.auth.write': '/edge-connection',
  'mqtt.auth.delete': '/edge-connection',
  'system.menu.config': '/settings/menu',
  'system.audit.log': '/settings/audit-log',
  'notify.manage': '/settings/notifications',
  'system.login.log': '/settings/login-log',
  'launchpad.view': '/launchpad',
  'project.view': '/project',
  'asset.file.read': '/OpenData',
  'asset.file.upload': '/OpenData',
  'vision.camera.page': '/vision',
  'vision.camera.read': '/vision',
  'vision.camera.manage': '/vision',
};

const codeOverrides: Record<string, string> = {
  'uns.app.read': 'nativeApp.title',
  uns: 'menu.uns',
  app: 'sidebar.app',
  analysis: 'sidebar.analysis',
  flow: 'common.flow',
  system: 'menu.system',
  'openapi.base': 'resource.openapi.base',
  'gateway.route.manage': 'route.routingManagement',
  'gateway.route.create': 'resource.gateway.route.create',
  'gateway.route.update': 'resource.gateway.route.update',
  'gateway.route.delete': 'route.routingManagement',
  'gateway.route.test': 'route.routingManagement',
  'apikey.manage': 'menu.apiKey',
  'apikey.create': 'resource.apikey.create',
  'apikey.update': 'resource.apikey.update',
  'apikey.delete': 'resource.apikey.delete',
  'oauth.client.manage': 'menu.oauthClient',
  'cluster.edge.manage': 'mqttAuth.edgeNode',
  'home.page': 'common.home',
  'uns.page': 'route.uns',
  'uns.overview.page': 'UnsOverview.title',
  'uns.collections.page': 'Collections.title',
  'uns.dataQuality.page': 'dataQuality.alarmWorkbench',
  'uns.read': 'route.uns',
  'uns.write': 'route.uns',
  'uns.delete': 'route.uns',
  'flow.runtime.view': 'route.collectionFlow',
  'flow.runtime.edit': 'route.collectionFlow',
  'flow.collection.page': 'route.collectionFlow',
  'flow.read': 'route.collectionFlow',
  'flow.manage': 'route.collectionFlow',
  'flow.event.page': 'route.eventFlow',
  'notebook.view': 'Notebook.title',
  'notebook.read': 'resource.notebook.read',
  'notebook.manage': 'resource.notebook.manage',
  'mqtt.auth.manage': 'menu.mqttAuth',
  'mqtt.auth.write': 'menu.mqttAuth',
  'mqtt.auth.delete': 'menu.mqttAuth',
  'system.menu.config': 'MenuConfiguration.menu',
  'system.menu.create': 'resource.system.menu.create',
  'system.menu.update': 'resource.system.menu.update',
  'system.menu.delete': 'resource.system.menu.delete',
  'iam.role.view': 'UserManagement',
  'iam.role.create': 'resource.iam.role.create',
  'iam.role.update': 'resource.iam.role.update',
  'iam.role.delete': 'UserManagement',
  'iam.resource.view': 'PermissionManagement.title',
  'iam.resource.create': 'resource.iam.resource.create',
  'iam.resource.update': 'resource.iam.resource.update',
  'iam.resource.delete': 'resource.iam.resource.delete',
  'system.audit.log': 'route.auditLog',
  'system.login.log': 'route.loginLog',
  'launchpad.view': 'Launchpad.title',
  'project.view': 'route.project',
  vision: 'sidebar.vision',
  'vision.camera.page': 'sidebar.vision',
  'vision.camera.read': 'resource.vision.camera.read',
  'vision.camera.manage': 'resource.vision.camera.manage',
};

const iconOverrides: Record<string, string> = {
  'uns.app.read': 'Project',
  system: 'menu.tag.system',
  app: 'menu.tag.appspace',
  analysis: 'menu.tag.connections',
  'iam.user.view': 'UserManagement',
  'iam.role.view': 'UserManagement',
  'iam.role.create': 'UserManagement',
  'iam.role.update': 'UserManagement',
  'iam.role.delete': 'UserManagement',
  'iam.resource.view': 'Authentication',
  'gateway.route.manage': 'RoutingManagement',
  'gateway.route.create': 'RoutingManagement',
  'gateway.route.update': 'RoutingManagement',
  'gateway.route.delete': 'RoutingManagement',
  'gateway.route.test': 'RoutingManagement',
  'apikey.manage': 'OpenData',
  'apikey.create': 'OpenData',
  'apikey.update': 'OpenData',
  'oauth.client.manage': 'Authentication',
  'cluster.edge.manage': 'SystemCascade',
  'home.page': 'Home',
  'system.menu.config': 'MenuConfiguration',
  'system.menu.create': 'MenuConfiguration',
  'system.menu.update': 'MenuConfiguration',
  'system.audit.log': 'Logs',
  'system.login.log': 'Authentication',
  uns: 'menu.tag.uns',
  'uns.page': 'Namespace',
  'uns.overview.page': 'Namespace',
  'uns.dataQuality.page': 'Logs',
  'uns.read': 'Namespace',
  'uns.write': 'Namespace',
  'uns.delete': 'Namespace',
  flow: 'menu.tag.connections',
  'flow.collection.page': 'SourceFlow',
  'flow.event.page': 'EventFlow',
  'notebook.view': 'Marimo',
  'notebook.read': 'Marimo',
  'notebook.manage': 'Marimo',
  'anchor.dir': 'menu.tag.anchor',
  'anchor.model': 'AnchorModel',
  'anchor.scene': 'AnchorScene',
  'mqtt.auth.manage': 'SystemCascade',
  'mqtt.auth.write': 'SystemCascade',
  'mqtt.auth.delete': 'SystemCascade',
  'project.view': 'Project',
  'launchpad.view': 'Launchpad',
  'iam.resource.create': 'Authentication',
  'iam.resource.update': 'Authentication',
  vision: 'menu.tag.vision',
  'vision.camera.page': 'VisionCamera',
  'vision.camera.read': 'VisionCamera',
  'vision.camera.manage': 'VisionCamera',
};

const nameOverrides: Record<string, string> = {
  'uns.app.read': 'nativeApp.title',
  uns: 'menu.uns',
  app: 'sidebar.app',
  analysis: 'sidebar.analysis',
  'uns.page': 'route.uns',
  'uns.overview.page': 'UnsOverview.title',
  'uns.collections.page': 'Collections.title',
  'uns.dataQuality.page': 'dataQuality.alarmWorkbench',
  'uns.read': 'resource.uns.read',
  'uns.write': 'resource.uns.write',
  'uns.delete': 'resource.uns.delete',
  'uns.manage': 'resource.uns.manage',
  flow: 'common.flow',
  'flow.collection.page': 'route.collectionFlow',
  'flow.read': 'resource.flow.read',
  'flow.manage': 'resource.flow.manage',
  'flow.event.page': 'route.eventFlow',
  'notebook.view': 'Notebook.title',
  'notebook.read': 'resource.notebook.read',
  'notebook.manage': 'resource.notebook.manage',
  'anchor.dir': 'menu.anchor',
  'anchor.model': 'menu.anchorModel',
  'anchor.scene': 'menu.anchorScene',
  'project.view': 'route.project',
  'project.manage': 'resource.project.manage',
  'launchpad.view': 'Launchpad.title',
  system: 'menu.system',
  'openapi.base': 'resource.openapi.base',
  'gateway.route.manage': 'route.routingManagement',
  'gateway.route.create': 'resource.gateway.route.create',
  'gateway.route.update': 'resource.gateway.route.update',
  'gateway.route.delete': 'resource.gateway.route.delete',
  'gateway.route.test': 'resource.gateway.route.test',
  'iam.user.view': 'UserManagement',
  'iam.role.view': 'UserManagement.role_setting',
  'iam.role.create': 'resource.iam.role.create',
  'iam.role.update': 'resource.iam.role.update',
  'iam.role.delete': 'resource.iam.role.delete',
  'apikey.manage': 'menu.apiKey',
  'apikey.create': 'resource.apikey.create',
  'apikey.update': 'resource.apikey.update',
  'apikey.delete': 'resource.apikey.delete',
  'system.menu.config': 'MenuConfiguration.menu',
  'system.menu.create': 'resource.system.menu.create',
  'system.menu.update': 'resource.system.menu.update',
  'system.menu.delete': 'resource.system.menu.delete',
  'system.audit.log': 'route.auditLog',
  'system.login.log': 'route.loginLog',
  'iam.resource.view': 'PermissionManagement.title',
  'iam.resource.create': 'resource.iam.resource.create',
  'iam.resource.update': 'resource.iam.resource.update',
  'iam.resource.delete': 'resource.iam.resource.delete',
  'oauth.client.manage': 'menu.oauthClient',
  'cluster.edge.manage': 'mqttAuth.edgeNode',
  'home.page': 'common.home',
  'mqtt.auth.manage': 'menu.mqttAuth',
  'mqtt.auth.write': 'resource.mqtt.auth.write',
  'mqtt.auth.delete': 'resource.mqtt.auth.delete',
  'asset.file.upload': 'resource.asset.file.upload',
  'asset.file.read': 'resource.asset.file.read',
  'asset.file.delete': 'resource.asset.file.delete',
  vision: 'sidebar.vision',
  'vision.camera.page': 'sidebar.vision',
  'vision.camera.read': 'resource.vision.camera.read',
  'vision.camera.manage': 'resource.vision.camera.manage',
};

const menuNameOverrides: Record<string, string> = {
  'uns.app.read': 'nativeApp.title',
  uns: 'menu.tag.uns',
  app: 'sidebar.app',
  analysis: 'sidebar.analysis',
  'uns.page': 'Namespace',
  'uns.dataQuality.page': 'dataQuality.alarmWorkbench',
  flow: 'menu.tag.connections',
  'flow.collection.page': 'common.flow',
  'flow.event.page': 'EventFlow',
  'notebook.view': 'Notebook',
  'anchor.dir': 'menu.anchor',
  'anchor.model': 'menu.anchorModel',
  'anchor.scene': 'menu.anchorScene',
  'project.view': 'Project',
  'launchpad.view': 'Launchpad',
  system: 'menu.tag.system',
  'gateway.route.manage': 'RoutingManagement',
  'iam.user.view': 'UserManagement',
  'apikey.manage': 'API Key',
  'system.menu.config': 'MenuConfiguration',
  'system.audit.log': 'AuditLog',
  'system.login.log': 'LoginLog',
  'oauth.client.manage': 'OAuth Client',
  'home.page': 'common.home',
  'mqtt.auth.manage': 'menu.mqttAuth',
  vision: 'sidebar.vision',
  'vision.camera.page': 'sidebar.vision',
};

const parentOverrides: Record<string, string> = {
  'uns.app.read': 'app',
  'uns.app.manage': 'uns.app.read',
  'uns.app.credentials': 'uns.app.read',
  'uns.page': 'uns',
  'uns.dataQuality.page': 'uns',
  'uns.read': 'uns.page',
  'uns.write': 'uns.page',
  'uns.delete': 'uns.page',
  'uns.dataQuality.read': 'uns.dataQuality.page',
  'uns.dataQuality.manage': 'uns.dataQuality.page',
  'flow.collection.page': 'uns',
  'flow.event.page': 'uns',
  'flow.read': 'flow.collection.page',
  'flow.manage': 'flow.collection.page',
  'notebook.view': 'analysis',
  'notebook.read': 'notebook.view',
  'notebook.manage': 'notebook.view',
  'project.view': 'app',
  'launchpad.view': 'app',
  'gateway.route.manage': 'system',
  'gateway.route.create': 'gateway.route.manage',
  'gateway.route.update': 'gateway.route.manage',
  'gateway.route.delete': 'gateway.route.manage',
  'gateway.route.test': 'gateway.route.manage',
  'iam.user.view': 'system',
  'iam.role.view': 'iam.user.view',
  'iam.role.create': 'iam.role.view',
  'iam.role.update': 'iam.role.view',
  'iam.role.delete': 'iam.role.view',
  'apikey.manage': 'system',
  'system.menu.config': 'system',
  'system.audit.log': 'system',
  'system.login.log': 'system',
  'iam.resource.view': 'system',
  'oauth.client.manage': 'system',
  'cluster.edge.manage': 'mqtt.auth.manage',
  'mqtt.auth.manage': 'uns',
  'mqtt.auth.write': 'mqtt.auth.manage',
  'mqtt.auth.delete': 'mqtt.auth.manage',
  'vision.camera.page': 'analysis',
  'vision.camera.read': 'vision.camera.page',
  'vision.camera.manage': 'vision.camera.page',
};

const sortOverrides: Record<string, number> = {
  'uns.app.read': 10,
  uns: 10,
  app: 20,
  analysis: 30,
  'home.page': 5,
  'uns.page': 10,
  'uns.dataQuality.page': 20,
  'uns.read': 10,
  'uns.write': 20,
  'uns.delete': 30,
  'uns.dataQuality.read': 10,
  'uns.dataQuality.manage': 20,
  flow: 20,
  'flow.collection.page': 10,
  'flow.event.page': 20,
  'flow.read': 30,
  'flow.manage': 40,
  'notebook.view': 45,
  'notebook.read': 10,
  'notebook.manage': 20,
  'anchor.dir': 25,
  'anchor.model': 10,
  'anchor.scene': 20,
  'project.view': 30,
  'launchpad.view': 40,
  system: 60,
  'gateway.route.manage': 10,
  'gateway.route.create': 10,
  'gateway.route.update': 20,
  'gateway.route.delete': 30,
  'gateway.route.test': 40,
  'iam.user.view': 20,
  'iam.role.view': 10,
  'iam.role.create': 20,
  'iam.role.update': 30,
  'iam.role.delete': 40,
  'apikey.manage': 30,
  'system.menu.config': 40,
  'system.audit.log': 50,
  'system.login.log': 51,
  'iam.resource.view': 60,
  'oauth.client.manage': 55,
  'cluster.edge.manage': 30,
  'mqtt.auth.manage': 50,
  'mqtt.auth.write': 10,
  'mqtt.auth.delete': 20,
  vision: 27,
  'vision.camera.page': 5,
  'vision.camera.read': 10,
  'vision.camera.manage': 20,
};

const menuResourceKeys = new Set([
  'uns.app.read',
  'iam.user.view',
  'gateway.route.manage',
  'apikey.manage',
  'home.page',
  'uns.page',
  'uns.dataQuality.page',
  'flow.collection.page',
  'flow.event.page',
  'mqtt.auth.manage',
  'notebook.view',
  'oauth.client.manage',
  'iam.resource.view',
  'system.menu.config',
  'system.audit.log',
  'system.login.log',
  'launchpad.view',
  'project.view',
  'anchor.model',
  'anchor.scene',
  'app',
  'analysis',
  'vision.camera.page',
]);

const nonNavigationResourceKeys = new Set([
  'app.management',
  'plugin.management',
  'app.space',
  'app.space.view',
  'appspace.view',
  'app.store',
  'app.market',
  'iam.role.view',
  'iam.role.create',
  'iam.role.update',
  'iam.role.delete',
  'openapi.base',
  'project.manage',
  'apikey.create',
  'apikey.update',
  'apikey.delete',
  'system.menu.create',
  'system.menu.update',
  'system.menu.delete',
  'iam.resource.create',
  'iam.resource.update',
  'iam.resource.delete',
  'asset.file.upload',
  'asset.file.read',
  'asset.file.delete',
]);

const retiredResourceKeys = new Set([
  'project.view',
  'project.manage',
  'flow',
  'vision',
  'app.management',
  'plugin.management',
  'app.space',
  'app.space.view',
  'appspace.view',
  'app.store',
  'app.market',
  'uns.browse',
  'uns.search',
  'uns.create',
  'uns.update',
  'uns.restore',
  'uns.attachment.read',
  'uns.attachment.manage',
  'flow.list',
  'flow.write',
  'flow.event.view',
  'flow.event.edit',
  'dashboard.view',
  'openapi.mock.ping',
  'mock.permission',
  'dev.tools',
  'dev.advanced.use',
  'gateway.route.write',
  'iam.role.write',
  'apikey.write',
  'system.menu.write',
  'iam.resource.write',
  'anchor',
]);

const hiddenMenuResourceKeys = new Set([
  // 对象关系图从 Data topology 切换进入。
  'uns.overview.page',
  // 集合管理从 UNS 内进入；保留页面权限与历史链接。
  'uns.collections.page',
  // 暂不展示独立告警菜单；保留 UNS 内的数据质量配置和检测权限。
  'uns.dataQuality.page',
  'flow',
  'vision',
  'flow.event.page',
  'cluster.edge.manage',
  'gateway.route.manage',
  'iam.resource.view',
  'system.menu.config',
  'iam.user.view',
  'apikey.manage',
  'oauth.client.manage',
  'system.audit.log',
  'notify',
  'notify.manage',
  'notify.message.center',
  'system.login.log',
]);

/** 不应出现在侧边栏 / 顶栏导航的菜单资源（权限仍保留，仅从导航树剔除） */
export const isHiddenSidebarMenuResourceKey = (resourceKey?: string | null) =>
  hiddenMenuResourceKeys.has(String(resourceKey || '').trim());

const virtualContainerResourceKeys = new Set(['app', 'analysis']);

const makeVirtualResource = (key: string): CoreResource => ({
  id: key,
  parentId: parentOverrides[key],
  resourceKey: key,
  name: nameOverrides[key] || key,
  type: 1,
  routePath: '',
  icon: iconOverrides[key] || '',
  sort: sortOverrides[key] || 0,
  actions: [],
});

const withVirtualParentResources = (resources: CoreResource[]) => {
  const byKey = new Map(resources.map((item) => [item.resourceKey, item]));
  const addParent = (key?: string) => {
    const parentKey = String(key || '');
    if (!parentKey || byKey.has(parentKey) || !virtualContainerResourceKeys.has(parentKey)) return;
    byKey.set(parentKey, makeVirtualResource(parentKey));
    addParent(parentOverrides[parentKey]);
  };
  resources.forEach((item) => addParent(parentOverrides[item.resourceKey]));
  return Array.from(byKey.values());
};

const buttonActions = Array.from(
  new Set([
    ...Object.values(ButtonPermission).map((code) => `button:${code}`),
    'button:uns.read',
    'button:uns.delete',
    'button:uns.manage',
    'button:flow.deploy',
    'button:project.manage',
    'button:gateway.manage',
    'button:gateway.route.manage',
    'button:gateway.route.create',
    'button:gateway.route.update',
    'button:gateway.route.delete',
    'button:gateway.route.test',
    'button:mqtt.auth.manage',
    'button:mqtt.auth.write',
    'button:mqtt.auth.delete',
    'button:apikey.manage',
    'button:apikey.create',
    'button:apikey.update',
    'button:apikey.delete',
    'button:system.menu.create',
    'button:system.menu.update',
    'button:system.menu.delete',
    'button:iam.resource.create',
    'button:iam.resource.update',
    'button:iam.resource.delete',
  ])
);

// 与后端独立资源对齐，不能被下面的 Namespace 旧读权限兼容规则扩大授权。
const integrationButtonResources: Record<string, string> = {
  [ButtonPermission['uns.agentView']]: 'unsagent.view',
  [ButtonPermission['uns.sources']]: 'uns.source',
  [ButtonPermission['uns.sourceManage']]: 'uns.source.manage',
  [ButtonPermission['uns.sourceCredentials']]: 'uns.source.credentials',
  [ButtonPermission['uns.streams']]: 'uns.stream',
  [ButtonPermission['uns.streamManage']]: 'uns.stream.manage',
  [ButtonPermission['uns.streamDeploy']]: 'uns.stream.deploy',
  [ButtonPermission['uns.streamExecute']]: 'uns.stream.execute',
  [ButtonPermission['uns.appManage']]: 'uns.app.manage',
  [ButtonPermission['uns.appCredentials']]: 'uns.app.credentials',
  [ButtonPermission['StreamRequests.manage']]: 'stream.requests.manage',
};

const buttonParentResourceKey = (code: string) => {
  if (Object.prototype.hasOwnProperty.call(integrationButtonResources, code)) {
    return integrationButtonResources[code];
  }
  if (code === 'Namespace.metadataWrite') return 'uns.write';
  if (code === 'Namespace.metadataDelete') return 'uns.delete';
  if (code === 'uns.read' || code === 'uns.delete' || code === 'uns.manage') {
    return code;
  }
  if (code === 'uns.write') return 'uns.manage';
  if (code.includes('_delete')) return 'uns.delete';
  if (code.includes('_add') || code.includes('_detail')) return 'uns.manage';
  if (code.includes('_copy') || code.includes('_paste')) return 'uns.manage';
  if (code.startsWith('Namespace.') || code.startsWith('Home.')) return 'uns.read';
  if (code === 'SourceFlow.node_management' || code === 'EventFlow.node_management') return 'flow.read';
  if (code.startsWith('SourceFlow.')) return 'flow.manage';
  if (code.startsWith('EventFlow.')) return 'flow.manage';
  if (code === 'Notebook.manage' || code.startsWith('Notebook.')) return 'notebook.manage';
  if (code === 'RoutingManagement.add') return 'gateway.route.create';
  if (code === 'RoutingManagement.edit') return 'gateway.route.update';
  if (code === 'RoutingManagement.delete') return 'gateway.route.delete';
  if (code === 'RoutingManagement.test') return 'gateway.route.test';
  if (
    code === 'gateway.route.create' ||
    code === 'gateway.route.update' ||
    code === 'gateway.route.delete' ||
    code === 'gateway.route.test'
  ) {
    return code;
  }
  if (code === 'gateway.manage' || code === 'gateway.route.manage') {
    return 'gateway.route.manage';
  }
  if (
    code === 'MqttAuth.add' ||
    code === 'MqttAuth.reset' ||
    code === 'MqttAuth.enable' ||
    code === 'MqttAuth.disable'
  ) {
    return 'mqtt.auth.write';
  }
  if (code === 'MqttAuth.delete') return 'mqtt.auth.delete';
  if (code === 'mqtt.auth.write' || code === 'mqtt.auth.delete') {
    return code;
  }
  if (code === 'mqtt.auth.manage') return 'mqtt.auth.manage';
  if (code === 'UserManagement.role_setting') return 'iam.role.view';
  if (code.startsWith('UserManagement.')) return 'iam.user.view';
  if (code === 'apikey.manage' || code === 'apikey.create' || code === 'apikey.update' || code === 'apikey.delete') {
    return code;
  }
  if (code === 'system.menu.create' || code === 'system.menu.update' || code === 'system.menu.delete') {
    return code;
  }
  if (code === 'iam.resource.create' || code === 'iam.resource.update' || code === 'iam.resource.delete') {
    return code;
  }
  if (code === 'MenuConfiguration.addMenu') return 'system.menu.create';
  if (code === 'MenuConfiguration.deleteMenu') return 'system.menu.delete';
  if (code.startsWith('MenuConfiguration.')) return 'system.menu.update';
  if (code === 'dataopen.delete') return 'apikey.delete';
  if (code === 'dataopen.addKey') return 'apikey.create';
  if (code.startsWith('dataopen.')) return 'apikey.update';
  if (code.startsWith('Project.')) return 'project.manage';
  if (code.startsWith('Vision.camera.')) return 'vision.camera.manage';
  return '';
};

const isNavigationResource = (item: CoreResource) => {
  if (nonNavigationResourceKeys.has(item.resourceKey)) return false;
  return item.type === 2 || item.type === 4 || item.type === 5 || menuResourceKeys.has(item.resourceKey);
};

const routeForResource = (item: CoreResource) => {
  if (!isNavigationResource(item)) return '';
  return routeOverrides[item.resourceKey] || item.routePath || '';
};

const isExternalRoute = (url?: string, urlType?: number) => {
  const value = String(url || '').trim();
  return urlType === 2 || /^https?:\/\//i.test(value) || value.startsWith('//');
};

export const resourceNameMessageKey = (resourceKey?: string) => nameOverrides[String(resourceKey || '')] || '';

export const buttonUrisForResourceKeys = (resources: CoreResource[] = [], keys: string[] = []) => {
  const keySet = new Set(keys);
  const flat = flattenResources(resources);
  const configuredButtons = flat
    .filter((item) => keySet.has(item.resourceKey))
    .flatMap((item) => item.actions || [])
    .filter(
      (action) =>
        action.enabled !== 0 && action.actionType === 'ui' && String(action.actionValue || '').startsWith('button:')
    )
    .map((action) => action.actionValue);
  const granted = new Set(flat.filter((item) => keySet.has(item.resourceKey)).map((item) => item.resourceKey));
  const fallbackButtons = buttonActions.filter((uri) =>
    granted.has(buttonParentResourceKey(uri.replace(/^button:/, '')))
  );
  return Array.from(new Set([...configuredButtons, ...fallbackButtons]));
};

export const flattenResources = (resources: CoreResource[] = []): CoreResource[] => {
  const out: CoreResource[] = [];
  const walk = (items: CoreResource[]) => {
    items.forEach((item) => {
      out.push(item);
      if (item.children?.length) {
        walk(item.children);
      }
    });
  };
  walk(resources);
  return out;
};

export const coreResourcesForResourceKeys = (keys: string[] = []): CoreResource[] => {
  const keySet = new Set<string>();
  const addKey = (key?: string) => {
    const normalized = String(key || '').trim();
    if (!normalized || retiredResourceKeys.has(normalized)) return;
    keySet.add(normalized);
    const parentKey = parentOverrides[normalized];
    if (parentKey) addKey(parentKey);
  };
  keys.forEach(addKey);
  return Array.from(keySet).map((key) => {
    const type =
      key === 'uns' || key === 'flow' || key === 'system' || key === 'app' || key === 'analysis'
        ? 1
        : menuResourceKeys.has(key)
          ? 2
          : 3;
    return {
      id: key,
      parentId: parentOverrides[key],
      resourceKey: key,
      name: nameOverrides[key] || key,
      type,
      routePath: routeOverrides[key] || '',
      icon: iconOverrides[key] || '',
      sort: sortOverrides[key] || 0,
      actions: [],
    };
  });
};

export const toLegacyResource = (
  item: CoreResource,
  parentKeyById: Map<string, string> = new Map(),
  options: ToLegacyResourcesOptions = {}
): ResourceProps => {
  const key = item.resourceKey;
  const url = routeForResource(item);
  const parentKey = key === 'home.page' ? '' : parentOverrides[key] || parentKeyById.get(String(item.parentId || ''));
  const parentId = parentKey ? resourceNodeId(parentKey) : undefined;
  const isMenu = Boolean(url) && isNavigationResource(item);
  const legacyUrl = isMenu ? url : '';
  const legacyUrlType = isMenu && isExternalRoute(legacyUrl, item.urlType) ? 2 : 1;
  const legacyType = item.type === 1 ? 1 : item.type === 4 ? 4 : item.type === 5 && isMenu ? 5 : isMenu ? 2 : 3;
  const hasCoreResourceId = Number.isFinite(Number(item.id));
  return {
    id: resourceNodeId(key),
    coreResourceId: String(item.id),
    resourceKey: key,
    parentId,
    type: legacyType,
    icon: item.icon || iconOverrides[key] || '',
    code: codeOverrides[key] || key,
    showName: (isMenu ? menuNameOverrides[key] : undefined) || nameOverrides[key] || item.name,
    sort: sortOverrides[key] ?? Number(item.sort || 0),
    url: legacyUrl,
    urlType: legacyUrlType,
    openType: Number(item.openType || 0),
    enable: options.includeHiddenMenus === true || !hiddenMenuResourceKeys.has(key),
    isFrontend: Boolean(legacyUrl) && legacyUrlType === 1,
    homeEnable: ['/uns', '/home', '/project', '/launchpad', '/edge-connection', '/vision'].includes(legacyUrl),
    editEnable: hasCoreResourceId && item.systemGenerated !== 1,
  };
};

type ToLegacyResourcesOptions = {
  includeActionButtons?: boolean;
  includeHiddenMenus?: boolean;
};

export const toLegacyResources = (resources: CoreResource[] = [], options: ToLegacyResourcesOptions = {}) => {
  const flatResources = withVirtualParentResources(flattenResources(resources)).filter((item) => {
    if (retiredResourceKeys.has(item.resourceKey)) return false;
    if (isRemovedFeatureEntry({ resourceKey: item.resourceKey, url: routeForResource(item) })) return false;
    if (
      options.includeActionButtons === false &&
      options.includeHiddenMenus !== true &&
      hiddenMenuResourceKeys.has(item.resourceKey)
    ) {
      return false;
    }
    return true;
  });
  const parentKeyById = new Map<string, string>();
  flatResources.forEach((item) => parentKeyById.set(String(item.id), item.resourceKey));
  const mapped = flatResources
    .map((item) => toLegacyResource(item, parentKeyById, options))
    .filter((item) => item.type === 1 || item.type === 2 || item.type === 3 || item.type === 4 || item.type === 5);
  if (options.includeActionButtons === false) {
    return mapped;
  }

  const legacyByResourceKey = new Map<string, ResourceProps>();
  flatResources.forEach((item) =>
    legacyByResourceKey.set(item.resourceKey, toLegacyResource(item, parentKeyById, options))
  );

  const buttonParentId = (resource: CoreResource) => {
    let parentId = String(resource.parentId || '');
    while (parentId) {
      const parent = flatResources.find((item) => String(item.id) === parentId);
      if (!parent) break;
      if (parent.type === 2 || parent.type === 5) return resourceNodeId(parent.resourceKey);
      parentId = String(parent.parentId || '');
    }
    return legacyByResourceKey.get(resource.resourceKey)?.parentId || legacyByResourceKey.get(resource.resourceKey)?.id;
  };

  const actionButtons = flatResources
    .flatMap((resource, resourceIndex) =>
      (resource.actions || [])
        .filter((action) => action.actionType === 'ui' && String(action.actionValue || '').startsWith('button:'))
        .filter((action) => action.enabled !== 0)
        .map((action, actionIndex) => ({
          id: action.actionValue,
          parentId: buttonParentId(resource),
          type: 3,
          code: action.actionValue.replace(/^button:/, ''),
          showName: action.actionValue,
          sort: 20000 + resourceIndex * 100 + actionIndex,
          enable: true,
        }))
    )
    .filter((item, index, list) => list.findIndex((other) => other.id === item.id) === index);
  const seenButtons = new Set(actionButtons.map((item) => item.id));
  const fallbackButtons = buttonActions
    .filter((uri) => !seenButtons.has(uri))
    .reduce<ResourceProps[]>((items, uri, index) => {
      const parent = legacyByResourceKey.get(buttonParentResourceKey(uri.replace(/^button:/, '')));
      // 独立动作没有导航父项时仍归到自身资源，不能在按钮目录阶段丢弃有效授权。
      const parentId = parent?.parentId || parent?.id;
      if (!parentId) return items;
      items.push({
        id: uri,
        parentId,
        type: 3,
        code: uri.replace(/^button:/, ''),
        showName: uri,
        sort: 30000 + index,
        enable: true,
      });
      return items;
    }, []);

  return [...mapped, ...actionButtons, ...fallbackButtons];
};

export const routeUrisForResourceKeys = (resources: CoreResource[] = [], keys: string[] = []) => {
  const keySet = new Set(keys);
  const uris = flattenResources(resources)
    .filter((item) => keySet.has(item.resourceKey))
    .map((item) => routeForResource(item))
    .filter(Boolean);
  const fallbackUris = keys.map((key) => routeOverrides[key]).filter(Boolean);

  return Array.from(new Set([...uris, ...fallbackUris])).map((uri) => ({
    uri,
    methods: ['get', 'post', 'put', 'delete'],
  }));
};

const lastPathPart = (value: any) =>
  String(value || '')
    .split('/')
    .filter(Boolean)
    .pop() || '';

const topicFolderNames: Record<number, string> = {
  1: 'State',
  2: 'Action',
  3: 'Metric',
};

const topicTypeFromLeafName = (leafName: string) => {
  const normalized = String(leafName || '')
    .trim()
    .toLowerCase();
  for (const [topicType, folderName] of Object.entries(topicFolderNames)) {
    if (normalized === folderName.toLowerCase()) {
      return Number(topicType);
    }
  }
  return 0;
};

export const mapUnsNode = (item: any) => {
  const schema = item?.schema || {};
  const schemaFields = Array.isArray(schema) ? schema : schema?.fields || [];
  const typeName = String(item?.type || '').toLowerCase();
  const pathType = typeName === 'file' || item?.type === 2 ? 2 : 0;
  const topicTypeValue = topicTypeNumber(item?.topicType);
  const path = item.namespace || item.path || item.alias || item.name || item.pathName || item.title;
  const leafName = lastPathPart(path) || item.pathName || item.title || item.name;
  const leafTopicType = topicTypeFromLeafName(leafName);
  const isTopicFolder =
    pathType === 0 &&
    ((topicTypeValue && String(leafName).trim().toLowerCase() === topicFolderNames[topicTypeValue].toLowerCase()) ||
      leafTopicType > 0);
  const topicFolderType = topicTypeValue || leafTopicType;
  const defaultFileDataType = topicTypeValue === 3 ? 1 : topicTypeValue > 0 ? 8 : 1;
  const explicitDataType = item?.dataType || undefined;
  const resolvedDataType =
    pathType === 2
      ? explicitDataType || schema?.dataType || defaultFileDataType
      : isTopicFolder
        ? (explicitDataType && explicitDataType >= 1 && explicitDataType <= 3 ? explicitDataType : undefined) ||
          topicFolderType ||
          schema?.dataType ||
          undefined
        : undefined;
  const resolvedFields = item?.fields || schemaFields;
  const displayName = item.displayName && !String(item.displayName).includes('/') ? item.displayName : '';
  const children = (item.children || []).map(mapUnsNode);
  const countChildren = Number(item.countChildren ?? children.length);
  const hasChildren =
    item.hasChildren !== undefined ? Boolean(item.hasChildren) : countChildren > 0 || children.length > 0;
  return {
    ...item,
    id: String(item.id),
    key: String(item.id),
    parentId: item.parentId ? String(item.parentId) : '',
    name: displayName || leafName,
    title: displayName || leafName,
    alias: item.alias || path,
    path,
    pathName: leafName,
    pathType,
    type: pathType === 2 ? 2 : 1,
    dataType: resolvedDataType,
    parentDataType:
      item?.parentDataType ||
      (pathType === 2 || isTopicFolder ? topicFolderType : undefined) ||
      schema?.parentDataType ||
      undefined,
    topicType: item?.topicType || '',
    fields: resolvedFields,
    jsonFields: item?.jsonFields || (Number(resolvedDataType) === 8 ? resolvedFields : undefined),
    persistence: Boolean(item?.persistence),
    withFlow: Boolean(item?.withFlow ?? schema?.withFlow ?? schema?.addFlow ?? schema?.mockData),
    mockData: Boolean(item?.mockData ?? schema?.mockData ?? schema?.addFlow),
    lastPayload: item?.lastPayload,
    hasChildren,
    countChildren,
    isLeaf: pathType === 2 || !hasChildren,
    children,
  };
};

export const topicTypeNumber = (value: unknown) => {
  const normalized = String(value || '')
    .trim()
    .toLowerCase();
  if (normalized === '1' || normalized === 'state') return 1;
  if (normalized === '2' || normalized === 'action') return 2;
  if (normalized === '3' || normalized === 'metric') return 3;
  return 0;
};

const normalizeFlowStatus = (value: any) => {
  const normalized = String(value || '')
    .trim()
    .toLowerCase();
  if (['deployed', 'running', 'run'].includes(normalized)) return 'RUNNING';
  if (['draft'].includes(normalized)) return 'DRAFT';
  if (['disabled', 'disable'].includes(normalized)) return 'DISABLED';
  if (['pending'].includes(normalized)) return 'PENDING';
  if (['stopped', 'stop'].includes(normalized)) return 'STOPPED';
  return normalized ? normalized.toUpperCase() : 'DRAFT';
};

export const mapFlow = (item: any) => {
  const id = item.id || item.id === 0 ? item.id : item.flowId;
  const runtimeFlowId = item.runtimeFlowId || item.flowId || id;
  const isGroup = String(item.nodeType || '').toLowerCase() === 'folder' || Number(item.nodeType) === 1;
  const parentId = item.parentId || item.groupId || 0;
  const flowStatus = item.status ?? item.flowStatus;
  const template = item.template || item.flowTemplate || 'node-red';
  const flowKind = String(item.flowKind || item.flowType || 'source').toLowerCase() === 'event' ? 'event' : 'source';
  const isPinned = Number(item.isFavorite) === 1 || Number(item.sortKey) > 0 || item.pinned === true;
  return {
    ...item,
    id: String(id),
    flowId: String(runtimeFlowId || ''),
    runtimeFlowId: String(runtimeFlowId || ''),
    flowName: item.name || item.flowName,
    name: item.name || item.flowName,
    groupId: parentId ? String(parentId) : '',
    parentId: parentId ? String(parentId) : '',
    category: isGroup ? 'group' : item.category || 'file',
    nodeType: isGroup ? 'folder' : 'flow',
    flowKind,
    flowStatus: isGroup ? '' : normalizeFlowStatus(flowStatus),
    status: isGroup ? '' : normalizeFlowStatus(flowStatus),
    rawFlowStatus: flowStatus,
    template,
    flowTemplate: template,
    createAt: item.createAt ?? item.createdTime ?? item.createTime,
    creator: item.creator || item.createdByName || item.operatorName || item.createdBy || '',
    description: item.description || '',
    sortKey: Number(item.sortKey || 0),
    isFavorite: Number(item.isFavorite || 0),
    pinned: isPinned,
    sort: isPinned ? 1 : 0,
  };
};

export const pageData = (list: any[] = []) => ({
  data: list,
  total: list.length,
  pageNo: 1,
  pageSize: Math.max(list.length, 20),
});
