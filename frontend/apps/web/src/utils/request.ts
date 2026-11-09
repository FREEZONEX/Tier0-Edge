import axios, { type AxiosRequestConfig } from 'axios';
import { getToken } from './auth-token';
import { message, notification } from 'antd';
import qs from 'qs';
import { getIntl } from '@/stores/i18n-store.ts';
import { isInIframe } from '@/utils/url-util.ts';

/**
 * 接口请求通用业务错误码
 */
export enum BusinessCodeEnum {
  /**
   * 业务状态码-成功
   */
  Success = 0,
  Success_Two = 200,
  /**
   * 业务状态码-失败
   */
  Fail = -10000,
}

/**
 * 自定义配置
 */
export enum CustomAxiosConfigEnum {
  /**
   * 返回业务响应值，eg. response.data  ==   {code:1,data:xxx}
   */
  BusinessResponse = '_businessResponse',
  /**
   * 当配置为 true时，请求失败了，不显示默认的提示信息
   */
  NoMessage = '_noMessage',
  /**
   * 当配置为 true时，表示必然返回数据
   */
  NoCode = '_noCode',
}

/**
 * 扩展 AxiosRequestConfig ，增加自定义的配置字段
 */
export interface AxiosWrapperRequestConfig extends AxiosRequestConfig {
  [CustomAxiosConfigEnum.BusinessResponse]?: boolean;
  [CustomAxiosConfigEnum.NoMessage]?: boolean;
  [CustomAxiosConfigEnum.NoCode]?: boolean;
}

export type EventStreamSubscription = {
  close: () => void;
};

type EventStreamOptions<T> = {
  onMessage: (data: T) => void;
  onError: (error: unknown) => void;
  onClose?: () => void;
};

const readEventBlock = <T>(block: string, onMessage: (data: T) => void) => {
  const data = block
    .split(/\r?\n/)
    .filter((line) => line.startsWith('data:'))
    .map((line) => line.slice(5).trimStart())
    .join('\n');
  if (data) {
    onMessage(JSON.parse(data) as T);
  }
};

/**
 * 原生 EventSource 无法携带当前登录态使用的 Authorization / X-Sa-Token 请求头。
 * 统一从请求层发起 fetch 流，保持和 ApiWrapper 相同的认证约定。
 */
export const subscribeAuthenticatedEventStream = <T>(
  url: string,
  { onMessage, onError, onClose }: EventStreamOptions<T>
): EventStreamSubscription => {
  const controller = new AbortController();
  let closed = false;

  const consume = async () => {
    let failed = false;
    try {
      const headers = new Headers({ Accept: 'text/event-stream' });
      const token = getToken();
      if (token) {
        headers.set('X-Sa-Token', token);
        headers.set('Authorization', `Bearer ${token}`);
      }
      const response = await window.fetch(url, {
        headers,
        credentials: 'same-origin',
        signal: controller.signal,
      });
      if (!response.ok) {
        throw new Error(`Event stream request failed (${response.status})`);
      }
      if (!response.body) {
        throw new Error('Event stream response body is unavailable');
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const blocks = buffer.split(/\r?\n\r?\n/);
        buffer = blocks.pop() || '';
        blocks.forEach((block) => readEventBlock<T>(block, onMessage));
      }
      buffer += decoder.decode();
      if (buffer.trim()) {
        readEventBlock<T>(buffer, onMessage);
      }
    } catch (error) {
      failed = true;
      if (!closed) {
        onError(error);
      }
    } finally {
      if (!closed && !failed) {
        onClose?.();
      }
    }
  };

  void consume();
  return {
    close: () => {
      if (closed) return;
      closed = true;
      controller.abort();
    },
  };
};

// create an axios instance
const service = axios.create({
  // baseURL: getBaseUrl(), // url = base url + request url
  withCredentials: true, // send cookies when cross-domain requests
  timeout: 600000, // request timeout
  transformResponse: [
    function (data) {
      try {
        return JSON.parse(data);
        // eslint-disable-next-line @typescript-eslint/no-unused-vars
      } catch (e) {
        return data;
      }
    },
  ],
});

/**
 * 增加底层参数序列化的通用逻辑
 * @param params 待序列化的参数
 * @returns
 */
service.defaults.paramsSerializer = function (params) {
  return qs.stringify(params, {
    indices: false,
    encoder: function (str: string, encoder, ...arr: []) {
      if (str.length === 0) {
        return str;
      }
      let string = str;
      if (typeof str !== 'string') {
        string = String(str);
      }
      return encoder((string || '').trim(), encoder, ...arr);
    },
  });
};

// request interceptor
service.interceptors.request.use(
  (config) => {
    const token = getToken();
    if (token) {
      config.headers['X-Sa-Token'] = token;
      config.headers.Authorization = `Bearer ${token}`;
    }
    return config;
  },
  (error) => {
    return Promise.reject(error);
  }
);

