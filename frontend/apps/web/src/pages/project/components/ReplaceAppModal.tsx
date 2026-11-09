import {
  abortMultipartUpload,
  getProjectAppDetail,
  getProjectAppImportPreview,
  replaceProjectApp,
  updateProjectApp,
  type VolumePolicy,
} from '@/apis/core-api';
import ProModal from '@/components/pro-modal';
import { useTranslate } from '@/hooks';
import type { AppItem } from '@/pages/project/types';
import { readAppBundleContents, type AppBundleContents, type AppBundleMeta } from '@/utils/app-bundle-meta';
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
import AppAdvancedFields, { isVolumeRowValid, type EnvVarItem, type VolumeItem } from './AppAdvancedFields';
import AppBundleUploadContent from './AppBundleUploadContent';
import bundleStyles from './AppBundleModal.module.scss';
import AppMediaFields, { type AppMediaKind } from './AppMediaFields';
import styles from './ReplaceAppModal.module.scss';

const { Dragger } = Upload;
const MAX_IMPORT_FILE_SIZE = 500 * 1024 * 1024;
const DEPLOY_POLL_INTERVAL_MS = 3000;
const MAX_DEPLOY_POLLS = 300;
const MAX_CONSECUTIVE_POLL_ERRORS = 5;

export interface ReplaceAppModalRef {
  onOpen: (app: AppItem) => void;
  onClose: (force?: boolean) => void;
}

export interface ReplaceAppModalProps {
  refreshRequest?: () => void;
}

