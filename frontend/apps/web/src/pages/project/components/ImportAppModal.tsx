import {
  abortMultipartUpload,
  getProjectAppDetail,
  getProjectAppImportPreview,
  importProjectApp,
  replaceProjectApp,
  updateProjectApp,
  type VolumePolicy,
} from '@/apis/core-api';
import { openConfirmModal } from '@/components/confirm-modal';
import ProModal from '@/components/pro-modal';
import { useTranslate } from '@/hooks';
import type { AppItem } from '@/pages/project/types';
import { readAppBundleContents, type AppBundleContents } from '@/utils/app-bundle-meta';
import {
  MULTIPART_THRESHOLD,
  calcFileFingerprint,
  calcPartCount,
  calcUploadPercent,
  uploadWithMultipart,
  type ResumeSession,
  type UploadProgress,
} from '@/utils/multipart-upload';
import { projectAppMediaUrl, uploadProjectAppMedia } from '@/utils/project-app-media';
import {
  Alert,
  App,
  Button,
  Flex,
  Input,
  Progress,
  Space,
  Spin,
  Upload,
  type UploadFile,
  type UploadProps,
} from 'antd';
import type { RcFile } from 'antd/es/upload';
import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState, type MouseEvent } from 'react';
import { useParams } from 'react-router';
import '@/pages/uns/components/import-modal/index.scss';
import { appNetworkAliasError } from '@/utils/app-network-alias';
import AppAdvancedFields, {
  isVolumeRowValid,
  schemaNameIssue,
  type EnvVarItem,
  type VolumeItem,
} from './AppAdvancedFields';
import AppBundleUploadContent from './AppBundleUploadContent';
import bundleStyles from './AppBundleModal.module.scss';
import AppMediaFields, { type AppMediaKind } from './AppMediaFields';

const { Dragger } = Upload;
const MAX_IMPORT_FILE_SIZE = 500 * 1024 * 1024;
const DEPLOY_POLL_INTERVAL_MS = 3000;
// Offline app packages may include node_modules and require pulling the runtime
// image on first deploy; allow up to 15 minutes before the UI gives up (the
// backend deployment itself continues asynchronously regardless).
const MAX_DEPLOY_POLLS = 300;
// When a deploy fails the backend removes the project_app record, so the detail
// request starts erroring. Treat consecutive errors as a failed deploy instead
// of polling until MAX_DEPLOY_POLLS.
const MAX_CONSECUTIVE_POLL_ERRORS = 5;

export interface AddGroupModalRef {
  onOpen: (type?: number, props?: any) => void;
  onClose: () => void;
}

export interface AddGroupModalProps {
  refreshRequest?: () => void;
  /** 项目内已有应用列表，用于同 App ID 覆盖更新的确认（可选，默认空） */
  existingApps?: AppItem[];
}

