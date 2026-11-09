import { coreApi } from './core-adapter';
import type { AxiosWrapperRequestConfig } from '@/utils/request';

// 视觉分析告警(sys_vision_alarm),供 Results 页告警列表展示。
export type VisionAlarm = {
  id: number;
  taskId: number;
  taskName?: string;
  cameraId: number;
  cameraName?: string;
  algorithmCode?: string;
  alarmType: string;
  message: string;
  eventId?: string;
  createdAt: number;
};

export type VisionAlarmListParams = {
  pageNo?: number;
  pageSize?: number;
  taskId?: number;
};

type VisionAlarmPage = {
  list?: VisionAlarm[];
  total?: number;
  page?: number;
  size?: number;
};

export const listVisionAlarms = async (params: VisionAlarmListParams = {}, config?: AxiosWrapperRequestConfig) => {
  const response = (await coreApi.get('/vision/alarms', {
    ...config,
    params: {
      page: params.pageNo,
      size: params.pageSize,
      taskId: params.taskId,
    },
  })) as VisionAlarmPage;
  return {
    data: response?.list || [],
    total: response?.total || 0,
    pageNo: response?.page || params.pageNo || 1,
    pageSize: response?.size || params.pageSize || 20,
  };
};

// 任务下各相机的最新识别结果(sys_vision_latest_result)。
export type VisionLatestResult = {
  taskId: number;
  cameraId: number;
  cameraName?: string;
  eventId?: string;
  objectCount: number;
  payload?: Record<string, unknown>;
  updatedAt: number;
};

type VisionLatestResultResp = {
  list?: VisionLatestResult[];
  taskId?: number;
};

export const getTaskLatestResult = async (id: number | string, config?: AxiosWrapperRequestConfig) => {
  const response = (await coreApi.get(`/vision/tasks/${id}/latest-result`, config)) as VisionLatestResultResp;
  return response?.list || [];
};

// 视觉分析事件(告警+数据结果统一流),Results 页事件卡片流展示。
export type VisionEventType = 'alarm' | 'data';
export type VisionReviewStatus = 'pending' | 'confirmed' | 'false_positive';

export type VisionEventSort = 'latest' | 'oldest';

export type VisionEventResultItem = {
  label: string;
  value: string;
};

export type VisionEvent = {
  id: number;
  taskId: number;
  taskName?: string;
  cameraId: number;
  cameraName?: string;
  algorithmCode?: string;
  algorithmName?: string;
  eventType: VisionEventType;
  reviewStatus: VisionReviewStatus;
  reviewRevision: number;
  reviewedBy?: number;
  reviewedAt?: number | null;
  eventName: string;
  level?: string;
  message?: string;
  resultContent?: VisionEventResultItem[];
  hasScreenshot: boolean;
  eventId?: string;
  evidenceId?: string;
  unsTopic?: string;
  retention?: string;
  createdAt: number;
};

export type VisionEventDetail = VisionEvent & {
  algorithmVersion?: string;
  modelVersion?: string;
};

export type VisionEventListParams = {
  pageNo?: number;
  pageSize?: number;
  search?: string;
  cameraId?: number;
  eventName?: string;
  taskId?: number;
  sort?: VisionEventSort;
  startTime?: number;
  endTime?: number;
  eventType?: VisionEventType;
  reviewStatus?: VisionReviewStatus;
};

type VisionEventPage = {
  list?: VisionEvent[];
  total?: number;
  page?: number;
  size?: number;
  eventNames?: string[];
};

export const listVisionEvents = async (params: VisionEventListParams = {}, config?: AxiosWrapperRequestConfig) => {
  const response = (await coreApi.get('/vision/events', {
    ...config,
    params: {
      page: params.pageNo,
      size: params.pageSize,
      search: params.search,
      cameraId: params.cameraId,
      eventName: params.eventName,
      taskId: params.taskId,
      sort: params.sort,
      startTime: params.startTime,
      endTime: params.endTime,
      eventType: params.eventType,
      reviewStatus: params.reviewStatus,
    },
  })) as VisionEventPage;
  return {
    data: response?.list || [],
    total: response?.total || 0,
    pageNo: response?.page || params.pageNo || 1,
    pageSize: response?.size || params.pageSize || 15,
    eventNames: response?.eventNames || [],
  };
};

export const getVisionEvent = (id: number | string) =>
  coreApi.get(`/vision/events/${id}`) as Promise<VisionEventDetail>;

export const deleteVisionEvent = (id: number | string) => coreApi.delete(`/vision/events/${id}`);

export const batchDeleteVisionEvents = (ids: number[]) => coreApi.post('/vision/events/batch-delete', { ids });

// 截图直接作为 <img src> 使用,浏览器同源请求自动携带 Cookie。
export const eventScreenshotUrl = (id: number | string) => `/api/core/vision/events/${id}/screenshot`;

export const reviewVisionEvent = (event: VisionEvent, status: VisionReviewStatus) =>
  coreApi.post(
    `/vision/events/${event.id}/review`,
    { status, revision: event.reviewRevision },
    { _noMessage: true }
  ) as Promise<VisionEventDetail>;
