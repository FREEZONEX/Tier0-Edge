import { coreApi } from './core-adapter';
import type { AxiosWrapperRequestConfig } from '@/utils/request';
import type { AlgorithmDefinition, AlgorithmModelStatus, AlgorithmType } from './algorithm';

// idle:任务已部署且 worker 存活,但当前不在运行计划的时间窗口内,不做推理。
export type VisionTaskStatus =
  'stopped' | 'starting' | 'running' | 'stopping' | 'error' | 'offline' | 'recovering' | 'idle';

export type VisionTaskCamera = { id: number; name: string; code: string; unsTopic?: string };

export type VisionTaskParams = {
  confThreshold?: number;
  iouThreshold?: number;
  inputWidth?: number;
  inputHeight?: number;
  detectionFrequency?: 'low' | 'standard' | 'high' | 'custom';
  samplingIntervalMs?: number;
  resultRetention?: 'permanent' | '30' | '60' | '180' | '365';
  resultPushIntervalMs?: number;
};

export type VisionScheduleRule = { weekdays: number[]; start: string; end: string };
export type VisionSchedule = { mode?: 'allDay' | 'custom'; timezone?: string; rules?: VisionScheduleRule[] };

// 检测区域(多边形)与计数线,坐标归一化 0..1。
export type VisionPoint = { x: number; y: number };
export type VisionZone = { name?: string; enabled?: boolean; points: VisionPoint[] };
export type VisionCountingLine = { start: VisionPoint; end: VisionPoint };
export type VisionCameraRegion = { zones?: VisionZone[]; countingLine?: VisionCountingLine };

export type VisionTaskAlgorithm = {
  id: number;
  name: string;
  algoType: AlgorithmType;
  modelStatus: AlgorithmModelStatus;
  definition?: AlgorithmDefinition | null;
};

export type VisionTask = {
  id: number;
  name: string;
  note: string;
  status: VisionTaskStatus;
  desiredStatus: 'stopped' | 'running';
  observedStatus: VisionTaskStatus;
  observedReason: string;
  lastHeartbeatAt: number | null;
  cameraIds: number[];
  cameras: VisionTaskCamera[];
  algorithmId: number;
  algorithm: VisionTaskAlgorithm | null;
  algorithmMissing: boolean;
  outputCategories: string[];
  regions?: Record<string, VisionCameraRegion>;
  params?: VisionTaskParams;
  parameterOverrides?: VisionTaskParams | null;
  schedule?: VisionSchedule;
  processedFrames: number;
  detectedObjects: number;
  /** 各相机最新一帧检出目标数之和(当前画面目标数,区别于累计 detectedObjects) */
  currentTargets: number;
  lastError: string;
  unsTopic: string;
  createdAt: number;
  updatedAt: number;
  configRevision?: string;
};

export type VisionTaskListParams = {
  pageNo?: number;
  pageSize?: number;
  search?: string;
  status?: string;
};

type VisionTaskPage = {
  list?: VisionTask[];
  total?: number;
  page?: number;
  size?: number;
};

export const listVisionTasks = async (params: VisionTaskListParams = {}, config?: AxiosWrapperRequestConfig) => {
  const response = (await coreApi.get('/vision/tasks', {
    ...config,
    params: {
      page: params.pageNo,
      size: params.pageSize,
      search: params.search,
      status: params.status,
    },
  })) as VisionTaskPage;
  return {
    data: response?.list || [],
    total: response?.total || 0,
    pageNo: response?.page || params.pageNo || 1,
    pageSize: response?.size || params.pageSize || 20,
  };
};

export type VisionTaskCreatePayload = {
  expectedRevision?: string;
  name: string;
  note?: string;
  cameraIds: number[];
  algorithmId: number;
  outputCategories?: string[];
  params?: VisionTaskParams;
  schedule?: VisionSchedule;
  regions?: { cameraId: number; zones?: VisionZone[]; countingLine?: VisionCountingLine | null }[];
};

export const createVisionTask = (payload: VisionTaskCreatePayload) =>
  coreApi.post('/vision/tasks', payload) as Promise<VisionTask>;

export const updateVisionTask = (
  id: number | string,
  payload: VisionTaskCreatePayload,
  config?: AxiosWrapperRequestConfig
) => coreApi.put(`/vision/tasks/${id}`, payload, config) as Promise<VisionTask>;

export const getVisionTask = (id: number | string) => coreApi.get(`/vision/tasks/${id}`) as Promise<VisionTask>;

export const deleteVisionTask = (id: number | string) => coreApi.delete(`/vision/tasks/${id}`);

export const startVisionTask = (id: number | string) =>
  coreApi.post(`/vision/tasks/${id}/start`, {}) as Promise<VisionTask>;

export const stopVisionTask = (id: number | string) =>
  coreApi.post(`/vision/tasks/${id}/stop`, {}) as Promise<VisionTask>;

export type VisionTaskTestFrameResult = {
  ok: boolean;
  error?: string;
  cameraId?: number | string;
  algorithmId?: number | string;
  inferenceMs?: number;
  capturedAt?: number;
  modelVersion?: string;
  frame?: { width: number; height: number };
  image?: string;
  metrics?: Record<string, unknown>;
  states?: Record<string, unknown>;
  objects?: Array<Record<string, unknown>>;
  [key: string]: unknown;
};

export const testVisionTaskFrame = (id: number | string, cameraId?: number, config?: AxiosWrapperRequestConfig) =>
  coreApi.post(
    `/vision/tasks/${id}/test-frame`,
    { cameraId },
    { ...config, timeout: 65000 }
  ) as Promise<VisionTaskTestFrameResult>;

export const saveTaskRegions = (
  id: number | string,
  payload: { cameraId: number; zones?: VisionZone[]; countingLine?: VisionCountingLine | null }
) => coreApi.put(`/vision/tasks/${id}/regions`, payload) as Promise<VisionTask>;

// 相机快照 URL(编辑器底图)。浏览器带 Cookie 直接作为 <img src>。
export const cameraSnapshotUrl = (cameraId: number | string) =>
  `/api/core/video/cameras/${cameraId}/snapshot?t=${Date.now()}`;
