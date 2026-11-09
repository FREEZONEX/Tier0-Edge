import { useState } from 'react';
import { Alert, App, Button, Drawer, Input, Modal, Pagination, Select, Table, type TableColumnsType } from 'antd';
import { Copy, RecentlyViewed, Renew, Search } from '@carbon/icons-react';
import { recordPage } from '@/apis/core-api/uns-v2';
import type { UnsMetadata, UnsRecord } from '@/apis/core-api/uns-metadata';
import { usePagination, useTranslate } from '@/hooks';
import { copyToClipboard } from '@/utils/common';
import SampleHistory from './sample-history';
import { displayValue } from './data-values';
import styles from './collection-detail.module.scss';

type Query = NonNullable<Parameters<typeof recordPage>[1]>;
const operators = { eq: '=', ne: '≠', gt: '>', gte: '≥', lt: '<', lte: '≤' };

export default function CollectionRecords({ collection }: { collection: UnsMetadata }) {
  const t = useTranslate();
  const { message } = App.useApp();
  const [field, setField] = useState<string>();
  const [operator, setOperator] = useState('eq');
  const [value, setValue] = useState('');
  const [invalid, setInvalid] = useState(false);
  const [failed, setFailed] = useState(false);
  const [query, setQuery] = useState<Pick<Query, 'filter' | 'sort'>>({});
  const [record, setRecord] = useState<UnsRecord>();
  const [history, setHistory] = useState<UnsRecord>();
  const properties = collection.data.recordSchema?.properties ?? {};
  const fields = [
    '_id',
    ...Object.keys(properties).filter(
      (key) =>
        key &&
        !/[.$\\\0]/.test(key) &&
        !['_id', '_meta', '__proto__', 'constructor', 'prototype'].includes(key) &&
        ['boolean', 'integer', 'number', 'string'].includes(properties[key].type)
    ),
  ];
  const numeric = field && ['integer', 'number'].includes(properties[field]?.type);
  const boolean = field && properties[field]?.type === 'boolean';
  const label = (key: string) => (key === '_id' ? 'ID' : properties[key]?.title || key);
  const { data, loading, pagination, setSearchParams, refreshRequest } = usePagination<UnsRecord>({
    fetchApi: async (
      { pageNo, pageSize, filter, sort }: Query & { pageNo: number; pageSize: number },
      { signal }: { signal: AbortSignal }
    ) => {
      try {
        const result = await recordPage(collection.id, { page: pageNo, size: pageSize, filter, sort }, signal);
        if (!signal.aborted) setFailed(false);
        return { data: result.list, total: result.total, pageNo };
      } catch (error) {
        if (!signal.aborted) setFailed(true);
        throw error;
      }
    },
  });
  const apply = (next: typeof query) => {
    setQuery(next);
    setSearchParams(next);
  };
  const filter = () => {
    const parsed = numeric ? Number(value) : boolean ? value === 'true' : value;
    if (
      numeric &&
      (!value.trim() ||
        !Number.isFinite(parsed) ||
        (properties[field!].type === 'integer' && !Number.isSafeInteger(parsed)))
    ) {
      setInvalid(true);
      return;
    }
    setInvalid(false);
    apply({ ...query, filter: field ? { [field]: { ['$' + operator]: parsed } } : undefined });
  };
  const names = [...new Set([...Object.keys(properties), ...data.flatMap((item) => Object.keys(item.data))])];
  const sortable = (key: string) => ({
    sorter: fields.includes(key),
    sortOrder:
      query.sort?.field === key ? (query.sort.direction === 1 ? ('ascend' as const) : ('descend' as const)) : null,
  });
  const columns: TableColumnsType<UnsRecord> = [
    {
      key: '_id',
      title: 'ID',
      fixed: 'left',
      width: 126,
      ...sortable('_id'),
      render: (_, row) => (
        <div className={styles.id}>
          <Button type="link" title={row.id} aria-label={row.id} onClick={() => setRecord(row)}>
            {row.id.length > 10 ? row.id.slice(0, 8) + '…' : row.id}
          </Button>
          <Button
            size="small"
            type="text"
            aria-label={t('common.copy')}
            icon={<Copy size={13} />}
            onClick={() =>
              copyToClipboard(row.id, (ok) =>
                ok ? message.success(t('common.copySuccess')) : message.error(t('common.copyFail'))
              )
            }
          />
        </div>
      ),
    },
    ...names.map((key) => ({
      key,
      title: (
        <span title={key}>
          {label(key)}
          {properties[key]?.unit && ' (' + properties[key].unit + ')'}
        </span>
      ),
      ...sortable(key),
      render: (_: unknown, row: UnsRecord) => <span className={styles.cell}>{displayValue(row.data[key])}</span>,
    })),
    ...(collection.data.sampleSchema
      ? [
          {
            key: 'history',
            title: t('common.history'),
            fixed: 'right' as const,
            width: 86,
            render: (_: unknown, row: UnsRecord) => (
              <Button type="text" size="small" icon={<RecentlyViewed size={14} />} onClick={() => setHistory(row)}>
                {t('common.view')}
              </Button>
            ),
          },
        ]
      : []),
  ];
  return (
    <div className={styles.records}>
      <form
        className={styles.query}
        onSubmit={(event) => {
          event.preventDefault();
          filter();
        }}
      >
        <Select
          aria-label={t('UnsMeta.field')}
          placeholder={t('common.all')}
          allowClear
          value={field}
          options={fields.map((key) => ({ value: key, label: label(key) }))}
          onChange={(key?: string) => {
            setField(key);
            setOperator('eq');
            setValue(key && properties[key]?.type === 'boolean' ? 'true' : '');
            setInvalid(false);
            if (!key) apply({ ...query, filter: undefined });
          }}
        />
        <Select
          aria-label={t('NamespaceView.operator')}
          disabled={!field}
          value={operator}
          onChange={setOperator}
          options={Object.entries(operators)
            .filter(([key]) => numeric || ['eq', 'ne'].includes(key))
            .map(([value, label]) => ({ value, label }))}
        />
        {boolean ? (
          <Select
            aria-label={t('NamespaceView.filterValue')}
            value={value}
            options={['true', 'false'].map((value) => ({ value, label: value }))}
            onChange={setValue}
          />
        ) : (
          <Input
            disabled={!field}
            aria-label={t('NamespaceView.filterValue')}
            placeholder={t('NamespaceView.filterValue')}
            status={invalid ? 'error' : undefined}
            value={value}
            onChange={(e) => {
              setValue(e.target.value);
              setInvalid(false);
            }}
          />
        )}
        <Button htmlType="submit" icon={<Search size={15} />} loading={loading}>
          {t('common.search')}
        </Button>
        <Button
          type="text"
          aria-label={t('common.refresh')}
          icon={<Renew size={16} />}
          onClick={() => refreshRequest()}
        />
      </form>
      {invalid && <Alert type="warning" message={t('NamespaceView.invalidValue')} />}
      {failed ? (
        <Alert
          type="error"
          message={t('UnsMeta.loadFailed')}
          action={<Button onClick={() => refreshRequest()}>{t('UnsMeta.retry')}</Button>}
        />
      ) : (
        <div className={styles.table}>
          <Table
            size="small"
            rowKey="id"
            loading={loading}
            dataSource={data}
            columns={columns}
            pagination={false}
            scroll={{ x: Math.max(470, names.length * 160 + 212), y: '100%' }}
            onChange={(_, __, sorter) => {
              const sort = Array.isArray(sorter) ? sorter[0] : sorter;
              apply({
                ...query,
                sort: sort?.order
                  ? { field: String(sort.columnKey), direction: sort.order === 'ascend' ? 1 : -1 }
                  : undefined,
              });
            }}
          />
        </div>
      )}
      {!failed && (
        <Pagination
          size="small"
          align="end"
          showSizeChanger
          showQuickJumper
          pageSizeOptions={[20, 50, 100]}
          current={pagination.page}
          pageSize={pagination.pageSize}
          total={pagination.total}
          showTotal={(count) => t('UnsMeta.recordCount', { count })}
          onChange={(page, pageSize) => pagination.onChange({ page, pageSize })}
        />
      )}
      <Modal open={!!record} footer={null} width={680} title={record?.id} onCancel={() => setRecord(undefined)}>
        {record && (
          <>
            <dl className={styles.values}>
              {Object.entries(record.data).map(([key, v]) => (
                <div key={key}>
                  <dt>{label(key)}</dt>
                  <dd>{displayValue(v)}</dd>
                </div>
              ))}
            </dl>
            {collection.data.sampleSchema && (
              <Button
                type="link"
                onClick={() => {
                  setHistory(record);
                  setRecord(undefined);
                }}
              >
                {t('common.history')}
              </Button>
            )}
            <details>
              <summary>{t('UnsMeta.rawData')}</summary>
              <pre className={styles.json}>{JSON.stringify(record, null, 2)}</pre>
            </details>
          </>
        )}
      </Modal>
      <Drawer open={!!history} width={740} title={history?.id} onClose={() => setHistory(undefined)}>
        {history && (
          <SampleHistory
            key={history.id}
            compact
            collection={collection}
            collectionId={collection.id}
            recordId={history.id}
          />
        )}
      </Drawer>
    </div>
  );
}
