import { coreApi } from './core-adapter';
import type { AxiosWrapperRequestConfig } from '@/utils/request';

export type CameraStatus = 1 | 2;
export type CameraTransport = 'rtsp' | 'rtmp_pull';
export type CameraRtpType = 'tcp' | 'udp';
export type CameraLiveStatus = 'online' | 'offline' | 'checking' | 'unknown';

export type VideoCamera = {
  id: number;
  cameraCode: string;
  name: string;
  location: string;
  description: string;
  status: CameraStatus;
  channelId: number;
  channelCode: string;
  transport: CameraTransport;
  sourceUrl: string;
  rtpType: CameraRtpType;
  app: string;
  stream: string;
  liveStatus: CameraLiveStatus;
  resolution: string;
  lastError: string;
  lastTestedAt: number;
  hasCredentials: boolean;
  createdAt: number;
  updatedAt: number;
};

export type VideoCameraPayload = {
  cameraCode: string;
  name: string;
  location?: string;
  description?: string;
  status: CameraStatus;
  sourceUrl: string;
  transport: CameraTransport;
  rtpType: CameraRtpType;
};

export type VideoCameraListParams = {
  pageNo?: number;
  pageSize?: number;
  search?: string;
  status?: number;
  transport?: string;
};

type VideoCameraPage = {
  list?: VideoCamera[];
  total?: number;
  page?: number;
  size?: number;
};

export const listVideoCameras = async (params: VideoCameraListParams = {}, config?: AxiosWrapperRequestConfig) => {
  const response = (await coreApi.get('/video/cameras', {
    ...config,
    params: {
      page: params.pageNo,
      size: params.pageSize,
      search: params.search,
      status: params.status,
      transport: params.transport,
    },
  })) as VideoCameraPage;
  return {
    data: response?.list || [],
    total: response?.total || 0,
    pageNo: response?.page || params.pageNo || 1,
    pageSize: response?.size || params.pageSize || 20,
  };
};

export const getVideoCamera = (id: number | string) => coreApi.get(`/video/cameras/${id}`) as Promise<VideoCamera>;

export const createVideoCamera = (payload: VideoCameraPayload) =>
  coreApi.post('/video/cameras', payload) as Promise<VideoCamera>;

export const updateVideoCamera = (id: number | string, payload: VideoCameraPayload) =>
  coreApi.put(`/video/cameras/${id}`, payload) as Promise<VideoCamera>;

export const deleteVideoCamera = (id: number | string) => coreApi.delete(`/video/cameras/${id}`);

export type CameraProbeParams = {
  cameraId?: number | string;
  sourceUrl?: string;
  transport?: CameraTransport;
  rtpType?: CameraRtpType;
};

export type CameraProbeResult = {
  reachable: boolean;
  resolution: string;
  reason?: string;
  /** 探测出画时后端顺带抓的一帧(data:image/jpeg;base64,...),用于新建表单里的预览。 */
  snapshot?: string;
};

// 测试连接：验证地址可达性与分辨率，不落库
export const probeVideoCamera = (params: CameraProbeParams) =>
  coreApi.post('/video/cameras/probe', params) as Promise<CameraProbeResult>;

// 重新连接：重新验证已保存摄像头并刷新连接状态
export const reconnectVideoCamera = (id: number | string) =>
  coreApi.post(`/video/cameras/${id}/reconnect`, {}) as Promise<VideoCamera>;

// 预览信令：把浏览器 offer SDP 交给媒体平面，换取 answer SDP
export const previewVideoCamera = (id: number | string, sdp: string, config?: AxiosWrapperRequestConfig) =>
  coreApi.post(`/video/cameras/${id}/preview`, { sdp }, config) as Promise<{ sdp: string }>;

// 结束预览：关闭预览拉流
export const stopVideoCameraPreview = (id: number | string) => coreApi.delete(`/video/cameras/${id}/preview`);
