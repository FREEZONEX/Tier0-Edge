// 分片直传 + 断点续传上传工具
//
// 大文件（>20MB）走后端 core multipart 端点（POST /api/core/assets/multipart/{init,part-urls,complete,abort}）：
// 先在服务端初始化上传会话，拿到各分片的预签名 URL 后由浏览器并发直传，全部完成后调用 complete 合并并返回 fileId。
// 上传会话（ResumeSession）由调用方持久化（如 sessionStorage），页面刷新后可跳过已完成分片继续上传。
// 小于等于 20MB 的小文件沿用原 uploadAttachment 单发上传，保持兼容。

import {
  abortMultipartUpload,
  completeMultipartUpload,
  getMultipartPartUrls,
  initMultipartUpload,
  uploadAttachment,
} from '@/apis/core-api';

/** 单个分片的最小大小（服务端分片下限） */
export const MIN_PART_SIZE = 5 * 1024 * 1024;
/** 超过该大小自动切换分片直传；小于等于该大小沿用原单发上传接口 */
export const MULTIPART_THRESHOLD = 20 * 1024 * 1024;
/** 默认分片大小 */
export const DEFAULT_PART_SIZE = 10 * 1024 * 1024;
/** 单片直传停滞判定（毫秒）：超过该时长没有任何传输进展才中止重试。
 * 注意不是单片总超时——弱网（跨境/小上行带宽）下单片传几分钟是正常的，
 * 只要有字节在流动就让它继续，避免"传到一半被超时杀掉重传"的死循环。 */
export const PART_STALL_TIMEOUT_MS = 60 * 1000;
/** 单片直传失败后的重试次数 */
export const PART_UPLOAD_MAX_RETRIES = 3;
/** 默认并发上传分片数 */
export const DEFAULT_CONCURRENCY = 4;

export interface MultipartInitResult {
  fileKey: string;
  filePath: string;
  uploadId: string;
  partSize: number;
  partCount: number;
  expiresAt?: string;
}

/** 上传完成（或单发上传）后的统一返回结构，fileId 用于后续业务接口 */
export interface MultipartCompleteResult {
  fileId: string | number;
  filePath?: string;
  fileUrl?: string;
  sizeBytes?: number;
  expiresAt?: string;
  [key: string]: unknown;
}

/** 上传会话（断点续传持久化单元） */
export interface ResumeSession {
  projectId: string;
  fileName: string;
  size: number;
  fileKey: string;
  uploadId: string;
  partSize: number;
  /** 已成功上传的分片（从 1 开始，含 ETag；complete 需要提交全部分片的 (partNumber, etag)） */
  completedParts: { partNumber: number; etag: string }[];
  updatedAt: number;
  /** 文件内容指纹（SHA-256 hex）。续传前必须校验，避免同名同大小的不同文件拼出混合分片 */
  fingerprint?: string;
}

export interface UploadProgress {
  doneParts: number;
  totalParts: number;
  percent: number;
  /** 已传输字节数（含分片内进行中的字节），用于细粒度进度展示 */
  loadedBytes?: number;
  /** 文件总字节数 */
  totalBytes?: number;
}

export interface MultipartUploadOptions {
  projectId: string;
  /** 期望分片大小（会被 clamp 到 >= MIN_PART_SIZE） */
  partSize?: number;
  /** 并发上传分片数，默认 4 */
  concurrency?: number;
  /** 分片进度回调 */
  onProgress?: (progress: UploadProgress) => void;
  /** 会话变更回调（init 完成后及每上传一片后触发，便于调用方持久化断点） */
  onSessionChange?: (session: ResumeSession) => void;
  /** 取消信号（取消进行中的分片直传） */
  signal?: AbortSignal;
  /** 断点续传会话（存在时跳过已完成分片直接续传，失败时保留会话不中止服务端资源） */
  resumeSession?: ResumeSession | null;
  /** 文件内容指纹（SHA-256 hex，见 calcFileFingerprint）。新建会话时写入会话，供下次续传前校验 */
  fingerprint?: string;
}

