import { useCallback, useEffect, useState } from 'react';
import { Alert, Empty, Input, Select, Spin, Tag } from 'antd';
import { Renew } from '@carbon/icons-react';
import { useSearchParams } from 'react-router';
import { usePagination, useTranslate } from '@/hooks';
import { metadataPage, unsV2Api } from '@/apis/core-api/uns-v2';
import type { UnsMetadata } from '@/apis/core-api/uns-metadata';
import ComButton from '@/components/com-button';
import ProTable from '@/components/pro-table';
import HelpTooltip from '@/components/help-tooltip';
import CollectionNavigator from '@/pages/collections/components/collection-navigator';
import Records from '@/pages/collections/components/records';
import Samples from '@/pages/collections/components/samples';
import { useBaseStore } from '@/stores/base';
import { metadataRowForDisplay, readableMetadataKinds, type MetadataRow } from './metadata-browser-model';
import styles from './data-browser.module.scss';

export function MetadataBrowser() {
  const t = useTranslate();
  const resourceKeys = useBaseStore((state) => state.currentUserInfo?.resourceKeys);
  const kinds = readableMetadataKinds(Array.isArray(resourceKeys) ? resourceKeys : []);
  if (!kinds.length) return <Empty description={t('SourceStream.forbidden')} />;
  // 权限变化时重置选择与分页，并卸载旧表格以取消在途请求。
  return <MetadataTable key={kinds.join(',')} kinds={kinds} />;
}

function MetadataTable({ kinds }: { kinds: string[] }) {
  const t = useTranslate();
  const [failed, setFailed] = useState(false);
  const [selectedKind, setSelectedKind] = useState(kinds[0]);
  const { data, loading, pagination, setSearchParams, refreshRequest } = usePagination<MetadataRow>({
    defaultParams: { kind: kinds[0] },
    fetchApi: async (
      { pageNo, pageSize, search, kind }: { pageNo: number; pageSize: number; search?: string; kind?: string },
      { signal }: { signal: AbortSignal }
    ) => {
      try {
        const response = await metadataPage<MetadataRow>(
          [kind || kinds[0]],
          { search, page: pageNo, size: pageSize },
          signal
        );
        if (!signal.aborted) setFailed(false);
        return { data: response.list.map(metadataRowForDisplay), total: response.total, pageNo };
      } catch (error) {
        if (!signal.aborted) setFailed(true);
        throw error;
      }
    },
  });
  return (
    <section className={styles.content}>
      <div className={styles.toolbar}>
        <Input.Search
          allowClear
          placeholder={t('DataBrowser.searchMeta')}
          onSearch={(search) => setSearchParams({ search }, false)}
        />
        <Select
          value={selectedKind}
          placeholder={t('DataBrowser.kind')}
          aria-label={t('DataBrowser.kind')}
          options={kinds.map((kind) => ({ value: kind, label: kind }))}
          onChange={(kind) => {
            setSelectedKind(kind);
            setSearchParams({ kind }, false);
          }}
        />
        <ComButton icon={<Renew />} aria-label={t('UnsMeta.refresh')} onClick={() => refreshRequest()} />
        <HelpTooltip title={t('DataBrowser.safeMeta')} />
      </div>
      {failed && <Alert type="error" message={t('UnsMeta.loadFailed')} />}
      <ProTable
        rowKey={(row) => `${row.kind}:${row.id}`}
        dataSource={failed ? [] : data}
        loading={loading}
        pagination={pagination}
        rowNavigationIndicator={false}
        columnFit={false}
        scroll={{ x: 960 }}
        expandable={{ expandedRowRender: (row) => <pre className={styles.json}>{JSON.stringify(row, null, 2)}</pre> }}
        columns={[
          {
            title: t('UnsMeta.name'),
            key: 'name',
            width: 220,
            render: (_: unknown, row: MetadataRow) => (typeof row.data.name === 'string' ? row.data.name : row.id),
          },
          { title: t('DataBrowser.id'), dataIndex: 'id', ellipsis: true, width: 280 },
          { title: t('DataBrowser.kind'), dataIndex: 'kind', width: 130, render: (kind: string) => <Tag>{kind}</Tag> },
          { title: t('DataBrowser.revision'), dataIndex: 'revision', width: 110 },
          {
            title: t('UnsMeta.updatedAt'),
            dataIndex: 'updatedAt',
            width: 200,
            render: (value: number) => (value ? new Date(value).toLocaleString() : '—'),
          },
        ]}
      />
    </section>
  );
}

export function CollectionDataBrowser({ mode }: { mode: 'record' | 'sample' }) {
  const t = useTranslate();
  const [params, setParams] = useSearchParams();
  const collectionId = params.get('collectionId') || '';
  const [collection, setCollection] = useState<UnsMetadata>();
  const [failed, setFailed] = useState(false);
  const [loading, setLoading] = useState(false);
  const [revision, setRevision] = useState(0);
  const select = useCallback(
    (id: string, replace = false) => {
      setParams(
        (old) => {
          const next = new URLSearchParams(old);
          next.set('collectionId', id);
          return next;
        },
        { replace }
      );
    },
    [setParams]
  );
  useEffect(() => {
    const controller = new AbortController();
    setCollection(undefined);
    setFailed(false);
    setLoading(!!collectionId);
    if (collectionId)
      unsV2Api
        .collectionGet(collectionId, controller.signal)
        .then((result) => {
          if (!controller.signal.aborted) {
            setCollection(result);
            setFailed(!result);
          }
        })
        .catch(() => {
          if (!controller.signal.aborted) setFailed(true);
        })
        .finally(() => {
          if (!controller.signal.aborted) setLoading(false);
        });
    return () => controller.abort();
  }, [collectionId, revision]);
  return (
    <div className={styles['collection-workspace']}>
      <CollectionNavigator selectedId={collectionId} revision={0} onSelect={select} readOnly />
      <div className={styles['collection-content']}>
        <Spin spinning={loading}>
          {failed ? (
            <Alert
              type="error"
              message={t('UnsMeta.loadFailed')}
              action={<ComButton onClick={() => setRevision((value) => value + 1)}>{t('UnsMeta.refresh')}</ComButton>}
            />
          ) : collection ? (
            <>
              <div className={styles['collection-heading']}>
                <h3>{collection.data.name}</h3>
                <code>{collection.id}</code>
              </div>
              {mode === 'record' ? (
                <Records key={collection.id} collection={collection} readOnly />
              ) : collection.data.sampleSchema ? (
                <Samples key={collection.id} collection={collection} inspect />
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('DataBrowser.noSampleSchema')} />
              )}
            </>
          ) : (
            !loading && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('DataBrowser.chooseCollection')} />
          )}
        </Spin>
      </div>
    </div>
  );
}