service.interceptors.response.use(
  (response) => {
    const res = response.data;
    const config: AxiosWrapperRequestConfig = response.config; // 获取请求的配置参数

    // 获取业务数据的响应码。接下来的情况，根据业务状态码判断并处理
    const code = res?.code;
    if (response?.request?.responseURL?.includes('/403') && response?.request?.responseText?.includes('html>')) {
      if (isInIframe([], 'webview')) {
        // omc特殊处理
        if (location.pathname !== '/403' && location.pathname !== '/freeLogin') {
          location.href = '/403';
        }
        return Promise.reject({ code: 403 });
      }
      // 特殊处理403弹框（后端接口直接重定向了）
      if (config?.[CustomAxiosConfigEnum.NoMessage] !== true) {
        notification.error({
          message: getIntl(
            'common.noPermissionMessage',
            {},
            "You don't have permission to do this, contact your administrator."
          ),
          // description: response.config?.url,
          duration: 2,
          showProgress: true,
        });
      }
      return Promise.reject({ code: 403 });
    }

    if (config?.[CustomAxiosConfigEnum.NoCode]) {
      return Promise.resolve(res);
    }
    if (code === BusinessCodeEnum.Success || code === BusinessCodeEnum.Success_Two) {
      if (config?.[CustomAxiosConfigEnum.BusinessResponse]) {
        // 如果要返回所有，请配置_businessResponse: true
        return Promise.resolve(res);
      }
      // 如果请求成功，直接返回数据
      return Promise.resolve(res.data);
    } else {
      if (config?.[CustomAxiosConfigEnum.NoMessage] !== true) {
        // HttpCode.Fail 的情况，这里提示，上层接失败处理
        message.error(
          res.msg
            ? getIntl(res.msg, {}, res.msg)
            : getIntl('common.serverBusy', {}, 'Server is busy, please try again later')
        );
      }
    }
    // 如果 是业务上的错误，reject {code,msg,data} 给业务层自己处理
    return Promise.reject({ ...res, config });
  },
  (error) => {
    const err = {
      code: BusinessCodeEnum.Fail,
      msg: error.message,
      data: error.response?.data?.data,
      error: error.response?.data?.error,
      config: error.config,
    };
    const config: AxiosWrapperRequestConfig | undefined = error.config;
    const showDefaultMessage = config?.[CustomAxiosConfigEnum.NoMessage] !== true;
    if (error.message === 'canceled') {
      // 取消请求
      console.log('error-msg: canceled');
    } else {
      if (error.response) {
        const responseCode = error.response?.data?.code;
        const responseMsg = error.response?.data?.msg;
        if (responseCode !== undefined) {
          err.code = responseCode;
        } else if (error.response.status === 403 || error.response.status === 404) {
          // 中间件返回纯文本 403/404 时 body 无 code 字段，用 HTTP status 兜底
          err.code = error.response.status;
        }
        if (responseMsg) {
          err.msg = responseMsg;
        }
        if (error.response.status === 401) {
          return Promise.reject(error);
        } else if (error.response.status === 403 && showDefaultMessage) {
          message.error(getIntl('common.noPermission', {}, 'No Permission'));
        } else if (error.response.status === 404 && showDefaultMessage) {
          message.error(getIntl('common.interfaceNotExist', {}, 'Interface does not exist'));
        } else if (showDefaultMessage) {
          message.error(
            responseMsg
              ? getIntl(responseMsg, {}, responseMsg)
              : getIntl('common.serverBusy', {}, 'Server is busy, please try again later')
          );
        }
      } else if (error.code === 'ECONNABORTED' && showDefaultMessage) {
        message.error(
          getIntl('common.requestTimeout', {}, 'Request timed out. Please check your network and try again.')
        );
      } else if (error.message === 'Network Error' && showDefaultMessage) {
        message.error(getIntl('common.networkFailed', {}, 'Network connection failed, please check your network'));
      } else if (showDefaultMessage) {
        message.error(getIntl('common.serverBusy', {}, 'Server is busy, please try again later'));
      }
    }

    return Promise.reject(err);
  }
);

/**
 * axios instance 的包装对象。
 */
export class ApiWrapper {
  baseUrl = '';
  constructor(requestBaseUrl = '') {
    const hostUrl = `${window.location.host}`;
    this.baseUrl = `//${hostUrl}${requestBaseUrl}`;
  }

  private request = async (apiConfig: AxiosWrapperRequestConfig): Promise<any> => {
    try {
      return await service.request(apiConfig);
    } catch (error) {
      return Promise.reject(error);
    }
  };

  put = <T = any>(url: string, data?: T, config?: AxiosWrapperRequestConfig) => {
    return this.request({
      url: this.baseUrl + url,
      method: 'put',
      data,
      ...config,
    });
  };

  post = <T = any>(url: string, data?: T, config?: AxiosWrapperRequestConfig) => {
    return this.request({
      url: this.baseUrl + url,
      method: 'post',
      data,
      ...config,
    });
  };

  get = (url: string, config?: AxiosWrapperRequestConfig) => {
    return this.request({
      url: this.baseUrl + url,
      method: 'get',
      ...config,
    });
  };

  delete = <T = any>(url: string, config?: AxiosWrapperRequestConfig, data?: T) => {
    return this.request({
      url: this.baseUrl + url,
      method: 'delete',
      data,
      ...config,
    });
  };

  upload = <T extends { name: string; value: any; fileName: string }>(
    url: string,
    fileItem: T,
    config?: AxiosWrapperRequestConfig
  ) => {
    const fd = new FormData();
    fd.append(fileItem.name, fileItem.value, fileItem.fileName);
    const configCopy = {
      ...config,
      headers: {
        ...(config?.headers || {}), // 浏览器会为 FormData 自动补 multipart boundary
      },
    };
    return this.request({ url: this.baseUrl + url, data: fd, method: 'put', ...configCopy });
  };
  uploads = <T extends { name: string; value: any; fileName: string }>(
    url: string,
    fileItemList: T[],
    config?: AxiosWrapperRequestConfig
  ) => {
    const fd = new FormData();
    fileItemList.forEach((fileItem) => {
      if (fileItem.value instanceof Blob) {
        fd.append(fileItem.name, fileItem.value, fileItem.fileName);
      } else {
        fd.append(fileItem.name, fileItem.value);
      }
    });
    const configCopy = {
      ...config,
      headers: {
        ...(config?.headers || {}), // 浏览器会为 FormData 自动补 multipart boundary
      },
    };
    return this.request({
      url: this.baseUrl + url,
      data: fd,
      method: 'put',
      ...configCopy,
    });
  };
}

export default service;
