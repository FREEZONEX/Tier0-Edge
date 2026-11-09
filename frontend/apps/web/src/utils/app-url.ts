const localHostnames = new Set(['localhost', '127.0.0.1', '0.0.0.0', '::1']);

interface NormalizeLocalAppUrlOptions {
  allowPrivateHostRewrite?: boolean;
}

const isPrivateIPv4 = (hostname: string) => {
  const parts = hostname.split('.').map((part) => Number(part));
  if (parts.length !== 4 || parts.some((part) => !Number.isInteger(part) || part < 0 || part > 255)) {
    return false;
  }
  const [first, second] = parts;
  return (
    first === 10 ||
    first === 127 ||
    (first === 192 && second === 168) ||
    (first === 172 && second >= 16 && second <= 31)
  );
};

const shouldUseCurrentLocalHost = (targetHostname: string) => {
  if (typeof window === 'undefined') {
    return false;
  }
  const currentHostname = window.location.hostname;
  if (!localHostnames.has(currentHostname) || currentHostname === targetHostname) {
    return false;
  }
  return localHostnames.has(targetHostname);
};

export const normalizeLocalAppUrl = (rawUrl: string, options: NormalizeLocalAppUrlOptions = {}) => {
  if (!rawUrl || !/^https?:\/\//i.test(rawUrl)) {
    return rawUrl;
  }
  try {
    const url = new URL(rawUrl);
    const shouldRewriteHost =
      shouldUseCurrentLocalHost(url.hostname) ||
      (options.allowPrivateHostRewrite &&
        typeof window !== 'undefined' &&
        localHostnames.has(window.location.hostname) &&
        isPrivateIPv4(url.hostname));
    if (shouldRewriteHost) {
      url.hostname = window.location.hostname;
      return url.toString();
    }
  } catch {
    return rawUrl;
  }
  return rawUrl;
};

/** 将通知的应用内路径限制在已授权应用的入口下，保留查询参数与锚点。 */
export const resolveAppNotificationUrl = (entryUrl: string, path: string | null) => {
  if (path === null) return entryUrl;
  if (!path.startsWith('/') || path.startsWith('//') || /[\\\s\p{Cc}\p{Cf}]/u.test(path)) return '';
  try {
    const pathname = path.split(/[?#]/, 1)[0];
    const decodedPath = decodeURIComponent(pathname);
    if (
      /%2f|%5c/i.test(pathname) ||
      /[\\\p{Cc}\p{Cf}]/u.test(decodedPath) ||
      decodedPath.split('/').some((segment) => segment === '.' || segment === '..')
    )
      return '';
    const entry = new URL(entryUrl, window.location.origin);
    if (!['http:', 'https:'].includes(entry.protocol)) return '';
    const basePath = entry.pathname.endsWith('/') ? entry.pathname : `${entry.pathname}/`;
    const base = new URL(entry);
    base.pathname = basePath;
    base.search = '';
    base.hash = '';
    const target = new URL(path.slice(1), base);
    if (target.origin !== entry.origin || !target.pathname.startsWith(basePath)) return '';
    for (const [key, value] of entry.searchParams) {
      if (!target.searchParams.has(key)) target.searchParams.append(key, value);
    }
    return target.toString();
  } catch {
    return '';
  }
};
