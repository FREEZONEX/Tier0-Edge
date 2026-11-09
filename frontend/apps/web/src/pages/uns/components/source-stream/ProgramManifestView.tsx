import type { FC } from 'react';
import { Collapse, Empty, Tag } from 'antd';
import type { ProgramManifest } from '@/apis/core-api/source-stream';
import { useTranslate } from '@/hooks';
import { SourceLink, TopicLink } from './resource-links';
import { programResourceKey } from './program-document';
import styles from './index.module.scss';

const ProgramManifestView: FC<{
  manifest?: ProgramManifest;
  label: string;
  compact?: boolean;
  streamName?: string;
}> = ({ manifest, label, compact, streamName }) => {
  const t = useTranslate();
  const entries = manifest?.entries.map((entry) => ({ ...entry, control: entry.request ?? entry.control })) ?? [];
  const controls = entries.filter((entry) => entry.control);
  const messages =
    manifest?.entries.filter((entry) => ['source-event', 'uns-event', 'http-request'].includes(entry.kind)) ?? [];
  const pushTargets = manifest?.topics.filter((topic) => ['write', 'read-write'].includes(topic.access ?? '')) ?? [];
  return (
    <section className={styles.section}>
      <h3>{label}</h3>
      {!manifest ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('SourceStream.manifestUnavailable')} />
      ) : (
        <>
          <div className={styles['manifest-summary']}>
            <Tag>{t('SourceStream.sourceCount', { count: manifest.sources.length })}</Tag>
            <Tag>{t('SourceStream.requestCount', { count: controls.length })}</Tag>
            <Tag>{t('SourceStream.messageCount', { count: messages.length })}</Tag>
            <Tag>{t('SourceStream.pushTargetCount', { count: pushTargets.length })}</Tag>
            <span>{t('SourceStream.declaredLinks')}</span>
          </div>
          <div className={compact ? styles.section : styles['manifest-flow']}>
            <section className={styles['process-block']}>
              <h4>{t('SourceStream.sourceConnections')}</h4>
              {!manifest.sources.length && <span>{t('SourceStream.noDependencies')}</span>}
              {manifest.sources.map((source) => (
                <div key={source.id} className={styles['manifest-resource']}>
                  <SourceLink id={Number(source.id)} />
                  <code>{source.id}</code>
                  {source.resources.map((ref) => (
                    <code key={ref}>{ref}</code>
                  ))}
                </div>
              ))}
            </section>
            <div className={styles['manifest-arrow']} aria-hidden="true">
              ↔
            </div>
            <section className={styles['process-block']}>
              <h4>{t('SourceStream.streamProgram')}</h4>
              {streamName && <strong>{streamName}</strong>}
              <span>{t('SourceStream.scriptUsesSources')}</span>
            </section>
            <div className={styles['manifest-arrow']} aria-hidden="true">
              ↔
            </div>
            <section className={styles['process-block']}>
              <h4>{t('SourceStream.unsData')}</h4>
              {!manifest.topics.length && <span>{t('SourceStream.noTopics')}</span>}
              {manifest.topics.map((resource) => (
                <div key={programResourceKey(resource)} className={styles['manifest-resource']}>
                  <div className={styles['manifest-summary']}>
                    <strong>
                      {t(resource.collectionId ? 'SourceStream.dataCollection' : 'SourceStream.unsTopic')}
                    </strong>
                    <Tag>{t(`SourceStream.resourceAccess.${resource.access || 'unspecified'}`)}</Tag>
                  </div>
                  <TopicLink
                    id={(resource.topicId || resource.collectionId)!}
                    space={resource.collectionId ? 'metadata' : resource.space}
                  />
                  <code>{resource.topicId || resource.collectionId}</code>
                </div>
              ))}
            </section>
          </div>
          <p className={styles.muted}>{t('SourceStream.unsDataMeaning')}</p>
          <Collapse
            ghost
            items={[
              {
                key: 'entries',
                label: t('SourceStream.runtimeEntries'),
                children: (
                  <div className={styles.section}>
                    {entries.map((entry) => (
                      <div key={entry.id} className={styles.section}>
                        <strong>{entry.id}</strong>
                        <Tag>{t(`SourceStream.entry.${entry.kind}`)}</Tag>
                        {entry.control && (
                          <div className={styles['manifest-resource']}>
                            <div className={styles['manifest-summary']}>
                              <Tag color="blue">{t('SourceStream.requestContract')}</Tag>
                              <strong>{entry.control.name}</strong>
                              <code>{entry.control.code}</code>
                            </div>
                            {entry.control.description && <span>{entry.control.description}</span>}
                            <div className={styles['manifest-summary']}>
                              <Tag>{t('SourceStream.contractVersion', { version: entry.control.contractVersion })}</Tag>
                              <Tag>
                                {t(
                                  entry.control.sideEffect === 'write'
                                    ? 'SourceStream.controlWrite'
                                    : 'SourceStream.controlRead'
                                )}
                              </Tag>
                              <Tag>
                                {t(
                                  entry.control.idempotency === 'required'
                                    ? 'SourceStream.idempotencyRequired'
                                    : entry.control.idempotency === 'optional'
                                      ? 'SourceStream.idempotencyOptional'
                                      : 'SourceStream.idempotencyNone'
                                )}
                              </Tag>
                              <Tag>{t('SourceStream.controlTimeout', { timeout: entry.control.timeoutMs })}</Tag>
                            </div>
                          </div>
                        )}
                        {entry.kind === 'http-request' && (
                          <code>
                            {entry.method} {entry.path}
                          </code>
                        )}
                        {entry.intervalMs && <span>{entry.intervalMs} ms</span>}
                        {entry.sourceId && <SourceLink id={Number(entry.sourceId)} />}
                        {entry.resourceRefs?.map((ref) => (
                          <span key={ref}>{ref}</span>
                        ))}
                        {entry.topicIds?.map((id) => {
                          const reference = manifest.topics.find((item) => programResourceKey(item) === id);
                          return reference ? (
                            <TopicLink key={id} id={reference.topicId!} space={reference.space} />
                          ) : (
                            <span key={id}>
                              {t('SourceStream.referenceUnavailable')} · {id}
                            </span>
                          );
                        })}
                      </div>
                    ))}
                  </div>
                ),
              },
            ]}
          />
        </>
      )}
    </section>
  );
};
export default ProgramManifestView;
