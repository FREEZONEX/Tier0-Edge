import { useEffect, useMemo, useState, type FC } from 'react';
import { Alert, Breadcrumb, Checkbox, Empty, Input, Pagination, Select, Space, Spin, Tag } from 'antd';
import { ArrowLeft, Information, Renew } from '@carbon/icons-react';
import { useLocation, useNavigate, useSearchParams } from 'react-router';
import ComButton from '@/components/com-button';
import ComDrawer from '@/components/com-drawer';
import { useTranslate } from '@/hooks';
import {
  graphKinds,
  unsGraphApi,
  type GraphKind,
  type GraphRecordPage,
  type GraphResult,
} from '@/apis/core-api/uns-graph';
import { recordNodeKey } from './graph-layout';
import { enterOverviewObject, overviewParent, preserveOverviewState, relationshipGraphLocation } from './navigation';
import GraphCanvas, { type SphereData } from './components/GraphCanvas';
import SampleHistory from './components/SampleHistory';
import styles from './index.module.scss';

function detailLocation(kind: string, id: string) {
  const params = new URLSearchParams();
  if (kind === 'source' || kind === 'stream') {
    params.set('view', kind);
    params.set(`${kind}Id`, id);
  } else if (kind === 'collection') {
    params.set('view', 'collections');
    params.set('collectionId', id);
  } else if (kind === 'directory' || kind === 'topic') {
    params.set('view', 'namespace');
    params.set('metaId', id);
  } else return undefined;
  return `/uns?${params}`;
}

