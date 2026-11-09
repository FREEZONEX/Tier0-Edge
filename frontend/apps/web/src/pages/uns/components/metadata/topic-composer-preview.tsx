import { useEffect, useState } from 'react';
import { Alert, Spin, Tabs } from 'antd';
import { useTranslate } from '@/hooks';
import ComButton from '@/components/com-button';
import { metadataApi, type FieldBinding, type UnsMetadata, type UnsRecord } from '@/apis/core-api/uns-metadata';
import { outputAt } from '../source-stream/workflow-data';
import { topicSchema } from './topic-composer-model';
import { json } from './schema-editor';
import styles from './topic-composer.module.scss';

export default function TopicComposerPreview({
  fields,
  items,
  records,
}: {
  fields: FieldBinding[];
  items: UnsMetadata[];
  records: Record<string, UnsRecord>;
}) {
  const t = useTranslate();
  const [preview, setPreview] = useState<{ data: object; missing: string[]; failed: boolean }>();
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setPreview(undefined);
    const ids = [...new Set(fields.filter((field) => field.kind === 'sample').map((field) => field.collectionId))];
    Promise.allSettled(
      ids.map((id) => metadataApi.samples(id, { recordId: records[id].id, size: 1 }, controller.signal))
    ).then((results) => {
      if (controller.signal.aborted) return;
      const samples = Object.fromEntries(
        results.map((result, index) => [
          ids[index],
          result.status === 'fulfilled' ? result.value.list[0]?.data : undefined,
        ])
      );
      const values = fields.map(
        (field) =>
          [
            field.name,
            outputAt(
              field.kind === 'sample' ? samples[field.collectionId] : records[field.collectionId]?.data,
              field.fieldPath
            ),
          ] as const
      );
      setPreview({
        data: Object.fromEntries(values.filter(([, value]) => value !== undefined)),
        missing: values.filter(([, value]) => value === undefined).map(([name]) => name),
        failed: results.some((result) => result.status === 'rejected'),
      });
    });
    return () => controller.abort();
  }, [fields, records, retry]);
  return (
    <>
      {fields.some((field) => field.kind === 'sample') && (
        <Alert type="info" message={t('TopicCompose.latestSample')} />
      )}
      <Tabs
        items={[
          {
            key: 'payload',
            label: t('TopicCompose.payload'),
            children: (
              <Spin spinning={!preview}>
                {preview?.failed && (
                  <Alert
                    type="error"
                    message={t('UnsMeta.previewFailed')}
                    action={<ComButton onClick={() => setRetry(retry + 1)}>{t('UnsMeta.refresh')}</ComButton>}
                  />
                )}
                {!!preview?.missing.length && (
                  <Alert type="warning" message={`${t('TopicCompose.missing')}: ${preview.missing.join(', ')}`} />
                )}
                <pre className={styles.preview}>{preview && json(preview.data)}</pre>
              </Spin>
            ),
          },
          {
            key: 'schema',
            label: t('UnsUi.schema'),
            children: <pre className={styles.preview}>{json(topicSchema(fields, items))}</pre>,
          },
        ]}
      />
    </>
  );
}
