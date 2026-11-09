import { Alert, Table } from 'antd';
import { useTranslate } from '@/hooks';
import type { AuditLogItem } from '@/apis/core-api/audit-log';
import styles from './index.module.scss';

const showValue = (value: unknown) =>
  value === undefined ? '—' : typeof value === 'string' ? value : JSON.stringify(value, null, 2);

export const AuditChanges = ({ detail }: { detail: AuditLogItem }) => {
  const t = useTranslate();
  let data: Record<string, unknown>;
  try {
    data = JSON.parse(detail.detailJson || '{}');
  } catch {
    return <Alert type="warning" showIcon message={t('auditLog.invalidDetail')} />;
  }
  if (!data || typeof data !== 'object') return null;
  const before = data.before && typeof data.before === 'object' ? (data.before as Record<string, unknown>) : {};
  const after = data.after && typeof data.after === 'object' ? (data.after as Record<string, unknown>) : {};
  const keys = [...new Set([...Object.keys(before), ...Object.keys(after)])];
  const changes = keys
    .filter((key) => JSON.stringify(before[key]) !== JSON.stringify(after[key]))
    .map((key) => ({ key, before: showValue(before[key]), after: showValue(after[key]) }));
  if (!changes.length) return null;
  return (
    <section className={styles.detailSection}>
      <div className={styles.sectionHeader}>{t('auditLog.changes')}</div>
      <Table
        size="small"
        rowKey="key"
        pagination={false}
        dataSource={changes}
        tableLayout="fixed"
        columns={[
          {
            title: t('auditLog.field'),
            dataIndex: 'key',
            width: 120,
            render: (value: string) => <span className={styles['change-value']}>{value}</span>,
          },
          {
            title: t('auditLog.before'),
            dataIndex: 'before',
            render: (value: string) => <pre className={styles['change-value']}>{value}</pre>,
          },
          {
            title: t('auditLog.after'),
            dataIndex: 'after',
            render: (value: string) => <pre className={styles['change-value']}>{value}</pre>,
          },
        ]}
      />
    </section>
  );
};
