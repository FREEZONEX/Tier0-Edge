import { useEffect, useState } from 'react';
import { Alert, Tag } from 'antd';
import { Renew } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import ProModal from '@/components/pro-modal';
import ProTable from '@/components/pro-table';
import { useTranslate } from '@/hooks';
import { metadataApi, type UnsMetadata, type ResolvedField } from '@/apis/core-api/uns-metadata';
import { displayTime, displayValue, SampleHistory } from './data-panel';
import styles from './index.module.scss';
export default function TopicView({
  meta,
  items,
  onSelect,
  api = metadataApi,
}: {
  meta: UnsMetadata;
  items: UnsMetadata[];
  onSelect: (id: string) => void;
  api?: Pick<typeof metadataApi, 'view' | 'samples'>;
}) {
  const t = useTranslate();
  const [fields, setFields] = useState<ResolvedField[]>([]);
  const [failed, setFailed] = useState(false);
  const [loading, setLoading] = useState(true);
  const [refresh, setRefresh] = useState(0);
  const [history, setHistory] = useState<ResolvedField>();
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    api
      .view(meta.id, controller.signal)
      .then((result) => {
        if (!controller.signal.aborted) {
          setFields(result.fields);
          setFailed(false);
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) setFailed(true);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [meta.id, meta.revision, refresh, api]);
  const collectionName = (id: string) => items.find((item) => item.id === id)?.data.name || id;
  return (
    <>
      <div className={styles.toolbar}>
        <p className={styles.hint}>{t('UnsMeta.topicHint')}</p>
        <ComButton icon={<Renew />} onClick={() => setRefresh((value) => value + 1)}>
          {t('UnsMeta.refresh')}
        </ComButton>
      </div>
      {failed && <Alert type="error" message={t('UnsMeta.loadFailed')} />}
      <ProTable
        rowKey={(row) => row.binding.name}
        columnFit={false}
        resizeable={false}
        dataSource={fields}
        loading={loading}
        pagination={false}
        scroll={{ x: 950 }}
        columns={[
          {
            title: t('UnsMeta.field'),
            width: 160,
            render: (_, row: ResolvedField) => (
              <>
                <strong>{row.binding.displayName || row.binding.name}</strong>
                <div className={styles.hint}>{row.binding.name}</div>
              </>
            ),
          },
          {
            title: t('UnsMeta.value'),
            width: 130,
            render: (_, row: ResolvedField) =>
              row.status === 'missing' ? <Tag>{t('UnsMeta.noValue')}</Tag> : <code>{displayValue(row.value)}</code>,
          },
          {
            title: t('UnsMeta.origin'),
            width: 250,
            render: (_, row: ResolvedField) => (
              <>
                <ComButton
                  className={styles['origin-link']}
                  type="link"
                  onClick={() => onSelect(row.binding.collectionId)}
                >
                  {collectionName(row.binding.collectionId)}
                </ComButton>
                <div className={styles.hint}>
                  {t(row.binding.kind === 'sample' ? 'UnsUi.sampleData' : 'UnsUi.recordData')} · {row.binding.recordId}{' '}
                  · {row.binding.fieldPath}
                </div>
              </>
            ),
          },
          { title: t('UnsMeta.updatedAt'), width: 215, render: (_, row: ResolvedField) => displayTime(row.time) },
          { title: t('UnsMeta.quality'), width: 90, render: (_, row: ResolvedField) => row.quality || '—' },
          {
            title: '',
            width: 90,
            render: (_, row: ResolvedField) =>
              row.binding.kind === 'sample' && (
                <ComButton type="link" onClick={() => setHistory(row)}>
                  {t('UnsMeta.history')}
                </ComButton>
              ),
          },
        ]}
      />
      {history && (
        <ProModal
          open
          width={1100}
          title={`${t('UnsMeta.history')} · ${history.binding.recordId}`}
          footer={null}
          onCancel={() => setHistory(undefined)}
        >
          <SampleHistory
            key={`${history.binding.collectionId}/${history.binding.recordId}`}
            collectionId={history.binding.collectionId}
            recordId={history.binding.recordId}
            collection={items.find((item) => item.id === history.binding.collectionId && item.kind === 'collection')}
            readSamples={api.samples}
          />
        </ProModal>
      )}
    </>
  );
}