/** 计算文件 SHA-256 指纹（hex）。大文件可能耗时 1-3 秒，仅分片上传场景需要 */
export const calcFileFingerprint = async (file: File): Promise<string> => {
  const digest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer());
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('');
};

/** 计算分片总数 */
export const calcPartCount = (size: number, partSize: number) => Math.max(1, Math.ceil(size / partSize));

/** 计算上传进度百分比（0-100） */
export const calcUploadPercent = (doneParts: number, totalParts: number) => {
  if (totalParts <= 0) return 0;
  return Math.min(100, Math.round((Math.max(0, doneParts) / totalParts) * 100));
};

const clampPartSize = (partSize?: number) => {
  if (!partSize || !Number.isFinite(partSize)) return DEFAULT_PART_SIZE;
  return Math.max(MIN_PART_SIZE, Math.floor(partSize));
};

/**
 * 直传单个分片。
 * 预签名 URL 签名通常不含 Content-Type，因此用空 type 的 Blob 包裹切片，
 * 避免浏览器自动附加 file 的 Content-Type 导致签名校验失败。
 *
 * 用 XMLHttpRequest 而不是 fetch：fetch 无法拿到上传过程的字节进度，
 * 弱网下单片传几分钟时 UI 进度一直停在 0%，且无法区分"停滞"和"慢但在动"。
 * 这里用 upload.onprogress 做停滞检测——只要有字节在流动就重置计时，
 * 超过 PART_STALL_TIMEOUT_MS 毫无进展才中止交给重试。
 */
const uploadPartOnce = async (
  url: string,
  blob: Blob,
  signal?: AbortSignal,
  onBytes?: (loaded: number) => void
): Promise<string> =>
  new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    let settled = false;
    let stalled = false;
    const onExternalAbort = () => xhr.abort();
    let stallTimer: ReturnType<typeof setTimeout>;
    const onStall = () => {
      stalled = true;
      xhr.abort();
    };
    const resetStallTimer = () => {
      clearTimeout(stallTimer);
      stallTimer = setTimeout(onStall, PART_STALL_TIMEOUT_MS);
    };
    const settle = (fn: () => void) => {
      if (settled) return;
      settled = true;
      clearTimeout(stallTimer);
      signal?.removeEventListener('abort', onExternalAbort);
      fn();
    };

    xhr.upload.addEventListener('progress', (event) => {
      resetStallTimer();
      onBytes?.(event.loaded);
    });
    xhr.addEventListener('load', () =>
      settle(() => {
        if (xhr.status >= 200 && xhr.status < 300) {
          const etag = (xhr.getResponseHeader('ETag') || '').replace(/^"|"$/g, '').trim();
          if (etag) {
            resolve(etag);
          } else {
            reject(new Error('upload part failed: missing etag'));
          }
        } else {
          reject(new Error(`upload part failed: http ${xhr.status}`));
        }
      })
    );
    xhr.addEventListener('error', () => settle(() => reject(new Error('upload part failed: network error'))));
    xhr.addEventListener('abort', () =>
      settle(() =>
        reject(
          stalled
            ? new Error('upload part stalled: no transfer progress')
            : new DOMException('The operation was aborted.', 'AbortError')
        )
      )
    );

    xhr.open('PUT', url);
    resetStallTimer();
    if (signal) {
      if (signal.aborted) {
        xhr.abort();
        return;
      }
      signal.addEventListener('abort', onExternalAbort, { once: true });
    }
    xhr.send(new Blob([blob], { type: '' }));
  });

const uploadPartWithRetry = async (
  url: string,
  blob: Blob,
  signal?: AbortSignal,
  onBytes?: (loaded: number) => void
): Promise<string> => {
  let lastError: unknown;
  for (let attempt = 0; attempt <= PART_UPLOAD_MAX_RETRIES; attempt += 1) {
    if (signal?.aborted) {
      throw lastError ?? new DOMException('The operation was aborted.', 'AbortError');
    }
    try {
      return await uploadPartOnce(url, blob, signal, onBytes);
    } catch (error) {
      lastError = error;
      // 已取消的信号不再重试
      if (signal?.aborted) {
        throw error;
      }
      // 停滞重试前把分片内已传字节清零，避免重复计入整体进度
      onBytes?.(0);
    }
  }
  throw lastError;
};

