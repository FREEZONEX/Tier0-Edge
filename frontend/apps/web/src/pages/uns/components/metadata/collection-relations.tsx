import { useEffect, useState } from 'react';
import { Alert, Button, Select, Table } from 'antd';
import { Flow } from '@carbon/icons-react';
import type { NativeAppGrant } from '@/apis/core-api/native-app';
import { unsV2Api } from '@/apis/core-api/uns-v2';
import type { UnsMetadata, Page } from '@/apis/core-api/uns-metadata';
import { useTranslate } from '@/hooks';
import { useIntegrationRoute } from '../source-stream/IntegrationTabs';
import { metadataPath } from './collection-usage';
import type useConnections from './use-connections';
import styles from './collection-detail.module.scss';

type Reference = { id: string; kind: string; data: { name?: string; collections?: NativeAppGrant[] } };

export default function CollectionRelations({
  collection,
  items,
  connections,
  onFlow,
  onSelect,
}: {
  collection: UnsMetadata;
  items: UnsMetadata[];
  connections: ReturnType<typeof useConnections>;
  onFlow?: () => void;
  onSelect: (id: string) => void;
}) {
  const t = useTranslate();
  const route = useIntegrationRoute();
  const [kind, setKind] = useState<'apps' | 'streams' | 'namespace'>(route.detail === 'topology' ? 'streams' : 'apps');
  const [apps, setApps] = useState<Reference[]>();
  const [failure, setFailure] = useState(false);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    if (kind !== 'apps') return;
    const controller = new AbortController();
    async function load() {
      const apps: Reference[] = [];
      let count = 0;
      for (let page = 1; ; page++) {
        const result = await unsV2Api.query<{ targets: { references: Page<Reference> }[] }>(
          'references/query',
          [{ queryId: 'apps', targets: [{ kind: 'collection', id: collection.id }], page, size: 200 }],
          controller.signal
        );
        const refs = result.results[0].targets[0].references;
        apps.push(...refs.list.filter((row) => row.kind === 'app'));
        count += refs.list.length;
        if (count >= refs.total || refs.list.length === 0) return apps;
      }
    }
    setFailure(false);
    setApps(undefined);
    load()
      .then((rows) => {
        if (!controller.signal.aborted) setApps(rows);
      })
      .catch(() => {
        if (!controller.signal.aborted) setFailure(true);
      });
    return () => controller.abort();
  }, [collection.id, kind, retry]);
  const namespaces = items.filter(
    (item) =>
      item.kind === 'topic' &&
      (item.data.collectionId === collection.id || item.data.fields?.some((f) => f.collectionId === collection.id))
  );
  const refIds = new Set(namespaces.map((row) => row.id));
  const streams = connections.readable
    ? connections.streams.filter((s) => {
        const target = s.activeDocument?.metadataTarget;
        return (
          s.activeVersion > 0 &&
          (target?.collectionId === collection.id ||
            refIds.has(target?.topicId || '') ||
            s.manifest?.topics?.some(
              (r) => r.collectionId === collection.id || (r.space !== 'legacy' && refIds.has(r.topicId || ''))
            ))
        );
      })
    : [];
  const rows =
    kind === 'apps'
      ? (apps ?? []).map((app) => ({
          id: app.id,
          name: app.data.name || app.id,
          access:
            app.data.collections
              ?.filter((grant) => grant.collectionId === collection.id)
              .flatMap((grant) => grant.permissions)
              .join(' / ') || '—',
        }))
      : kind === 'streams'
        ? streams.map((s) => ({ id: String(s.id), name: s.name }))
        : namespaces.map((node) => ({ id: node.id, name: metadataPath(node, items) }));
  return (
    <section className={styles.related}>
      <div className={styles.relationBar}>
        <Select
          aria-label={t('NamespaceView.viewRelations')}
          value={kind}
          onChange={setKind}
          options={[
            { value: 'apps', label: t('Collections.kindApp') },
            { value: 'streams', label: t('SourceStream.stream') },
            { value: 'namespace', label: t('UnsMeta.namespace') },
          ]}
        />
        {onFlow && (
          <Button icon={<Flow size={16} />} onClick={onFlow}>
            {t('NamespaceView.connections')}
          </Button>
        )}
      </div>
      {kind === 'streams' && !connections.readable ? (
        <Alert message={t('common.noPermission')} />
      ) : (kind === 'apps' && failure) || (kind === 'streams' && connections.failed) ? (
        <Alert
          type="error"
          message={t('UnsMeta.loadFailed')}
          action={kind === 'apps' && <Button onClick={() => setRetry((n) => n + 1)}>{t('UnsMeta.retry')}</Button>}
        />
      ) : (
        <Table
          size="small"
          rowKey="id"
          dataSource={rows}
          loading={kind === 'apps' ? !apps : kind === 'streams' && connections.loading}
          pagination={{ pageSize: 20, hideOnSinglePage: true }}
          columns={[
            {
              title: t('UnsMeta.name'),
              dataIndex: 'name',
              render: (name: string, row) =>
                kind === 'apps' ? (
                  name
                ) : (
                  <Button
                    type="link"
                    onClick={() =>
                      kind === 'streams' ? route.go({ view: 'stream', streamId: Number(row.id) }) : onSelect(row.id)
                    }
                  >
                    {name}
                  </Button>
                ),
            },
            { title: 'ID', dataIndex: 'id' },
            ...(kind === 'apps' ? [{ title: t('NamespaceView.access'), dataIndex: 'access' }] : []),
          ]}
        />
      )}
    </section>
  );
}
