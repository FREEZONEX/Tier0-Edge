import { useCookies } from 'react-cookie';
import {
  LOGIN_URL,
  OMC_MODEL,
  APP_COMMUNITY_TOKEN,
  SYS_COMMUNITY_TOKEN,
  APP_USER_TIPS_ENABLE,
} from '@/common-types/constans';
import { useEffect } from 'react';
import { message } from 'antd';
import { APP_USER_GUIDE_ROUTES } from '@/common-types/constans';
import { storageOpt } from '@/utils/storage';
import { useBaseStore } from '@/stores/base';
import Cookies from 'js-cookie';
import { isPublicAccessPath } from '@/utils/public-access';
import { getToken, isDevScopedTokenEnabled } from '@/utils/auth';
import { getLaunchpadStandaloneLoginPath } from '@/utils/launchpad-site';

let isRedirectingForExpiredCookie = false;

// 登录失效控制
const CookieContext = () => {
  const systemInfo = useBaseStore((state) => state.systemInfo);

  const [cookies, setCookie, removeCookie] = useCookies([APP_COMMUNITY_TOKEN, SYS_COMMUNITY_TOKEN]);

  useEffect(() => {
    // cookie发生改变删除guide routes信息
    storageOpt.remove(APP_USER_GUIDE_ROUTES);
    // cookie发生改变重置tips展示状态
    storageOpt.remove(APP_USER_TIPS_ENABLE);
    // 清空
    storageOpt.remove('personInfo');

    const currentToken = getToken();
    const sysToken = cookies?.[SYS_COMMUNITY_TOKEN];
    if (!isDevScopedTokenEnabled() && !cookies?.[APP_COMMUNITY_TOKEN] && sysToken) {
      setCookie(APP_COMMUNITY_TOKEN, sysToken, { path: '/' });
      removeCookie(SYS_COMMUNITY_TOKEN, { path: '/' });
      return;
    }

    if (!currentToken) {
      const currentPath = window.location.pathname;
      if (isPublicAccessPath(currentPath)) {
        isRedirectingForExpiredCookie = false;
        return;
      }
      if (isRedirectingForExpiredCookie) {
        return;
      }
      isRedirectingForExpiredCookie = true;
      if (import.meta.env.MODE === 'development') {
        message.error('开发环境cookie已失效，正在跳转登录页');
      }
      if (Cookies.get(OMC_MODEL)) {
        console.warn('omc——cookie失效');
        window.location.replace('/403');
      } else {
        const handleRedirect = () => {

          console.log('登录cookie不存在，要跳转到登录页');
          window.location.replace(getLaunchpadStandaloneLoginPath(systemInfo?.loginPath || LOGIN_URL));
        };

        handleRedirect();
      }
    } else {
      isRedirectingForExpiredCookie = false;
    }
  }, [cookies?.[SYS_COMMUNITY_TOKEN], cookies?.[APP_COMMUNITY_TOKEN], removeCookie, setCookie, systemInfo?.loginPath]);

  return null;
};

export default CookieContext;