const ImportAppModal = forwardRef<AddGroupModalRef, AddGroupModalProps>(({ refreshRequest, existingApps }, ref) => {
  const { projectId } = useParams<{ projectId: string }>();
  const [visible, setVisible] = useState(false);
  const [fileList, setFileList] = useState<UploadFile[]>([]);
  const [uploading, setUploading] = useState(false);
  const [saveLoading, setSaveLoading] = useState(false);
  const [uploadError, setUploadError] = useState('');
  const [uploadedFileInfo, setUploadedFileInfo] = useState<Record<string, unknown> | null>(null);
  // 分片直传进度（仅大文件分片上传时有值）
  const [uploadProgress, setUploadProgress] = useState<UploadProgress | null>(null);
  // 残留上传会话：页面刷新/中断后存在，用于断点续传提示
  const [residualSession, setResidualSession] = useState<ResumeSession | null>(null);
  const [appName, setAppName] = useState('');
  const [appDescription, setAppDescription] = useState('');
  const [bundleAppId, setBundleAppId] = useState<string>();
  const [bundleVersion, setBundleVersion] = useState<number>();
  const [iconAssetId, setIconAssetId] = useState<number>();
  const [coverAssetId, setCoverAssetId] = useState<number>();
  const [iconUrl, setIconUrl] = useState<string>();
  const [coverUrl, setCoverUrl] = useState<string>();
  const [uploadingMediaKind, setUploadingMediaKind] = useState<AppMediaKind>();
  const [deployedAppId, setDeployedAppId] = useState<string | null>(null);
  const [deployStatus, setDeployStatus] = useState<string | null>(null);
  const [deployError, setDeployError] = useState<string | null>(null);
  // 「高级」区：schema 名（首次导入可编辑，预占后不可变）、环境变量与磁盘映射（均完整替换语义）
  const [schemaName, setSchemaName] = useState('');
  const [networkAlias, setNetworkAlias] = useState('');
  const [advancedEnvVars, setAdvancedEnvVars] = useState<EnvVarItem[]>([]);
  const [advancedSchemaDefault, setAdvancedSchemaDefault] = useState('');
  const [advancedEnvTouched, setAdvancedEnvTouched] = useState(false);
  const [advancedVolumes, setAdvancedVolumes] = useState<VolumeItem[]>([]);
  const [advancedVolumeTouched, setAdvancedVolumeTouched] = useState(false);
  // 预览接口失败标记：Schema/环境变量未预填，Schema 留空时将回退自动命名，需提示用户
  const [advancedPreviewFailed, setAdvancedPreviewFailed] = useState(false);
  // 预览进行中标记：preview 未返回时 Schema 尚未预填，禁止提交避免静默回退 app_<自增ID>
  const [advancedPreviewLoading, setAdvancedPreviewLoading] = useState(false);
  const uploadRootRef = useRef<HTMLDivElement>(null);
  const pollTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  // 预览请求代际号：换包/并发时丢弃过期响应，避免旧包的 Schema 预填覆盖新包
  const previewSeqRef = useRef(0);
  const pollCountRef = useRef(0);
  // 取消进行中分片直传的信号
  const uploadSignalRef = useRef<AbortController | null>(null);
  // 当前上传会话（供取消/失败时中止服务端资源）
  const activeSessionRef = useRef<ResumeSession | null>(null);
  const bundleMediaPreviewUrlsRef = useRef<Partial<Record<AppMediaKind, string>>>({});
  const formatMessage = useTranslate();
  const { message, modal } = App.useApp();

  const storageKey = projectId ? `tier0:project:${projectId}:importAppState` : '';
  // 分片上传会话独立持久化，与表单状态（importAppState）互不干扰
  const uploadSessionKey = projectId ? `tier0:project:${projectId}:appImportUpload` : '';

  const loadUploadSession = useCallback((): ResumeSession | null => {
    if (!uploadSessionKey) return null;
    try {
      const raw = sessionStorage.getItem(uploadSessionKey);
      return raw ? (JSON.parse(raw) as ResumeSession) : null;
    } catch {
      return null;
    }
  }, [uploadSessionKey]);

  const saveUploadSession = useCallback(
    (session: ResumeSession) => {
      if (!uploadSessionKey) return;
      try {
        sessionStorage.setItem(uploadSessionKey, JSON.stringify(session));
      } catch {
        // sessionStorage 写入失败时静默忽略，不影响主流程
      }
    },
    [uploadSessionKey]
  );

  const clearUploadSession = useCallback(() => {
    if (!uploadSessionKey) return;
    try {
      sessionStorage.removeItem(uploadSessionKey);
    } catch {
      // ignore
    }
  }, [uploadSessionKey]);

  const persistState = useCallback(() => {
    if (!storageKey) return;
    const state = {
      fileList,
      uploadedFileInfo,
      appName,
      appDescription,
      iconAssetId,
      coverAssetId,
      iconUrl: iconAssetId ? iconUrl : undefined,
      coverUrl: coverAssetId ? coverUrl : undefined,
      deployedAppId,
      deployStatus,
      deployError,
      uploadError,
      saveLoading,
      networkAlias,
      schemaName,
      advancedEnvVars,
      advancedSchemaDefault,
      advancedEnvTouched,
      advancedVolumes,
      advancedVolumeTouched,
    };
    try {
      sessionStorage.setItem(storageKey, JSON.stringify(state));
    } catch {
      // sessionStorage 写入失败时静默忽略，不影响主流程
    }
  }, [
    storageKey,
    fileList,
    uploadedFileInfo,
    appName,
    appDescription,
    iconAssetId,
    coverAssetId,
    iconUrl,
    coverUrl,
    deployedAppId,
    deployStatus,
    deployError,
    uploadError,
    saveLoading,
    networkAlias,
    schemaName,
    advancedEnvVars,
    advancedSchemaDefault,
    advancedEnvTouched,
    advancedVolumes,
    advancedVolumeTouched,
  ]);

  const restoreState = useCallback(() => {
    if (!storageKey) return;
    try {
      const raw = sessionStorage.getItem(storageKey);
      if (!raw) return;
      const state = JSON.parse(raw);
      if (state.fileList) setFileList(state.fileList);
      if (state.uploadedFileInfo !== undefined) setUploadedFileInfo(state.uploadedFileInfo);
      if (state.appName !== undefined) setAppName(state.appName);
      if (state.appDescription !== undefined) setAppDescription(state.appDescription);
      if (state.iconAssetId !== undefined) setIconAssetId(state.iconAssetId);
      if (state.coverAssetId !== undefined) setCoverAssetId(state.coverAssetId);
      if (state.iconUrl !== undefined) setIconUrl(state.iconUrl);
      if (state.coverUrl !== undefined) setCoverUrl(state.coverUrl);
      if (state.deployedAppId !== undefined) setDeployedAppId(state.deployedAppId);
      if (state.deployStatus !== undefined) setDeployStatus(state.deployStatus);
      if (state.deployError !== undefined) setDeployError(state.deployError);
      if (state.uploadError !== undefined) setUploadError(state.uploadError);
      if (state.saveLoading !== undefined) setSaveLoading(state.saveLoading);
      if (state.networkAlias !== undefined) setNetworkAlias(state.networkAlias);
      if (state.schemaName !== undefined) setSchemaName(state.schemaName);
      if (state.advancedEnvVars !== undefined) setAdvancedEnvVars(state.advancedEnvVars);
      if (state.advancedSchemaDefault !== undefined) setAdvancedSchemaDefault(state.advancedSchemaDefault);
      if (state.advancedEnvTouched !== undefined) setAdvancedEnvTouched(state.advancedEnvTouched);
      if (state.advancedVolumes !== undefined) setAdvancedVolumes(state.advancedVolumes);
      if (state.advancedVolumeTouched !== undefined) setAdvancedVolumeTouched(state.advancedVolumeTouched);
      // 没有 appId 的 loading 无法恢复轮询，直接重置避免卡死。
      if (!state.deployedAppId) {
        setSaveLoading(false);
      }
    } catch {
      // 解析失败时忽略，使用初始空状态
    }
  }, [storageKey]);

  const clearPersistedState = useCallback(() => {
    if (!storageKey) return;
    try {
      sessionStorage.removeItem(storageKey);
    } catch {
      // ignore
    }
  }, [storageKey]);

  const clearBundleMediaPreviews = useCallback((kind?: AppMediaKind) => {
    const kinds: AppMediaKind[] = kind ? [kind] : ['icon', 'cover'];
    for (const currentKind of kinds) {
      const url = bundleMediaPreviewUrlsRef.current[currentKind];
      if (url) {
        URL.revokeObjectURL(url);
        delete bundleMediaPreviewUrlsRef.current[currentKind];
      }
    }
  }, []);

  // 中止服务端上传会话（幂等，失败忽略）
  const abortSession = useCallback(async (session: ResumeSession) => {
    try {
      await abortMultipartUpload({ fileKey: session.fileKey, uploadId: session.uploadId });
    } catch {
      // 服务端会话可能已过期或已中止，忽略
    }
  }, []);

  // 取消进行中的上传并清理会话状态（用户移除文件 / 关闭弹窗时调用）。
  // 页面刷新后重新打开弹窗只有残留会话（activeSessionRef 为空），同样需要中止服务端会话避免资源泄漏。
  const cancelActiveUpload = useCallback(() => {
    uploadSignalRef.current?.abort();
    const session = activeSessionRef.current ?? residualSession;
    if (session) {
      void abortSession(session);
    }
    activeSessionRef.current = null;
    setResidualSession(null);
    setUploadProgress(null);
    clearUploadSession();
  }, [abortSession, clearUploadSession, residualSession]);

  // 放弃残留会话：中止服务端资源并从本地清除，重新开始上传
  const discardResidualUpload = useCallback(() => {
    const session = residualSession;
    if (session) {
      void abortSession(session);
    }
    activeSessionRef.current = null;
    setResidualSession(null);
    setUploadProgress(null);
    clearUploadSession();
  }, [abortSession, clearUploadSession, residualSession]);

  // 组件挂载时先恢复持久化状态，避免下面的 persistState 用空状态覆盖 sessionStorage。
  useEffect(() => {
    restoreState();
  }, [restoreState]);

  // 页面刷新/重新打开时检测未完成的分片上传会话，提示用户续传。
  useEffect(() => {
    setResidualSession(loadUploadSession());
  }, [loadUploadSession]);

  useEffect(() => {
    persistState();
  }, [persistState]);

  useEffect(() => () => clearBundleMediaPreviews(), [clearBundleMediaPreviews]);

  const clearDeployPoll = () => {
    if (pollTimerRef.current) {
      clearInterval(pollTimerRef.current);
      pollTimerRef.current = null;
    }
    pollCountRef.current = 0;
  };

  const resetDeployState = () => {
    clearDeployPoll();
    setDeployedAppId(null);
    setDeployStatus(null);
    setDeployError(null);
  };

  const onOpen = () => {
    setVisible(true);
    restoreState();
    // 重新打开时重新检测残留上传会话（页面刷新场景）
    setResidualSession(loadUploadSession());
  };

  const onClose = (force?: boolean) => {
    // 部署中仅隐藏弹窗，保留轮询和 sessionStorage 状态，再次打开可恢复
    if (!force && deployedAppId) {
      setVisible(false);
      return;
    }
    refreshRequest?.();
    resetDeployState();
    // 关闭弹窗即中止未完成的分片上传并清理会话
    cancelActiveUpload();
    setFileList([]);
    setUploading(false);
    setSaveLoading(false);
    setUploadError('');
    setUploadedFileInfo(null);
    setAppName('');
    setAppDescription('');
    setBundleAppId(undefined);
    setBundleVersion(undefined);
    setIconAssetId(undefined);
    setCoverAssetId(undefined);
    clearBundleMediaPreviews();
    setIconUrl(undefined);
    setCoverUrl(undefined);
    setUploadingMediaKind(undefined);
    setSchemaName('');
    setNetworkAlias('');
    setAdvancedEnvVars([]);
    setAdvancedSchemaDefault('');
    setAdvancedEnvTouched(false);
    setAdvancedVolumes([]);
    setAdvancedVolumeTouched(false);
    setAdvancedPreviewFailed(false);
    setAdvancedPreviewLoading(false);
    setVisible(false);
    clearPersistedState();
  };

  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  // Upload errors can have different shapes, extract readable text by priority.
  const getUploadErrorText = useCallback(
    (error: any) => {
      // axios 超时错误的 message 格式为 "timeout of Xms exceeded"，直接映射为 i18n 文案。
      const rawMsg =
        typeof error?.msg === 'string' ? error.msg : typeof error?.message === 'string' ? error.message : '';
      if (/^timeout of \d+ms exceeded$/i.test(rawMsg)) {
        return formatMessage('common.requestTimeout');
      }
      if (typeof error?.msg === 'string' && error.msg) {
        return formatMessage(error.msg, {}, error.msg);
      }
      if (typeof error?.message === 'string' && error.message) {
        return formatMessage(error.message, {}, error.message);
      }
      if (typeof error?.data === 'string' && error.data) {
        return formatMessage(error.data, {}, error.data);
      }
      if (Array.isArray(error?.data)) {
        return error.data.join('; ');
      }
      return formatMessage('common.serverBusy');
    },
    [formatMessage]
  );

  const updateUploadFileStatus = (file: RcFile, status: UploadFile['status']) => {
    setFileList([
      {
        uid: file.uid,
        name: file.name,
        size: file.size,
        type: file.type,
        status,
      },
    ]);
  };

  const doUpload = async (file: RcFile): Promise<Record<string, unknown> | false> => {
    if (!projectId) {
      setUploadError(formatMessage('common.serverBusy'));
      updateUploadFileStatus(file, 'error');
      return false;
    }
    setUploading(true);
    setUploadedFileInfo(null);
    setUploadError('');
    setUploadProgress(null);

    const controller = new AbortController();
    uploadSignalRef.current = controller;
    const isMultipart = file.size > MULTIPART_THRESHOLD;

    // 大文件计算内容指纹：续传前必须校验文件一致性，避免刷新后选择了
    // 同名同大小的另一个包，把旧分片和新文件拼成混合结果
    let fingerprint: string | undefined;
    if (isMultipart) {
      try {
        fingerprint = await calcFileFingerprint(file);
      } catch {
        // 指纹计算失败（如非安全上下文）时降级：不校验也不允许续传，只允许全新上传
        fingerprint = undefined;
      }
    }

    // 命中残留会话且文件名/大小/内容指纹一致 → 断点续传；否则视为全新上传。
    // 无指纹的旧版本会话无法校验文件一致性，一律不续传（按新文件处理并中止旧会话）
    const pendingSession =
      residualSession &&
      residualSession.fileName === file.name &&
      residualSession.size === file.size &&
      Boolean(residualSession.fingerprint) &&
      residualSession.fingerprint === fingerprint
        ? residualSession
        : null;
    // 选择了新文件时，先尽力中止遗留的旧上传会话，避免服务端资源泄漏
    if (residualSession && !pendingSession) {
      void abortSession(residualSession);
    }

    try {
      const result = await uploadWithMultipart(file, {
        projectId,
        concurrency: 4,
        resumeSession: pendingSession,
        signal: controller.signal,
        fingerprint,
        onProgress: (progress) => setUploadProgress(progress),
        onSessionChange: (session) => {
          activeSessionRef.current = session;
          saveUploadSession(session);
        },
      });
      setUploadedFileInfo(result as Record<string, unknown>);
      activeSessionRef.current = null;
      setResidualSession(null);
      clearUploadSession();
      updateUploadFileStatus(file, 'done');
      return result as Record<string, unknown>;
    } catch (error) {
      const aborted = controller.signal.aborted;
      if (!aborted) {
        if (isMultipart && !pendingSession) {
          // 大文件首次上传失败：给友好的重试指引（续传失败时保留详细错误 + 续传提示）
          setUploadError(
            formatMessage('project.importApp.uploadFailed', {}, 'Upload failed. Please select the file again to retry.')
          );
        } else {
          setUploadError(getUploadErrorText(error));
        }
        updateUploadFileStatus(file, 'error');
      }
      if (aborted || !pendingSession) {
        // 取消或全新上传失败：helper 已中止服务端会话，清理本地会话状态
        activeSessionRef.current = null;
        setResidualSession(null);
        clearUploadSession();
      } else {
        // 续传失败：保留会话，Alert 重新出现，提示用户重新选择文件继续上传
        setResidualSession(loadUploadSession());
      }
      setUploadProgress(null);
      return false;
    } finally {
      setUploading(false);
      uploadSignalRef.current = null;
    }
  };

  const uploadMediaFile = async (kind: AppMediaKind, file: File) => {
    if (!projectId) {
      throw new Error('invalid-project');
    }
    setUploadingMediaKind(kind);
    try {
      const assetId = await uploadProjectAppMedia(file, projectId, 'projectAppImportMedia');
      if (kind === 'icon') {
        clearBundleMediaPreviews('icon');
        setIconAssetId(assetId);
        setIconUrl(projectAppMediaUrl(assetId));
      } else {
        clearBundleMediaPreviews('cover');
        setCoverAssetId(assetId);
        setCoverUrl(projectAppMediaUrl(assetId));
      }
    } catch (error) {
      setUploadError(getUploadErrorText(error));
    } finally {
      setUploadingMediaKind(undefined);
    }
  };

  const previewBundleMedia = (contents: AppBundleContents) => {
    clearBundleMediaPreviews();
    if (contents.icon) {
      const url = URL.createObjectURL(contents.icon);
      bundleMediaPreviewUrlsRef.current.icon = url;
      setIconUrl(url);
    }
    if (contents.cover) {
      const url = URL.createObjectURL(contents.cover);
      bundleMediaPreviewUrlsRef.current.cover = url;
      setCoverUrl(url);
    }
  };

  const beforeUpload: UploadProps['beforeUpload'] = async (file) => {
    const fileType = file.name.split('.').pop()?.toLowerCase();
    if (fileType !== 'zip') {
      message.warning(formatMessage('common.fileFormatType', { fileType: '.zip' }));
      return Upload.LIST_IGNORE;
    }

    if (file.size > MAX_IMPORT_FILE_SIZE) {
      message.warning(formatMessage('common.fileSizeMax', { size: '500MB' }));
      return Upload.LIST_IGNORE;
    }

    setUploadError('');
    setUploadedFileInfo(null);
    setAppName('');
    setAppDescription('');
    setBundleAppId(undefined);
    setBundleVersion(undefined);
    setIconAssetId(undefined);
    setCoverAssetId(undefined);
    clearBundleMediaPreviews();
    setIconUrl(undefined);
    setCoverUrl(undefined);
    // 换包时必须清空「高级」区状态，避免上一个包的 Schema/环境变量残留被提交到新包
    setSchemaName('');
    setAdvancedSchemaDefault('');
    setAdvancedEnvVars([]);
    setAdvancedVolumes([]);
    setAdvancedEnvTouched(false);
    setAdvancedVolumeTouched(false);
    setAdvancedPreviewFailed(false);
    setAdvancedPreviewLoading(false);
    previewSeqRef.current += 1;
    updateUploadFileStatus(file, 'uploading');
    const fallbackName = file.name.replace(/\.zip$/i, '').trim();
    let contents: AppBundleContents | undefined;
    try {
      contents = await readAppBundleContents(file);
      setBundleAppId(contents.meta.appId);
      setBundleVersion(contents.meta.version);
      setAppName((contents.meta.name || fallbackName).slice(0, 64));
      setAppDescription((contents.meta.description || '').slice(0, 200));
    } catch {
      // 兼容没有标准 meta.json 的旧包，名称继续沿用文件名回退规则。
      setBundleAppId(undefined);
      setBundleVersion(undefined);
      setAppName(fallbackName.slice(0, 64));
      setAppDescription('');
    }
    const uploaded = await doUpload(file);
    if (uploaded) {
      if (contents) {
        try {
          previewBundleMedia(contents);
        } catch (error) {
          setUploadError(getUploadErrorText(error));
        }
      }
      // 上传成功后读取 zip 头部（artifact.toml env + meta.json 身份），预填「高级」区。
      // 非 artifact 包或预览失败时高级区保持为空（前端隐藏或空展示）。
      // 服务端预览只依赖上传结果（fileId），不依赖前端读包（部分合法包为 wrapper 目录布局，
      // 前端 findRootEntries 读不到根级 meta.json，此时更需预览完成预填）
      const fileId = (uploaded as any)?.fileId ?? (uploaded as any)?.id;
      if (projectId && fileId) {
        const seq = ++previewSeqRef.current;
        setAdvancedPreviewLoading(true);
        try {
          const preview = await getProjectAppImportPreview(projectId, fileId);
          if (seq !== previewSeqRef.current) {
            return false;
          }
          if (preview?.schemaDefault) {
            setAdvancedSchemaDefault(preview.schemaDefault);
            setSchemaName(preview.schemaDefault);
          }
          if (Array.isArray(preview?.envVars)) {
            setAdvancedEnvVars(preview.envVars);
          }
        } catch {
          if (seq !== previewSeqRef.current) {
            return false;
          }
          // 预览失败不阻塞导入流程，但需提示用户：Schema 未预填，留空将回退自动命名
          setAdvancedPreviewFailed(true);
        } finally {
          if (seq === previewSeqRef.current) {
            setAdvancedPreviewLoading(false);
          }
        }
      }
    }
    return false;
  };

  const openFileDialog = () => {
    if (uploading || saveLoading) {
      return;
    }
    uploadRootRef.current?.querySelector<HTMLInputElement>('input[type="file"]')?.click();
  };

  const handleUploadClick = (event: MouseEvent<HTMLDivElement>) => {
    const target = event.target as Element | null;
    if (target?.closest('.ant-upload-list')) {
      return;
    }
    openFileDialog();
  };

  const saveImportedAppMetadata = useCallback(
    async (appId: string) => {
      if (!projectId) {
        throw new Error('invalid-project');
      }
      await updateProjectApp(projectId, appId, {
        name: appName.trim(),
        description: appDescription.trim(),
        iconAssetId: iconAssetId || 0,
        coverAssetId: coverAssetId || 0,
      });
    },
    [appDescription, appName, coverAssetId, iconAssetId, projectId]
  );

  // 同项目已存在相同 App ID 的应用（matchedApp），替换确认弹窗带包内 appId 与已有应用来源 appId 的差异
  const matchedApp = bundleAppId ? (existingApps ?? []).find((app) => app.sourceAppId === bundleAppId) : undefined;
  // 版本未增加提醒：参考 SaaS 规则（版本为递增整数），包内版本低于或等于当前应用版本时仅警告、不阻断。
  // 当前应用版本缺失或无法解析为数字时跳过比较。
  const currentVersion = Number(matchedApp?.version);
  const versionNotIncreased =
    bundleVersion !== undefined &&
    matchedApp !== undefined &&
    matchedApp.version !== '' &&
    !Number.isNaN(currentVersion) &&
    bundleVersion <= currentVersion;

  const confirmReplace = () =>
    new Promise<boolean>((resolve) => {
      openConfirmModal(modal, {
        title: formatMessage('project.replace.title', {}, 'Replace App'),
        content: formatMessage(
          'project.importApp.replaceConfirm',
          { bundleAppId: bundleAppId ?? '' },
          'An app with the same App ID already exists in this project. Replace and update it?'
        ),
        okText: formatMessage('common.confirm'),
        cancelText: formatMessage('common.cancel'),
        onOk: () => resolve(true),
        onCancel: () => resolve(false),
      });
    });

  // 确认框：预填过的 Schema 被清空时二次确认，避免静默回退到 app_<自增ID> 且不可再改
  const confirmEmptySchema = () =>
    new Promise<boolean>((resolve) => {
      openConfirmModal(modal, {
        title: formatMessage('project.importApp.schemaEmptyConfirmTitle', {}, 'Empty schema name'),
        content: formatMessage(
          'project.importApp.schemaEmptyConfirm',
          {},
          'No schema name is set. The platform will auto-generate one (app_<appId>) after import, and it cannot be changed afterwards. Continue?'
        ),
        okText: formatMessage('common.confirm'),
        cancelText: formatMessage('common.cancel'),
        onOk: () => resolve(true),
        onCancel: () => resolve(false),
      });
    });

  const onSave = async () => {
    const aliasError = appNetworkAliasError(networkAlias);
    if (aliasError) {
      setUploadError(formatMessage(aliasError));
      return;
    }
    if (!uploadedFileInfo) {
      message.warning(formatMessage('project.import.uploadRequired'));
      return;
    }
    if (!appName.trim()) {
      message.warning(formatMessage('project.import.appNameRequired'));
      return;
    }
    if (!projectId) {
      message.warning(formatMessage('common.serverBusy'));
      return;
    }
    // 预览未返回时 Schema 尚未预填（大包后端解析可能超过 10 秒），此时提交会静默回退自动命名
    if (advancedPreviewLoading) {
      message.warning(
        formatMessage('project.importApp.previewing', {}, 'Still reading the app bundle, please wait a moment')
      );
      return;
    }

    const canRetryMetadataSave = Boolean(deployedAppId) && (deployStatus === 'success' || deployStatus === 'Active');
    if (deployedAppId && canRetryMetadataSave) {
      try {
        setSaveLoading(true);
        setDeployError(null);
        await saveImportedAppMetadata(deployedAppId);
        message.success(formatMessage('common.optsuccess'));
        refreshRequest?.();
        onCloseRef.current(true);
      } catch (error) {
        setDeployError(getUploadErrorText(error));
        setSaveLoading(false);
      }
      return;
    }

    // 磁盘映射轻校验：填写过的行两个路径都必填且须以 / 开头，避免提交明显非法的挂载
    if (advancedVolumes.some((item) => !isVolumeRowValid(item))) {
      message.warning(
        formatMessage(
          'project.importApp.volumePathInvalid',
          {},
          'Host and container paths must be absolute paths starting with "/"'
        )
      );
      return;
    }

    // Schema 名防呆：非法/保留名直接拦截；包内提供过默认名（预填）却被清空时二次确认。
    // 老包/静态包本就无默认名，保持现状直接放行（后端按设计回退自动命名）。
    const schemaIssue = schemaNameIssue(schemaName);
    if (schemaIssue === 'invalid' || schemaIssue === 'reserved') {
      message.warning(
        formatMessage(
          schemaIssue === 'invalid' ? 'project.importApp.schemaInvalid' : 'project.importApp.schemaReserved',
          {},
          schemaIssue === 'invalid'
            ? 'Schema name must be 1-63 chars of lowercase letters, digits or underscores'
            : 'Schema name is a reserved system name'
        )
      );
      return;
    }
    if (schemaIssue === 'required' && advancedSchemaDefault) {
      const confirmed = await confirmEmptySchema();
      if (!confirmed) {
        return;
      }
    }

    if (matchedApp) {
      // 同项目已存在相同 App ID 的应用：确认后改走覆盖更新（替换接口），
      // 后续部署轮询/元数据保存链路与导入成功后保持一致。
      const confirmed = await confirmReplace();
      if (!confirmed) {
        // 用户取消确认，中止提交
        return;
      }
    }

    try {
      setSaveLoading(true);
      // 「高级」区载荷：schema 仅首次导入提交；env / volumes 均为完整替换语义（用户改过才带，未改走 inherit）
      const advancedPayload = {
        schemaName: schemaName.trim(),
        envPolicy: (advancedEnvTouched ? 'replace' : 'inherit') as VolumePolicy,
        envVars: advancedEnvVars
          .filter((item) => item.key.trim() !== '')
          .map((item) => ({ key: item.key.trim(), value: item.value })),
        volumePolicy: (advancedVolumeTouched ? 'replace' : 'inherit') as VolumePolicy,
        volumes: advancedVolumes
          .filter((item) => item.hostPath.trim() !== '' || item.containerPath.trim() !== '')
          .map((item) => ({
            hostPath: item.hostPath.trim(),
            containerPath: item.containerPath.trim(),
            readOnly: Boolean(item.readOnly),
          })),
      };
      if (matchedApp) {
        const response = await replaceProjectApp(projectId, String(matchedApp.appId), {
          ...uploadedFileInfo,
          description: appDescription.trim(),
          networkAlias: networkAlias.trim(),
          envPolicy: advancedPayload.envPolicy,
          envVars: advancedPayload.envVars,
          volumePolicy: advancedPayload.volumePolicy,
          volumes: advancedPayload.volumes,
        });
        setDeployedAppId(String(response?.appId || matchedApp.appId));
        return;
      }
      const data = await importProjectApp(projectId, {
        ...uploadedFileInfo,
        appName: appName.trim(),
        description: appDescription.trim(),
        networkAlias: networkAlias.trim(),
        schemaName: advancedPayload.schemaName,
        envPolicy: advancedPayload.envPolicy,
        envVars: advancedPayload.envVars,
        volumePolicy: advancedPayload.volumePolicy,
        volumes: advancedPayload.volumes,
      });
      // 服务端兜底决策：客户端预查（existingApps）过期或未命中时后端返回 replaceRequired，
      // 同样弹确认后走覆盖更新，避免 200 无 appId 被当作通用失败
      if (data?.replaceRequired) {
        const targetAppId = String(data?.targetAppId || '');
        setSaveLoading(false);
        if (!targetAppId) {
          setUploadError(formatMessage('common.serverBusy'));
          return;
        }
        const confirmed = await confirmReplace();
        if (!confirmed) {
          return;
        }
        try {
          setSaveLoading(true);
          const response = await replaceProjectApp(projectId, targetAppId, {
            ...uploadedFileInfo,
            description: appDescription.trim(),
            networkAlias: networkAlias.trim(),
            envPolicy: advancedPayload.envPolicy,
            envVars: advancedPayload.envVars,
            volumePolicy: advancedPayload.volumePolicy,
            volumes: advancedPayload.volumes,
          });
          setDeployedAppId(String(response?.appId || targetAppId));
        } catch (error: any) {
          setUploadError(getUploadErrorText(error));
          setSaveLoading(false);
        }
        return;
      }
      const newAppId = String(data?.appId || '');
      if (!newAppId) {
        setSaveLoading(false);
        setUploadError(formatMessage('common.serverBusy'));
        return;
      }
      setDeployedAppId(newAppId);
    } catch (error: any) {
      setUploadError(getUploadErrorText(error));
      setSaveLoading(false);
    }
  };

  useEffect(() => {
    if (!deployedAppId || !projectId) {
      return;
    }

    pollCountRef.current = 0;
    let consecutiveErrors = 0;
    setDeployStatus('deploying');
    setDeployError(null);

    const checkDeploy = async () => {
      if (pollCountRef.current >= MAX_DEPLOY_POLLS) {
        // 达到轮询上限：部署结果未知，停止轮询并进入专用 timeout 终态，
        // 提示用户稍后从应用列表确认最终状态（不再轮询、不重复提交）。
        clearDeployPoll();
        setDeployedAppId(null);
        setSaveLoading(false);
        setDeployStatus('timeout');
        setDeployError(formatMessage('project.deployTimeout'));
        refreshRequest?.();
        clearPersistedState();
        return;
      }

      pollCountRef.current += 1;

      try {
        const detail = await getProjectAppDetail(projectId, deployedAppId);
        consecutiveErrors = 0;
        const status = detail?.status || detail?.data?.status;
        const currentDeployStatus = detail?.deployStatus || detail?.data?.deployStatus;
        const currentDeployError = detail?.deployError || detail?.data?.deployError;

        setDeployStatus(currentDeployStatus || status || null);

        // deployStatus 优先：先判失败，避免 Replace 回退态（status=Active + deployStatus=failed）误判成功
        if (currentDeployStatus === 'failed' || status === 'Failed') {
          clearDeployPoll();
          setDeployedAppId(null);
          setSaveLoading(false);
          setDeployError(currentDeployError || formatMessage('project.deployFailed'));
          refreshRequest?.();
          clearPersistedState();
          return;
        }

        if (currentDeployStatus === 'success' || (!currentDeployStatus && status === 'Active')) {
          clearDeployPoll();
          try {
            await saveImportedAppMetadata(deployedAppId);
          } catch (error) {
            setSaveLoading(false);
            setDeployError(getUploadErrorText(error));
            refreshRequest?.();
            return;
          }
          message.success(formatMessage('common.optsuccess'));
          refreshRequest?.();
          onCloseRef.current(true);
          return;
        }
      } catch {
        // 部署失败后后端会删除应用记录，详情接口开始持续报错；
        // 连续多次报错视为部署失败，避免用户空等到轮询上限。
        consecutiveErrors += 1;
        if (consecutiveErrors >= MAX_CONSECUTIVE_POLL_ERRORS) {
          clearDeployPoll();
          setDeployedAppId(null);
          setSaveLoading(false);
          setDeployStatus('failed');
          setDeployError(formatMessage('project.deployFailed'));
          refreshRequest?.();
          clearPersistedState();
        }
      }
    };

    void checkDeploy();
    pollTimerRef.current = setInterval(() => {
      void checkDeploy();
    }, DEPLOY_POLL_INTERVAL_MS);

    return () => {
      clearDeployPoll();
    };
  }, [
    deployedAppId,
    projectId,
    formatMessage,
    message,
    refreshRequest,
    clearPersistedState,
    getUploadErrorText,
    saveImportedAppMetadata,
  ]);

  useImperativeHandle(ref, () => ({
    onOpen,
    onClose,
  }));

  const isDeploying = saveLoading && (deployStatus === 'deploying' || deployStatus === 'Deploying');
  const canRetryMetadataSave = Boolean(deployedAppId) && (deployStatus === 'success' || deployStatus === 'Active');

  return (
    <ProModal
      open={visible}
      onCancel={() => onClose()}
      title={formatMessage('project.importApp')}
      className="importModalWrap attachment-upload-modal"
      width={560}
      maskClosable={false}
      fullScreenable={false}
      styles={{ body: { maxHeight: '68vh', overflowY: 'auto', paddingInlineEnd: 12 } }}
    >
      <div ref={uploadRootRef} className={bundleStyles['upload-section']} onClick={handleUploadClick}>
        <Dragger
          className="uploadWrap"
          action=""
          accept=".zip"
          maxCount={1}
          fileList={fileList}
          disabled={uploading || advancedPreviewLoading || saveLoading}
          beforeUpload={beforeUpload}
          openFileDialogOnClick={false}
          onRemove={() => {
            cancelActiveUpload();
            setFileList([]);
            setUploadError('');
            setUploadedFileInfo(null);
            setAppName('');
            setAppDescription('');
            setBundleAppId(undefined);
            setBundleVersion(undefined);
            setIconAssetId(undefined);
            setCoverAssetId(undefined);
            setIconUrl(undefined);
            setCoverUrl(undefined);
          }}
        >
          <AppBundleUploadContent />
        </Dragger>
      </div>

      {uploading && uploadProgress && uploadProgress.totalParts > 0 ? (
        <div className={bundleStyles['upload-progress']}>
          <Progress percent={uploadProgress.percent} size="small" status="active" />
          <div className={bundleStyles['upload-progress-text']}>
            {formatMessage('project.importApp.uploading', { percent: uploadProgress.percent }, 'Uploading {percent}%')}
          </div>
        </div>
      ) : null}

      {residualSession && !uploading && !saveLoading ? (
        <Alert
          className={bundleStyles['resume-alert']}
          type="warning"
          showIcon
          message={formatMessage(
            'project.importApp.resumeHint',
            {
              fileName: residualSession.fileName,
              percent: calcUploadPercent(
                residualSession.completedParts.length,
                calcPartCount(residualSession.size, residualSession.partSize)
              ),
            },
            'Detected an unfinished upload ({fileName}, {percent}%). Select the same file again to continue.'
          )}
          action={
            <Space>
              <Button size="small" type="primary" onClick={openFileDialog}>
                {formatMessage('project.importApp.resumeUpload', {}, 'Resume')}
              </Button>
              <Button size="small" onClick={discardResidualUpload}>
                {formatMessage('project.importApp.reupload', {}, 'Re-upload')}
              </Button>
            </Space>
          }
        />
      ) : null}

      {uploadedFileInfo && (
        <div className={bundleStyles['details-section']}>
          <div>
            <label className={bundleStyles['field-label']}>
              {formatMessage('AppManagement.appName')}
              <span className={bundleStyles['required-mark']}>*</span>
            </label>
            <Input
              placeholder={formatMessage('AppManagement.appName')}
              value={appName}
              onChange={(e) => setAppName(e.target.value)}
              maxLength={64}
              disabled={uploading || saveLoading}
            />
          </div>
          <div className={bundleStyles['meta-grid']}>
            <div>
              <label className={bundleStyles['field-label']}>
                {formatMessage('project.replace.bundleAppId', {}, 'Bundle App ID')}
              </label>
              <Input value={bundleAppId ?? ''} disabled />
            </div>
            <div>
              <label className={bundleStyles['field-label']}>
                {formatMessage('project.replace.bundleVersion', {}, 'Bundle Version')}
              </label>
              <Input value={bundleVersion !== undefined ? String(bundleVersion) : ''} disabled />
            </div>
          </div>
          <div>
            <label className={bundleStyles['field-label']}>{formatMessage('common.description')}</label>
            <Input.TextArea
              placeholder={formatMessage('apps.descriptionPlaceholder')}
              value={appDescription}
              onChange={(e) => setAppDescription(e.target.value)}
              maxLength={200}
              rows={3}
              disabled={uploading || saveLoading}
            />
          </div>
          <AppMediaFields
            coverUrl={coverUrl}
            iconUrl={iconUrl}
            disabled={uploading || saveLoading || Boolean(uploadingMediaKind)}
            uploadingKind={uploadingMediaKind}
            onSelect={uploadMediaFile}
          />
          <AppAdvancedFields
            networkAlias={networkAlias}
            onNetworkAliasChange={setNetworkAlias}
            schemaDefault={advancedSchemaDefault}
            defaultEnvVars={advancedEnvVars}
            defaultVolumes={advancedVolumes}
            disabled={uploading || saveLoading}
            onChange={({ schemaName: nextSchema, envVars: nextEnv, volumes: nextVolumes, volumeTouched }) => {
              if (nextSchema !== undefined) setSchemaName(nextSchema);
              setAdvancedEnvVars(nextEnv);
              setAdvancedVolumes(nextVolumes);
              setAdvancedEnvTouched(true);
              if (volumeTouched) setAdvancedVolumeTouched(true);
            }}
          />
        </div>
      )}

      {versionNotIncreased ? (
        <Alert
          className={bundleStyles['error-alert']}
          type="warning"
          showIcon
          message={formatMessage(
            'project.replace.versionNotIncreased',
            { bundleVersion: String(bundleVersion), currentVersion: matchedApp?.version ?? '' },
            'The package version is not higher than the current App version. The version will not increase after replacement.'
          )}
        />
      ) : null}

      {advancedPreviewFailed ? (
        <Alert
          className={bundleStyles['error-alert']}
          type="warning"
          showIcon
          message={formatMessage(
            'project.importApp.previewFailed',
            {},
            'Failed to pre-read the app bundle. If the schema name is left empty, an auto-generated name will be used.'
          )}
        />
      ) : null}

      {uploadError ? (
        <Alert className={bundleStyles['error-alert']} type="error" showIcon message={uploadError} />
      ) : null}

      {deployError ? (
        <Alert
          className={bundleStyles['error-alert']}
          type="error"
          showIcon
          message={formatMessage(deployError, {}, deployError)}
        />
      ) : null}

      {isDeploying ? (
        <Flex className={bundleStyles['deploy-status']} align="center" gap={8}>
          <Spin size="small" />
          <span>{formatMessage('project.deploying')}</span>
        </Flex>
      ) : null}

      <Button
        className={bundleStyles['submit-button']}
        loading={saveLoading}
        color="primary"
        variant="solid"
        block
        disabled={
          uploading ||
          advancedPreviewLoading ||
          Boolean(uploadingMediaKind) ||
          isDeploying ||
          !uploadedFileInfo ||
          !appName.trim() ||
          (Boolean(deployedAppId) && !canRetryMetadataSave)
        }
        onClick={onSave}
      >
        {canRetryMetadataSave ? formatMessage('common.save') : formatMessage('common.import', {}, 'Import')}
      </Button>
    </ProModal>
  );
});

export default ImportAppModal;
