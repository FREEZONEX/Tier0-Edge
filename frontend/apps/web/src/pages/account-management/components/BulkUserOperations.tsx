import { Download, Upload as UploadIcon } from '@carbon/icons-react';
import { Alert, Button, Flex, notification as notificationApi, Upload, type UploadFile } from 'antd';
import { forwardRef, useImperativeHandle, useRef, useState } from 'react';

import {
  createUserExportJob,
  createUserImportJob,
  createUserImportTemplate,
  getAttachment,
  getUserJob,
  subscribeUserJobEvents,
  uploadUserImportFile,
  type UserExportRequest,
  type UserAsyncJob,
} from '@/apis/core-api';
import ComButton from '@/components/com-button';
import ProModal from '@/components/pro-modal';
import { useTranslate } from '@/hooks';
import { downloadFn } from '@/utils/blob';

import styles from './BulkUserOperations.module.scss';

type BulkUserOperationsProps = {
  filters: {
    keyword: string;
    statuses: string[];
    roleIds: string[];
    groupIds: string[];
  };
};

export type BulkUserOperationsHandle = {
  openImport: () => void;
  startExport: () => Promise<void>;
};

type ImportFeedback =
  | { status: 'processing' }
  | { status: 'partial'; failedRows: number; errorFileId: number }
  | { status: 'error'; message: string };

const userJobTerminal = (job: UserAsyncJob) =>
  job.status === 'succeeded' || job.status === 'failed' || job.status === 'canceled';

const pollUserJob = async (jobKey: string) => {
  while (true) {
    const job = await getUserJob(jobKey);
    if (userJobTerminal(job)) return job;
    await new Promise((resolve) => window.setTimeout(resolve, 2000));
  }
};

const waitForUserJob = (jobKey: string): Promise<UserAsyncJob> =>
  new Promise((resolve, reject) => {
    let eventStream: { close: () => void } | undefined;
    let settled = false;
    let fallingBack = false;
    const finish = (job: UserAsyncJob) => {
      if (settled) return;
      settled = true;
      eventStream?.close();
      resolve(job);
    };
    const fallbackToPolling = () => {
      if (settled || fallingBack) return;
      fallingBack = true;
      eventStream?.close();
      void pollUserJob(jobKey).then(finish).catch(reject);
    };
    try {
      eventStream = subscribeUserJobEvents(jobKey, {
        onMessage: (job) => {
          if (userJobTerminal(job)) finish(job);
        },
        onError: fallbackToPolling,
        onClose: fallbackToPolling,
      });
    } catch {
      fallbackToPolling();
    }
  });

const exportFileName = () => {
  const stamp = new Date().toISOString().replaceAll(':', '').replace('T', '-').slice(0, 15);
  return `users-export-${stamp}.xlsx`;
};

const formatImportFailureReason = (formatMessage: ReturnType<typeof useTranslate>, errorMessage?: string) => {
  const message = errorMessage?.trim() || '';
  if (message.startsWith('account.')) return formatMessage(message);
  const missingColumn = /^Users sheet missing column (.+)$/i.exec(message);
  if (missingColumn) {
    return formatMessage('account.bulk.importMissingColumn', { column: missingColumn[1] });
  }
  if (/^missing required sheet Users$/i.test(message)) {
    return formatMessage('account.bulk.importUsersSheetRequired');
  }
  if (/^Users sheet is empty$/i.test(message)) {
    return formatMessage('account.bulk.importUsersSheetEmpty');
  }
  if (/^open workbook:/i.test(message)) {
    return formatMessage('account.bulk.importWorkbookOpenFailed');
  }
  return formatMessage('account.bulk.importWorkbookInvalid');
};

const downloadImportErrorFile = async (fileId: number) => {
  const data = await getAttachment({ fileId });
  downloadFn({ data, name: 'user-import-errors.xlsx' });
};

