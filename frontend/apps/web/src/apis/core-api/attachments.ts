// 模板实例的文件上传

import { CustomAxiosConfigEnum } from '@/utils/request';
import { coreApi } from './core-adapter';

// 获取列表
export const getAttachmentsList = async (params?: Record<string, any>) => {
  if (!params?.ownerId) return { list: [], total: 0 };
  return coreApi.get('/assets', {
    params: {
      ownerType: 'unsNode',
      ownerId: params?.ownerId,
    },
  });
};
// 删除
export const deleteAttachments = async (params?: Record<string, any>) => {
  const bindingId = params?.bindingId || params?.id;
  if (!bindingId) return {};
  return coreApi.delete(`/asset-bindings/${bindingId}`);
};
// 下载
export const getAttachment = async (params?: Record<string, unknown>) =>
  coreApi.get(`/assets/${params?.objectName || params?.fileId || params?.id}/download`, {
    responseType: 'blob',
    [CustomAxiosConfigEnum.NoCode]: true,
  });
// 上传（params.signal 可选：透传给 axios，供调用方中止上传）
export const uploadAttachment = async (data: any, params: any) => {
  const query: Record<string, any> = {};
  if (params?.ownerId) {
    query.ownerType = params?.ownerType || 'unsNode';
    query.ownerId = params.ownerId;
  }
  return coreApi.uploads(`/assets`, data, {
    method: 'post',
    params: query,
    signal: params?.signal,
  });
};

// 分片直传：初始化上传会话（大文件专用，返回 fileKey/uploadId/分片大小等信息）
// 注意：projectId 在 URL/上下文里是字符串，但后端 init 请求体要求 int64，必须转数字
//（字符串会触发 go-zero "type mismatch for field projectId" 400）。
export const initMultipartUpload = async (data: {
  projectId: string;
  fileName: string;
  contentType: string;
  size: number;
  partSize?: number;
}) =>
  coreApi.post(
    '/assets/multipart/init',
    { ...data, projectId: Number(data.projectId) },
    {
      [CustomAxiosConfigEnum.NoMessage]: true,
    }
  );
// 分片直传：获取分片预签名上传 URL
export const getMultipartPartUrls = async (data: { fileKey: string; uploadId: string; partNumbers: number[] }) =>
  coreApi.post('/assets/multipart/part-urls', data, {
    [CustomAxiosConfigEnum.NoMessage]: true,
  });
// 分片直传：合并分片，返回 fileId 等最终文件信息
export const completeMultipartUpload = async (data: {
  fileKey: string;
  uploadId: string;
  parts: { partNumber: number; etag: string }[];
}) =>
  coreApi.post('/assets/multipart/complete', data, {
    [CustomAxiosConfigEnum.NoMessage]: true,
  });
// 分片直传：中止上传会话，释放服务端资源
export const abortMultipartUpload = async (data: { fileKey: string; uploadId: string }) =>
  coreApi.post('/assets/multipart/abort', data, {
    [CustomAxiosConfigEnum.NoMessage]: true,
  });
