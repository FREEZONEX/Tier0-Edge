//环境变量-类型提示
interface ImportMetaEnv {
  readonly REACT_APP_BASE_URL: string;
  /** 本地开发代理地址 */
  readonly API_PROXY_URL: string;
  /** app的title */
  readonly VITE_APP_TITLE: string;
  readonly VITE_LANGUAGE: string;
  readonly VITE_OS_LANG: string;
  readonly VITE_APP_LANG: string;
  readonly VITE_LAUNCHPAD_PORT?: string;
  readonly REACT_APP_LOCAL_LANG: string;
  readonly REACT_APP_OS_LANG: string;
  //加入更多环境变量...
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

interface Window {
  __TIER0_RUNTIME_CONFIG__?: {
    launchpadPort?: string;
    launchpadStandalone?: boolean;
    sourceStreamEnabled?: boolean;
  };
}
