import { coreApi } from './core-adapter';
import type { AxiosWrapperRequestConfig } from '@/utils/request';

export type VisionModelSource = 'builtin' | 'custom';
export type VisionModelStatus = 'available' | 'missing' | 'error';

export type VisionModel = {
  id: number;
  modelCode: string;
  name: string;
  version: string;
  source: VisionModelSource;
  status: VisionModelStatus;
  modelType: string;
  labels?: string[];
  license: string;
  fileName: string;
  sizeBytes: number;
  refCount: number;
  createdAt: number;
};

export type VisionModelListParams = {
  pageNo?: number;
  pageSize?: number;
  search?: string;
  source?: string;
};

type VisionModelPage = {
  list?: VisionModel[];
  total?: number;
  page?: number;
  size?: number;
};

export const listVisionModels = async (params: VisionModelListParams = {}, config?: AxiosWrapperRequestConfig) => {
  const response = (await coreApi.get('/vision/models', {
    ...config,
    params: {
      page: params.pageNo,
      size: params.pageSize,
      search: params.search,
      source: params.source,
    },
  })) as VisionModelPage;
  return {
    data: response?.list || [],
    total: response?.total || 0,
    pageNo: response?.page || params.pageNo || 1,
    pageSize: response?.size || params.pageSize || 20,
  };
};

// 上传 ONNX 模型文件,创建模型记录。
export const uploadVisionModel = (file: File) => {
  const fd = new FormData();
  fd.append('file', file, file.name);
  return coreApi.post('/vision/models', fd) as Promise<VisionModel>;
};

export type VisionModelUpdatePayload = {
  name?: string;
  license?: string;
  sourceUrl?: string;
  modelType?: string;
  inputWidth?: number;
  inputHeight?: number;
  labels?: string[];
};

export const updateVisionModel = (id: number | string, payload: VisionModelUpdatePayload) =>
  coreApi.put(`/vision/models/${id}`, payload) as Promise<VisionModel>;

// 删除模型(被算法引用时后端返回错误)。
export const deleteVisionModel = (id: number | string) => coreApi.delete(`/vision/models/${id}`);
