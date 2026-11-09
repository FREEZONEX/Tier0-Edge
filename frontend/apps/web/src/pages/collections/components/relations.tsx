import { useEffect, useState } from 'react';
import { Alert, Drawer } from 'antd';
import { useNavigate } from 'react-router';
import { useTranslate } from '@/hooks';
import ComButton from '@/components/com-button';
import ProTable from '@/components/pro-table';
import { type UnsMetadata, type Page } from '@/apis/core-api/uns-metadata';
import { unsV2Api } from '@/apis/core-api/uns-v2';
import styles from '../index.module.scss';

const kindLabels: Record<string, string> = {
  directory: 'Collections.kindDirectory',
  folder: 'Collections.kindDirectory',
  topic: 'Collections.kindTopic',
  collection: 'Collections.kindCollection',
  source: 'Collections.kindSource',
  stream: 'Collections.kindStream',
  source_state: 'Collections.kindSourceState',
  source_plan: 'Collections.kindSourcePlan',
  source_version: 'Collections.kindSourceVersion',
  app: 'Collections.kindApp',
  subscription: 'Collections.kindSubscription',
  task: 'Collections.kindTask',
};

export default function Relations({ collection }: { collection: UnsMetadata }) {
  const t = useTranslate();
  const navigate = useNavigate();
  const [rows, setRows] = useState<UnsMetadata[]>([]);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    unsV2Api
      .query<{ targets: { references: Page<UnsMetadata> }[] }>(
        'references/query',
        [{ queryId: 'refs', targets: [{ kind: 'collection', id: collection.id }], page: 1, size: 200 }],
        controller.signal
      )
      .then((result) => {
        if (!controller.signal.aborted) setRows(result.results[0].targets[0].references.list);
      })
      .catch(() => {
        if (!controller.signal.aborted) setFailed(true);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [collection.id]);
  if (failed) return <Alert type="error" message={t('UnsMeta.loadFailed')} />;
  return (
    <ProTable
      rowKey={(row) => `${row.kind}/${row.id}`}
      loading={loading}
      dataSource={rows}
      pagination={false}
      columns={[
        {
          title: t('UnsMeta.type'),
          dataIndex: 'kind',
          render: (kind: string) => (kindLabels[kind] ? t(kindLabels[kind]) : kind),
        },
        { title: t('UnsMeta.name'), render: (_, row) => row.data.name },
        { title: t('Collections.referenceId'), dataIndex: 'id' },
        {
          title: t('common.operation'),
          render: (_, row) =>
            (row.kind as string) === 'topic' ? (
              <ComButton
                type="link"
                onClick={() => navigate(`/uns?view=namespace&metaId=${encodeURIComponent(row.id)}`)}
              >
                {t('Collections.openTopic')}
              </ComButton>
            ) : (
              <ConfigPreview row={row} />
            ),
        },
      ]}
    />
  );
}

function ConfigPreview({ row }: { row: UnsMetadata }) {
  const t = useTranslate();
  const [open, setOpen] = useState(false);
  return (
    <>
      <ComButton type="link" onClick={() => setOpen(true)}>
        {t('common.detail')}
      </ComButton>
      <Drawer open={open} width={760} title={row.data.name} onClose={() => setOpen(false)}>
        <pre className={styles.code}>{JSON.stringify(row.data, null, 2)}</pre>
      </Drawer>
    </>
  );
}
