import PayloadViewSegmented, { type PayloadViewMode } from '../topic-detail/PayloadViewSegmented';
import { useState } from 'react';
import { Alert, Input, Space, Spin, Tabs } from 'antd';
import { ArrowLeft, Renew } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import ProTable from '@/components/pro-table';
import { usePagination, useTranslate } from '@/hooks';
import { metadataApi, type MetaSchema, type UnsMetadata, type UnsRecord } from '@/apis/core-api/uns-metadata';
import { json } from './schema-editor';
import styles from './index.module.scss';
import { DataValue, displayTime, recordName } from './data-values';
import SampleHistory from './sample-history';
import DataSection from './data-section';
import Payload from '../topic-detail/Payload';
import RawData from '../topic-detail/RawData';
import DefinitionTypeTag from '../topic-detail/DefinitionTypeTag';
import definitionStyles from '../topic-detail/Definition.module.scss';

export { displayValue, displayTime } from './data-values';
export { default as SampleHistory } from './sample-history';
type PageQuery = { pageNo: number; pageSize: number; search?: string; startTime?: number; endTime?: number };

export default function DataPanel({
  collection,
  api = metadataApi,
  tableMode = false,
}: {
  collection: UnsMetadata;
  api?: Pick<typeof metadataApi, 'records' | 'samples'>;
  tableMode?: boolean;
}) {
  const t = useTranslate();
  const [failed, setFailed] = useState(false);
  const [selectedId, setSelectedId] = useState<string>();
  const [showList, setShowList] = useState(tableMode);
  const [payloadView, setPayloadView] = useState<PayloadViewMode>('code');
  const [search, setSearch] = useState('');
  const { data, loading, pagination, setSearchParams, refreshRequest } = usePagination<UnsRecord>({
    fetchApi: async ({ pageNo, pageSize, search }: PageQuery, { signal }: { signal: AbortSignal }) => {
      try {
        const result = await api.records(collection.id, { page: pageNo, size: pageSize, search }, signal);
        if (!signal.aborted) setFailed(false);
        return { data: result.list, total: result.total, pageNo: result.page };
      } catch (error) {
        if (!signal.aborted) setFailed(true);
        throw error;
      }
    },
  });
  const autoRecord = !showList && !search && pagination.total === 1 ? data[0] : undefined;
  const selected = data.find((record) => record.id === selectedId) || autoRecord;
  const openRecord = (record: UnsRecord) => {
    setSelectedId(record.id);
    setShowList(false);
  };
  return (
    <>
      <DataSection
        compact={tableMode}
        title={t('UnsUi.payload')}
        extra={
          <Space size={4} wrap onClick={(event) => event.stopPropagation()}>
            {selected && (
              <PayloadViewSegmented
                value={payloadView}
                onChange={setPayloadView}
                tableTitle={t('common.table')}
                codeTitle={t('UnsMeta.rawData')}
              />
            )}
            <ComButton
              type="text"
              icon={<Renew />}
              aria-label={t('UnsMeta.refresh')}
              title={t('UnsMeta.refresh')}
              loading={loading}
              onClick={() => refreshRequest()}
            />
          </Space>
        }
      >
        {failed && <Alert type="error" message={t('UnsMeta.loadFailed')} />}
        {selected ? (
          <>
            {(pagination.total > 1 || tableMode) && (
              <ComButton
                type="text"
                icon={<ArrowLeft />}
                onClick={() => {
                  setSelectedId(undefined);
                  setShowList(true);
                }}
              >
                {t('UnsMeta.backToRecords')}
              </ComButton>
            )}
            {pagination.total > 1 && <div className={styles['record-id']}>{recordName(selected)}</div>}
            {payloadView === 'code' ? (
              <RawData payload={selected.data} className="payload-code-view" />
            ) : (
              <Payload
                websocketData={{
                  data: selected.data,
                  dt: Object.fromEntries(Object.keys(selected.data).map((key) => [key, selected.updatedAt])),
                }}
                fields={[]}
              />
            )}
            {collection.data.sampleSchema && (
              <SampleHistory
                key={selected.id}
                collectionId={collection.id}
                recordId={selected.id}
                collection={collection}
                readSamples={api.samples}
              />
            )}
          </>
        ) : !tableMode && !search && data.length === 0 && !failed ? (
          <Spin spinning={loading}>
            <RawData className="payload-code-view" />
          </Spin>
        ) : (
          <>
            <div className={styles.toolbar}>
              <Input.Search
                allowClear
                placeholder={t('UnsMeta.searchRecord')}
                onSearch={(value) => {
                  setSearch(value);
                  setSearchParams({ search: value });
                }}
              />
            </div>
            <ProTable
              rowKey="id"
              columnFit={false}
              dataSource={failed ? [] : data}
              loading={loading}
              pagination={{
                current: pagination.page,
                pageSize: pagination.pageSize,
                total: pagination.total,
                onChange: (page, pageSize) => pagination.onChange({ page, pageSize }),
                showSizeChanger: true,
              }}
              scroll={{ x: 765 }}
              onRow={tableMode ? (record: UnsRecord) => ({ onClick: () => openRecord(record) }) : undefined}
              columns={
                tableMode
                  ? [
                      {
                        title: t('UnsMeta.recordNameId'),
                        width: 150,
                        render: (_, record: UnsRecord) => (
                          <ComButton type="text" onClick={() => openRecord(record)}>
                            {record.id}
                          </ComButton>
                        ),
                      },
                      ...Array.from(
                        new Set([
                          ...Object.keys(collection.data.recordSchema?.properties || {}),
                          ...data.flatMap((record) => Object.keys(record.data)),
                        ])
                      ).map((key) => ({
                        key,
                        title: collection.data.recordSchema?.properties?.[key]?.title || key,
                        width: 170,
                        render: (_: unknown, record: UnsRecord) => (
                          <DataValue
                            value={record.data[key]}
                            unit={collection.data.recordSchema?.properties?.[key]?.unit}
                          />
                        ),
                      })),
                      { title: t('UnsMeta.updatedAt'), dataIndex: 'updatedAt', width: 180, render: displayTime },
                    ]
                  : [
                      {
                        title: t('UnsMeta.recordNameId'),
                        width: 230,
                        render: (_, record: UnsRecord) => (
                          <div>
                            <ComButton className={styles['origin-link']} type="link" onClick={() => openRecord(record)}>
                              {recordName(record)}
                            </ComButton>
                            <small className={styles['record-id']}>{record.id}</small>
                          </div>
                        ),
                      },
                      {
                        title: t('UnsMeta.recordSummary'),
                        width: 320,
                        render: (_, record: UnsRecord) => (
                          <div className={styles['record-summary']}>
                            {Object.entries(record.data)
                              .filter(([key]) => !['name', 'displayName', 'title'].includes(key))
                              .slice(0, 3)
                              .map(([name, value]) => (
                                <div key={name}>
                                  <span className={styles.hint}>
                                    {collection.data.recordSchema?.properties?.[name]?.title || name}
                                  </span>
                                  <DataValue
                                    value={value}
                                    unit={collection.data.recordSchema?.properties?.[name]?.unit}
                                  />
                                </div>
                              ))}
                          </div>
                        ),
                      },
                      { title: t('UnsMeta.updatedAt'), dataIndex: 'updatedAt', width: 215, render: displayTime },
                    ]
              }
            />
          </>
        )}
      </DataSection>
    </>
  );
}

