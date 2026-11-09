import { useEffect, useState } from 'react';
import { Alert, Empty, Spin, Tag } from 'antd';
import { useTranslate } from '@/hooks';
import { metadataApi, type UnsMetadata, type UnsRecord } from '@/apis/core-api/uns-metadata';
import { DataValue } from './data-values';
import styles from './index.module.scss';

export default function CollectionPreview({
  collection,
  readRecords = metadataApi.records,
}: {
  collection: UnsMetadata;
  readRecords?: typeof metadataApi.records;
}) {
  const t = useTranslate();
  const [result, setResult] = useState<{ id: string; record?: UnsRecord; failed?: boolean }>();
  useEffect(() => {
    const controller = new AbortController();
    readRecords(collection.id, { page: 1, size: 1 }, controller.signal)
      .then((page) => {
        if (!controller.signal.aborted) setResult({ id: collection.id, record: page.list[0] });
      })
      .catch(() => {
        if (!controller.signal.aborted) setResult({ id: collection.id, failed: true });
      });
    return () => controller.abort();
  }, [collection.id, readRecords]);
  return (
    <section className={styles['collection-preview']}>
      <strong>{t('UnsMeta.collectionPreview')}</strong>
      <div className={styles['preview-fields']}>
        {Object.entries(collection.data.recordSchema?.properties || {}).map(([name, schema]) => (
          <Tag key={name}>
            {name} · {schema.type}
            {schema.unit ? ` · ${schema.unit}` : ''}
          </Tag>
        ))}
      </div>
      <Spin spinning={result?.id !== collection.id}>
        {result?.id === collection.id &&
          (result.failed ? (
            <Alert type="warning" message={t('UnsMeta.previewFailed')} />
          ) : result.record ? (
            <div className={styles['record-summary']}>
              {Object.entries(result.record.data).map(([name, value]) => (
                <div key={name}>
                  <span className={styles.hint}>{collection.data.recordSchema?.properties?.[name]?.title || name}</span>
                  <DataValue value={value} unit={collection.data.recordSchema?.properties?.[name]?.unit} />
                </div>
              ))}
            </div>
          ) : (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('UnsMeta.noPreviewRecords')} />
          ))}
      </Spin>
    </section>
  );
}
