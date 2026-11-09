import { useEffect, useState } from 'react';
import { Alert, Collapse, Empty, Input, Pagination, Radio, Spin } from 'antd';
import { useTranslate } from '@/hooks';
import ComButton from '@/components/com-button';
import { metadataApi, type UnsMetadata, type UnsRecord } from '@/apis/core-api/uns-metadata';
import { recordName } from './data-values';
import { json } from './schema-editor';
import styles from './topic-composer.module.scss';

export default function TopicRecordPicker({
  collection,
  value,
  onChange,
}: {
  collection: UnsMetadata;
  value?: UnsRecord;
  onChange: (record: UnsRecord) => void;
}) {
  const t = useTranslate();
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const [result, setResult] = useState<{ list: UnsRecord[]; total: number }>({ list: [], total: 0 });
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setFailed(false);
    const timer = setTimeout(() => {
      metadataApi
        .records(collection.id, { search, page, size: 10 }, controller.signal)
        .then((data) => {
          if (!controller.signal.aborted) setResult(data);
        })
        .catch(() => {
          if (!controller.signal.aborted) setFailed(true);
        })
        .finally(() => {
          if (!controller.signal.aborted) setLoading(false);
        });
    }, 200);
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [collection.id, search, page, retry]);
  return (
    <section className={styles['record-picker']}>
      <strong>{collection.data.name}</strong>
      <Input.Search
        aria-label={t('TopicCompose.searchRecords')}
        placeholder={t('TopicCompose.searchRecords')}
        value={search}
        onChange={(event) => {
          setSearch(event.target.value);
          setPage(1);
        }}
      />
      {value && (
        <div>
          {t('TopicCompose.selectedRecord')}: {recordName(value)} · {value.id}
          <Collapse
            items={[
              {
                key: 'record',
                label: t('UnsUi.recordData'),
                children: <pre className={styles.preview}>{json(value.data)}</pre>,
              },
            ]}
          />
        </div>
      )}
      {failed ? (
        <Alert
          type="error"
          message={t('UnsMeta.loadFailed')}
          action={<ComButton onClick={() => setRetry(retry + 1)}>{t('UnsMeta.refresh')}</ComButton>}
        />
      ) : (
        <Spin spinning={loading}>
          <Radio.Group
            value={value?.id}
            className={styles.records}
            disabled={loading}
            onChange={(event) => {
              const record = result.list.find((item) => item.id === event.target.value);
              if (record) onChange(record);
            }}
          >
            {result.list.map((record) => (
              <Radio key={record.id} value={record.id}>
                {recordName(record)} <small>{record.id}</small>
              </Radio>
            ))}
          </Radio.Group>
          {!loading && !result.list.length && (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('UnsMeta.noPreviewRecords')} />
          )}
          <Pagination
            size="small"
            current={page}
            pageSize={10}
            total={result.total}
            showSizeChanger={false}
            hideOnSinglePage
            onChange={setPage}
          />
        </Spin>
      )}
    </section>
  );
}