/** 小文件（<=20MB）沿用原单发上传接口，返回与分片 complete 一致的统一结构 */
const uploadSingle = async (file: File, projectId: string, signal?: AbortSignal): Promise<MultipartCompleteResult> => {
  const res = await uploadAttachment([{ value: file, name: 'files', fileName: file.name }], {
    alias: '__templates__',
    ownerType: 'projectApp',
    ownerId: projectId,
    source: 'project',
    // 透传取消信号：弹窗关闭/移除文件时中止仍在进行的单发上传
    signal,
  });
  // 请求在中止前一刻完成的竞态兜底：结果作废，避免调用方把旧结果写进已重置的弹窗
  if (signal?.aborted) {
    throw new DOMException('The operation was aborted.', 'AbortError');
  }
  const list = res?.list ?? res?.data?.list;
  const item = (Array.isArray(list) ? list[0] : null) ?? res;
  const fileId = item?.fileId ?? item?.id ?? item?.objectName ?? item?.assetId;
  if (fileId === undefined || fileId === null || fileId === '') {
    throw new Error('upload-failed');
  }
  return {
    ...item,
    fileId,
    filePath: item?.filePath,
    fileUrl: item?.fileUrl,
    sizeBytes: Number(item?.sizeBytes ?? item?.size ?? 0) || undefined,
  };
};

/** 并发上传缺失分片，并把新传分片的 ETag 合并进 completed（同步写入 session 供持久化） */
const uploadMissingParts = async (
  file: File,
  session: ResumeSession,
  partSize: number,
  totalParts: number,
  completed: Map<number, string>,
  concurrency: number,
  onProgress?: (progress: UploadProgress) => void,
  onSessionChange?: (session: ResumeSession) => void,
  signal?: AbortSignal
): Promise<void> => {
  const missing = Array.from({ length: totalParts }, (_, i) => i + 1).filter((n) => !completed.has(n));
  if (missing.length === 0) {
    return;
  }

  // 一次拉取所有缺失分片的预签名 URL（分片数量有限，避免逐片多次往返）
  const partUrlsResp = await getMultipartPartUrls({
    fileKey: session.fileKey,
    uploadId: session.uploadId,
    partNumbers: missing,
  });
  const urlByPart = new Map<number, string>();
  (partUrlsResp?.partUrls || []).forEach((item: { partNumber?: number; url?: string }) => {
    if (item?.url) {
      urlByPart.set(Number(item.partNumber), item.url);
    }
  });

  const queue = [...missing];
  // 字节级进度：已完成分片的字节 + 进行中分片各自已传字节。
  // 弱网下单片可能传几分钟，按分片数跳变会显得"卡死"，按字节汇报才能看到持续进展。
  const partByteLength = (partNumber: number) => Math.min(partSize, file.size - (partNumber - 1) * partSize);
  let doneBytes = 0;
  for (const partNumber of completed.keys()) {
    doneBytes += partByteLength(partNumber);
  }
  const inFlightLoaded = new Map<number, number>();
  const reportProgress = () => {
    let inFlight = 0;
    for (const loaded of inFlightLoaded.values()) {
      inFlight += loaded;
    }
    const loadedBytes = Math.min(file.size, doneBytes + inFlight);
    onProgress?.({
      doneParts: completed.size,
      totalParts,
      // complete 前最多报 99%，100% 由 complete 成功后上报
      percent: Math.min(99, Math.round((loadedBytes / file.size) * 100)),
      loadedBytes,
      totalBytes: file.size,
    });
  };
  const worker = async () => {
    while (queue.length > 0) {
      const partNumber = queue.shift()!;
      const url = urlByPart.get(partNumber);
      if (!url) {
        throw new Error(`missing part url: ${partNumber}`);
      }
      const start = (partNumber - 1) * partSize;
      const end = Math.min(start + partSize, file.size);
      const etag = await uploadPartWithRetry(url, file.slice(start, end), signal, (loaded) => {
        inFlightLoaded.set(partNumber, loaded);
        reportProgress();
      });
      completed.set(partNumber, etag);
      doneBytes += partByteLength(partNumber);
      inFlightLoaded.delete(partNumber);
      session.completedParts = Array.from(completed, ([part, partEtag]) => ({ partNumber: part, etag: partEtag })).sort(
        (a, b) => a.partNumber - b.partNumber
      );
      session.updatedAt = Date.now();
      onSessionChange?.(session);
      reportProgress();
    }
  };
  await Promise.all(Array.from({ length: Math.min(concurrency, missing.length) }, () => worker()));
};

