import { useEffect, useRef, useState } from 'react';
import { Flex, Progress, notification as notificationApi } from 'antd';
import { createUnsImportJob, getUnsImportJob, uploadUnsImportFile } from '@/apis/core-api/uns';
import { useTranslate } from '@/hooks';
import { useBaseStore } from '@/stores/base';

// Keep tracking outside the dialog so closing it does not stop the background task feedback.
export const useUnsImportJob = (initTreeData: (options: { reset: boolean }) => void) => {
  const formatMessage = useTranslate();
  const userId = useBaseStore((state) => state.currentUserInfo?.sub);
  const storageKey = `uns-import-job:${userId || 'anonymous'}`;
  const [jobKey, setJobKey] = useState<string>();
  const [submitting, setSubmitting] = useState(false);
  const submittingRef = useRef(false);
  const [notification, contextHolder] = notificationApi.useNotification({
    maxCount: 3,
    placement: 'bottomRight',
    stack: { threshold: 3 },
  });
  const callbacks = useRef({ initTreeData, formatMessage });
  useEffect(() => {
    callbacks.current = { initTreeData, formatMessage };
  }, [initTreeData, formatMessage]);

  useEffect(() => {
    try {
      setJobKey(sessionStorage.getItem(storageKey) || undefined);
    } catch {
      // Tracking still works when browser storage is unavailable.
    }
  }, [storageKey]);

  useEffect(() => {
    if (!jobKey) return;
    let stopped = false;
    let dismissed = false;
    let timer: ReturnType<typeof setTimeout>;
    const controller = new AbortController();
    const key = `uns-import-${jobKey}`;
    const showProgress = (progress = 0, history = false, reconnecting = false) => {
      if (dismissed) return;
      const { formatMessage } = callbacks.current;
      notification.info({
        key,
        duration: 0,
        message: formatMessage('uns.importRunning'),
        description: (
          <Flex vertical gap={8}>
            <span>
              {formatMessage(
                reconnecting ? 'uns.importReconnecting' : history ? 'uns.importHistory' : 'uns.importBackground'
              )}
            </span>
            <Progress percent={Math.min(progress, 99)} size="small" />
          </Flex>
        ),
        onClose: () => {
          dismissed = true;
        },
      });
    };
    let progress = 0;
    let history = false;
    showProgress();
    const poll = async () => {
      let job;
      try {
        job = await getUnsImportJob(jobKey, controller.signal);
      } catch {
        if (stopped) return;
        showProgress(progress, history, true);
        timer = setTimeout(poll, 2000);
        return;
      }
      if (stopped) return;
      const { formatMessage, initTreeData } = callbacks.current;
      if (['succeeded', 'failed', 'canceled'].includes(job.status)) {
        try {
          sessionStorage.removeItem(storageKey);
        } catch {
          /* Storage is optional. */
        }
        setJobKey(undefined);
        if (job.status === 'succeeded') {
          notification.success({
            key,
            duration: 6,
            showProgress: true,
            message: formatMessage('uns.importFinished'),
            description: formatMessage('uns.importSummary', {
              imported: job.resultJson?.imported || 0,
              updated: job.resultJson?.updated || 0,
              skipped: job.resultJson?.skipped || 0,
            }),
          });
          initTreeData({ reset: true });
        } else {
          notification.error({
            key,
            duration: 0,
            message: formatMessage('uns.importFailed'),
            description:
              job.resultJson?.phase === 'historyFailed'
                ? formatMessage('uns.importHistoryFailed', { reason: job.errorMessage || '' })
                : job.errorMessage || formatMessage('uns.importFailed'),
          });
          if (job.resultJson?.phase === 'historyFailed') initTreeData({ reset: true });
        }
        return;
      }
      progress = job.progress;
      history = job.resultJson?.phase === 'history';
      showProgress(progress, history);
      timer = setTimeout(poll, 2000);
    };
    void poll();
    return () => {
      stopped = true;
      clearTimeout(timer);
      controller.abort();
    };
  }, [jobKey, storageKey, notification]);

  const submitImport = async (file: File) => {
    if (submittingRef.current || jobKey) return false;
    submittingRef.current = true;
    setSubmitting(true);
    try {
      const fileId = await uploadUnsImportFile(file);
      const job = await createUnsImportJob(fileId);
      try {
        sessionStorage.setItem(storageKey, job.jobKey);
      } catch {
        /* Storage is optional. */
      }
      setJobKey(job.jobKey);
      return true;
    } catch (error) {
      notification.error({
        duration: 0,
        message: formatMessage('uns.importFailed'),
        description:
          error instanceof Error && error.message !== 'uns.importFailed'
            ? error.message
            : formatMessage('uns.importFailed'),
      });
      return false;
    } finally {
      submittingRef.current = false;
      setSubmitting(false);
    }
  };

  return { submitImport, busy: submitting || Boolean(jobKey), submitting, contextHolder };
};
