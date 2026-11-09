import type { FC } from 'react';
import { Descriptions, Empty, Tag } from 'antd';
import { javascript } from '@codemirror/lang-javascript';
import { useTranslate } from '@/hooks';
import ComButton from '@/components/com-button';
import ProCodemirror from '@/components/pro-codemirror';
import type { Source, Stream } from '@/apis/core-api/source-stream';
import TopicLinks from './TopicLinks';
import { useIntegrationRoute } from './IntegrationTabs';
import styles from './index.module.scss';
import { readStreamDocument } from './StreamConfigEditor';

const StreamOverview: FC<{ source: Source; stream: Stream; targetsOnly?: boolean }> = ({
  source,
  stream,
  targetsOnly,
}) => {
  const t = useTranslate();
  const { go } = useIntegrationRoute();
  const doc = readStreamDocument(JSON.stringify(stream.document), source);
  if (!doc) return <Empty description={t('SourceStream.invalidConfiguration')} />;
  const resources = doc.nativeResourceRefs?.length ? doc.nativeResourceRefs : [doc.nativeResourceRef].filter(Boolean);
  const targets =
    doc.topicIds.length || doc.metadataTarget ? (
      <TopicLinks ids={doc.topicIds} metadataId={doc.metadataTarget?.topicId} />
    ) : (
      <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('SourceStream.noTargets')} />
    );
  if (targetsOnly)
    return (
      <section className={styles.card}>
        <h3>{t('SourceStream.topics')}</h3>
        {targets}
      </section>
    );
  return (
    <div className={styles.section}>
      <section className={styles.card}>
        <h3>{t('common.detail')}</h3>
        <Descriptions
          layout="vertical"
          column={2}
          items={[
            { key: 'id', label: t('SourceStream.streamId'), children: stream.id },
            { key: 'model', label: t('SourceStream.model'), children: t(`SourceStream.model.${stream.model}`) },
            {
              key: 'status',
              label: t('SourceStream.status'),
              children: t(
                `SourceStream.${stream.status === 'enabled' && stream.executionState ? 'execution.' + stream.executionState : 'status.' + stream.status}`
              ),
            },
            { key: 'sourceRevision', label: t('SourceStream.configurationRevision'), children: source.configRevision },
            {
              key: 'version',
              label: t('SourceStream.activeVersion'),
              children: stream.activeVersion || t('SourceStream.notDeployed'),
            },
            { key: 'topics', label: t('SourceStream.topics'), children: doc.metadataTarget ? 1 : doc.topicIds.length },
          ]}
        />
        {doc.description && <p className={styles.muted}>{doc.description}</p>}
      </section>
      <section className={styles.card}>
        <h3>{t('SourceStream.process')}</h3>
        <div className={styles['process-step']}>
          <span className={styles['step-number']}>1</span>
          <div className={styles.section}>
            <h4>{t(stream.model === 'subscribe' ? 'SourceStream.topics' : 'SourceStream.inputSource')}</h4>
            {stream.model === 'subscribe' ? (
              targets
            ) : (
              <ComButton className={styles['jump-card']} onClick={() => go({ view: 'source', sourceId: source.id })}>
                <strong>{source.name}</strong>
                <span>
                  {source.protocol.toUpperCase()} · {t(`SourceStream.health.${source.health}`)}
                </span>
              </ComButton>
            )}
            <div>
              {resources.map((ref) => (
                <Tag key={ref}>{source.resources.find((r) => r.id === ref)?.name || ref}</Tag>
              ))}
            </div>
          </div>
        </div>
        <div className={styles['process-step']}>
          <span className={styles['step-number']}>2</span>
          <div className={styles.section}>
            <h4>{t('SourceStream.transform')}</h4>
            <ProCodemirror
              value={doc.script.code}
              extensions={[javascript()]}
              readOnly
              editable={false}
              showHint={false}
              maxHeight="360px"
            />
            {doc.responseScript && (
              <>
                <h4>{t('SourceStream.responseScript')}</h4>
                <ProCodemirror
                  value={doc.responseScript.code}
                  extensions={[javascript()]}
                  readOnly
                  editable={false}
                  showHint={false}
                />
              </>
            )}
          </div>
        </div>
        <div className={styles['process-step']}>
          <span className={styles['step-number']}>3</span>
          <div className={styles.section}>
            <h4>
              {t(
                stream.model === 'subscribe'
                  ? 'SourceStream.source'
                  : stream.model === 'request-reply'
                    ? 'SourceStream.responseScript'
                    : 'SourceStream.topics'
              )}
            </h4>
            {stream.model === 'subscribe' ? (
              <ComButton onClick={() => go({ view: 'source', sourceId: source.id })}>{source.name}</ComButton>
            ) : stream.model === 'request-reply' ? (
              <span>{t('SourceStream.model.request-reply')}</span>
            ) : (
              targets
            )}
          </div>
        </div>
      </section>
    </div>
  );
};
export default StreamOverview;
