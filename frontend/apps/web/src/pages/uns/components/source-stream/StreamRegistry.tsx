import { useSourceStreamApi, useSourceStreamScope } from '@/apis/core-api/source-stream-scope';
import { useState, type FC } from 'react';
import { Alert, Badge, Dropdown, Input, Select } from 'antd';
import { Add, Flow, OverflowMenuVertical, Renew } from '@carbon/icons-react';
import { type Stream, type StreamFilter } from '@/apis/core-api/source-stream';
import { ButtonPermission } from '@/common-types/button-permission';
import ComButton from '@/components/com-button';
import ConnectList from './ConnectList';
import ConnectDisplay, { type ConnectGrouping } from './ConnectDisplay';
import { ConnectHeaderActions } from './ConnectHeaderActions';
import { usePagination, useTranslate } from '@/hooks';
import { useIntegrationRoute } from './IntegrationTabs';
import { formatRegistryTime } from './registry-time';
import styles from './registry.module.scss';
import sharedStyles from './index.module.scss';

const StreamRegistry: FC<{ embedded?: boolean }> = ({ embedded = false }) => {
  const sourceStreamApi = useSourceStreamApi();
  const remoteReadOnly = useSourceStreamScope().readOnly;
  const t = useTranslate();
  const { go, topicId } = useIntegrationRoute();
  const [failed, setFailed] = useState(false);
  const [group, setGroup] = useState<ConnectGrouping>('status');
  const [filters, setFilters] = useState<Pick<StreamFilter, 'search' | 'status'>>({});
  const { data, loading, pagination, setSearchParams, refreshRequest } = usePagination<Stream>({
    fetchApi: async (
      { pageNo, pageSize, ...filter }: StreamFilter & { pageNo: number; pageSize: number },
      { signal }: { signal: AbortSignal }
    ) => {
      try {
        const result = await sourceStreamApi.streams(
          { ...filter, topicId: topicId || undefined, page: pageNo, size: pageSize },
          signal
        );
        if (!signal.aborted) setFailed(false);
        return { data: result.list, total: result.total, pageNo };
      } catch (error) {
        if (!signal.aborted) setFailed(true);
        throw error;
      }
    },
  });
  const filterList = (patch: Pick<StreamFilter, 'search' | 'status'>) => {
    setFilters((previous) => ({ ...previous, ...patch }));
    setSearchParams(patch, false);
  };
  const openStream = (stream: Stream, tab?: string) =>
    go({
      view: 'stream',
      streamId: stream.id,
      topicId: topicId || undefined,
      tab,
    });
  const runMode = (stream: Stream) => {
    const entries = (stream.draftManifest ?? stream.manifest)?.entries;
    if (entries?.length) return [...new Set(entries.map(({ kind }) => t(`StreamRegistry.mode.${kind}`)))].join(' · ');
    if (stream.executionMode)
      return t(`Workflow.runMode.${stream.executionMode === 'mqtt-subscription' ? 'source-event' : 'request'}`);
    return '—';
  };
  return (
    <section className={`${styles.registry} ${embedded ? styles.embedded : ''}`}>
      <div className={styles.toolbar}>
        <Input.Search
          allowClear
          placeholder={t('SourceStream.search')}
          aria-label={t('SourceStream.stream')}
          onSearch={(search) => filterList({ search })}
        />
        <Select
          allowClear
          className={styles.filter}
          placeholder={t('SourceStream.status')}
          aria-label={t('SourceStream.status')}
          onChange={(status) => filterList({ status })}
          options={['draft', 'enabled', 'disabled'].map((value) => ({
            value,
            label: t(`SourceStream.status.${value}`),
          }))}
        />

        <ConnectDisplay group={group} onGroup={setGroup} />
        <ConnectHeaderActions embedded={embedded}>
          <div className={styles.actions}>
            <ComButton icon={<Renew />} aria-label={t('SourceStream.refresh')} onClick={() => refreshRequest()} />
            {!remoteReadOnly && (
              <ComButton
                auth={ButtonPermission['uns.streamManage']}
                className={sharedStyles['manage-button']}
                icon={<Add />}
                onClick={() => go({ view: 'stream', create: 'stream' })}
              >
                {t('SourceStream.addStream')}
              </ComButton>
            )}
          </div>
        </ConnectHeaderActions>
      </div>
      {failed && <Alert type="error" showIcon message={t('SourceStream.loadFailed')} />}
      <ConnectList
        rows={failed ? [] : data}
        loading={loading}
        pagination={pagination}
        groupBy={(stream) => (group === 'none' ? '' : stream.status)}
        groupLabel={(key) => t(`SourceStream.status.${key}`)}
        onOpen={openStream}
        empty={t(filters.search || filters.status || topicId ? 'common.noData' : 'StreamRegistry.empty')}
        columns={[
          {
            title: t('SourceStream.name'),
            dataIndex: 'name',
            width: 320,
            render: (name: string, stream: Stream) => (
              <div className={styles.identity}>
                <span className={styles.icon}>
                  <Flow size={22} />
                </span>
                <ComButton
                  type="link"
                  onClick={(event) => {
                    event.stopPropagation();
                    openStream(stream);
                  }}
                >
                  {name}
                </ComButton>
              </div>
            ),
          },
          {
            title: t('Workflow.runMode'),
            key: 'mode',
            width: 200,
            render: (_: unknown, stream: Stream) => runMode(stream),
          },
          {
            title: t('SourceStream.status'),
            dataIndex: 'status',
            width: 150,
            render: (status: Stream['status']) => (
              <Badge status={status === 'enabled' ? 'success' : 'default'} text={t(`SourceStream.status.${status}`)} />
            ),
          },
          {
            title: t('SourceRegistry.updatedAt'),
            dataIndex: 'updatedAt',
            width: 190,
            render: formatRegistryTime,
          },
          {
            title: '',
            key: 'actions',
            width: 60,
            render: (_: unknown, stream: Stream) => (
              <div onClick={(event) => event.stopPropagation()}>
                <Dropdown
                  trigger={['click']}
                  menu={{
                    items: [
                      { key: 'open', label: t('common.open'), onClick: () => openStream(stream) },
                      { key: 'runs', label: t('SourceStream.runs'), onClick: () => openStream(stream, 'runs') },
                    ],
                  }}
                >
                  <ComButton type="text" icon={<OverflowMenuVertical />} aria-label={t('SourceStream.manage')} />
                </Dropdown>
              </div>
            ),
          },
        ]}
      />
    </section>
  );
};

export default StreamRegistry;
