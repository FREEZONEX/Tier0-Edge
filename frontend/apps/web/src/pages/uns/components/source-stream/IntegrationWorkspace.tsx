import { useEffect, useState, type FC } from 'react';
import { Alert, Empty, Spin } from 'antd';
import { ArrowLeft } from '@carbon/icons-react';
import ComLayout from '@/components/com-layout';
import ComContent from '@/components/com-layout/ComContent';
import ComButton from '@/components/com-button';
import ComDrawer from '@/components/com-drawer';
import { ButtonPermission } from '@/common-types/button-permission';
import { hasPermission } from '@/utils/auth';
import { useTranslate } from '@/hooks';
import { sourceStreamApi, type Source, type Stream } from '@/apis/core-api/source-stream';
import { metadataApi, type UnsMetadata } from '@/apis/core-api/uns-metadata';
import { useBaseStore } from '@/stores/base';
import { canReadUnsMetadata } from '../../metadata-permission';
import IntegrationTabs, { useIntegrationRoute } from './IntegrationTabs';
import SourceCreateDialog from './SourceCreateDialog';
import SourceDetail from './SourceDetail';
import SourceRegistry from './SourceRegistry';
import { ConnectHeaderProvider } from './ConnectHeaderActions';
import StreamDetail from './StreamDetail';
import styles from './index.module.scss';
import StreamRegistry from './StreamRegistry';
import ConnectWorkspace from './ConnectWorkspace';

function topicSelection(topic: UnsMetadata, ancestors: UnsMetadata[]) {
  if (topic.kind !== 'topic' || topic.deletedAt) throw new Error('Invalid Topic context');
  const names = [topic.data.name];
  const visited = new Set([topic.id]);
  const parents = new Map(ancestors.map((folder) => [folder.id, folder]));
  let parentId = topic.data.parentId;
  while (parentId) {
    if (visited.has(parentId)) throw new Error('Cyclic Topic context');
    visited.add(parentId);
    const folder = parents.get(parentId);
    if (!folder || folder.kind !== 'folder' || folder.deletedAt) throw new Error('Unavailable Topic parent');
    names.unshift(folder.data.name);
    parentId = folder.data.parentId;
  }
  return { id: topic.id, namespace: names.join('/') };
}

