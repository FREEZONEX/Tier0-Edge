import { Alert, Empty, Space, Spin, Tag } from 'antd';
import { ArrowLeft, ArrowRight, DataBase, Flow, Document, Add } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import { useTranslate } from '@/hooks';
import type { UnsMetadata } from '@/apis/core-api/uns-metadata';
import { isProgramDocument, type MetadataTarget } from '@/apis/core-api/source-stream';
import { ButtonPermission } from '@/common-types/button-permission';
import { useIntegrationRoute } from '../source-stream/IntegrationTabs';
import type useConnections from './use-connections';
import styles from './index.module.scss';

function sameMapping(a: MetadataTarget, b: MetadataTarget) {
  const paths = [
    'topicId',
    'collectionId',
    'recordId',
    'recordIdPath',
    'recordPath',
    'samplePath',
    'timePath',
    'qualityPath',
  ] as const;
  return (
    Boolean(a.sampleEnabled) === Boolean(b.sampleEnabled) && paths.every((key) => (a[key] || '') === (b[key] || ''))
  );
}

export default function Topology({
  meta,
  items,
  connections,
  onSelect,
  onRefresh,
}: {
  meta: UnsMetadata;
  items: UnsMetadata[];
  connections: ReturnType<typeof useConnections>;
  onSelect: (id: string) => void;
  onRefresh: () => void;
}) {
  const t = useTranslate();
  const route = useIntegrationRoute();
  const collections = new Set(
    [
      meta.kind === 'collection' ? meta.id : meta.data.collectionId,
      ...(meta.data.fields || []).map((f) => f.collectionId),
    ].filter(Boolean)
  );
  const links = connections.streams.flatMap((stream) => {
    const active = stream.activeDocument?.metadataTarget;
    const draft = stream.document.metadataTarget;
    const targets = active ? [{ target: active, active: true }] : [];
    if (draft && (!active || !sameMapping(draft, active))) targets.push({ target: draft, active: false });
    return targets
      .filter(({ target }) => collections.has(target.collectionId) || target.topicId === meta.id)
      .map((entry) => ({ ...entry, stream }));
  });
  const programLinks = connections.streams.flatMap((stream) => {
    if (!isProgramDocument(stream.document)) return [];
    const versions = [
      ...(stream.manifest ? [{ manifest: stream.manifest, active: true }] : []),
      ...(stream.draftManifest && JSON.stringify(stream.draftManifest) !== JSON.stringify(stream.manifest)
        ? [{ manifest: stream.draftManifest, active: false }]
        : []),
    ];
    return versions.flatMap(({ manifest, active }) =>
      manifest.topics
        .filter((target) => {
          const topic = items.find((item) => item.id === target.topicId);
          return target.topicId === meta.id || collections.has(target.collectionId || topic?.data.collectionId);
        })
        .map((target) => ({ stream, manifest, active, target }))
    );
  });
  return (
    <section className={styles['topology-section']}>
      <div className={styles.toolbar}>
        <div>
          <h3>{t('UnsMeta.topology')}</h3>
          <span className={styles.hint}>{t('UnsMeta.topologyHint')}</span>
        </div>
      </div>
      {!connections.readable ? (
        <Alert type="warning" message={t('SourceStream.forbidden')} />
      ) : connections.failed ? (
        <Alert
          type="error"
          message={t('SourceStream.loadFailed')}
          action={<ComButton onClick={onRefresh}>{t('UnsMeta.retry')}</ComButton>}
        />
      ) : (
        <Spin spinning={connections.loading}>
          <div className={styles['topology-canvas']}>
            {programLinks.map(({ stream, manifest, active, target }) => {
              const resourceId = target.topicId || target.collectionId!;
              const resource = items.find((item) => item.id === resourceId);
              return (
                <div className={styles['topology-lane']} key={`program-${stream.id}-${active}-${resourceId}`}>
                  <div className={styles['topology-node']}>
                    <span className={styles['node-label']}>
                      <DataBase />
                      {t('SourceStream.dependencies')}
                    </span>
                    {manifest.sources.length ? (
                      manifest.sources.map((ref) => {
                        const source = connections.sources.find((item) => String(item.id) === ref.id);
                        return (
                          <ComButton
                            type="link"
                            key={ref.id}
                            onClick={() => route.go({ view: 'source', sourceId: Number(ref.id) })}
                          >
                            {source?.name || ref.id}
                          </ComButton>
                        );
                      })
                    ) : (
                      <span>{t('SourceStream.noDependencies')}</span>
                    )}
                  </div>
                  <ArrowLeft className={styles['flow-arrow']} />
                  <button
                    className={styles['topology-node']}
                    onClick={() => route.go({ view: 'stream', streamId: stream.id })}
                  >
                    <span className={styles['node-label']}>
                      <Flow />
                      Stream
                    </span>
                    <strong>{stream.name}</strong>
                    <Tag>
                      {active
                        ? t(`SourceStream.execution.${stream.executionState || 'ready'}`)
                        : t('UnsMeta.draftMapping')}
                    </Tag>
                  </button>
                  {target.access === 'read' ? (
                    <ArrowLeft className={styles['flow-arrow']} />
                  ) : target.access === 'read-write' ? (
                    <Space>
                      <ArrowLeft className={styles['flow-arrow']} />
                      <ArrowRight className={styles['flow-arrow']} />
                    </Space>
                  ) : (
                    <ArrowRight className={styles['flow-arrow']} />
                  )}
                  <button className={styles['topology-node']} onClick={() => onSelect(resourceId)}>
                    <span className={styles['node-label']}>
                      <Document />
                      {t(resource?.kind === 'collection' ? 'UnsMeta.collection' : 'UnsMeta.topic')}
                    </span>
                    <strong>{resource?.data.name || resourceId}</strong>
                    <Tag>
                      {t(
                        target.access === 'read'
                          ? 'SourceStream.unsReadAccess'
                          : target.access === 'read-write'
                            ? 'SourceStream.unsReadWriteAccess'
                            : 'SourceStream.unsWriteAccess'
                      )}
                    </Tag>
                  </button>
                </div>
              );
            })}
            {links.map(({ stream, target, active }) => {
              const source = connections.sources.find((s) => s.id === stream.sourceId);
              const collection = items.find((c) => c.id === target.collectionId);
              return (
                <div className={styles['topology-lane']} key={`${stream.id}-${active}`}>
                  <button
                    className={styles['topology-node']}
                    onClick={() => route.go({ view: 'source', sourceId: stream.sourceId, tab: 'connection' })}
                  >
                    <span className={styles['node-label']}>
                      <DataBase />
                      Source · {source?.protocol}
                    </span>
                    <strong>{source?.name || String(stream.sourceId)}</strong>
                    <span>{t('UnsMeta.sourceSettings')}</span>
                  </button>
                  <ArrowRight className={styles['flow-arrow']} />
                  <button
                    className={styles['topology-node']}
                    onClick={() => route.go({ view: 'stream', streamId: stream.id, sourceId: stream.sourceId })}
                  >
                    <span className={styles['node-label']}>
                      <Flow />
                      Stream
                    </span>
                    <strong>{stream.name}</strong>
                    <Tag>
                      {active
                        ? t(`SourceStream.execution.${stream.executionState || 'ready'}`)
                        : t('UnsMeta.draftMapping')}
                    </Tag>
                    <span>{t('UnsMeta.mappingSettings')}</span>
                  </button>
                  <ArrowRight className={styles['flow-arrow']} />
                  <button className={styles['topology-node']} onClick={() => onSelect(target.topicId)}>
                    <span className={styles['node-label']}>
                      <Document />
                      {t('UnsMeta.topic')}
                    </span>
                    <strong>{items.find((item) => item.id === target.topicId)?.data.name || target.topicId}</strong>
                    <span>{collection?.data.name || target.collectionId}</span>
                    <span>{target.sampleEnabled ? 'Record + Sample' : 'Record'}</span>
                  </button>
                </div>
              );
            })}
            {!connections.loading && links.length === 0 && programLinks.length === 0 && (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('UnsMeta.noConnections')}>
                <ComButton
                  auth={ButtonPermission['uns.streamManage']}
                  icon={<Add />}
                  onClick={() => route.go({ view: 'source' })}
                >
                  {t('UnsMeta.connectSource')}
                </ComButton>
              </Empty>
            )}
          </div>
          {meta.data.fields?.length ? (
            <div className={styles['reference-strip']}>
              <strong>{t('UnsMeta.bindings')}</strong>
              {meta.data.fields.map((field) => (
                <Space key={field.name}>
                  <Tag>{field.kind}</Tag>
                  <span>
                    {items.find((c) => c.id === field.collectionId)?.data.name} / {field.recordId} / {field.fieldPath}
                  </span>
                  <ArrowRight />
                  <strong>{field.displayName || field.name}</strong>
                </Space>
              ))}
            </div>
          ) : null}
        </Spin>
      )}
    </section>
  );
}
