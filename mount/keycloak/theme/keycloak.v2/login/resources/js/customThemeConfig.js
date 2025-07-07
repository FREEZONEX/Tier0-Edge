import { request } from "./request.js";

const checkUrl = (dom, url, defaultUrl) => {
  dom.src = url;
  dom.onerror = function () {
    this.onerror = null;
    this.src = defaultUrl;
  };
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
          darkLogoIcon || "/files/system/resource/supos/logo-dark.png",
          `${keycloakUrl}/img/supos-logo-dark.svg`
        );
        checkUrl(
          sloganDom,
          darkSloganIcon,
          `${keycloakUrl}/img/slogan-dark-${lang}.png`
        );
        loginArrowDom.style.backgroundImage = `url(${keycloakUrl}/img/login-arrow-dark.svg)`;
        loginLeft.style.backgroundImage = darkBackgroundIcon
          ? `url(${darkBackgroundIcon})`
          : `url(${keycloakUrl}/img/login-background.png)`;
      }
    } else {
      //亮色主题
      classList.remove(DARK_MODE_CLASS);
      if (logoDom && loginArrowDom && sloganDom) {
        checkUrl(
          logoDom,
          brightLogoIcon || "/files/system/resource/supos/logo-light.png",
          `${keycloakUrl}/img/supos-logo.svg`
        );
        checkUrl(
          sloganDom,
          brightSloganIcon,
          `${keycloakUrl}/img/slogan-light-${lang}.png`
        );
        loginArrowDom.style.backgroundImage = `url(${keycloakUrl}/img/login-arrow.svg)`;

        loginLeft.style.backgroundImage = brightBackgroundIcon
          ? `url(${brightBackgroundIcon})`
          : `url(${keycloakUrl}/img/login-background.png)`;
      }
    }
  }

  try {
    const themeConfig = await request(`/inter-api/supos/theme/getConfig`);
    if (themeConfig) {
      updateDarkMode(themeConfig?.loginPageType, themeConfig);
    } else {
      const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
      updateDarkMode(mediaQuery.matches);
      mediaQuery.addEventListener("change", (event) =>
        updateDarkMode(event.matches)
      );
    }
    document.body.style.opacity = 1;
  } catch (err) {
    document.body.style.opacity = 1;
    console.log(err);
  }
};