export function SchemaView({ collection }: { collection: UnsMetadata }) {
  const t = useTranslate();
  const renderSchema = (schema?: MetaSchema) => (
    <>
      <ProTable
        rowKey="name"
        bordered
        size="middle"
        resizeable={false}
        className={`${definitionStyles.schemaTable} ${styles['schema-table']}`}
        pagination={false}
        columnFit={false}
        scroll={{ x: 520 }}
        dataSource={Object.entries(schema?.properties || {}).map(([name, field]) => ({ name, ...field }))}
        columns={[
          {
            title: t('uns.key'),
            dataIndex: 'name',
            ellipsis: true,
            render: (name) => (
              <span className={definitionStyles.schemaKeyCell} title={name}>
                {name}
              </span>
            ),
          },
          { title: t('UnsMeta.name'), dataIndex: 'title', render: (value) => value || '—' },
          {
            title: t('UnsMeta.type'),
            dataIndex: 'type',
            width: 128,
            render: (type) => <DefinitionTypeTag type={type} />,
          },
          {
            title: t('UnsMeta.unit'),
            dataIndex: 'unit',
            width: 96,
            render: (unit) => <span className={definitionStyles.schemaUnitCell}>{unit || ''}</span>,
          },
          {
            title: t('UnsMeta.required'),
            width: 100,
            render: (_, field) =>
              t(schema?.required?.includes(field.name) ? 'UnsMeta.trueValue' : 'UnsMeta.falseValue'),
          },
          { title: t('UnsMeta.description'), dataIndex: 'description', render: (value) => value || '—' },
        ]}
      />
      <details className={styles['raw-section']}>
        <summary>{t('UnsMeta.schemaJson')}</summary>
        <RawData payload={json(schema)} className="payload-code-view" />
      </details>
    </>
  );
  if (!collection.data.sampleSchema) return renderSchema(collection.data.recordSchema);
  return (
    <Tabs
      items={[
        {
          key: 'record',
          label: t('UnsUi.recordData'),
          children: renderSchema(collection.data.recordSchema),
        },
        ...(collection.data.sampleSchema
          ? [
              {
                key: 'sample',
                label: t('UnsUi.sampleData'),
                children: renderSchema(collection.data.sampleSchema),
              },
            ]
          : []),
      ]}
    />
  );
}
