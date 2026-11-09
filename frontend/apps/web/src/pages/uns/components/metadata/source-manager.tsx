import { useState } from 'react';
import { Alert, Empty, Input, Space, Spin, Tag } from 'antd';
import { Add, ArrowLeft, Settings, DataBase, Flow } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import ProModal from '@/components/pro-modal';
import { ButtonPermission } from '@/common-types/button-permission';
import { useTranslate } from '@/hooks';
import type { UnsMetadata } from '@/apis/core-api/uns-metadata';
import SourceDetail from '../source-stream/SourceDetail';
import StreamDetail from '../source-stream/LegacyStreamDetail';
import ScriptStreamDetail from '../source-stream/ScriptStreamDetail';
import SourceCreateDialog from '../source-stream/SourceCreateDialog';
import { useIntegrationRoute } from '../source-stream/IntegrationTabs';
import type useConnections from './use-connections';
import styles from './index.module.scss';

export default function SourceManager({
  connections,
  items,
  onChanged,
}: {
  connections: ReturnType<typeof useConnections>;
  items: UnsMetadata[];
  onChanged: () => void;
}) {
  const t = useTranslate();
  const route = useIntegrationRoute();
  const [search, setSearch] = useState('');
  const { sources, streams, failed, loading, readable } = connections;
  const stream = streams.find((item) => item.id === route.streamId);
  const source = sources.find((item) => item.id === (stream?.sourceId || route.sourceId));
  const topic = items.find((item) => item.id === route.metaId && item.kind === 'topic');
  const editing = Boolean(source && (route.view === 'source' || stream || route.create === 'stream'));
  const close = () => route.go({ view: 'metadata', metaId: route.metaId, detail: route.detail });
  const back = () => route.go({ view: 'source' });
  return (
    <ProModal
      open
      width={1280}
      title={t(editing ? 'UnsMeta.connectionSettings' : 'UnsMeta.sourceManager')}
      footer={null}
      onCancel={close}
      maskClosable={false}
    >
      <div className={styles['manager-body']}>
        {!readable ? (
          <Alert type="warning" message={t('SourceStream.forbidden')} />
        ) : (
          <>
            {failed && (
              <Alert
                type="error"
                message={t('SourceStream.loadFailed')}
                action={<ComButton onClick={onChanged}>{t('UnsMeta.retry')}</ComButton>}
              />
            )}
            {route.view === 'stream' &&
            ((!route.streamId && route.create === 'script') || stream?.model === 'script') ? (
              <ScriptStreamDetail
                key={stream?.id || 'new'}
                stream={stream}
                initialSource={source}
                onChanged={(saved) => {
                  onChanged();
                  route.go(saved ? { view: 'stream', streamId: saved.id } : { view: 'stream' }, true);
                }}
              />
            ) : editing && source ? (
              <>
                <ComButton icon={<ArrowLeft />} onClick={back}>
                  {t('UnsMeta.allSources')}
                </ComButton>
                {route.view === 'stream' ? (
                  <StreamDetail
                    key={stream?.id || `new-${source.id}`}
                    source={source}
                    stream={stream}
                    initialTarget={
                      !stream && topic?.data.collectionId
                        ? { topicId: topic.id, collectionId: topic.data.collectionId, recordIdPath: 'id' }
                        : undefined
                    }
                    onChanged={(saved) => {
                      onChanged();
                      route.go(
                        saved
                          ? { view: 'stream', streamId: saved.id, sourceId: saved.sourceId }
                          : { view: 'source', sourceId: source.id },
                        true
                      );
                    }}
                  />
                ) : (
                  <SourceDetail
                    key={source.id}
                    source={source}
                    onChanged={(saved) => {
                      onChanged();
                      if (!saved) route.go({ view: 'source' }, true);
                    }}
                  />
                )}
              </>
            ) : (
              <>
                <div className={styles.toolbar}>
                  <Input.Search
                    allowClear
                    placeholder={t('SourceStream.search')}
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                  />
                  <ComButton
                    auth={ButtonPermission['uns.sourceManage']}
                    type="primary"
                    icon={<Add />}
                    onClick={() => route.go({ view: 'source', create: 'source' })}
                  >
                    {t('SourceStream.addSource')}
                  </ComButton>
                </div>
                <Spin spinning={loading}>
                  <div className={styles['source-grid']}>
                    {sources
                      .filter(
                        (item) =>
                          `${item.name} ${item.protocol}`.toLowerCase().includes(search.toLowerCase()) ||
                          streams.some(
                            (s) => s.sourceId === item.id && s.name.toLowerCase().includes(search.toLowerCase())
                          )
                      )
                      .map((item) => (
                        <section key={item.id} className={styles['source-card']}>
                          <div className={styles['card-heading']}>
                            <DataBase size={24} />
                            <Tag>{item.protocol}</Tag>
                          </div>
                          <h3>{item.name}</h3>
                          <p className={styles.hint}>{item.document.description || item.document.endpoint}</p>
                          <Space wrap>
                            <Tag>{t(`SourceStream.health.${item.health}`)}</Tag>
                          </Space>
                          <ComButton
                            icon={<Settings />}
                            onClick={() => route.go({ view: 'source', sourceId: item.id })}
                          >
                            {t('UnsMeta.sourceSettings')}
                          </ComButton>
                          <div className={styles['stream-list']}>
                            {streams
                              .filter((s) => s.sourceId === item.id)
                              .map((s) => (
                                <ComButton
                                  key={s.id}
                                  type="text"
                                  icon={<Flow />}
                                  onClick={() => route.go({ view: 'stream', streamId: s.id, sourceId: s.sourceId })}
                                >
                                  <span>{s.name}</span>
                                  <Tag>{t(`SourceStream.status.${s.status}`)}</Tag>
                                </ComButton>
                              ))}
                            <ComButton
                              auth={ButtonPermission['uns.streamManage']}
                              type="link"
                              icon={<Add />}
                              onClick={() =>
                                route.go({
                                  view: 'stream',
                                  sourceId: item.id,
                                  create:
                                    item.protocol === 'http-api' || item.protocol === 'oauth2' ? 'script' : 'stream',
                                })
                              }
                            >
                              {t('SourceStream.addStream')}
                            </ComButton>
                          </div>
                        </section>
                      ))}
                  </div>
                  {!loading && !failed && sources.length === 0 && <Empty description={t('SourceStream.noObjects')} />}
                </Spin>
              </>
            )}
          </>
        )}
      </div>
      {route.create === 'source' && (
        <SourceCreateDialog
          open
          onClose={back}
          onCreated={(created) => {
            onChanged();
            route.go({ view: 'source', sourceId: created.id }, true);
          }}
        />
      )}
    </ProModal>
  );
}
