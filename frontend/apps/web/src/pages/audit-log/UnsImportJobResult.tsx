import { Progress, Tag, Typography } from 'antd';
import type { AuditLogItem } from '@/apis/core-api/audit-log';
import type { UnsImportJob } from '@/apis/core-api/uns';
import { useTranslate } from '@/hooks';
import styles from './index.module.scss';

export const UnsImportJobResult = ({ detail }: { detail: AuditLogItem }) => {
  const formatMessage = useTranslate();
  if (detail.resType !== 'UNS' || detail.businessType.toLowerCase() !== 'import') return null;
  let job: UnsImportJob | undefined;
  try {
    const data = JSON.parse(detail.detailJson || '{}');
    job =
      data.job ||
      (data.jobKey
        ? {
            jobKey: data.jobKey,
            status: data.status || data.jobStatus,
            progress: data.status === 'succeeded' ? 100 : 0,
            resultJson: data,
            errorMessage: data.error,
          }
        : undefined);
  } catch {
    return null;
  }
  if (!job?.status) return null;
  const terminal = ['succeeded', 'failed', 'canceled'].includes(job.status);
  const statusKeys = {
    pending: 'uns.importPending',
    running: 'uns.importRunning',
    succeeded: 'uns.importFinished',
    failed: 'uns.importFailed',
    canceled: 'uns.importCanceled',
  };
  return (
    <section className={styles.detailSection}>
      <div className={styles.sectionHeader}>{formatMessage('uns.importTask')}</div>
      <Typography.Paragraph copyable>{job.jobKey}</Typography.Paragraph>
      <Tag color={job.status === 'succeeded' ? 'success' : terminal ? 'error' : 'processing'}>
        {formatMessage(statusKeys[job.status] || 'uns.importRunning')}
      </Tag>
      {!terminal && <Progress percent={Math.min(job.progress, 99)} size="small" />}
      {job.resultJson?.phase === 'history' && (
        <Typography.Paragraph>{formatMessage('uns.importHistory')}</Typography.Paragraph>
      )}
      {job.resultJson && (terminal || job.resultJson.imported !== undefined) && (
        <Typography.Paragraph>
          {formatMessage('uns.importSummary', {
            imported: job.resultJson.imported || 0,
            updated: job.resultJson.updated || 0,
            skipped: job.resultJson.skipped || 0,
          })}
        </Typography.Paragraph>
      )}
      {job.errorMessage && (
        <Typography.Paragraph type="danger">
          {job.resultJson?.phase === 'historyFailed'
            ? formatMessage('uns.importHistoryFailed', { reason: job.errorMessage })
            : job.errorMessage}
        </Typography.Paragraph>
      )}
    </section>
  );
};
