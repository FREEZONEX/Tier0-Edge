import { useEffect, useState, type FC } from 'react';
import { Alert, Empty, Space, Spin, Tag } from 'antd';
import { Add, ArrowRight, Renew } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import { ButtonPermission } from '@/common-types/button-permission';
import { hasPermission } from '@/utils/auth';
import { useTranslate } from '@/hooks';
import { sourceStreamApi, isProgramDocument, type Stream } from '@/apis/core-api/source-stream';
import { useIntegrationRoute } from './IntegrationTabs';
import styles from './index.module.scss';
import { SourceLink } from './resource-links';

const TopicStreamRelations: FC<{ topicId: number; topicName?: string }> = ({ topicId, topicName }) => {
  const t = useTranslate();
  const { go } = useIntegrationRoute();
  const [refresh, setRefresh] = useState(0);
  const [streams, setStreams] = useState<Stream[]>([]);
  const [total, setTotal] = useState(0);
  const [failed, setFailed] = useState(false);
  const [loading, setLoading] = useState(false);
  const allowed = hasPermission(ButtonPermission['uns.streams']);
  useEffect(() => {
    if (!allowed) return;
    let alive = true;
    setLoading(true);
    setFailed(false);
    sourceStreamApi
      .relations(topicId)
      .then(async (result) => {
        if (alive) {
          setStreams(result.list);
          setTotal(result.total);
        }
      })
      .catch(() => {
        if (alive) setFailed(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [topicId, allowed, refresh]);
  if (!allowed) return null;
  return (
    <div className={styles.section}>
      <div className={styles.row}>
        <strong>{t('SourceStream.connections')}</strong>
        <Space wrap>
          <ComButton
            aria-label={t('SourceStream.refresh')}
            icon={<Renew />}
            onClick={() => setRefresh((value) => value + 1)}
          />
          <ComButton
            auth={ButtonPermission['uns.sourceManage']}
            icon={<Add />}
            onClick={() => go({ view: 'source', create: 'source' })}
          >
            {t('SourceStream.addSource')}
          </ComButton>
        </Space>
      </div>
      <Spin spinning={loading}>
        {failed ? (
          <Alert type="error" showIcon message={t('SourceStream.loadFailed')} />
        ) : !loading && !streams.length ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('SourceStream.noObjects')} />
        ) : (
          streams.map((stream) => {
            const dependencies = isProgramDocument(stream.document)
              ? (stream.manifest ?? stream.draftManifest)?.sources.map((source) => Number(source.id)) || []
              : stream.model === 'script' && Array.isArray(stream.document.sources)
                ? stream.document.sources.map((source) => source.sourceId)
                : stream.sourceId
                  ? [stream.sourceId]
                  : [];
            const fromTopic =
              stream.model === 'subscribe' ||
              (isProgramDocument(stream.document) &&
                (stream.manifest ?? stream.draftManifest)?.entries.some(
                  (entry) => entry.kind === 'uns-event' && entry.topicIds?.includes(String(topicId))
                ));
            const sourceNode = (
              <div className={styles['lineage-node']}>
                <span className={styles.muted}>{t('SourceStream.source')}</span>
                {dependencies.map((id) => (
                  <SourceLink key={id} id={id} />
                ))}
                {!dependencies.length && <span>{t('SourceStream.noObjects')}</span>}
              </div>
            );
            const topicNode = (
              <div className={styles['lineage-node']}>
                <span className={styles.muted}>{t('uns.topic')}</span>
                <strong>{topicName || topicId}</strong>
              </div>
            );
            return (
              <div className={styles.lineage} key={stream.id}>
                {fromTopic ? topicNode : sourceNode}
                <ArrowRight size={20} />
                <div className={styles['lineage-node']}>
                  <span className={styles.muted}>{t('SourceStream.stream')}</span>
                  <ComButton
                    type="link"
                    onClick={() => go({ view: 'stream', streamId: stream.id, sourceId: stream.sourceId })}
                  >
                    {stream.name}
                  </ComButton>
                  <Tag>
                    {t(
                      `SourceStream.${stream.status === 'enabled' && stream.executionState ? 'execution.' + stream.executionState : 'status.' + stream.status}`
                    )}
                  </Tag>
                </div>
                <ArrowRight size={20} />
                {fromTopic ? sourceNode : topicNode}
              </div>
            );
          })
        )}
        {total > streams.length && (
          <ComButton onClick={() => go({ view: 'stream', topicId })}>{t('SourceStream.viewAll')}</ComButton>
        )}
      </Spin>
    </div>
  );
};

export default TopicStreamRelations;
