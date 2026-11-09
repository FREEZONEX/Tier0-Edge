import { useEffect, useState } from 'react';
import { Alert, Empty, Input, Pagination, Spin, Tag } from 'antd';
import { Add, DataBase, Renew } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import { ButtonPermission } from '@/common-types/button-permission';
import { usePagination, useTranslate } from '@/hooks';
import { loadMetadata, type UnsMetadata } from '@/apis/core-api/uns-metadata';
import { unsV2Api } from '@/apis/core-api/uns-v2';
import MetadataEditor from '@/pages/uns/components/metadata/metadata-editor';
import useConnections from '@/pages/uns/components/metadata/use-connections';
import styles from '../index.module.scss';
import { StoragePolicyButton } from './storage-policy';

export default function CollectionNavigator({
  selectedId,
  revision,
  onSelect,
  readOnly = false,
}: {
  selectedId: string;
  revision: number;
  onSelect: (id: string, replace?: boolean) => void;
  readOnly?: boolean;
}) {
  const t = useTranslate();
  const [create, setCreate] = useState(false);
  const [opening, setOpening] = useState(false);
  const [failed, setFailed] = useState(false);
  const [items, setItems] = useState<UnsMetadata[]>([]);
  const connections = useConnections(revision, create);
  const { data, loading, pagination, setSearchParams, refreshRequest } = usePagination<UnsMetadata>({
    fetchApi: async (
      { pageNo, pageSize, search }: { pageNo: number; pageSize: number; search?: string },
      { signal }: { signal: AbortSignal }
    ) => {
      try {
        const response = await unsV2Api.collectionList({ page: pageNo, size: pageSize, search }, signal);
        if (!signal.aborted) setFailed(false);
        return { data: response.list, total: response.total, pageNo };
      } catch (error) {
        if (!signal.aborted) setFailed(true);
        throw error;
      }
    },
  });
  useEffect(() => {
    if (!selectedId && !failed && data.length) onSelect(data[0].id, true);
  }, [data, selectedId, failed, onSelect]);
  useEffect(() => {
    if (revision) refreshRequest();
    // Only collection edits trigger a list refresh; the pagination hook callback is not stable.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [revision]);
  const openCreate = async () => {
    setOpening(true);
    try {
      setItems(await loadMetadata());
      setCreate(true);
    } finally {
      setOpening(false);
    }
  };
  return (
    <aside className={styles.navigator} aria-label={t('Collections.title')}>
      <div className={styles['navigator-heading']}>
        <h2>{t('Collections.title')}</h2>
        <div className={styles['navigator-actions']}>
          <ComButton type="text" icon={<Renew />} onClick={() => refreshRequest()} aria-label={t('UnsMeta.refresh')} />
          {!readOnly && (
            <ComButton
              type="primary"
              icon={<Add />}
              loading={opening}
              auth={ButtonPermission['uns.metadataWrite']}
              onClick={openCreate}
              aria-label={t('Collections.create')}
            />
          )}
        </div>
      </div>
      <StoragePolicyButton />
      <Input.Search
        allowClear
        placeholder={t('Collections.search')}
        onSearch={(search) => setSearchParams({ search })}
      />
      <div className={styles['collection-list']}>
        <Spin spinning={loading}>
          {failed ? (
            <Alert
              type="error"
              message={t('UnsMeta.loadFailed')}
              action={<ComButton onClick={() => refreshRequest()}>{t('UnsMeta.refresh')}</ComButton>}
            />
          ) : data.length ? (
            data.map((row) => (
              <button
                type="button"
                key={row.id}
                className={`${styles['collection-item']} ${selectedId === row.id ? styles.active : ''}`}
                aria-current={selectedId === row.id ? 'true' : undefined}
                onClick={() => onSelect(row.id)}
              >
                <DataBase size={20} />
                <span className={styles['collection-copy']}>
                  <strong title={row.data.name}>{row.data.name}</strong>
                  <span className={styles.muted} title={row.id}>
                    {row.id}
                  </span>
                  <Tag>{t(row.data.sampleSchema ? 'Collections.withSamples' : 'Collections.recordsOnly')}</Tag>
                  {row.originNodeId && <Tag title={row.originNodeId}>Edge · {row.originNodeId.slice(-8)}</Tag>}
                </span>
              </button>
            ))
          ) : (
            !loading && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />
          )}
        </Spin>
      </div>
      <Pagination
        size="small"
        simple
        current={pagination.page}
        pageSize={pagination.pageSize}
        total={pagination.total}
        showSizeChanger={false}
        hideOnSinglePage
        onChange={(page) => pagination.onChange(page)}
      />
      {create && (
        <MetadataEditor
          kind="collection"
          standalone
          items={items}
          connections={connections}
          onClose={() => setCreate(false)}
          onSaved={(row) => {
            setCreate(false);
            refreshRequest();
            onSelect(row.id);
          }}
        />
      )}
    </aside>
  );
}
