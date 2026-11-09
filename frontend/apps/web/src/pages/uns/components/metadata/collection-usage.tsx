import { Alert, Space, Tag } from 'antd';
import { useTranslate } from '@/hooks';
import type { UnsMetadata } from '@/apis/core-api/uns-metadata';
import type useConnections from './use-connections';
import styles from './index.module.scss';

export function metadataPath(item: UnsMetadata, items: UnsMetadata[]) {
  const parts = [item.data.name];
  const seen = new Set([item.id]);
  let parent = items.find((entry) => entry.id === item.data.parentId);
  while (parent && !seen.has(parent.id)) {
    seen.add(parent.id);
    parts.unshift(parent.data.name);
    parent = items.find((entry) => entry.id === parent?.data.parentId);
  }
  return parts.join(' / ');
}

export default function CollectionUsage({
  collection,
  items,
  connections,
}: {
  collection: UnsMetadata;
  items: UnsMetadata[];
  connections: ReturnType<typeof useConnections>;
}) {
  const t = useTranslate();
  const topics = items.filter(
    (item) =>
      item.kind === 'topic' &&
      (item.data.collectionId === collection.id ||
        item.data.fields?.some((field) => field.collectionId === collection.id))
  );
  const streams = connections.streams.filter((stream) =>
    [stream.document.metadataTarget, stream.activeDocument?.metadataTarget].some(
      (target) => target?.collectionId === collection.id
    )
  );
  return (
    <section className={styles['collection-usage']}>
      <Alert type="warning" showIcon message={t('UnsMeta.sharedSchemaWarning')} />
      <strong>{t('UnsMeta.usedBy')}</strong>
      {topics.map((topic) => (
        <div key={topic.id}>
          <Tag>{t('UnsMeta.topic')}</Tag>
          {metadataPath(topic, items)}
        </div>
      ))}
      {streams.map((stream) => (
        <Space key={stream.id} wrap>
          <Tag>Stream</Tag>
          {connections.sources.find((source) => source.id === stream.sourceId)?.name} / {stream.name}
        </Space>
      ))}
      {connections.loading ? (
        <span>{t('UnsMeta.checkingReferences')}</span>
      ) : connections.failed || !connections.readable ? (
        <Alert type="warning" message={t('UnsMeta.referencesUnknown')} />
      ) : (
        !topics.length && !streams.length && <span>{t('UnsMeta.noReferences')}</span>
      )}
    </section>
  );
}