/**
 * 使用指定分片大小执行一次完整的分片上传（init 或续传 → 直传缺失分片 → complete）。
 *
 * 成功返回 complete 结果；失败抛出错误。失败时：
 * - 续传模式（传入了 resumeSession）保留服务端会话，调用方可再次续传；
 * - 全新上传失败时自动调用 abort 释放服务端资源。
 */
const uploadWithSession = async (
  file: File,
  options: MultipartUploadOptions,
  resumeSession?: ResumeSession | null,
  partSize?: number
): Promise<MultipartCompleteResult> => {
  const { projectId, onProgress, onSessionChange, signal } = options;
  const concurrency = Math.max(1, Math.floor(options.concurrency || DEFAULT_CONCURRENCY));

  if (signal?.aborted) {
    throw new DOMException('The operation was aborted.', 'AbortError');
  }

  let session: ResumeSession;
  if (resumeSession) {
    // 断点续传：复用已有会话，跳过已完成分片
    session = { ...resumeSession };
  } else {
    const initResult = await initMultipartUpload({
      projectId,
      fileName: file.name,
      contentType: file.type || 'application/octet-stream',
      size: file.size,
      partSize: clampPartSize(partSize),
    });
    session = {
      projectId,
      fileName: file.name,
      size: file.size,
      fileKey: initResult.fileKey,
      uploadId: initResult.uploadId,
      partSize: Number(initResult.partSize) || clampPartSize(partSize),
      completedParts: [],
      updatedAt: Date.now(),
      fingerprint: options.fingerprint,
    };
  }

  const effectivePartSize = Math.max(MIN_PART_SIZE, Number(session.partSize) || DEFAULT_PART_SIZE);
  const totalParts = calcPartCount(file.size, effectivePartSize);
  // 会话可能来自旧文件或服务端已重置，过滤非法分片号；缺 ETag 的分片视为未完成重新上传
  const completed = new Map<number, string>();
  for (const item of session.completedParts || []) {
    if (
      item &&
      Number.isInteger(item.partNumber) &&
      item.partNumber >= 1 &&
      item.partNumber <= totalParts &&
      typeof item.etag === 'string' &&
      item.etag
    ) {
      completed.set(item.partNumber, item.etag);
    }
  }

  onSessionChange?.(session);
  // 初始进度：续传场景按已完成分片的字节估算
  {
    let resumedBytes = 0;
    for (const partNumber of completed.keys()) {
      resumedBytes += Math.min(effectivePartSize, file.size - (partNumber - 1) * effectivePartSize);
    }
    onProgress?.({
      doneParts: completed.size,
      totalParts,
      percent: Math.min(99, Math.round((resumedBytes / file.size) * 100)),
      loadedBytes: resumedBytes,
      totalBytes: file.size,
    });
  }

  try {
    await uploadMissingParts(
      file,
      session,
      effectivePartSize,
      totalParts,
      completed,
      concurrency,
      onProgress,
      onSessionChange,
      signal
    );
    // uploadMissingParts 已把新传分片 etag 合并进 completed，汇总全部（含续传前已完成）分片后提交
    const parts = Array.from(completed, ([partNumber, etag]) => ({ partNumber, etag })).sort(
      (a, b) => a.partNumber - b.partNumber
    );
    const result = await completeMultipartUpload({
      fileKey: session.fileKey,
      uploadId: session.uploadId,
      parts,
    });
    onProgress?.({ doneParts: totalParts, totalParts, percent: 100, loadedBytes: file.size, totalBytes: file.size });
    return result as MultipartCompleteResult;
  } catch (error) {
    // 续传模式保留会话供下次续传；全新上传失败则直接中止服务端会话
    if (!resumeSession) {
      try {
        await abortMultipartUpload({ fileKey: session.fileKey, uploadId: session.uploadId });
      } catch {
        // 中止失败（如会话已过期）不影响主错误抛出
      }
    }
    throw error;
  }
};

