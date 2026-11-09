import { Collapse, Tabs, Tag } from 'antd';
import { Close } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import { useTranslate } from '@/hooks';
import type { UnsMetadata, UnsRecord } from '@/apis/core-api/uns-metadata';
import { FieldsTable, recordName } from '@/pages/uns/components/metadata/data-values';
import SampleHistory from '@/pages/uns/components/metadata/sample-history';
import styles from '../index.module.scss';

export default function RecordDetail({
  record,
  collection,
  onClose,
}: {
  record: UnsRecord;
  collection: UnsMetadata;
  onClose: () => void;
}) {
  const t = useTranslate();
  return (
    <section className={styles['record-detail']} aria-label={recordName(record)}>
      <div className={styles['detail-heading']}>
        <div>
          <h3>{recordName(record)}</h3>
          <span className={styles.muted}>{record.id}</span>
        </div>
        <ComButton type="text" icon={<Close />} aria-label={t('common.close')} onClick={onClose} />
      </div>
      {record.sourceId && (
        <Tag>
          {t('Collections.managedRecord')} · {record.sourceId}
        </Tag>
      )}
      <Tabs
        defaultActiveKey={collection.data.sampleSchema ? 'samples' : 'record'}
        items={[
          {
            key: 'record',
            label: t('Collections.recordDetails'),
            children: (
              <>
                <FieldsTable
                  data={record.data}
                  schema={collection.data.recordSchema}
                  origin="Record"
                  time={record.updatedAt}
                />
                <Collapse
                  items={[
                    {
                      key: 'json',
                      label: t('UnsMeta.rawData'),
                      children: <pre className={styles.code}>{JSON.stringify(record.data, null, 2)}</pre>,
                    },
                  ]}
                />
              </>
            ),
          },
          ...(collection.data.sampleSchema
            ? [
                {
                  key: 'samples',
                  label: t('Collections.samples'),
                  children: (
                    <SampleHistory collectionId={collection.id} recordId={record.id} collection={collection} compact />
                  ),
                },
              ]
            : []),
        ]}
      />
    </section>
  );
}
