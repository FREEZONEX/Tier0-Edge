import { matchPath, useLocation, useNavigate } from 'react-router';
import { useEffect } from 'react';
import { useBaseStore } from '@/stores/base';

const matchesKnownPath = (pathname: string, routePath?: string) => {
  if (!routePath?.startsWith('/')) return false;
  return Boolean(matchPath({ path: routePath, caseSensitive: false, end: true }, pathname));
};

const NotPage = ({ knownPaths = [] }: { knownPaths?: string[] }) => {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  useEffect(() => {
    const originMenu = useBaseStore.getState().originMenu;
    const isKnownRoute =
      knownPaths.some((path) => matchesKnownPath(pathname, path)) ||
      originMenu?.some((item) =>
        [item?.url, item?.code ? `/${item.code}` : undefined].some((path) => matchesKnownPath(pathname, path))
      );
    navigate(isKnownRoute ? '/403' : '/404', { replace: true });
  }, [knownPaths, navigate, pathname]);

  return null;
};

export default NotPage;