const ReplaceAppModal = forwardRef<ReplaceAppModalRef, ReplaceAppModalProps>(({ refreshRequest }, ref) => {
  const { projectId } = useParams<{ projectId: string }>();
  const formatMessage = useTranslate();
  const { message } = App.useApp();
  const [visible, setVisible] = useState(false);
  const [targetApp, setTargetApp] = useState<AppItem>();
  const [bundleMeta, setBundleMeta] = useState<AppBundleMeta>();
  // 名称与描述允许用户在替换前编辑（与导入弹窗一致），初值取包内 meta、回退目标应用信息
  const [replacementName, setReplacementName] = useState('');
  const [replacementDescription, setReplacementDescription] = useState('');
  const [fileList, setFileList] = useState<UploadFile[]>([]);
  const [uploadedFileInfo, setUploadedFileInfo] = useState<Record<string, unknown> | null>(null);
  const [uploading, setUploading] = useState(false);
  const [saveLoading, setSaveLoading] = useState(false);
  const [uploadError, setUploadError] = useState('');
  const [appIdMismatch, setAppIdMismatch] = useState(false);
  const [versionNotIncreased, setVersionNotIncreased] = useState(false);
  const [deployedAppId, setDeployedAppId] = useState<string>();
  const [deployStatus, setDeployStatus] = useState<string>();
  const [deployError, setDeployError] = useState<string>();
  const [iconAssetId, setIconAssetId] = useState<number>();
  const [coverAssetId, setCoverAssetId] = useState<number>();
  const [iconUrl, setIconUrl] = useState<string>();
  const [coverUrl, setCoverUrl] = useState<string>();
  const [uploadingMediaKind, setUploadingMediaKind] = useState<AppMediaKind>();
  // 分片直传进度（仅大文件分片上传时有值）
  const [uploadProgress, setUploadProgress] = useState<UploadProgress | null>(null);
  // 残留上传会话：页面刷新/中断后存在，用于断点续传提示
  const [residualSession, setResidualSession] = useState<ResumeSession | null>(null);
  // Replace 可编辑环境变量（schema 不可变，沿用目标应用既有 schema）与磁盘映射
  const [networkAlias, setNetworkAlias] = useState('');
  const [advancedEnvVars, setAdvancedEnvVars] = useState<EnvVarItem[]>([]);
  const [advancedEnvTouched, setAdvancedEnvTouched] = useState(false);
  // 磁盘映射预填目标应用现有值（打开弹窗时从详情读取），用户改过才走 replace
  const [advancedVolumes, setAdvancedVolumes] = useState<VolumeItem[]>([]);
  const [advancedVolumeTouched, setAdvancedVolumeTouched] = useState(false);
  const uploadRootRef = useRef<HTMLDivElement>(null);
  const pollTimerRef = useRef<ReturnType<typeof setInterval>>();
  const pollCountRef = useRef(0);
  // 取消进行中分片直传的信号
  const uploadSignalRef = useRef<AbortController | null>(null);
  // 当前上传会话（供取消/失败时中止服务端资源）
  const activeSessionRef = useRef<ResumeSession | null>(null);
  const bundleMediaPreviewUrlsRef = useRef<Partial<Record<AppMediaKind, string>>>({});

  // 分片上传会话独立持久化，key 与导入弹窗区分，避免同项目两个弹窗互相干扰
  const uploadSessionKey = projectId ? `tier0:project:${projectId}:appReplaceUpload` : '';

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

  const clearDeployPoll = useCallback(() => {
    if (pollTimerRef.current) {
      clearInterval(pollTimerRef.current);
      pollTimerRef.current = undefined;
    }
    pollCountRef.current = 0;
  }, []);

  const resetState = useCallback(() => {
    clearDeployPoll();
    clearBundleMediaPreviews();
    setBundleMeta(undefined);
    setReplacementName('');
    setReplacementDescription('');
    setFileList([]);
    setUploadedFileInfo(null);
    setUploading(false);
    setSaveLoading(false);
    setUploadError('');
    setAppIdMismatch(false);
    setVersionNotIncreased(false);
    setDeployedAppId(undefined);
    setDeployStatus(undefined);
    setDeployError(undefined);
    setIconAssetId(undefined);
    setCoverAssetId(undefined);
    setIconUrl(undefined);
    setCoverUrl(undefined);
    setUploadingMediaKind(undefined);
    setNetworkAlias('');
    setAdvancedEnvVars([]);
    setAdvancedEnvTouched(false);
    setAdvancedVolumes([]);
    setAdvancedVolumeTouched(false);
    setUploadProgress(null);
    setResidualSession(null);
  }, [clearBundleMediaPreviews, clearDeployPoll]);

  useEffect(() => () => clearBundleMediaPreviews(), [clearBundleMediaPreviews]);

  const onOpen = (app: AppItem) => {
    resetState();
    setTargetApp(app);
    setNetworkAlias(app.networkAlias || '');
    // 重新打开时检测未完成的分片上传会话（页面刷新场景），提示用户续传
    setResidualSession(loadUploadSession());
    setVisible(true);
    // 预填目标应用现有磁盘映射（env 仍走包 preview，schema 不可变）；
    // 详情获取失败不阻塞替换流程，磁盘映射按空展示，inherit 语义保留现有挂载
    void (async () => {
      if (!projectId) {
        return;
      }
      try {
        const detail = await getProjectAppDetail(projectId, String(app.appId));
        const detailVolumes = detail?.volumes || detail?.data?.volumes;
        if (Array.isArray(detailVolumes)) {
          setAdvancedVolumes(
            detailVolumes.map((item) => ({
              hostPath: item.hostPath || '',
              containerPath: item.containerPath || '',
              readOnly: Boolean(item.readOnly),
            }))
          );
        }
      } catch {
        // 忽略，保持空列表
      }
    })();
  };

  const onClose = (force?: boolean) => {
    if (!force && deployedAppId) {
      setVisible(false);
      return;
    }
    // 关闭弹窗即中止未完成的分片上传并清理会话
    cancelActiveUpload();
    resetState();
    setTargetApp(undefined);
    setVisible(false);
  };

  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  const getUploadErrorText = (error: any) => {
    const rawMessage =
      typeof error?.msg === 'string' ? error.msg : typeof error?.message === 'string' ? error.message : '';
    if (/^timeout of \d+ms exceeded$/i.test(rawMessage)) {
      return formatMessage('common.requestTimeout');
    }
    if (rawMessage) {
      return formatMessage(rawMessage, {}, rawMessage);
    }
    return formatMessage('common.serverBusy');
  };

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

  const validateBundleIdentity = useCallback(
    async (file: RcFile) => {
      if (!targetApp || !projectId) {
        throw new Error('invalid-target');
      }

      const contents = await readAppBundleContents(file);
      const { meta } = contents;
      // 与后端 validateReplaceIdentity 对齐：身份只按 appId 判定，包内 appId 对比目标应用记录的来源 App ID，
      // 不一致时硬阻断；workspaceId/projectId 仅记录不参与校验。
      // 目标应用未记录来源身份（手工创建/旧数据）时不预校验，交给后端裁决。
      setAppIdMismatch(Boolean(targetApp.sourceAppId) && meta.appId !== targetApp.sourceAppId);
      // 版本检查：参考 SaaS 规则（版本为递增整数），包内版本低于或等于当前版本时仅警告、不阻断。
      // 当前应用版本缺失或无法解析为数字时跳过比较。
      const currentVersion = Number(targetApp.version);
      const hasCurrentVersion = targetApp.version !== '' && !Number.isNaN(currentVersion);
      setVersionNotIncreased(meta.version !== undefined && hasCurrentVersion && meta.version <= currentVersion);
      setBundleMeta(meta);
      return contents;
    },
    [projectId, targetApp]
  );

  const uploadMediaFile = async (kind: AppMediaKind, file: File) => {
    if (!targetApp) {
      throw new Error('invalid-target');
    }
    setUploadingMediaKind(kind);
    try {
      const assetId = await uploadProjectAppMedia(file, targetApp.appId);
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
      // 小于等于阈值沿用单发上传、大文件分片直传，由 uploadWithMultipart 内部统一处理，
      // 返回与分片 complete 一致的结构（含 fileId），后续 replace 提交直接使用
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
    setBundleMeta(undefined);
    setUploadedFileInfo(null);
    setAppIdMismatch(false);
    setVersionNotIncreased(false);
    setIconAssetId(undefined);
    setCoverAssetId(undefined);
    clearBundleMediaPreviews();
    setIconUrl(undefined);
    setCoverUrl(undefined);
    updateUploadFileStatus(file, 'uploading');
    let contents: AppBundleContents;
    try {
      contents = await validateBundleIdentity(file);
    } catch (error) {
      const reason = error instanceof Error ? error.message : '';
      setUploadError(
        reason === 'meta-missing'
          ? formatMessage(
              'project.replace.metaMissing',
              {},
              'This app package is missing required information and cannot be used for replacement.'
            )
          : formatMessage(
              'project.replace.invalidBundle',
              {},
              'This app package is invalid. Select a valid .zip app package.'
            )
      );
      updateUploadFileStatus(file, 'error');
      return false;
    }
    const uploaded = await doUpload(file);
    if (uploaded) {
      try {
        previewBundleMedia(contents);
      } catch (error) {
        setUploadError(getUploadErrorText(error));
      }
      // 上传成功后读取 zip 头部，预填「高级」区环境变量（schema 不可变，不预填）
      const fileId = (uploaded as any)?.fileId ?? (uploaded as any)?.id;
      if (projectId && fileId) {
        try {
          const preview = await getProjectAppImportPreview(projectId, fileId);
          if (Array.isArray(preview?.envVars)) {
            setAdvancedEnvVars(preview.envVars);
            setAdvancedEnvTouched(false);
          }
        } catch {
          // 预览失败不阻塞替换流程，env 按空处理
        }
      }
    }
    return false;
  };

  const openFileDialog = () => {
    if (!uploading && !saveLoading) {
      uploadRootRef.current?.querySelector<HTMLInputElement>('input[type="file"]')?.click();
    }
  };

  const handleUploadClick = (event: MouseEvent<HTMLDivElement>) => {
    const target = event.target as Element | null;
    if (!target?.closest('.ant-upload-list')) {
      openFileDialog();
    }
  };

  // 包内 meta 或目标应用变化时同步名称/描述初值（按后端上限截断，避免受控值超长标红）；
  // 用户编辑后仅在重新上传包/换目标时被覆盖
  useEffect(() => {
    setReplacementName((bundleMeta?.name || targetApp?.name || '').slice(0, 64));
    setReplacementDescription((bundleMeta?.description || targetApp?.description || '').slice(0, 200));
  }, [bundleMeta, targetApp]);

  const onReplace = async () => {
    const aliasError = appNetworkAliasError(networkAlias);
    if (aliasError) {
      setUploadError(formatMessage(aliasError));
      return;
    }
    if (!uploadedFileInfo || !bundleMeta) {
      message.warning(formatMessage('project.import.uploadRequired'));
      return;
    }
    if (!projectId || !targetApp) {
      message.warning(formatMessage('common.serverBusy'));
      return;
    }
    // appId 不一致硬阻断（与后端 validateReplaceIdentity 对齐），不允许确认放行
    if (appIdMismatch) {
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

    setSaveLoading(true);
    setDeployError(undefined);
    try {
      const response = await replaceProjectApp(projectId, String(targetApp.appId), {
        ...uploadedFileInfo,
        description: replacementDescription.trim(),
        // Replace 可编辑 env / volumes（完整替换语义，用户改过才带，未改走 inherit），schema 不可变不提交
        networkAlias: networkAlias.trim(),
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
      });
      setDeployedAppId(String(response?.appId || targetApp.appId));
    } catch (error) {
      setUploadError(getUploadErrorText(error));
      setSaveLoading(false);
    }
  };

  useEffect(() => {
    if (!deployedAppId || !projectId || !targetApp) {
      return;
    }

    pollCountRef.current = 0;
    let consecutiveErrors = 0;
    setDeployStatus('deploying');

    const checkDeploy = async () => {
      if (pollCountRef.current >= MAX_DEPLOY_POLLS) {
        // 达到轮询上限：部署结果未知，停止轮询并进入专用 timeout 终态，
        // 提示用户稍后从应用列表确认最终状态（不再轮询、不重复提交）。
        clearDeployPoll();
        setDeployedAppId(undefined);
        setSaveLoading(false);
        setDeployStatus('timeout');
        setDeployError(formatMessage('project.deployTimeout'));
        refreshRequest?.();
        return;
      }
      pollCountRef.current += 1;

      try {
        const detail = await getProjectAppDetail(projectId, String(targetApp.appId));
        consecutiveErrors = 0;
        const status = detail?.status || detail?.data?.status;
        const currentDeployStatus = detail?.deployStatus || detail?.data?.deployStatus;
        const currentDeployError = detail?.deployError || detail?.data?.deployError;
        setDeployStatus(currentDeployStatus || status);

        // deployStatus 优先：先判失败，避免 Replace 回退态（status=Active + deployStatus=failed）误判成功
        if (currentDeployStatus === 'failed' || status === 'Failed') {
          clearDeployPoll();
          setDeployedAppId(undefined);
          setSaveLoading(false);
          setDeployError(currentDeployError || formatMessage('project.deployFailed'));
          refreshRequest?.();
          return;
        }

        if (currentDeployStatus === 'success' || (!currentDeployStatus && status === 'Active')) {
          clearDeployPoll();
          await updateProjectApp(projectId, String(targetApp.appId), {
            name: replacementName.trim() || bundleMeta?.name || targetApp.name,
            description:
              replacementDescription.trim() || (bundleMeta?.description || targetApp.description || '').slice(0, 200),
            // 未手动换图时传 0，由后端保留刚从新包导入的媒体；
            // 不再用弹窗打开时的旧 asset ID 覆盖新包资源。
            iconAssetId: iconAssetId || 0,
            coverAssetId: coverAssetId || 0,
          });
          message.success(formatMessage('project.replace.success', {}, 'App replaced successfully'));
          refreshRequest?.();
          onCloseRef.current(true);
          return;
        }
      } catch {
        consecutiveErrors += 1;
        if (consecutiveErrors >= MAX_CONSECUTIVE_POLL_ERRORS) {
          clearDeployPoll();
          setDeployedAppId(undefined);
          setSaveLoading(false);
          setDeployStatus('failed');
          setDeployError(formatMessage('project.deployFailed'));
          refreshRequest?.();
        }
      }
    };

    void checkDeploy();
    pollTimerRef.current = setInterval(() => void checkDeploy(), DEPLOY_POLL_INTERVAL_MS);
    return clearDeployPoll;
  }, [
    bundleMeta,
    clearDeployPoll,
    coverAssetId,
    deployedAppId,
    formatMessage,
    iconAssetId,
    message,
    projectId,
    refreshRequest,
    replacementDescription,
    replacementName,
    targetApp,
  ]);

  useImperativeHandle(ref, () => ({
    onOpen,
    onClose,
  }));

  const isDeploying = saveLoading && (deployStatus === 'deploying' || deployStatus === 'Deploying');

  return (
    <ProModal
      open={visible}
      onCancel={() => onClose()}
      title={formatMessage('project.replace.title', {}, 'Replace App')}
      className="importModalWrap attachment-upload-modal"
      width={544}
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
          disabled={uploading || saveLoading}
          beforeUpload={beforeUpload}
          openFileDialogOnClick={false}
          onRemove={() => {
            cancelActiveUpload();
            setFileList([]);
            setUploadedFileInfo(null);
            setBundleMeta(undefined);
            setUploadError('');
            setAppIdMismatch(false);
            setVersionNotIncreased(false);
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

      {appIdMismatch ? (
        <Alert
          className={styles['identity-warning']}
          type="error"
          showIcon
          message={formatMessage(
            'project.replace.appIdMismatchBlock',
            { bundleAppId: bundleMeta?.appId ?? '', currentAppId: String(targetApp?.appId ?? '') },
            'The App ID in the package does not match the current App and cannot be replaced.'
          )}
        />
      ) : null}

      {versionNotIncreased ? (
        <Alert
          className={styles['identity-warning']}
          type="warning"
          showIcon
          message={formatMessage(
            'project.replace.versionNotIncreased',
            { bundleVersion: String(bundleMeta?.version ?? ''), currentVersion: targetApp?.version ?? '' },
            'The package version is not higher than the current App version. The version will not increase after replacement.'
          )}
        />
      ) : null}

      {bundleMeta && uploadedFileInfo ? (
        <div className={bundleStyles['details-section']}>
          <div>
            <label className={bundleStyles['field-label']}>{formatMessage('common.name')}</label>
            <Input
              placeholder={formatMessage('apps.namePlaceholder')}
              value={replacementName}
              onChange={(e) => setReplacementName(e.target.value)}
              maxLength={64}
              disabled={uploading || saveLoading}
            />
          </div>
          <div className={bundleStyles['meta-grid']}>
            <div>
              <label className={bundleStyles['field-label']}>
                {formatMessage('project.replace.bundleAppId', {}, 'Bundle App ID')}
              </label>
              <Input value={bundleMeta.appId} disabled />
            </div>
            <div>
              <label className={bundleStyles['field-label']}>
                {formatMessage('project.replace.bundleVersion', {}, 'Bundle Version')}
              </label>
              <Input value={bundleMeta.version !== undefined ? String(bundleMeta.version) : ''} disabled />
            </div>
          </div>
          <div>
            <label className={bundleStyles['field-label']}>{formatMessage('common.description')}</label>
            <Input.TextArea
              placeholder={formatMessage('apps.descriptionPlaceholder')}
              value={replacementDescription}
              onChange={(e) => setReplacementDescription(e.target.value)}
              maxLength={200}
              rows={2}
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
            schemaDefault={targetApp?.schemaName || undefined}
            schemaReadOnly
            defaultEnvVars={advancedEnvVars}
            defaultVolumes={advancedVolumes}
            disabled={uploading || saveLoading}
            onChange={({ envVars: nextEnv, volumes: nextVolumes, envTouched, volumeTouched }) => {
              setAdvancedEnvVars(nextEnv);
              setAdvancedVolumes(nextVolumes);
              if (envTouched) setAdvancedEnvTouched(true);
              if (volumeTouched) setAdvancedVolumeTouched(true);
            }}
          />
        </div>
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
        disabled={uploading || Boolean(uploadingMediaKind) || isDeploying || !uploadedFileInfo || appIdMismatch}
        onClick={onReplace}
      >
        {formatMessage('project.replace.confirm', {}, 'Confirm Replace App')}
      </Button>
    </ProModal>
  );
});

ReplaceAppModal.displayName = 'ReplaceAppModal';

export default ReplaceAppModal;
