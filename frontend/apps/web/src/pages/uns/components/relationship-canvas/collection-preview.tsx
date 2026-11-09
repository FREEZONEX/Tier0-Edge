import { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Empty, Spin, Tooltip } from 'antd';
import { ArrowLeft, Close, Renew } from '@carbon/icons-react';
import { useTranslate } from '@/hooks';
import {
  topologyRecords,
  topologyLatestSample,
  type TopologyMeta,
  type TopologyRecords,
} from '@/apis/core-api/uns-topology';
import type { UnsRecord, UnsSample } from '@/apis/core-api/uns-metadata';
import { displayTime, displayValue, recordName } from '../metadata/data-values';
import styles from './index.module.scss';

interface Props {
  collection: TopologyMeta;
  refresh: number;
  onClose: () => void;
  onOpen: () => void;
  onCount: (id: string, count: number) => void;
}

/** 固定展示最近更新的 20 条业务记录；采样标签进入该记录的最新值。 */
export default function CollectionPreview({ collection, refresh, onClose, onOpen, onCount }: Props) {
  const t = useTranslate();
  const valueLabel = (value: unknown) =>
    value !== null && typeof value === 'object'
      ? t(Array.isArray(value) ? 'UnsMeta.arrayValue' : 'UnsMeta.objectValue', { count: Object.keys(value).length })
      : displayValue(value);
  const [records, setRecords] = useState<TopologyRecords>();
  const [record, setRecord] = useState<UnsRecord>();
  const [sample, setSample] = useState<UnsSample | null>();
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const [sampleBusy, setSampleBusy] = useState(false);
  const [sampleFailed, setSampleFailed] = useState(false);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setBusy(true);
    setFailed(false);
    topologyRecords(collection, 1, '', controller.signal)
      .then((result) => {
        if (controller.signal.aborted) return;
        setRecords(result);
        onCount(collection.id, result.total);
      })
      .catch(() => {
        if (!controller.signal.aborted) setFailed(true);
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [collection, refresh, retry, onCount]);
  useEffect(() => {
    if (!record) return;
    const controller = new AbortController();
    setSample(undefined);
    setSampleBusy(true);
    setSampleFailed(false);
    topologyLatestSample(collection.id, record.id, controller.signal)
      .then((result) => {
        if (!controller.signal.aborted) setSample(result);
      })
      .catch(() => {
        if (!controller.signal.aborted) setSampleFailed(true);
      })
      .finally(() => {
        if (!controller.signal.aborted) setSampleBusy(false);
      });
    return () => controller.abort();
  }, [collection.id, record, refresh, retry]);
  const fields = useMemo(
    () =>
      [...new Set(records?.list.flatMap((row) => Object.keys(row.data)) || [])]
        .filter((key) => !['name', 'displayName', 'title'].includes(key))
        .slice(0, 2),
    [records]
  );
  const samples = useMemo(
    () => new Map(records?.latestSamples?.map((entry) => [entry.recordId, entry.sample])),
    [records]
  );
  const hasSamples = [...samples.values()].some(Boolean);
  const grid = {
    gridTemplateColumns: `minmax(0, 1.2fr)${fields.length ? ` repeat(${fields.length}, minmax(0, 1fr))` : ''}${hasSamples ? ' 58px' : ''}`,
  };
  const retryButton = (
    <Button size="small" onClick={() => setRetry((value) => value + 1)}>
      {t('UnsOverview.retry')}
    </Button>
  );
  return (
    <aside className={styles['collection-preview']} aria-label={t('UnsGraph.dataPreview')}>
      <header className={styles['preview-title']}>
        <strong title={collection.data.name}>{collection.data.name}</strong>
        <Button
          type="text"
          size="small"
          icon={<Renew size={16} />}
          aria-label={t('UnsGraph.refresh')}
          onClick={() => setRetry((value) => value + 1)}
        />
        <Button
          type="text"
          size="small"
          icon={<Close size={18} />}
          aria-label={t('UnsGraph.close')}
          onClick={onClose}
        />
      </header>
      <div className={styles['compact-preview']}>
        {record ? (
          <>
            <div className={styles['compact-preview-heading']}>
              <Button type="text" size="small" icon={<ArrowLeft size={16} />} onClick={() => setRecord(undefined)}>
                {t('UnsGraph.latestRecords')}
              </Button>
              <span>{t('UnsGraph.latestSample')}</span>
            </div>
            <strong className={styles['sample-record-name']} title={recordName(record)}>
              {recordName(record)}
            </strong>
            {sampleBusy ? (
              <Spin />
            ) : sampleFailed ? (
              <Alert type="error" message={t('UnsGraph.previewFailed')} action={retryButton} />
            ) : sample ? (
              <>
                <time className={styles['preview-muted']}>{displayTime(sample.time)}</time>
                <div className={styles['latest-sample-fields']}>
                  {Object.entries(sample.data).map(([key, value]) => (
                    <div key={key}>
                      <span title={key}>{collection.data.sampleSchema?.properties?.[key]?.title || key}</span>
                      <strong title={displayValue(value)}>
                        {valueLabel(value)} {collection.data.sampleSchema?.properties?.[key]?.unit}
                      </strong>
                    </div>
                  ))}
                </div>
                {!!sample.quality && (
                  <small className={styles['preview-muted']}>
                    {t('UnsMeta.quality')}: {sample.quality}
                  </small>
                )}
              </>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('Collections.noSamples')} />
            )}
          </>
        ) : (
          <>
            <div className={styles['compact-preview-heading']}>
              <strong>{t('UnsGraph.latestRecords')}</strong>
              <small>
                {records?.list.length ?? 0} / {records?.total ?? '—'}
              </small>
            </div>
            {busy ? (
              <Spin />
            ) : failed ? (
              <Alert type="error" message={t('UnsGraph.previewFailed')} action={retryButton} />
            ) : !records?.list.length ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('UnsGraph.empty')} />
            ) : (
              <div className={styles['latest-records']} role="table" aria-label={t('UnsGraph.latestRecords')}>
                <div className={styles['latest-record-head']} role="row" style={grid}>
                  <span role="columnheader">{t('UnsUi.recordData')}</span>
                  {fields.map((key) => (
                    <span role="columnheader" key={key} title={key}>
                      {collection.data.recordSchema?.properties?.[key]?.title || key}
                    </span>
                  ))}
                  {hasSamples && <span role="columnheader">{t('UnsGraph.sample')}</span>}
                </div>
                <div className={styles['latest-record-body']}>
                  {records.list.map((row) => (
                    <Tooltip
                      key={row.id}
                      title={<pre className={styles['record-tooltip']}>{JSON.stringify(row.data, null, 2)}</pre>}
                      placement="left"
                    >
                      <div className={styles['latest-record-row']} role="row" style={grid}>
                        <span role="cell" title={`${row.id} · ${displayTime(row.updatedAt)}`}>
                          {recordName(row)}
                        </span>
                        {fields.map((key) => (
                          <span role="cell" key={key} title={displayValue(row.data[key])}>
                            {valueLabel(row.data[key])}
                          </span>
                        ))}
                        {hasSamples && (
                          <span role="cell">
                            {samples.get(row.id) && (
                              <button className={styles['sample-label']} onClick={() => setRecord(row)}>
                                {t('UnsGraph.sample')}
                              </button>
                            )}
                          </span>
                        )}
                      </div>
                    </Tooltip>
                  ))}
                </div>
              </div>
            )}
          </>
        )}
      </div>
      <footer className={styles['preview-footer']}>
        <Button size="small" onClick={onOpen}>
          {t('UnsGraph.fullData')}
        </Button>
      </footer>
    </aside>
  );
}
