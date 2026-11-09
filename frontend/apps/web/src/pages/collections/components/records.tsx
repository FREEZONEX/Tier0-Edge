import { useState } from 'react';
import { App, Alert, Input, Space } from 'antd';
import { Add, Edit, Renew, TrashCan } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import ProTable from '@/components/pro-table';
import { openConfirmModal } from '@/components/confirm-modal';
import { ButtonPermission } from '@/common-types/button-permission';
import { usePagination, useTranslate } from '@/hooks';
import { metadataApi, type UnsMetadata, type UnsRecord, type UnsSample } from '@/apis/core-api/uns-metadata';
import { latestSamples } from '@/apis/core-api/uns-v2';
import { DataValue, displayTime, recordName } from '@/pages/uns/components/metadata/data-values';
import RecordEditor from './record-editor';
import RecordDetail from './record-detail';
import styles from '../index.module.scss';
type PageQuery = { pageNo: number; pageSize: number; search?: string };
type Latest = Record<string, UnsSample | null>;
export default function Records({ collection, readOnly = false }: { collection: UnsMetadata; readOnly?: boolean }) {
  const t = useTranslate();
  const { modal, message } = App.useApp();
  const [latest, setLatest] = useState<Latest>({});
  const [selectedId, setSelectedId] = useState<string>();
  const [detailRevision, setDetailRevision] = useState(0);
  const [editor, setEditor] = useState<{ record?: UnsRecord }>();
  const [failed, setFailed] = useState(false);
  const { data, loading, pagination, setSearchParams, refreshRequest } = usePagination<UnsRecord>({
    fetchApi: async ({ pageNo, pageSize, search }: PageQuery, { signal }: { signal: AbortSignal }) => {
      try {
        const records = await metadataApi.records(collection.id, { page: pageNo, size: pageSize, search }, signal);
        const latest = collection.data.sampleSchema
          ? await latestSamples(
              collection.id,
              records.list.map((record) => record.id),
              signal
            )
          : [];
        if (!signal.aborted) {
          setLatest(Object.fromEntries(latest.map((entry) => [entry.recordId, entry.sample])));
          setFailed(false);
        }
        return { data: records.list, total: records.total, pageNo };
      } catch (error) {
        if (!signal.aborted) setFailed(true);
        throw error;
      }
    },
  });
  const selected = failed ? undefined : data.find((row) => row.id === selectedId);
  const refresh = () => {
    setDetailRevision((v) => v + 1);
    refreshRequest();
  };
  const remove = (record: UnsRecord) =>
    openConfirmModal(modal, {
      danger: true,
      title: t('common.delete'),
      content: record.id,
      onOk: async () => {
        await metadataApi.deleteRecord(record);
        message.success(t('UnsMeta.saved'));
        refresh();
      },
    });
  const columns = [
    {
      title: t('UnsMeta.records'),
      key: 'identity',
      width: 220,
      render: (_: unknown, row: UnsRecord) => (
        <button type="button" className={styles['record-link']} onClick={() => setSelectedId(row.id)}>
          <strong>{recordName(row)}</strong>
          <span className={styles.muted}>{row.id}</span>
        </button>
      ),
    },
    ...Object.entries(collection.data.recordSchema?.properties || {}).map(([name, schema]) => ({
      title: schema.title || name,
      key: name,
      width: 160,
      render: (_: unknown, row: UnsRecord) => <DataValue value={row.data[name]} unit={schema.unit} />,
    })),
    ...(collection.data.sampleSchema
      ? [
          {
            title: t('Collections.latestSample'),
            key: 'latest',
            width: 220,
            render: (_: unknown, row: UnsRecord) => {
              const sample = latest[row.id];
              return sample ? (
                <div>
                  {Object.entries(sample.data)
                    .slice(0, 3)
                    .map(([name, value]) => (
                      <div key={name} className={styles['sample-preview']}>
                        <span className={styles.muted}>
                          {collection.data.sampleSchema?.properties?.[name]?.title || name}
                        </span>
                        <DataValue value={value} unit={collection.data.sampleSchema?.properties?.[name]?.unit} />
                      </div>
                    ))}
                  {Object.keys(sample.data).length > 3 && <span className={styles.muted}>…</span>}
                  <small className={styles.muted}>{displayTime(sample.time)}</small>
                </div>
              ) : (
                <span className={styles.muted}>{t('Collections.noSamples')}</span>
              );
            },
          },
        ]
      : []),
    {
      title: t('UnsMeta.updatedAt'),
      key: 'updated',
      width: 210,
      render: (_: unknown, row: UnsRecord) => displayTime(row.updatedAt),
    },
    {
      title: t('common.operation'),
      key: 'operations',
      width: 108,
      render: (_: unknown, row: UnsRecord) => (
        <Space onClick={(event) => event.stopPropagation()}>
          <ComButton
            type="text"
            icon={<Edit />}
            disabled={!!row.sourceId}
            auth={ButtonPermission['uns.metadataWrite']}
            onClick={() => setEditor({ record: row })}
            aria-label={t('common.edit')}
          />
          <ComButton
            type="text"
            danger
            icon={<TrashCan />}
            disabled={!!row.sourceId}
            auth={ButtonPermission['uns.metadataDelete']}
            onClick={() => remove(row)}
            aria-label={t('common.delete')}
          />
        </Space>
      ),
    },
  ];
  return (
    <>
      <div className={styles.toolbar}>
        <Input.Search
          allowClear
          placeholder={t('UnsMeta.searchRecord')}
          onSearch={(search) => setSearchParams({ search })}
        />
        <Space>
          <ComButton icon={<Renew />} onClick={refresh} aria-label={t('UnsMeta.refresh')} />
          {!readOnly && (
            <ComButton icon={<Add />} auth={ButtonPermission['uns.metadataWrite']} onClick={() => setEditor({})}>
              {t('Collections.addRecord')}
            </ComButton>
          )}
        </Space>
      </div>
      {failed && <Alert type="error" message={t('UnsMeta.loadFailed')} />}
      <ProTable
        rowKey="id"
        expandable={
          readOnly
            ? {
                expandedRowRender: (row) => <pre className={styles.code}>{JSON.stringify(row, null, 2)}</pre>,
              }
            : undefined
        }
        dataSource={failed ? [] : data}
        size="small"
        columnFit={false}
        scroll={{ x: columns.reduce((total, column) => total + column.width, 0), y: selected ? 290 : 480 }}
        rowClassName={(row) => (row.id === selectedId ? styles['selected-row'] : '')}
        loading={loading}
        pagination={{
          current: pagination.page,
          pageSize: pagination.pageSize,
          total: pagination.total,
          onChange: (page, pageSize) => {
            setSelectedId(undefined);
            pagination.onChange({ page, pageSize });
          },
          showSizeChanger: true,
        }}
        columns={readOnly ? columns.filter((column) => column.key !== 'operations') : columns}
        onRow={(row) => ({ onClick: () => setSelectedId(row.id) })}
      />
      {selected && (
        <RecordDetail
          key={selected.id + '/' + detailRevision}
          record={selected}
          collection={collection}
          onClose={() => setSelectedId(undefined)}
        />
      )}
      {editor && (
        <RecordEditor
          collection={collection}
          record={editor.record}
          onClose={() => setEditor(undefined)}
          onSaved={() => {
            setEditor(undefined);
            refresh();
          }}
        />
      )}
    </>
  );
}
