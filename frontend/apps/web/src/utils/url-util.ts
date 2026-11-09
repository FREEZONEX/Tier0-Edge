import defaultIconUrl from '@/assets/home-icons/default.svg';
import {
  CUSTOM_APP_ICON_PRE,
  CUSTOM_MENU_ICON,
  CUSTOM_MENU_ICON_PRE,
  CUSTOM_MENU_ICON_PRE1,
  MENU_TARGET_PATH,
  STORAGE_PATH,
} from '@/common-types/constans.ts';

export function getOpenAiUrl() {
  if (import.meta.env.MODE !== 'production') {
    return import.meta.env.REACT_APP_BASE_URL || '';
  }
  return '';
}

export function getBaseUrl() {
  return import.meta.env.REACT_APP_BASE_URL || window.location.origin;
}

export function getDevProxyBaseUrl() {
  if (import.meta.env.MODE !== 'development') {
    return '';
  }

  const apiBaseUrl = import.meta.env.REACT_APP_BASE_URL || process.env.API_PROXY_URL || '';
  if (!apiBaseUrl || typeof window === 'undefined') {
    return '/iframe';
  }

  try {
    const apiUrl = new URL(apiBaseUrl, window.location.origin);
    const currentUrl = new URL(window.location.origin);
    const sameOrigin = apiUrl.origin === currentUrl.origin;
    const sameLoopbackPort =
      apiUrl.protocol === currentUrl.protocol &&
      apiUrl.port === currentUrl.port &&
      ['localhost', '127.0.0.1', '[::1]'].includes(apiUrl.hostname) &&
      ['localhost', '127.0.0.1', '[::1]'].includes(currentUrl.hostname);

    return sameOrigin || sameLoopbackPort ? '' : '/iframe';
  } catch {
    return '/iframe';
  }
}

export function getFileName(path: string) {
  return path.split('/').pop(); // 切割路径并返回最后一个部分
}

export function getBaseFileName(path: string) {
  if (!path) return path;
  const parts = path.split('/');
  const fileName = parts.pop();
  return fileName ? fileName.replace('.html', '') : '';
}

const CHARTREUSE_UNSUPPORTED_ICONS = new Set([
  'Apps',
  'AppBuilder',
  'DevTools',
  'Home',
  'Homepage',
  'Launchpad',
  'Project',
  'Settings',
  'UNS',
  'menu.tag.apps',
  'menu.tag.appspace',
  'menu.tag.connections',
  'menu.tag.settings',
  'menu.tag.system',
  'menu.tag.uns',
]);

// 设置参数
export const getSearchParamsString = (obj: any) => {
  if (!obj) return '';
  return new URLSearchParams(obj).toString();
};

// 获取
export const getSearchParamsObj = (str?: string) => {
  if (!str) return {};
  const obj: any = {};
  const searchParams = new URLSearchParams(str);
  searchParams.forEach((value, key) => {
    if (value === 'null') {
      obj[key] = null; // 处理 "null" 字符串
    } else if (value === 'undefined') {
      obj[key] = undefined; // 处理 "undefined" 字符串
    } else {
      obj[key] = value; // 其他情况
    }
  });
  return obj;
};

export const getImageSrcByTheme = (theme: string, iconName?: string) => {
  const fallbackImageUrl = defaultIconUrl; // 前端静态资源的默认图标
  if (!iconName) {
    return { themeImageUrl: '', defaultImageUrl: '', fallbackImageUrl };
  }
  if (iconName.includes(CUSTOM_MENU_ICON_PRE) || iconName.includes(CUSTOM_MENU_ICON_PRE1)) {
    // 自定义上传图片地址
    return {
      themeImageUrl: `${CUSTOM_MENU_ICON}?objectName=${encodeURI(iconName)}`,
      defaultImageUrl: `${CUSTOM_MENU_ICON}?objectName=${encodeURI(iconName)}`,
      fallbackImageUrl,
    };
  }
  if (iconName?.includes(CUSTOM_APP_ICON_PRE)) {
    return {
      themeImageUrl: iconName,
      defaultImageUrl: iconName,
      fallbackImageUrl,
    };
  }
  const baseUrl = `${getBaseUrl()}${STORAGE_PATH}${MENU_TARGET_PATH}/`;

  // 检查iconName是否已经包含有效的文件后缀
  // 常见的图片扩展名列表
  const validExtensions = ['.svg', '.png', '.jpg', '.jpeg', '.gif', '.webp', '.ico'];
  let iconNameWithoutExt = iconName;
  let extension = '.svg';
  let hasExtension = false;

  // 获取最后一个点的位置
  const lastDotIndex = iconName.lastIndexOf('.');

  // 如果存在点，并且点后面的内容是有效的扩展名
  if (lastDotIndex !== -1) {
    const possibleExt = iconName.substring(lastDotIndex).toLowerCase();
    hasExtension = validExtensions.includes(possibleExt);

    if (hasExtension) {
      extension = possibleExt; // 获取扩展名（包括点）
      iconNameWithoutExt = iconName.substring(0, lastDotIndex); // 获取不包含扩展名的文件名
    }
  }

  // 如果没有有效扩展名，则整个iconName作为文件名，使用默认扩展名
  if (!hasExtension) {
    iconNameWithoutExt = iconName;
  }

  const supportsChartreuse = theme.includes('chartreuse') && !CHARTREUSE_UNSUPPORTED_ICONS.has(iconNameWithoutExt);
  // 拼接带主题后缀的文件名
  const themeImageUrl = `${baseUrl}${iconNameWithoutExt}${supportsChartreuse ? '-chartreuse' : ''}${extension}`;
  // 默认文件名
  const defaultImageUrl = `${baseUrl}${iconNameWithoutExt}${extension}`;
  return { themeImageUrl, defaultImageUrl, fallbackImageUrl };
};

export const getSearchParamsArray = (params?: URLSearchParams) => {
  if (!params) return [];
  return Array.from(params.entries()).map(([key, value]: [string, string]) => ({
    key,
    value,
  }));
};

export function ensureUrlProtocol(url: string) {
  if (!url) return url;
  const _url = url.trim();
  // 匹配任意合法协议（字母开头，后接 ://）
  if (!/^[a-z]+:\/\//i.test(_url)) {
    return 'http://' + _url;
  }
  return url;
}

export const checkImageExists = (url: string) => {
  return new Promise((resolve) => {
    const img = new Image();
    img.onload = () => resolve(true);
    img.onerror = () => resolve(false);
    img.src = url;
  });
};

// 判断是否在iframe中 以及 是否 匹配了iframe name
export function isInIframe(iframeNames: string[], type: string = 'iframe') {
  try {
    const isWebView = type === 'webview' && navigator.userAgent.includes('WebView/');
    if (isWebView) return true;
    if (!iframeNames?.length) {
      return window.self !== window.top;
    } else {
      return iframeNames.includes(window.name) && window.self !== window.top;
    }
  } catch (err) {
    console.log(err);
    return true;
  }
}

// 获取url完整地址
export const getIconUrl = (iconUrl?: string) => {
  if (!iconUrl) return '';
  if (iconUrl?.includes(CUSTOM_MENU_ICON_PRE) || iconUrl?.includes(CUSTOM_MENU_ICON_PRE1)) {
    return `${CUSTOM_MENU_ICON}?objectName=${encodeURI(iconUrl)}`;
  }
  if (iconUrl?.includes(CUSTOM_APP_ICON_PRE)) {
    return `${iconUrl}`;
  }
  return `${STORAGE_PATH}${MENU_TARGET_PATH}/${encodeURI(iconUrl)}`;
};
