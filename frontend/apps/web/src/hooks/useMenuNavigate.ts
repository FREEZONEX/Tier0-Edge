import { useNavigate } from 'react-router';
import type { ResourceProps } from '@/stores/types';
import { App } from 'antd';

const isNamespaceEntry = (item?: ResourceProps) => {
  if (!item) return false;
  const code = item.code?.trim().toLowerCase();
  const resourceKey = item.resourceKey?.trim().toLowerCase();
  const url = item.url?.trim().toLowerCase();

  return code === 'uns' || resourceKey === 'uns.page' || url === '/uns';
};

const useMenuNavigate = (props: { msg?: string } = {}) => {
  const { msg } = props;
  const navigate = useNavigate();
  const { message } = App.useApp();

  const handleNavigate = (item?: ResourceProps) => {
    if (!item) {
      message.warning(msg ?? '菜单不存在');
      return;
    }
    if (isNamespaceEntry(item)) {
      navigate('/uns', {
        state: {
          resetUnsLanding: Date.now(),
        },
      });
      return;
    }
    if (item.urlType === 1) {
      navigate(item.url!);
    } else if (item.urlType === 2) {
      // 外部/子应用菜单：openType=1 新窗口，否则走 /${code} 动态 iframe 路由承载（url 不是 SPA 路由）
      if (item.openType === 1) {
        window.open(item.url);
      } else {
        navigate(`/${item.code}`, {
          state: {
            url: item.url,
            showName: item.showName,
            code: item.code,
          },
        });
      }
    } else if (item.url) {
      navigate(item.url);
    } else {
      if (item?.openType === 1) {
        window.open(item.url);
      } else {
        navigate(`/${item.code}`, {
          state: {
            url: item.url,
            showName: item.showName,
            code: item.code,
            // iframeRealUrl: realHost,
          },
        });
      }
    }
    // if (item.isFrontend) {
    //   navigate(item.menu!.url);
    // } else {
    //   if (item?.openType !== undefined) {
    //     const { port, protocol, host, name } = item.service as any;
    //     const path = item.menu?.url?.split(name)?.[1] || '';
    //     const realHost = port ? `${protocol}://${host}:${port}` : `${protocol}://${host}`;
    //     if (item?.openType === '1') {
    //       window.open(realHost + path);
    //     } else if (item?.openType === '2') {
    //       window.open(item.indexUrl);
    //     } else {
    //       navigate(`/${item.key}`, {
    //         state: {
    //           url: path,
    //           name: item.name,
    //           iframeRealUrl: realHost,
    //         },
    //       });
    //     }
    //   } else {
    //     navigate(`/${item.key}`, {
    //       state: {
    //         url: item?.menu?.url,
    //         name: item.name,
    //         iframeRealUrl:
    //           item?.menuProtocol && item?.menuHost && item?.menuPort
    //             ? `${item?.menuProtocol}://${item?.menuHost}:${item?.menuPort}${item?.menu?.url}`
    //             : undefined,
    //       },
    //     });
    //   }
    // }
  };

  return handleNavigate;
};

export default useMenuNavigate;
