import { request } from "./request.js";

const checkUrl = (dom, url, defaultUrl) => {
  if(url){
    dom.src = url;
    dom.onerror = function () {
      this.onerror = null;
      this.src = defaultUrl;
    };
  }else{
    if (defaultUrl) {
      dom.style.display = "none";
    }
    dom.src = defaultUrl;
  }
};

export const handleTheme = async (keycloakUrl, lang) => {
  const DARK_MODE_CLASS = "pf-v5-theme-dark";
  const { classList } = document.documentElement;
  const loginLeft = document.querySelector(".pf-v5-c-login-left");
  const logoDom = document.querySelector(".supos-logo");
  const loginArrowDom = document.querySelector(".pf-v5-c-login-l-t-right");
  const sloganDom = document.querySelector(".supos-login-slogan");

  const favicon = document.getElementById("dynamic-favicon");

  function updateDarkMode(isEnabled, themeConfig) {
    const {
      brightBackgroundIcon,
      brightLogoIcon,
      brightSloganIcon,
      darkBackgroundIcon,
      darkLogoIcon,
      darkSloganIcon,
      browseIcon,
    } = themeConfig || {};

    // 浏览器图标
    // 检测 SVG 是否加载失败
    const faviconIcon = new Image();
    if (browseIcon) {
      favicon.href = browseIcon;
    }
    faviconIcon.src = favicon.href;
    faviconIcon.onerror = function () {
      // 替换为备用图标
      favicon.href = "/log.svg";
    };

    if (isEnabled) {
      //暗色主题
      classList.add(DARK_MODE_CLASS);
      if (logoDom && loginArrowDom && sloganDom) {
        checkUrl(
          logoDom,
          darkLogoIcon || `${keycloakUrl}/img/supos-logo-dark.svg`,
          `${keycloakUrl}/img/supos-logo-dark.svg`
        );
        checkUrl(
          sloganDom,
          darkSloganIcon,
        );
        loginArrowDom.style.backgroundImage = `url(${keycloakUrl}/img/login-arrow-dark.svg)`;
        loginLeft.style.background = darkBackgroundIcon
          ? `url(${darkBackgroundIcon})`
          : `url(${keycloakUrl}/img/login-background.png) center center / cover no-repeat`;
      }
    } else {
      //亮色主题
      classList.remove(DARK_MODE_CLASS);
      if (logoDom && loginArrowDom && sloganDom) {
        checkUrl(
          logoDom,
          brightLogoIcon || `${keycloakUrl}/img/supos-logo.svg`,
          `${keycloakUrl}/img/supos-logo.svg`
        );
        checkUrl(
          sloganDom,
          brightSloganIcon,
          `${keycloakUrl}/img/slogan-light-${lang}.png`
        );
        loginArrowDom.style.backgroundImage = `url(${keycloakUrl}/img/login-arrow.svg)`;

        loginLeft.style.background = brightBackgroundIcon
          ? `url(${brightBackgroundIcon})`
          : `url(${keycloakUrl}/img/login-background.png) center center / cover no-repeat`;
      }
    }
  }

  const useDefaultTheme = () => {
    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    updateDarkMode(mediaQuery.matches);
    mediaQuery.addEventListener("change", (event) =>
      updateDarkMode(event.matches)
    );
  };

  try {
    const themeConfig = await request(`/inter-api/supos/theme/getConfig`);
    if (themeConfig) {
      updateDarkMode(themeConfig?.loginPageType, themeConfig);
    } else {
      useDefaultTheme();
    }
    document.body.style.opacity = 1;
  } catch (err) {
    useDefaultTheme();
    document.body.style.opacity = 1;
    console.log(err);
  }
};