const BulkUserOperations = forwardRef<BulkUserOperationsHandle, BulkUserOperationsProps>(({ filters }, ref) => {
  const formatMessage = useTranslate();
  const [notification, notificationContextHolder] = notificationApi.useNotification({
    maxCount: 3,
    placement: 'bottomRight',
    stack: { threshold: 3 },
  });
  const [importOpen, setImportOpen] = useState(false);
  const [fileList, setFileList] = useState<UploadFile[]>([]);
  const [importing, setImporting] = useState(false);
  const [templateLoading, setTemplateLoading] = useState(false);
  const [importFeedback, setImportFeedback] = useState<ImportFeedback>();
  const [errorFileLoading, setErrorFileLoading] = useState(false);
  const activeImportJobKey = useRef<string>();
  const importOpenRef = useRef(false);
  const importProcessing = importFeedback?.status === 'processing';

  const resetImportDialog = () => {
    importOpenRef.current = false;
    setImportOpen(false);
    setFileList([]);
    setImportFeedback(undefined);
    activeImportJobKey.current = undefined;
  };

  const closeImport = () => {
    if (importing) return;
    resetImportDialog();
  };

  const startExport = async () => {
    const request: UserExportRequest = {
      scope: 'filtered',
      keyword: filters.keyword,
      statuses: filters.statuses.map((status) => (status === 'true' ? 'enabled' : 'disabled')),
      roleIds: filters.roleIds,
      groupIds: filters.groupIds,
      sensitiveMode: 'masked',
    };
    try {
      const accepted = await createUserExportJob(request);
      const notificationKey = `user-export-${accepted.jobKey}`;
      notification.info({
        key: notificationKey,
        placement: 'bottomRight',
        closable: true,
        duration: 6,
        showProgress: true,
        message: formatMessage('account.bulk.exportRunning'),
        description: formatMessage('account.bulk.exportRunningDesc'),
      });
      void waitForUserJob(accepted.jobKey)
        .then(async (job) => {
          if (job.status !== 'succeeded') {
            throw new Error(job.errorMessage || 'account.bulk.exportFailed');
          }
          const resultFileId = Number(job.resultJson?.resultFileId || 0);
          if (!resultFileId) throw new Error('account.bulk.exportFailed');
          const data = await getAttachment({ fileId: resultFileId });
          const fileName = String(job.resultJson?.fileName || exportFileName());
          const downloadExportFile = () => downloadFn({ data, name: fileName });
          downloadExportFile();
          notification.success({
            key: notificationKey,
            placement: 'bottomRight',
            closable: true,
            duration: 6,
            showProgress: true,
            message: formatMessage('account.bulk.exportDownloaded'),
          });
        })
        .catch((error) => {
          const errorKey = (error as Error)?.message || 'account.bulk.exportFailed';
          notification.error({
            key: notificationKey,
            placement: 'bottomRight',
            closable: true,
            duration: null,
            message: formatMessage(errorKey, {}, errorKey),
          });
        });
    } catch (error) {
      const errorKey = (error as Error)?.message || 'account.bulk.exportFailed';
      notification.error({
        placement: 'bottomRight',
        closable: true,
        duration: null,
        message: formatMessage(errorKey, {}, errorKey),
      });
    }
  };

  useImperativeHandle(ref, () => ({
    openImport: () => {
      setFileList([]);
      setImportFeedback(undefined);
      activeImportJobKey.current = undefined;
      importOpenRef.current = true;
      setImportOpen(true);
    },
    startExport,
  }));

  const downloadTemplate = async () => {
    setTemplateLoading(true);
    try {
      const template = await createUserImportTemplate();
      const data = await getAttachment({ fileId: template.fileId });
      downloadFn({ data, name: template.fileName || 'user-import-template.xlsx' });
    } finally {
      setTemplateLoading(false);
    }
  };

  const confirmImport = async () => {
    const file = fileList[0]?.originFileObj;
    if (!file) {
      setImportFeedback({ status: 'error', message: formatMessage('account.bulk.fileRequired') });
      return;
    }
    if (!file.name.toLowerCase().endsWith('.xlsx')) {
      setImportFeedback({ status: 'error', message: formatMessage('account.bulk.xlsxOnly') });
      return;
    }
    setImporting(true);
    setImportFeedback(undefined);
    try {
      const sourceFileId = await uploadUserImportFile(file as File);
      const accepted = await createUserImportJob(sourceFileId);
      activeImportJobKey.current = accepted.jobKey;
      setImportFeedback({ status: 'processing' });
      const notificationKey = `user-import-${accepted.jobKey}`;
      void waitForUserJob(accepted.jobKey)
        .then(async (job) => {
          const showInImportDialog = importOpenRef.current && activeImportJobKey.current === accepted.jobKey;
          if (job.status !== 'succeeded') {
            const feedback: ImportFeedback = {
              status: 'error',
              message: formatMessage('account.bulk.importFailed', {
                reason: formatImportFailureReason(formatMessage, job.errorMessage),
              }),
            };
            if (showInImportDialog) {
              setImportFeedback(feedback);
            } else {
              notification.error({
                key: notificationKey,
                placement: 'bottomRight',
                closable: true,
                duration: null,
                message: feedback.message,
              });
            }
            return;
          }
          const errorFileId = Number(job.resultJson?.errorFileId || 0);
          if (job.resultJson?.partial || errorFileId > 0) {
            const failedRows = Math.max(
              Number(job.resultJson?.users?.failed || 0),
              Number(job.resultJson?.groups?.failed || 0),
              Number(job.resultJson?.memberships?.failed || 0)
            );
            if (showInImportDialog) {
              setImportFeedback({ status: 'partial', failedRows, errorFileId });
            }
            const partialMessage = formatMessage('account.bulk.importPartial', { count: failedRows });
            if (!errorFileId) {
              if (!showInImportDialog) {
                notification.error({
                  key: notificationKey,
                  placement: 'bottomRight',
                  closable: true,
                  duration: null,
                  message: partialMessage,
                });
              }
              return;
            }
            if (showInImportDialog) return;
            let description = formatMessage('account.bulk.errorFileReady');
            try {
              await downloadImportErrorFile(errorFileId);
              description = formatMessage('account.bulk.errorAutoDownloaded');
            } catch {
              description = formatMessage('account.bulk.errorAutoDownFailed');
            }
            notification.error({
              key: notificationKey,
              placement: 'bottomRight',
              closable: true,
              duration: null,
              message: partialMessage,
              description,
              actions: (
                <Button type="link" danger onClick={() => void downloadImportErrorFile(errorFileId)}>
                  {formatMessage('account.bulk.downloadErrors')}
                </Button>
              ),
            });
            return;
          }
          if (showInImportDialog) {
            resetImportDialog();
          }
          notification.success({
            key: notificationKey,
            placement: 'bottomRight',
            closable: true,
            duration: 6,
            showProgress: true,
            message: formatMessage('account.bulk.importSuccess'),
          });
        })
        .catch((error) => {
          const feedback: ImportFeedback = {
            status: 'error',
            message: formatMessage('account.bulk.importFailed', {
              reason: formatImportFailureReason(formatMessage, (error as Error)?.message),
            }),
          };
          const showInImportDialog = importOpenRef.current && activeImportJobKey.current === accepted.jobKey;
          if (showInImportDialog) {
            setImportFeedback(feedback);
          } else {
            notification.error({
              key: notificationKey,
              placement: 'bottomRight',
              closable: true,
              duration: null,
              message: feedback.message,
            });
          }
        });
    } catch (error) {
      const feedback: ImportFeedback = {
        status: 'error',
        message: formatMessage('account.bulk.importFailed', {
          reason: formatImportFailureReason(formatMessage, (error as Error)?.message),
        }),
      };
      setImportFeedback(feedback);
    } finally {
      setImporting(false);
    }
  };

  const downloadImportErrors = async (fileId: number) => {
    setErrorFileLoading(true);
    try {
      await downloadImportErrorFile(fileId);
    } finally {
      setErrorFileLoading(false);
    }
  };

  const importModal = (
    <ProModal
      open={importOpen}
      title={formatMessage('account.bulk.importUsers')}
      width={680}
      fullScreenable={false}
      destroyOnHidden
      maskClosable={false}
      onCancel={closeImport}
    >
      <Flex vertical gap={16}>
        <div className={styles['template-action']}>
          <Button icon={<Download size={16} />} loading={templateLoading} onClick={() => void downloadTemplate()}>
            {formatMessage('account.bulk.downloadTemplate')}
          </Button>
        </div>
        <Upload.Dragger
          className={styles['import-upload']}
          accept=".xlsx"
          maxCount={1}
          disabled={importing || importProcessing}
          beforeUpload={() => false}
          fileList={fileList}
          onChange={({ fileList: nextFileList }) => {
            setFileList(nextFileList.slice(-1));
            setImportFeedback(undefined);
          }}
        >
          <div className={styles['upload-content']}>
            <UploadIcon size={32} />
            <p>{formatMessage('common.clickOrDragForUpload')}</p>
            <span>{formatMessage('account.bulk.xlsxOnly')}</span>
          </div>
        </Upload.Dragger>
        {importFeedback?.status === 'processing' ? (
          <Alert type="info" showIcon message={formatMessage('account.bulk.importRunning')} />
        ) : null}
        {importFeedback?.status === 'partial' ? (
          <Alert
            type="error"
            showIcon
            message={formatMessage('account.bulk.importPartial', {
              count: importFeedback.failedRows,
            })}
            action={
              importFeedback.errorFileId ? (
                <Button
                  type="link"
                  danger
                  loading={errorFileLoading}
                  onClick={() => void downloadImportErrors(importFeedback.errorFileId)}
                >
                  {formatMessage('account.bulk.downloadErrors')}
                </Button>
              ) : null
            }
          />
        ) : null}
        {importFeedback?.status === 'error' ? <Alert type="error" showIcon message={importFeedback.message} /> : null}
        <Flex gap={8} justify="flex-end" className={styles.footer}>
          <ComButton disabled={importing} onClick={closeImport}>
            {formatMessage('common.cancel')}
          </ComButton>
          <ComButton
            type="primary"
            loading={importing}
            disabled={importProcessing}
            onClick={() => void confirmImport()}
          >
            {formatMessage('common.import')}
          </ComButton>
        </Flex>
      </Flex>
    </ProModal>
  );

  return (
    <>
      {notificationContextHolder}
      {importModal}
    </>
  );
});

BulkUserOperations.displayName = 'BulkUserOperations';

export default BulkUserOperations;