/** 按当前分片大小生成后续可用的降级分片大小梯度（逐级减半，下限 MIN_PART_SIZE） */
const buildPartSizeFallbacks = (fromPartSize: number): number[] => {
  const sizes: number[] = [];
  let size = fromPartSize;
  while (size > MIN_PART_SIZE) {
    size = Math.max(MIN_PART_SIZE, Math.floor(size / 2));
    sizes.push(size);
  }
  return sizes;
};

/**
 * 分片直传主入口。
 *
 * - 无 resumeSession 且文件 <= 20MB：沿用原 uploadAttachment 单发上传；
 * - 否则先 init 初始化（或复用 resumeSession 续传），并发直传缺失分片，最后 complete 合并。
 *
 * 失败语义：续传模式（传入了 resumeSession）失败时保留服务端会话，调用方可再次续传；
 * 全新上传失败时自动调用 abort 释放服务端资源。
 *
 * 弱网降级：某级分片上传失败（停滞/重试耗尽）且非用户取消、当前分片大小仍大于下限时，
 * 自动 abort 当前会话并用更小的分片大小重新 init 上传（S3 multipart 边界由 partSize 决定，
 * 无法在会话内动态改单片大小）。降级后的新会话按新 partSize 持久化，续传沿用该大小。
 */
export const uploadWithMultipart = async (
  file: File,
  options: MultipartUploadOptions
): Promise<MultipartCompleteResult> => {
  const { projectId, partSize: requestedPartSize, signal, resumeSession } = options;

  // 无残留会话且文件 <= 20MB：沿用原单发上传，保持兼容
  if (!resumeSession && file.size <= MULTIPART_THRESHOLD) {
    return uploadSingle(file, projectId, signal);
  }

  if (signal?.aborted) {
    throw new DOMException('The operation was aborted.', 'AbortError');
  }

  // 第一级：优先复用调用方传入的续传会话（沿用其 partSize）；否则按请求分片大小新建
  const basePartSize = resumeSession
    ? Math.max(MIN_PART_SIZE, Number(resumeSession.partSize) || DEFAULT_PART_SIZE)
    : clampPartSize(requestedPartSize);
  const fallbackSizes = buildPartSizeFallbacks(basePartSize);

  // 依次尝试：原始分片大小（可续传）→ 降级后的更小分片（全新上传）
  for (let index = 0; index <= fallbackSizes.length; index += 1) {
    if (signal?.aborted) {
      throw new DOMException('The operation was aborted.', 'AbortError');
    }
    const attemptPartSize = index === 0 ? basePartSize : fallbackSizes[index - 1];
    const useResume = index === 0 ? resumeSession : null;
    try {
      return await uploadWithSession(file, options, useResume, attemptPartSize);
    } catch (error) {
      // 用户取消或已是最后一级（最小分片）：不再降级，抛出错误
      if (signal?.aborted || index === fallbackSizes.length) {
        throw error;
      }
      // 弱网降级：当前会话失败且分片仍可再缩小 → 用更小分片重试。
      // uploadWithSession 已对全新上传执行 abort；降级后的级次不沿用旧 ETag（边界不同），
      // 由下一次 init 通过 onSessionChange 持久化新会话，这里不做覆盖。
    }
  }
  throw new DOMException('The operation was aborted.', 'AbortError');
};
