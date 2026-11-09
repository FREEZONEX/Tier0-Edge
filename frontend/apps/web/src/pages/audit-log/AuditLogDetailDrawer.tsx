import { Download } from '@carbon/icons-react';
import { App, Button, Collapse, Drawer, Space, Tag, Tooltip, Typography } from 'antd';
import { useState } from 'react';

import { getAttachment } from '@/apis/core-api';
import type { AuditLogItem } from '@/apis/core-api/audit-log';
import { Close } from '@/components/lucide-icon/carbon';
import { useTranslate } from '@/hooks';
import { formatTimestamp } from '@/utils/format';
import { downloadFn } from '@/utils/blob';
import {
  getAuditActionLabel,
  getAuditModuleLabel,
  getAuditOperatorLabel,
  getAuditResourceLabel,
  getAuditResultLabel,
  getAuditScopeLabel,
} from './presentation';
import styles from './index.module.scss';
import { AuditChanges } from './AuditChanges';
import { UnsImportJobResult } from './UnsImportJobResult';
import TerminalInputSummary from './TerminalInputSummary';

const { Paragraph } = Typography;

const formatDetailJson = (value?: string) => {
  const raw = value?.trim();
  if (!raw) return '{}';
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
};

const parseJobResult = (value?: string) => {
  try {
    const detail = JSON.parse(value || '{}') as {
      sourceFileId?: string | number;
      resultFileId?: string | number;
      errorFileId?: string | number;
      error?: string;
    };
    return {
      sourceFileId: Number(detail.sourceFileId || 0),
      resultFileId: Number(detail.resultFileId || 0),
      errorFileId: Number(detail.errorFileId || 0),
      error: String(detail.error || '').trim(),
    };
  } catch {
    return { sourceFileId: 0, resultFileId: 0, errorFileId: 0, error: '' };
  }
};

type AuditLogDetailDrawerProps = {
  open: boolean;
  loading: boolean;
  detail?: AuditLogItem | null;
  onClose: () => void;
  onRelated?: (detail: AuditLogItem) => void;
};

