// DNS 单个标签的协议上限，与通用业务名称长度分开维护。
export const APP_NETWORK_ALIAS_MAX_LENGTH = 63;

const RESERVED_NAMES = new Set([
  'localhost',
  'backend',
  'zlm',
  'vision-worker',
  'opcua-server',
  'tsdb',
  'db-bootstrap',
  'redis',
  'emqx',
  'sourceflow',
  'nodered',
  'eventflow',
  'marimo',
  'uns-agent-api',
  'uns-agent-worker',
  'rustfs',
  'rustfs-init',
  'host',
]);

export const appNetworkAliasError = (value: string): string => {
  const alias = value.trim();
  if (!alias) return '';
  if (
    alias.length > APP_NETWORK_ALIAS_MAX_LENGTH ||
    !/^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/.test(alias) ||
    !/[a-z]/.test(alias)
  )
    return 'project.networkAliasInvalid';
  if (RESERVED_NAMES.has(alias) || /^(app|tier0|enterprise)-/.test(alias)) {
    return 'project.networkAliasReserved';
  }
  return '';
};
