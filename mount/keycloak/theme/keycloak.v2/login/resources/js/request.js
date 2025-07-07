// request.js

// 基础 URL（根据你的项目修改）
const BASE_URL = "";

// 默认配置
const defaultOptions = {
  headers: {
    "Content-Type": "application/json",
    // 可以在这里加 token，比如：
    // 'Authorization': 'Bearer ' + localStorage.getItem('token')
  },
};

/**
 * 封装 fetch 请求
 * @param {string} url       请求地址
 * @param {object} options   fetch 配置项
 * @param {boolean} isJson   是否自动解析 JSON（默认是）
 */
export const request = async (url, options = {}, isJson = true) => {
  const fullUrl = url.startsWith("http") ? url : BASE_URL + url;

  const config = {
    ...defaultOptions,
    ...options,
    headers: {
      ...defaultOptions.headers,
      ...(options.headers || {}),
    },
  };

  try {
    const response = await fetch(fullUrl, config);

    if (!response.ok) {
      return undefined;
      // throw new Error(`HTTP 错误! 状态码: ${response.status}`);
    }

    if (isJson) {
      const res = await response.json();
      const { code, data } = res || {};
      if (code === 200) {
        return data;
      }
    } else {
      return await response.text();
    }
  } catch (error) {
    throw error;
  }
};
