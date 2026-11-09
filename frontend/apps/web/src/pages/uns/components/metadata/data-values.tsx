import { Tag } from 'antd';
import dayjs from 'dayjs';
import ProTable from '@/components/pro-table';
import { useTranslate } from '@/hooks';
import type { MetaSchema, UnsRecord } from '@/apis/core-api/uns-metadata';
import styles from './index.module.scss';

export const displayValue = (value: unknown) =>
  value === undefined ? '—' : typeof value === 'string' ? value : JSON.stringify(value);
export const displayTime = (value?: number) => (value ? dayjs(value).format('YYYY-MM-DD HH:mm:ss.SSS') : '—');
export const recordName = (record: UnsRecord) =>
  [record.data.name, record.data.displayName, record.data.title].find(
    (value): value is string => typeof value === 'string' && !!value.trim()
  ) || record.id;

export function DataValue({ value, unit }: { value: unknown; unit?: string }) {
  const t = useTranslate();
  if (value !== null && typeof value === 'object') {
    return (
      <details className={styles['nested-value']}>
        <summary>
          {t(Array.isArray(value) ? 'UnsMeta.arrayValue' : 'UnsMeta.objectValue', { count: Object.keys(value).length })}
        </summary>
        <pre className={styles.code}>{JSON.stringify(value, null, 2)}</pre>
      </details>
    );
  }
  return (
    <span className={styles['data-value']}>
      {typeof value === 'boolean' ? (
        <Tag>{t(value ? 'UnsMeta.trueValue' : 'UnsMeta.falseValue')}</Tag>
      ) : (
        displayValue(value)
      )}
      {value != null && unit && <small className={styles['value-unit']}>{unit}</small>}
    </span>
  );
}

export function FieldsTable({
  data,
  schema,
  origin,
  time,
}: {
  data: Record<string, unknown>;
  schema?: MetaSchema;
  origin: 'Record' | 'Sample';
  time?: number;
}) {
  const t = useTranslate();
  const fields = [...new Set([...Object.keys(schema?.properties || {}), ...Object.keys(data)])];
  return (
    <ProTable
      rowKey="name"
      size="small"
      pagination={false}
      columnFit={false}
      resizeable={false}
      scroll={{ x: 640 }}
      dataSource={fields.map((name) => ({ name }))}
      columns={[
        {
          title: t('UnsMeta.field'),
          dataIndex: 'name',
          width: 180,
          render: (name: string) => (
            <div>
              <strong>{schema?.properties?.[name]?.title || name}</strong>
              {schema?.properties?.[name]?.title && <small className={styles['record-id']}>{name}</small>}
            </div>
          ),
        },
        {
          title: t('UnsMeta.currentValue'),
          width: 240,
          render: (_, row) => <DataValue value={data[row.name]} unit={schema?.properties?.[row.name]?.unit} />,
        },
        {
          title: t('UnsMeta.valueOrigin'),
          width: 90,
          render: () => <Tag>{t(origin === 'Record' ? 'UnsUi.recordData' : 'UnsUi.sampleData')}</Tag>,
        },
        {
          title: t('UnsMeta.dataTime'),
          width: 220,
          render: () => <span className={styles['data-time']}>{displayTime(time)}</span>,
        },
      ]}
    />
  );
}