export const AuditLogDetailDrawer = ({ open, loading, detail, onClose, onRelated }: AuditLogDetailDrawerProps) => {
  const [downloadingFileId, setDownloadingFileId] = useState(0);
  const formatMessage = useTranslate();
  const { message } = App.useApp();
  const translate = (id: string, opt?: any, defaultMessage?: string) => formatMessage(id, opt, defaultMessage);
  const t = (id: string, fallback: string, opt?: Record<string, unknown>) => translate(id, opt, fallback);
  const jobResult = parseJobResult(detail?.detailJson);
  const rawFailure = jobResult.error || detail?.resultMsg || '';
  const failureReason = /^[\w]+\.[\w.]+$/.test(rawFailure) ? translate(rawFailure, {}, rawFailure) : rawFailure;
  const canViewRelated =
    detail?.resId && (!['UserManagement', 'Role', 'Resource'].includes(detail.resType) || /^\d+$/.test(detail.resId));
  const downloadJobFile = async (fileId: number, name: string) => {
    setDownloadingFileId(fileId);
    try {
      const data = await getAttachment({ fileId });
      downloadFn({ data, name });
    } catch {
      message.error(t('auditLog.downloadFailed', 'Download failed'));
    } finally {
      setDownloadingFileId(0);
    }
  };

  return (
    <Drawer
      rootClassName={styles.auditDrawer}
      open={open}
      width={Math.min(900, window.innerWidth)}
      title={t('auditLog.detailTitle', 'Audit Details')}
      closable={false}
      destroyOnClose
      onClose={onClose}
      extra={
        <Tooltip title={t('common.close', 'Close')}>
          <Button color="default" variant="text" onClick={onClose} icon={<Close size={20} />} />
        </Tooltip>
      }
      classNames={{
        body: styles.auditDrawerBody,
      }}
    >
      {loading ? (
        <Paragraph>{t('common.loading', 'Loading...')}</Paragraph>
      ) : detail ? (
        <div className={styles.detailPanel}>
          <Paragraph className={styles['audit-summary']}>
            {t('auditLog.summary', '{operator} · {action} · {resource}', {
              operator: getAuditOperatorLabel(detail),
              action: getAuditActionLabel(detail.businessType, translate),
              resource: getAuditResourceLabel(detail, translate),
            })}
          </Paragraph>
          {canViewRelated && onRelated && (
            <Button onClick={() => onRelated(detail)}>{t('auditLog.related', 'Related object logs')}</Button>
          )}
          <section className={styles.detailSection}>
            <div className={styles.sectionHeader}>{t('auditLog.overview', 'Overview')}</div>
            <div className={styles.detailGrid}>
              <div className={styles.detailItem}>
                <span className={styles.detailLabel}>{t('auditLog.time', 'Time')}</span>
                <span className={styles.detailValue}>{formatTimestamp(detail.createdAt) || '-'}</span>
              </div>
              <div className={styles.detailItem}>
                <span className={styles.detailLabel}>{t('auditLog.operator', 'Operator')}</span>
                <span className={styles.detailValue}>{getAuditOperatorLabel(detail)}</span>
              </div>
              <div className={styles.detailItem}>
                <span className={styles.detailLabel}>{t('auditLog.scope', 'Scope')}</span>
                <span className={styles.detailValue}>{getAuditScopeLabel(detail, translate)}</span>
              </div>
              <div className={styles.detailItem}>
                <span className={styles.detailLabel}>{t('auditLog.module', 'Module')}</span>
                <span className={styles.detailValue}>
                  {getAuditModuleLabel(detail.scopeType, detail.scopeName, detail.resType, translate)}
                </span>
              </div>
              <div className={styles.detailItem}>
                <span className={styles.detailLabel}>{t('auditLog.action', 'Action')}</span>
                <span className={styles.detailValue}>{getAuditActionLabel(detail.businessType, translate)}</span>
              </div>
              <div className={styles.detailItem}>
                <span className={styles.detailLabel}>{t('auditLog.result', 'Result')}</span>
                <Tag
                  color={
                    detail.code === 202 ? 'processing' : detail.code >= 200 && detail.code < 400 ? 'success' : 'error'
                  }
                >
                  {getAuditResultLabel(detail, translate)}
                </Tag>
              </div>
              <div className={`${styles.detailItem} ${styles.fullSpan}`}>
                <span className={styles.detailLabel}>{t('auditLog.resource', 'Resource')}</span>
                <span className={styles.detailValue}>{getAuditResourceLabel(detail, translate)}</span>
              </div>
            </div>
          </section>

          <AuditChanges detail={detail} />
          <UnsImportJobResult detail={detail} />
          <TerminalInputSummary detailJson={detail.detailJson} />
          {jobResult.error || (detail.result === 'failed' && detail.resultMsg) ? (
            <section className={styles.detailSection}>
              <div className={styles.sectionHeader}>{t('auditLog.failureReason', 'Failure Reason')}</div>
              <div className={styles.failureReason}>{failureReason}</div>
            </section>
          ) : null}

          {jobResult.sourceFileId || jobResult.resultFileId || jobResult.errorFileId ? (
            <section className={styles.detailSection}>
              <div className={styles.sectionHeader}>{t('auditLog.jobFiles', 'Task Files')}</div>
              <Space wrap className={styles.jobFiles}>
                {jobResult.sourceFileId ? (
                  <Button
                    type="link"
                    icon={<Download size={16} />}
                    loading={downloadingFileId === jobResult.sourceFileId}
                    onClick={() =>
                      void downloadJobFile(
                        jobResult.sourceFileId,
                        detail.resType === 'UNS' ? 'uns-import.json' : 'user-import.xlsx'
                      )
                    }
                  >
                    {formatMessage('auditLog.downloadSource')}
                  </Button>
                ) : null}
                {jobResult.resultFileId ? (
                  <Button
                    icon={<Download size={16} />}
                    loading={downloadingFileId === jobResult.resultFileId}
                    onClick={() => void downloadJobFile(jobResult.resultFileId, 'users.xlsx')}
                  >
                    {t('auditLog.downloadResult', 'Download Result')}
                  </Button>
                ) : null}
                {jobResult.errorFileId ? (
                  <Button
                    danger
                    icon={<Download size={16} />}
                    loading={downloadingFileId === jobResult.errorFileId}
                    onClick={() => void downloadJobFile(jobResult.errorFileId, 'user-import-errors.xlsx')}
                  >
                    {t('auditLog.downloadErrors', 'Download Error File')}
                  </Button>
                ) : null}
              </Space>
            </section>
          ) : null}

          <section className={styles.detailSection}>
            <div className={styles.sectionHeader}>{t('auditLog.request', 'Request')}</div>
            <div className={styles.detailGrid}>
              <div className={styles.detailItem}>
                <span className={styles.detailLabel}>{t('auditLog.code', 'Code')}</span>
                <span className={styles.detailValue}>{detail.code || '-'}</span>
              </div>
              <div className={styles.detailItem}>
                <span className={styles.detailLabel}>{t('auditLog.requestId', 'Request ID')}</span>
                <span className={styles.detailValue}>
                  <Typography.Text copyable={Boolean(detail.requestId)}>
                    {detail.requestId || t('auditLog.notRecorded', 'Not recorded')}
                  </Typography.Text>
                </span>
              </div>
              <div className={styles.detailItem}>
                <span className={styles.detailLabel}>{t('auditLog.ip', 'IP')}</span>
                <span className={styles.detailValue}>{detail.ip || t('auditLog.notRecorded', 'Not recorded')}</span>
              </div>
              <div className={`${styles.detailItem} ${styles.fullSpan}`}>
                <span className={styles.detailLabel}>{t('auditLog.userAgent', 'User Agent')}</span>
                <span className={styles.detailValue}>
                  {detail.userAgent || t('auditLog.notRecorded', 'Not recorded')}
                </span>
              </div>
            </div>
          </section>

          <Collapse
            items={[
              {
                key: 'json',
                label: t('auditLog.detailJson', 'Details JSON'),
                children: <pre className={styles.detailJson}>{formatDetailJson(detail.detailJson)}</pre>,
              },
            ]}
          />
        </div>
      ) : (
        <div className={styles.emptyDetail}>{t('auditLog.noDetail', 'No details available')}</div>
      )}
    </Drawer>
  );
};