const IntegrationWorkspace: FC = () => {
  const t = useTranslate();
  const route = useIntegrationRoute();
  const [source, setSource] = useState<Source>();
  const [stream, setStream] = useState<Stream>();
  const [initialTopic, setInitialTopic] = useState<{ id: string; namespace: string }>();
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);
  const [refresh, setRefresh] = useState(0);
  // 权限变化须驱动重新渲染；不能只读取 getState 后保留旧对象上下文。
  const user = useBaseStore((state) => state.currentUserInfo);
  useBaseStore((state) => state.buttonList);
  const authEnable = useBaseStore((state) => state.systemInfo?.authEnable);
  const allowed = hasPermission(ButtonPermission[route.view === 'source' ? 'uns.sources' : 'uns.streams']);
  const sourceAllowed = hasPermission(ButtonPermission['uns.sources']);
  const streamAllowed = hasPermission(ButtonPermission['uns.streams']);
  const metadataAllowed = canReadUnsMetadata(user, authEnable);
  useEffect(() => {
    let alive = true;
    const controller = new AbortController();
    setSource(undefined);
    setStream(undefined);
    setInitialTopic(undefined);
    setFailed(false);
    if (!allowed || (!route.sourceId && !route.streamId && !route.metaId)) {
      setLoading(false);
      return;
    }
    setLoading(true);
    (async () => {
      const selectedStream =
        route.view === 'stream' && route.streamId ? await sourceStreamApi.stream(route.streamId) : undefined;
      const ownerId = selectedStream ? selectedStream.sourceId : route.sourceId;
      const selectedSource = ownerId && sourceAllowed ? await sourceStreamApi.source(ownerId) : undefined;
      const topicContextRequested =
        metadataAllowed && !selectedStream && route.view === 'stream' && route.create === 'stream' && route.metaId;
      const topic = topicContextRequested
        ? await metadataApi.find('topic', route.metaId!, false, controller.signal)
        : undefined;
      if (!alive) return;
      if (topicContextRequested && !topic) throw new Error('Unavailable Topic context');
      // 祖先一次取回，不随目录深度逐层发起 HTTP 请求。
      const ancestors = topic?.data.parentId
        ? await metadataApi.namespaceAncestors(topic.data.parentId, controller.signal)
        : [];
      const selection = topic ? topicSelection(topic, ancestors) : undefined;
      if (alive) {
        setInitialTopic(selection);
        setStream(selectedStream);
        setSource(selectedSource);
      }
    })()
      .catch(() => {
        if (alive) setFailed(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
      controller.abort();
    };
  }, [
    route.sourceId,
    route.streamId,
    route.view,
    route.create,
    route.metaId,
    allowed,
    sourceAllowed,
    metadataAllowed,
    user.sub,
  ]);
  const selectedTopic =
    allowed &&
    metadataAllowed &&
    !loading &&
    !failed &&
    route.view === 'stream' &&
    route.create === 'stream' &&
    !route.streamId &&
    initialTopic?.id === route.metaId
      ? initialTopic
      : undefined;
  const changedSource = (next?: Source) => {
    setSource(next);
    setRefresh((v) => v + 1);
    if (!next) route.go({ view: 'source' });
  };
  const changedStream = async (next?: Stream) => {
    setStream(next);
    setRefresh((v) => v + 1);
    if (!next) route.go({ view: 'stream', topicId: route.topicId || undefined });
    else if (!route.streamId)
      route.go(
        {
          view: 'stream',
          sourceId: next.sourceId || undefined,
          streamId: next.id,
          topicId: route.topicId || undefined,
        },
        true
      );
    else if (next.sourceId && sourceAllowed) {
      setLoading(true);
      try {
        // 内嵌 Stream 的修改也会更新 Source 修订号，后续测试和部署需使用新版本。
        setSource(await sourceStreamApi.source(next.sourceId));
      } catch {
        setFailed(true);
      } finally {
        setLoading(false);
      }
    }
  };
  const programEditor = route.view === 'stream';
  const detail = (
    <div className={programEditor ? styles['stream-workspace-content'] : styles.content}>
      <Spin spinning={loading}>
        {!allowed ? (
          <Alert type="error" message={t('SourceStream.forbidden')} showIcon />
        ) : route.view === 'stream' && route.create === 'stream' && !route.streamId && !!route.topicId ? (
          <Alert
            type="warning"
            showIcon
            message={t('Workflow.currentTopicRequired')}
            action={
              <ComButton onClick={() => route.go({ view: 'namespace' })}>{t('SourceStream.namespace')}</ComButton>
            }
          />
        ) : failed ? (
          <Alert
            type="error"
            showIcon
            message={t('SourceStream.loadFailed')}
            action={<ComButton onClick={() => window.location.reload()}>{t('SourceStream.refresh')}</ComButton>}
          />
        ) : !loading && route.view === 'stream' ? (
          <StreamDetail
            key={stream?.id || `new-${source?.id || 0}`}
            stream={stream}
            source={source}
            initialTopicName={selectedTopic?.namespace}
            onChanged={changedStream}
          />
        ) : source ? (
          <SourceDetail key={source.id} source={source} onChanged={changedSource} embedded />
        ) : (
          !loading &&
          (route.view === 'stream' && route.create === 'stream' && !route.streamId ? (
            <StreamDetail onChanged={changedStream} />
          ) : (
            <Empty description={t('SourceStream.selectObject')} />
          ))
        )}
      </Spin>
    </div>
  );
  return (
    <ConnectHeaderProvider>
      <ComLayout className="unsContainer">
        <ComContent wrapperStyle={{ minWidth: 0 }} style={{ padding: 0, display: 'flex', minHeight: 0 }}>
          <section className={styles['public-workspace']}>
            <div className={styles['registry-navigation']}>
              <IntegrationTabs />
            </div>
            {allowed ? (
              <div
                className={`${styles['registry-workspace']} ${route.view === 'source' && route.layout === 'canvas' ? styles['connect-canvas-workspace'] : ''}`}
              >
                {route.view === 'source' ? (
                  route.layout === 'canvas' ? (
                    <ConnectWorkspace
                      refreshKey={refresh}
                      sourceAllowed={sourceAllowed}
                      streamAllowed={streamAllowed}
                      metadataAllowed={metadataAllowed}
                      onList={() => route.go({ view: 'source', layout: 'list' })}
                    />
                  ) : (
                    <SourceRegistry key={user.sub} refreshKey={refresh} />
                  )
                ) : route.streamId || route.create ? (
                  <>
                    {!programEditor && (
                      <div className={styles['registry-back']}>
                        <ComButton
                          type="text"
                          icon={<ArrowLeft />}
                          onClick={() => route.go({ view: 'stream', topicId: route.topicId || undefined })}
                        >
                          {t('StreamRegistry.allStreams')}
                        </ComButton>
                      </div>
                    )}
                    {detail}
                  </>
                ) : (
                  <StreamRegistry key={refresh + ':' + route.topicId} />
                )}
                <ComDrawer
                  open={route.view === 'source' && !!route.sourceId}
                  title={<h2 style={{ margin: 0, fontSize: 'inherit' }}>{source?.name || t('SourceStream.source')}</h2>}
                  width={800}
                  destroyOnClose
                  onClose={() => route.go({ view: 'source', layout: route.layout === 'canvas' ? 'canvas' : 'list' })}
                  styles={{ body: { padding: 0 } }}
                >
                  {route.view === 'source' && detail}
                </ComDrawer>
              </div>
            ) : (
              <div className={styles['registry-workspace']}>{detail}</div>
            )}
          </section>
        </ComContent>
        <SourceCreateDialog
          open={
            allowed &&
            route.view === 'source' &&
            route.create === 'source' &&
            hasPermission(ButtonPermission['uns.sourceManage'])
          }
          onClose={() => route.go({ view: 'source' })}
          onCreated={() => {
            setRefresh((v) => v + 1);
            route.go({ view: 'source' }, true);
          }}
        />
      </ComLayout>
    </ConnectHeaderProvider>
  );
};

export default IntegrationWorkspace;
