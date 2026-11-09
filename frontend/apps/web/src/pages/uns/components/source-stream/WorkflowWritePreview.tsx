import { Alert, Tag } from 'antd';
import ProTable from '@/components/pro-table';
import HelpTooltip from '@/components/help-tooltip';
import { useTranslate } from '@/hooks';
import type { StreamTestResult } from '@/apis/core-api/source-stream';
import { displayValue } from './workflow-data';
import styles from './workflow.module.scss';

export default function WorkflowWritePreview({
  targets,
  collectionNames,
}: {
  targets: NonNullable<StreamTestResult['targets']>;
  collectionNames: Record<string, string>;
}) {
  const t = useTranslate();
  const rows = targets.flatMap((target, index) =>
    target.preview ? [{ ...target.preview, collectionId: target.collectionId, index }] : []
  );
  if (!rows.length) return null;
  const states = ['created', 'updated', 'unchanged', 'error'] as const;
  const colors: Record<string, string> = { created: 'green', updated: 'blue', unchanged: 'default', error: 'red' };
  return (
    <div className={styles['form-stack']}>
      <strong>
        {t('Output.writePreview')} <HelpTooltip title={t('Output.writePreviewHelp')} />
      </strong>
      <div className={styles['inline-row']}>
        {states.map((state) => (
          <Tag key={state} color={colors[state]}>
            {t(`Output.status.${state}`)} · {rows.filter((row) => row.status === state).length}
          </Tag>
        ))}
      </div>
      <ProTable
        rowKey="index"
        resizeable={false}
        pagination={{ pageSize: 5, showSizeChanger: false }}
        dataSource={rows}
        columns={[
          ...(new Set(rows.map((row) => row.collectionId)).size > 1
            ? [
                {
                  title: t('Output.collection'),
                  dataIndex: 'collectionId',
                  render: (id: string) => collectionNames[id] || id,
                },
              ]
            : []),
          { title: t('Output.recordIdentity'), dataIndex: 'recordId', ellipsis: true },
          { title: t('Output.writeMode'), render: (_, row) => t(`Output.mode.${row.mode}`) },
          {
            title: t('Output.result'),
            render: (_, row) => <Tag color={colors[row.status]}>{t(`Output.status.${row.status}`)}</Tag>,
          },
        ]}
        expandable={{
          defaultExpandedRowKeys: [rows[0].index],
          expandedRowRender: (row) =>
            row.error ? (
              <Alert type="error" message={t(`Output.error.${row.error}`)} />
            ) : row.changes.length ? (
              <ProTable
                rowKey="field"
                resizeable={false}
                pagination={false}
                dataSource={row.changes}
                columns={[
                  { title: t('Workflow.outputField'), dataIndex: 'field' },
                  {
                    title: t('Output.before'),
                    render: (_, field) => (field.beforeExists ? displayValue(field.before) : t('Output.absent')),
                  },
                  {
                    title: t('Output.after'),
                    render: (_, field) => (field.afterExists ? displayValue(field.after) : t('Output.absent')),
                  },
                ]}
              />
            ) : (
              <span>{t('Output.noChanges')}</span>
            ),
        }}
      />
    </div>
  );
}
