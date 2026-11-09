import { coreApi } from './core-adapter';
import type { AxiosWrapperRequestConfig } from '@/utils/request';

export type AlgorithmSource = 'builtin' | 'custom';
export type AlgorithmModelStatus = 'available' | 'missing' | 'error';
export type AlgorithmType =
  'object_detection' | 'helmet' | 'intrusion' | 'line_counting' | 'ppe' | 'behavior' | 'interaction' | 'environment';

// 算法行为定义(definition JSON):输出项、空间配置类型、推送间隔显隐等。
export type AlgorithmDefinition = {
  outputs?: string[];
  spatialMode?: 'zones' | 'line' | 'none';
  showPushInterval?: boolean;
  paramsSchema?: Record<string, unknown>;
  rules?: Record<string, unknown>;
};

// 算法关联模型的摘要视图。
export type AlgorithmModelRef = {
  id: number;
  name: string;
  version: string;
  status: AlgorithmModelStatus;
  modelType: string;
  inputWidth?: number;
  inputHeight?: number;
};

export type VisionAlgorithm = {
  id: number;
  algorithmCode: string;
  name: string;
  source: AlgorithmSource;
  algoType: AlgorithmType;
  description: string;
  scene: string;
  labels: string[];
  defaultOutput: string[];
  modelFormat: string;
  modelStatus: AlgorithmModelStatus;
  modelFile: string;
  modelError: string;
  modelId?: number;
  model?: AlgorithmModelRef | null;
  definition?: AlgorithmDefinition | null;
  createdAt: number;
  updatedAt: number;
};

export type VisionAlgorithmListParams = {
  pageNo?: number;
  pageSize?: number;
  search?: string;
  source?: string;
  algoType?: string;
  modelStatus?: string;
};

type VisionAlgorithmPage = {
  list?: VisionAlgorithm[];
  total?: number;
  page?: number;
  size?: number;
};

export const listVisionAlgorithms = async (
  params: VisionAlgorithmListParams = {},
  config?: AxiosWrapperRequestConfig
) => {
  const response = (await coreApi.get('/vision/algorithms', {
    ...config,
    params: {
      page: params.pageNo,
      size: params.pageSize,
      search: params.search,
      source: params.source,
      algoType: params.algoType,
      modelStatus: params.modelStatus,
    },
  })) as VisionAlgorithmPage;
  return {
    data: response?.list || [],
    total: response?.total || 0,
    pageNo: response?.page || params.pageNo || 1,
    pageSize: response?.size || params.pageSize || 20,
  };
};

export const getVisionAlgorithm = (id: number | string) =>
  coreApi.get(`/vision/algorithms/${id}`) as Promise<VisionAlgorithm>;

export type VisionAlgorithmCreatePayload = {
  algorithmCode: string;
  name: string;
  algoType: AlgorithmType;
  description?: string;
  scene?: string;
  labels?: string[];
  defaultOutput?: string[];
  modelId?: number;
  // definition JSON 字符串(Phase A 前端不做可视化编辑,不传或传空)。
  definition?: string;
};

export const createVisionAlgorithm = (payload: VisionAlgorithmCreatePayload) =>
  coreApi.post('/vision/algorithms', payload) as Promise<VisionAlgorithm>;

export type VisionAlgorithmUpdatePayload = {
  name: string;
  description?: string;
  scene?: string;
  labels?: string[];
  defaultOutput?: string[];
  modelId?: number;
  // definition JSON 字符串(Phase A 前端不做可视化编辑,不传或传空)。
  definition?: string;
};

export const updateVisionAlgorithm = (id: number | string, payload: VisionAlgorithmUpdatePayload) =>
  coreApi.put(`/vision/algorithms/${id}`, payload) as Promise<VisionAlgorithm>;

// 上传/更新 ONNX 模型文件
export const uploadAlgorithmModel = (id: number | string, file: File) => {
  const fd = new FormData();
  fd.append('file', file, file.name);
  return coreApi.post(`/vision/algorithms/${id}/model`, fd) as Promise<VisionAlgorithm>;
};

// 导入 ZIP 算法包
export const importAlgorithmPackage = (file: File) => {
  const fd = new FormData();
  fd.append('file', file, file.name);
  return coreApi.post('/vision/algorithms/import', fd) as Promise<VisionAlgorithm>;
};

export const deleteVisionAlgorithm = (id: number | string) => coreApi.delete(`/vision/algorithms/${id}`);
