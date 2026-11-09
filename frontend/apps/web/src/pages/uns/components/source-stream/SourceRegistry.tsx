import { useSourceStreamApi, useSourceStreamScope } from '@/apis/core-api/source-stream-scope';
import { useEffect, useRef, useState } from 'react';
import { Alert, App, Badge, Dropdown, Input, Select, Space, Tag } from 'antd';
import { Add, Flow, OverflowMenuVertical, Renew } from '@carbon/icons-react';
import { type ProtocolDefinition, type Source, type SourceFilter } from '@/apis/core-api/source-stream';
import { ButtonPermission } from '@/common-types/button-permission';
import ComButton from '@/components/com-button';
import ConnectList from './ConnectList';
import SourceProtocolIcon from './SourceProtocolIcon';
import ConnectDisplay, { type ConnectGrouping } from './ConnectDisplay';
import { ConnectHeaderActions } from './ConnectHeaderActions';
import { openConfirmModal } from '@/components/confirm-modal';
import { usePagination, useTranslate } from '@/hooks';
import { hasPermission } from '@/utils/auth';
import { sourceTypeLabelKey } from './source-types';
import { authorizeSource } from './SourceOAuth';
import { useIntegrationRoute, useRegistryRevision } from './IntegrationTabs';
import { formatRegistryTime } from './registry-time';
import styles from './registry.module.scss';
import sharedStyles from './index.module.scss';

