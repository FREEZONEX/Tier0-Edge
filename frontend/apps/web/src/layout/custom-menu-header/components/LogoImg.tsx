import { Image as AntImage, type ImageProps } from 'antd';
import { type FC, useEffect, useState } from 'react';
import { productLogo, productName } from '@/utils/product-edition';
import LoadingDots from '@/layout/custom-menu-header/components/LoadingDots.tsx';
import { MENU_TARGET_PATH, STORAGE_PATH } from '@/common-types/constans';
import { checkImageExists, getBaseUrl } from '@/utils/url-util';
import { useBaseStore } from '@/stores/base';

interface IconImgProps extends Partial<ImageProps> {
  isDark: boolean;
}
const LogoImg: FC<IconImgProps> = ({ isDark, onClick, ...props }) => {
  const [imageSrc, setImageSrc] = useState('');
  const { systemInfo } = useBaseStore((state) => ({
    systemInfo: state.systemInfo,
  }));
  const defaultLogo = productLogo(systemInfo, isDark);
  const themeLogo = systemInfo.themeConfig?.[isDark ? 'dark' : 'light']?.logo;

  useEffect(() => {
    let active = true;
    setImageSrc('');
    const loadImage = async () => {
      const baseUrl = `${getBaseUrl()}${STORAGE_PATH}${MENU_TARGET_PATH}/`;
      // 从服务器加载菜单静态资源。
      const hasTheme = themeLogo;
      if (!hasTheme) {
        setImageSrc(defaultLogo);
        return;
      }
      const themeLogoUrl = `${baseUrl}${hasTheme}?t=${new Date().getTime()}`;
      const themeExists = await checkImageExists(themeLogoUrl);
      if (!active) return;
      if (themeExists) {
        setImageSrc(themeLogoUrl);
      } else {
        setImageSrc(defaultLogo);
      }
    };
    loadImage();
    return () => {
      active = false;
    };
  }, [defaultLogo, themeLogo]);
  return (
    <div
      onClick={onClick}
      style={{
        cursor: 'pointer',
        minWidth: 50,
        overflow: 'hidden',
        marginRight: 8,
      }}
    >
      {!imageSrc ? (
        <LoadingDots color={isDark ? 'white' : '#333'} />
      ) : (
        <AntImage src={imageSrc} preview={false} fallback={defaultLogo} alt={productName(systemInfo)} {...props} />
      )}
    </div>
  );
};

export default LogoImg;