const OverviewWorkspace: FC = () => {
  const t = useTranslate('UnsOverview');
  const navigate = useNavigate();
  const location = useLocation();
  const [params, setParams] = useSearchParams();
  const [graph, setGraph] = useState<GraphResult>();
  const [records, setRecords] = useState<GraphRecordPage>();
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [recordsLoading, setRecordsLoading] = useState(false);
  const [recordsFailed, setRecordsFailed] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const [information, setInformation] = useState<SphereData>();
  const [searchInput, setSearchInput] = useState(params.get('search') || '');
  const requestedKind = params.get('kind');
  const kind = graphKinds.includes(requestedKind as GraphKind) ? (requestedKind as GraphKind) : undefined;
  const id = kind ? params.get('id') || '' : '';
  const isFocus = !!id;
  const recordId = params.get('recordId') || '';
  const recordCollection = params.get('collectionId') || '';
  const isRecord = !!recordId && !!recordCollection;
  const page = Math.max(1, Number(params.get('page')) || 1);
  const recordPage = Math.max(1, Number(params.get('recordPage')) || 1);
  // 默认突出业务对象；显式清空筛选可查看任务、运行状态等全部对象。
  const kindsValue = params.get('kinds') ?? (isFocus ? '' : 'source,stream,collection,topic,directory,app');
  const kinds = useMemo(
    () => kindsValue.split(',').filter((value): value is GraphKind => graphKinds.includes(value as GraphKind)),
    [kindsValue]
  );
  const search = params.get('search') || '';
  const archived = params.get('archived') === 'true';
  const change = (values: Record<string, string | undefined>) =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        Object.entries(values).forEach(([key, value]) =>
          value === undefined ? next.delete(key) : next.set(key, value)
        );
        return next;
      },
      { state: preserveOverviewState(location.state) }
    );
  useEffect(() => setSearchInput(search), [search]);
  useEffect(() => {
    if (isRecord) return;
    const controller = new AbortController();
    setLoading(true);
    setFailed(false);
    setGraph(undefined);
    setRecords(undefined);
    unsGraphApi
      .graph(
        {
          mode: isFocus ? 'focus' : 'overview',
          targets: isFocus ? [{ kind: kind!, id }] : undefined,
          kinds: kinds.length ? kinds : undefined,
          search: search || undefined,
          page,
          size: 100,
          includeArchived: archived,
        },
        controller.signal
      )
      .then((result) => {
        if (!controller.signal.aborted) setGraph(result);
      })
      .catch(() => {
        if (!controller.signal.aborted) setFailed(true);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [isRecord, isFocus, kind, id, page, kinds, search, archived, refresh]);
  const collectionId =
    graph?.collections.find((collection) => collection.collectionId === recordCollection)?.collectionId ||
    graph?.collections[0]?.collectionId ||
    '';
  const focus = graph?.nodes.find((node) => node.kind === kind && node.id === id);
  const scopedRecords = useMemo(() => {
    if (focus?.kind !== 'topic' || focus.data.viewMode !== 'fields' || !Array.isArray(focus.data.fields))
      return undefined;
    return [
      ...new Set(
        focus.data.fields.flatMap((value: unknown) => {
          if (!value || typeof value !== 'object') return [];
          const field = value as Record<string, unknown>;
          return field.collectionId === collectionId && typeof field.recordId === 'string' ? [field.recordId] : [];
        })
      ),
    ];
  }, [focus, collectionId]);
  const scopeKey = scopedRecords?.join('|');
  useEffect(() => {
    if (isRecord || !isFocus || !collectionId) {
      setRecords(undefined);
      return;
    }
    const controller = new AbortController();
    setRecords(undefined);
    setRecordsLoading(true);
    setRecordsFailed(false);
    unsGraphApi
      .records(collectionId, recordPage, controller.signal, scopedRecords)
      .then((value) => {
        if (!controller.signal.aborted) setRecords(value);
      })
      .catch(() => {
        if (!controller.signal.aborted) setRecordsFailed(true);
      })
      .finally(() => {
        if (!controller.signal.aborted) setRecordsLoading(false);
      });
    return () => controller.abort();
  }, [collectionId, recordPage, isRecord, isFocus, refresh, scopedRecords, scopeKey]);
  const metas = useMemo(() => {
    const merged = new Map(graph?.nodes.map((node) => [node.key, node]) || []);
    if (isFocus) graph?.collections.forEach((collection) => merged.set(collection.key, collection));
    return [...merged.values()];
  }, [graph, isFocus]);
  const samples = useMemo(
    () =>
      Object.fromEntries(
        records?.latestSamples.map(({ recordId: record, sample }) => [recordNodeKey(collectionId, record), !!sample]) ||
          []
      ),
    [records, collectionId]
  );
  const shownKinds = [...new Set(metas.map((node) => node.kind))];
  const open = (data: SphereData) => {
    setInformation(undefined);
    let changes: Record<string, string | undefined>;
    if (data.meta) {
      if (data.meta.kind === kind && data.meta.id === id) {
        setInformation(data);
        return;
      }
      changes = {
        kind: data.meta.kind,
        id: data.meta.id,
        recordId: undefined,
        collectionId: undefined,
        page: undefined,
        recordPage: undefined,
        samplePage: undefined,
        time: undefined,
        from: undefined,
        to: undefined,
      };
    } else if (data.record)
      changes = {
        collectionId: data.record.collectionId,
        recordId: data.record.id,
        samplePage: undefined,
        time: undefined,
        from: undefined,
        to: undefined,
      };
    else return;
    const destination = enterOverviewObject(location.search, changes, location.state);
    navigate(relationshipGraphLocation(destination.search), { state: destination.state });
  };
  const goBack = (target: 'previous' | 'overview' | 'focus' = 'previous') => {
    const destination = overviewParent(location.search, location.state, target);
    setInformation(undefined);
    navigate(relationshipGraphLocation(destination.search), { state: destination.state });
  };
  const detail = information?.meta
    ? detailLocation(information.meta.kind, information.meta.id)
    : information?.record
      ? `/uns?${new URLSearchParams({ view: 'collections', collectionId: information.record.collectionId, recordId: information.record.id })}`
      : undefined;
  return (
    <main className={styles.workspace}>
      {(isFocus || isRecord) && (
        <header className={styles.header}>
          <Breadcrumb
            items={[
              {
                title: (
                  <button
                    className={styles['breadcrumb-link']}
                    onClick={() => {
                      if (isFocus || isRecord) goBack('overview');
                    }}
                  >
                    {t('title')}
                  </button>
                ),
              },
              ...(isFocus
                ? [
                    {
                      title: (
                        <button
                          className={styles['breadcrumb-link']}
                          onClick={() => {
                            if (isRecord) goBack('focus');
                          }}
                        >
                          {focus?.name || id}
                        </button>
                      ),
                    },
                  ]
                : []),
              ...(isRecord ? [{ title: recordId }] : []),
            ]}
          />
          <Space>
            {(isFocus || isRecord) && (
              <ComButton icon={<ArrowLeft />} onClick={() => goBack()}>
                {t('back')}
              </ComButton>
            )}
            {focus && !isRecord && (
              <ComButton
                icon={<Information />}
                onClick={() => setInformation({ meta: focus, kind: focus.kind, name: focus.name })}
              >
                {t('information')}
              </ComButton>
            )}
            <ComButton icon={<Renew />} onClick={() => setRefresh((value) => value + 1)} aria-label={t('refresh')} />
          </Space>
        </header>
      )}
      {!isRecord && (
        <div className={styles.toolbar}>
          <Input.Search
            value={searchInput}
            onChange={(event) => setSearchInput(event.target.value)}
            allowClear
            placeholder={t('search')}
            onSearch={(value) => change({ search: value || undefined, page: undefined })}
            className={styles.search}
          />
          <Select
            mode="multiple"
            maxTagCount="responsive"
            aria-label={t('kinds')}
            placeholder={t('kinds')}
            value={kinds}
            options={graphKinds.map((value) => ({ value, label: t(`kind.${value}`) }))}
            onChange={(value: GraphKind[]) => change({ kinds: value.join(','), page: undefined })}
            className={styles['kind-select']}
          />
          <Checkbox
            checked={archived}
            onChange={(event) => change({ archived: event.target.checked ? 'true' : undefined, page: undefined })}
          >
            {t('archived')}
          </Checkbox>
          {!isFocus && (
            <ComButton icon={<Renew />} onClick={() => setRefresh((value) => value + 1)} aria-label={t('refresh')} />
          )}
        </div>
      )}
      {!isRecord && isFocus && !loading && !failed && !!graph?.collections.length && (
        <div className={styles['record-controls']}>
          <Select
            aria-label={t('chooseCollection')}
            value={collectionId}
            options={graph.collections.map((collection) => ({
              value: collection.collectionId,
              label: collection.name || collection.collectionId,
            }))}
            onChange={(value) => change({ collectionId: value, recordPage: undefined })}
          />
          <span>
            {t('collectionGroup', {
              current: graph.collections.findIndex((collection) => collection.collectionId === collectionId) + 1,
              total: graph.collections.length,
            })}
          </span>
          <span>
            {t('records')} · {recordsLoading ? <Spin size="small" /> : (records?.total ?? 0)}
          </span>
          {records && (
            <Pagination
              size="small"
              current={recordPage}
              pageSize={50}
              total={records.total}
              showSizeChanger={false}
              onChange={(value) => change({ recordPage: String(value) })}
            />
          )}
          {recordsFailed && (
            <ComButton danger onClick={() => setRefresh((value) => value + 1)}>
              {t('loadFailed')}
            </ComButton>
          )}
          {records?.historyFailed && <span className={styles.muted}>{t('historyFailed')}</span>}
        </div>
      )}
      {isRecord ? (
        <SampleHistory collectionId={recordCollection} recordId={recordId} refresh={refresh} />
      ) : (
        <div className={styles['graph-workspace']}>
          {failed ? (
            <div className={styles['center-state']}>
              <Alert
                type="error"
                message={t('loadFailed')}
                action={<ComButton onClick={() => setRefresh((value) => value + 1)}>{t('retry')}</ComButton>}
              />
            </div>
          ) : loading ? (
            <div className={styles['center-state']}>
              <Spin />
            </div>
          ) : !metas.length ? (
            <div className={styles['center-state']}>
              <Empty description={t('empty')} />
            </div>
          ) : (
            <>
              <GraphCanvas
                key={`${isFocus ? `${kind}:${id}` : 'overview'}:${page}:${kindsValue}:${search}:${collectionId}:${recordPage}`}
                cacheKey={`${isFocus ? `${kind}:${id}` : 'overview'}:${page}:${kindsValue}:${search}:${collectionId}:${recordPage}:${archived}`}
                metas={metas}
                relations={graph?.edges || []}
                records={records?.list || []}
                samples={samples}
                historyFailed={records?.historyFailed}
                focus={focus?.key}
                onOpen={open}
                onInformation={setInformation}
              />
              <div className={styles.legend}>
                <div className={styles['legend-kinds']}>
                  {shownKinds.map((value) => (
                    <span key={value}>
                      <i style={{ background: `var(--ui-graph-${value.replace(/_/g, '-')})` }} />
                      {t(`kind.${value}`)}
                    </span>
                  ))}
                  {records?.list.length ? (
                    <span>
                      <i style={{ background: 'var(--ui-graph-record)' }} />
                      {t('kind.record')}
                    </span>
                  ) : null}
                </div>
                <span>{t('shown', { shown: graph?.nodes.length || 0, total: graph?.total || 0 })}</span>
                {!!graph?.unloadedRelations && <span>{t('unloaded', { count: graph.unloadedRelations })}</span>}
                <Pagination
                  size="small"
                  current={page}
                  pageSize={isFocus ? 99 : 100}
                  total={isFocus ? Math.max(0, (graph?.total || 0) - 1) : graph?.total || 0}
                  showSizeChanger={false}
                  onChange={(value) => change({ page: String(value) })}
                />
              </div>
            </>
          )}
        </div>
      )}
      <ComDrawer
        title={information?.name || t('information')}
        open={!!information}
        onClose={() => setInformation(undefined)}
        width="min(520px, 100vw)"
      >
        {information && (
          <div className={styles['information-panel']}>
            <Tag>{t(`kind.${information.kind}`)}</Tag>
            <p className={styles['object-id']}>{information.meta?.id || information.record?.id}</p>
            {information.meta && (
              <p>
                {t('revision')}: {information.meta.revision} ·{' '}
                {t(information.meta.archived ? 'archivedState' : 'active')}
              </p>
            )}
            <pre className={styles.code}>{JSON.stringify(information.meta?.data || information.record, null, 2)}</pre>
            {detail && <ComButton onClick={() => navigate(detail)}>{t('openDetail')}</ComButton>}
          </div>
        )}
      </ComDrawer>
    </main>
  );
};

const UnsOverview: FC = () => {
  return (
    <div className={styles.workspace}>
      <OverviewWorkspace />
    </div>
  );
};
export default UnsOverview;