export default function SourceRegistry({
  onCanvas,
  embedded = false,
  refreshKey = 0,
}: {
  onCanvas?: () => void;
  embedded?: boolean;
  refreshKey?: number;
}) {
  const sourceStreamApi = useSourceStreamApi();
  const remoteReadOnly = useSourceStreamScope().readOnly;
  const sourceScope = useSourceStreamScope();
  const t = useTranslate();
  const { changed } = useRegistryRevision();
  const { go, sourceId } = useIntegrationRoute();
  const { message, modal } = App.useApp();
  const [failed, setFailed] = useState(false);
  const [group, setGroup] = useState<ConnectGrouping>('status');
  const [cards, setCards] = useState(false);
  const [busyId, setBusyId] = useState<number>();
  const [protocols, setProtocols] = useState<ProtocolDefinition[]>([]);
  const [catalogFailed, setCatalogFailed] = useState(false);
  useEffect(() => {
    let alive = true;
    sourceStreamApi
      .protocols()
      .then((catalog) => {
        if (alive) {
          setProtocols(catalog.list);
          setCatalogFailed(false);
        }
      })
      .catch(() => {
        if (alive) setCatalogFailed(true);
      });
    return () => {
      alive = false;
    };
  }, [sourceStreamApi]);
  const canManage = !remoteReadOnly && hasPermission(ButtonPermission['uns.sourceManage']);
  const canCredentials = !remoteReadOnly && hasPermission(ButtonPermission['uns.sourceCredentials']);
  const { data, loading, pagination, setSearchParams, refreshRequest } = usePagination<Source>({
    fetchApi: async (
      { pageNo, pageSize, ...filter }: SourceFilter & { pageNo: number; pageSize: number },
      { signal }: { signal: AbortSignal }
    ) => {
      try {
        const result = await sourceStreamApi.sources({ ...filter, page: pageNo, size: pageSize }, signal);
        if (!signal.aborted) setFailed(false);
        return { data: result.list, total: result.total, pageNo };
      } catch (error) {
        if (!signal.aborted) setFailed(true);
        throw error;
      }
    },
  });
  const lastRefresh = useRef(refreshKey);
  useEffect(() => {
    if (lastRefresh.current === refreshKey) return;
    lastRefresh.current = refreshKey;
    refreshRequest();
  }, [refreshKey, refreshRequest]);
  const run = async (source: Source, action: () => Promise<void>) => {
    setBusyId(source.id);
    try {
      await action();
    } catch {
      // The API adapter reports the error; retain the current list and filters.
    } finally {
      setBusyId(undefined);
      refreshRequest();
    }
  };
  const remove = async (source: Source) => {
    const impact = await sourceStreamApi.impact(source.id);
    openConfirmModal(modal, {
      title: t('SourceStream.deleteSource'),
      content: (
        <Space direction="vertical">
          <span>{source.name}</span>
          <span>{t('SourceStream.deleteSourceHint')}</span>
          <span>{t('SourceStream.deleteImpactHint', { count: impact.count })}</span>
          {impact.streams.map((stream) => (
            <span key={stream.id}>{stream.name}</span>
          ))}
        </Space>
      ),
      danger: true,
      okText: t('common.delete'),
      cancelText: t('common.cancel'),
      onOk: async () => {
        await sourceStreamApi.deleteSource(source.id, {
          expectedRevision: source.revision,
          confirmedImpactRevision: impact.impactRevision,
        });
        refreshRequest();
        changed();
      },
    });
  };
  return (
    <section className={`${styles.registry} ${embedded ? styles.embedded : ''}`}>
      <div className={styles.toolbar}>
        <Input.Search
          allowClear
          placeholder={t('SourceStream.search')}
          aria-label={t('SourceStream.source')}
          onSearch={(search) => setSearchParams({ search }, false)}
        />
        <Select
          allowClear
          className={styles.filter}
          placeholder={t('SourceStream.protocol')}
          aria-label={t('SourceStream.protocol')}
          onChange={(protocol) => setSearchParams({ protocol }, false)}
          options={protocols.map(({ id: value }) => ({
            value,
            label: t(
              value === 'http-api' ? 'SourceStream.catalogName.http-client' : `SourceStream.catalogName.${value}`
            ),
          }))}
        />
        <Select
          allowClear
          className={styles.filter}
          placeholder={t('SourceStream.connectionState')}
          aria-label={t('SourceStream.connectionState')}
          onChange={(health) => setSearchParams({ health }, false)}
          options={['healthy', 'unhealthy', 'unverified'].map((value) => ({
            value,
            label: t(`SourceStream.health.${value}`),
          }))}
        />

        <ConnectDisplay group={group} onGroup={setGroup} cards={cards} onCards={setCards} />
        <ConnectHeaderActions embedded={embedded}>
          <div className={styles.actions}>
            <ComButton icon={<Renew />} aria-label={t('SourceStream.refresh')} onClick={() => refreshRequest()} />
            {onCanvas && (
              <ComButton icon={<Flow />} onClick={onCanvas}>
                {t('UnsConnect.canvas')}
              </ComButton>
            )}
            {!remoteReadOnly && (
              <ComButton
                auth={ButtonPermission['uns.sourceManage']}
                className={sharedStyles['manage-button']}
                icon={<Add />}
                onClick={() => go({ view: 'source', create: 'source' })}
              >
                {t('SourceStream.addSource')}
              </ComButton>
            )}
          </div>
        </ConnectHeaderActions>
      </div>
      {(failed || catalogFailed) && <Alert type="error" showIcon message={t('SourceStream.loadFailed')} />}
      <ConnectList
        rows={failed ? [] : data}
        loading={loading}
        pagination={pagination}
        cards={cards}
        activeId={sourceId}
        groupBy={(source) =>
          group === 'none'
            ? ''
            : group === 'type'
              ? sourceTypeLabelKey(source.document ?? source)
              : source.document.desiredState === 'disabled'
                ? 'disabled'
                : source.health
        }
        groupLabel={(key) =>
          group === 'type'
            ? t(key)
            : t(key === 'disabled' ? 'SourceStream.status.disabled' : `SourceStream.health.${key}`)
        }
        onOpen={(source) => go({ view: 'source', sourceId: source.id })}
        empty={t('SourceRegistry.empty')}
        columns={[
          {
            title: t('SourceStream.source'),
            dataIndex: 'name',
            width: 280,
            render: (name: string, source: Source) => (
              <div className={styles.identity}>
                <SourceProtocolIcon protocol={source.document.protocol} />
                <div className={styles.identityText}>
                  <ComButton
                    type="link"
                    onClick={(event) => {
                      event.stopPropagation();
                      go({ view: 'source', sourceId: source.id });
                    }}
                  >
                    {name}
                  </ComButton>
                  {canManage && (
                    <div className={styles.endpoint} title={source.document.endpoint}>
                      {source.document.endpoint}
                    </div>
                  )}
                </div>
              </div>
            ),
          },
          {
            title: t('SourceRegistry.integration'),
            key: 'integration',
            width: 180,
            render: (_: unknown, source: Source) => t(sourceTypeLabelKey(source.document ?? source)),
          },
          {
            title: t('SourceStream.connectionState'),
            key: 'health',
            width: 190,
            render: (_: unknown, source: Source) => (
              <Space direction="vertical" size={4}>
                <Badge
                  status={source.health === 'healthy' ? 'success' : source.health === 'unhealthy' ? 'error' : 'default'}
                  text={t(`SourceStream.health.${source.health}`)}
                />
                {source.oauthStatus && <Tag>{t(`SourceStream.oauth.${source.oauthStatus}`)}</Tag>}
              </Space>
            ),
          },
          {
            title: t('SourceRegistry.updatedAt'),
            dataIndex: 'updatedAt',
            width: 185,
            render: formatRegistryTime,
          },
          {
            title: '',
            key: 'actions',
            width: 60,
            render: (_: unknown, source: Source) => (
              <Dropdown
                trigger={['click']}
                menu={{
                  items: [
                    ...(canManage || canCredentials
                      ? [
                          {
                            key: 'manage',
                            label: t('SourceStream.manage'),
                            onClick: () => go({ view: 'source', sourceId: source.id, tab: 'connection' }),
                          },
                        ]
                      : []),
                    {
                      key: 'details',
                      label: t('common.detail'),
                      onClick: () => go({ view: 'source', sourceId: source.id }),
                    },
                    ...(canCredentials &&
                    ((source.document?.http?.oauth && source.document.http.oauth.grantType !== 'client_credentials') ||
                      source.document?.sourceType === 'ngp')
                      ? [
                          {
                            key: 'authorize',
                            label: t(
                              source.oauthStatus === 'authorized'
                                ? 'SourceStream.oauth.reauthorize'
                                : 'SourceStream.oauth.authorize'
                            ),
                            onClick: () => {
                              if (!source.document.credentialRef)
                                go({ view: 'source', sourceId: source.id, tab: 'connection' });
                              else
                                void run(source, () => authorizeSource(source.id, sourceStreamApi, sourceScope.nodeId));
                            },
                          },
                        ]
                      : []),
                    ...(hasPermission(ButtonPermission['uns.streams'])
                      ? [
                          {
                            key: 'streams',
                            label: t('SourceRegistry.viewStreams'),
                            onClick: () => go({ view: 'source', sourceId: source.id, tab: 'streams' }),
                          },
                        ]
                      : []),
                    ...(canManage
                      ? [
                          {
                            key: 'test',
                            label: t('SourceStream.testConnection'),
                            onClick: () =>
                              void run(source, async () => {
                                const result = await sourceStreamApi.probe(source.id);
                                if (result.health === 'healthy') message.success(t('SourceStream.connectionVerified'));
                                else message.warning(`${result.stage} · ${result.errorCode}: ${result.message}`);
                              }),
                          },
                          { type: 'divider' as const },
                          {
                            key: 'delete',
                            danger: true,
                            label: t('SourceStream.deleteSource'),
                            onClick: () => void run(source, () => remove(source)),
                          },
                        ]
                      : []),
                  ],
                }}
              >
                <ComButton
                  type="text"
                  icon={<OverflowMenuVertical />}
                  loading={busyId === source.id}
                  disabled={busyId !== undefined}
                  aria-label={t('SourceStream.manage')}
                />
              </Dropdown>
            ),
          },
        ]}
      />
    </section>
  );
}
