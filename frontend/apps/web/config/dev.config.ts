import dotenv from 'dotenv';
import colors from 'picocolors';

export interface DevInfo {
  BASE_URL: string;
  API_PROXY_URL?: string;
  SINGLE_API_PROXY_URL?: string;
  SINGLE_API_PROXY_LIST?: string;
  VITE_PROXY_SECURE?: boolean;
  VITE_ASSET_PREFIX: string;
  VITE_REMOTE_PREFIX?: string;
  VITE_ENABLE_LOCAL_REMOTE?: string;
  VITE_ENABLE_HOST: boolean;
  VITE_ALLOWED_HOSTS?: string;
  LAUNCHPAD_PORT?: string;
  VITE_LAUNCHPAD_PORT?: string;
}

/**
 * @description 开发代理配置
 * */
export const parseConfig = (config: any) => {
  const newConfig: any = {};
  Object.keys(config).forEach((key) => {
    const value = config[key];
    newConfig[key] = value === 'true' ? true : value === 'false' ? false : value;
  });
  return newConfig;
};

// agent-svc: uns-agent 集成（2026-07-15）——网关路由 /agent-svc/** 反代 uns-agent-api，dev 模式同样要转发，否则 Agent tab 的聊天/画布接口在本地开发态 404。
export const proxyList = [
  'runtime-env.js',
  'api',
  'openapi',
  'gateway',
  'chat2db/api',
  'files/system/resource',
  'kong-admin',
  'agent-svc',
];

export const getProxy = (baseUrl?: string, singleList?: string, singleUrl?: string, secure?: boolean) => {
  const proxyConfig: any = {};
  const resolveSecure = (target?: string) => (target?.startsWith('https://') ? (secure ?? false) : undefined);

  if (singleList) {
    const targetUrl = singleUrl || baseUrl;
    singleList?.split(',')?.forEach?.((name) => {
      proxyConfig[`/${name}`] = {
        target: targetUrl,
        changeOrigin: true,
        ws: true,
        secure: resolveSecure(targetUrl),
      };
    });
  }

  if (baseUrl) {
    proxyList.forEach((name) => {
      proxyConfig[`/${name}`] = {
        target: baseUrl,
        changeOrigin: true,
        ws: true,
        secure: resolveSecure(baseUrl),
        // vpn代理
        // agent: new HttpsProxyAgent('http://127.0.0.1:7897'),
      };
    });
  }
  // 给iframe加个代理
  proxyConfig['/iframe'] = {
    target: baseUrl || 'http://127.0.0.1:8088',
    changeOrigin: true,
    secure: resolveSecure(baseUrl || 'http://127.0.0.1:8088'),
    rewrite: (path: any) => path.replace(/^\/iframe/, ''),
  };
  // 给chat2db加个代理
  proxyConfig['/chat2db/home/'] = {
    target: baseUrl || 'http://127.0.0.1:8088',
    changeOrigin: true,
    secure: resolveSecure(baseUrl || 'http://127.0.0.1:8088'),
  };
  return proxyConfig;
};

// == 开发信息
export const getDevInfo = (): DevInfo => {
  const explicitEnv = { ...process.env };
  const isProdCli = explicitEnv.NODE_ENV === 'production';
  const result = dotenv.config({
    path: ['.env', '.env.local'],
    quiet: true,
  });
  const envConfig = parseConfig(result.parsed || {});
  const defaultApiProxyUrl = isProdCli ? undefined : 'http://127.0.0.1:8088';
  const defaultAssetPrefix = isProdCli ? undefined : `http://127.0.0.1:${explicitEnv.PORT || '5173'}`;
  const launchpadPort =
    explicitEnv.LAUNCHPAD_PORT ||
    explicitEnv.VITE_LAUNCHPAD_PORT ||
    envConfig.LAUNCHPAD_PORT ||
    envConfig.VITE_LAUNCHPAD_PORT;
  const mergedConfig = {
    ...envConfig,
    API_PROXY_URL: explicitEnv.API_PROXY_URL || envConfig.API_PROXY_URL || defaultApiProxyUrl,
    SINGLE_API_PROXY_URL: explicitEnv.SINGLE_API_PROXY_URL || envConfig.SINGLE_API_PROXY_URL,
    SINGLE_API_PROXY_LIST: explicitEnv.SINGLE_API_PROXY_LIST || envConfig.SINGLE_API_PROXY_LIST,
    VITE_PROXY_SECURE: explicitEnv.VITE_PROXY_SECURE || envConfig.VITE_PROXY_SECURE,
    VITE_ASSET_PREFIX: explicitEnv.VITE_ASSET_PREFIX || envConfig.VITE_ASSET_PREFIX || defaultAssetPrefix,
    VITE_REMOTE_PREFIX: explicitEnv.VITE_REMOTE_PREFIX || envConfig.VITE_REMOTE_PREFIX,
    VITE_ENABLE_LOCAL_REMOTE: explicitEnv.VITE_ENABLE_LOCAL_REMOTE || envConfig.VITE_ENABLE_LOCAL_REMOTE,
    VITE_ENABLE_HOST: explicitEnv.VITE_ENABLE_HOST || envConfig.VITE_ENABLE_HOST,
    VITE_ALLOWED_HOSTS: explicitEnv.VITE_ALLOWED_HOSTS || envConfig.VITE_ALLOWED_HOSTS,
    LAUNCHPAD_PORT: launchpadPort,
    VITE_LAUNCHPAD_PORT: launchpadPort,
  };
  if (isProdCli && !explicitEnv.VITE_ASSET_PREFIX) {
    mergedConfig.VITE_ASSET_PREFIX = '/';
  }
  return parseConfig(mergedConfig) as DevInfo;
};

export const logDevInfo = (info: DevInfo) => {
  const isProdCli = process.env.NODE_ENV === 'production';
  if (isProdCli) return;
  const {
    API_PROXY_URL,
    SINGLE_API_PROXY_URL,
    SINGLE_API_PROXY_LIST,
    VITE_PROXY_SECURE,
    VITE_ASSET_PREFIX,
    VITE_REMOTE_PREFIX,
    VITE_ENABLE_LOCAL_REMOTE,
    VITE_ENABLE_HOST,
    LAUNCHPAD_PORT,
    VITE_LAUNCHPAD_PORT,
  } = info;
  console.log('---------- 开发信息 ----------');
  console.log(colors.gray('接口代理'), API_PROXY_URL, '\n');
  console.log(colors.gray('特殊接口代理'), SINGLE_API_PROXY_URL, '\n');
  console.log(colors.gray('特殊接口List'), SINGLE_API_PROXY_LIST, '\n');
  console.log(colors.gray('HTTPS证书校验'), VITE_PROXY_SECURE ?? false, '\n');
  console.log(colors.gray('host-dev地址'), VITE_ASSET_PREFIX, '\n');
  console.log(colors.gray('remote-dev地址'), VITE_REMOTE_PREFIX, '\n');
  console.log(colors.gray('是否启用本地模块联邦'), VITE_ENABLE_LOCAL_REMOTE, '\n');
  console.log(colors.gray('代理host开启'), VITE_ENABLE_HOST, '\n');
  console.log(colors.gray('Launchpad独立端口'), LAUNCHPAD_PORT, '\n');
  console.log(colors.gray('Launchpad浏览器端口'), VITE_LAUNCHPAD_PORT, '\n');

  if (!VITE_ASSET_PREFIX) {
    console.error(colors.red('VITE_ASSET_PREFIX 未配置'));
  }
};

export const logBuildTime = () =>
  new Date().toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: true, // 24小时制
  });
