import { useEffect, useMemo, useState, type FC } from 'react';
import { Alert, DatePicker, Empty, Select, Space, Spin, Tabs } from 'antd';
import { useLocation, useSearchParams } from 'react-router';
import dayjs from 'dayjs';
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip as ChartTooltip, XAxis, YAxis } from 'recharts';
import ComButton from '@/components/com-button';
import ComDrawer from '@/components/com-drawer';
import ProTable from '@/components/pro-table';
import { useTranslate } from '@/hooks';
import { unsGraphApi } from '@/apis/core-api/uns-graph';
import type { Page, UnsRecord, UnsSample } from '@/apis/core-api/uns-metadata';
import { displayTime } from '@/pages/uns/components/metadata/data-values';
import { numericPaths, numericValue } from '../graph-layout';
import { preserveOverviewState } from '../navigation';
import styles from '../index.module.scss';

interface SampleHistoryProps {
  collectionId: string;
  recordId: string;
  refresh: number;
}
const SampleHistory: FC<SampleHistoryProps> = ({ collectionId, recordId, refresh }) => {
  const t = useTranslate('UnsOverview');
  const [params, setParams] = useSearchParams();
  const location = useLocation();
  const [record, setRecord] = useState<UnsRecord>();
  const [samples, setSamples] = useState<Page<UnsSample>>();
  const [hasHistory, setHasHistory] = useState<boolean>();
  const [samplesEnabled, setSamplesEnabled] = useState<boolean>();
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [retry, setRetry] = useState(0);
  const [selected, setSelected] = useState<UnsSample>();
  const [trend, setTrend] = useState(false);
  const [field, setField] = useState<string>();
  const [clock, setClock] = useState(() => Date.now());
  const mode = ['24h', 'all', 'custom'].includes(params.get('time') || '') ? params.get('time')! : '24h';
  const page = Math.max(1, Number(params.get('samplePage')) || 1);
  const from = params.get('from');
  const to = params.get('to');
  useEffect(() => setClock(Date.now()), [refresh, retry, mode]);
  const range = useMemo(() => {
    if (mode === 'custom' && Number(from) >= 0 && Number(to) > Number(from))
      return { from: Number(from), to: Number(to) };
    return { from: mode === 'all' ? 0 : clock - 86400000, to: clock + 1 };
  }, [mode, from, to, clock]);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setFailed(false);
    setSelected(undefined);
    setSamplesEnabled(undefined);
    unsGraphApi
      .recordHistory(collectionId, recordId, { startTime: range.from, endTime: range.to, page }, controller.signal)
      .then(({ record: current, history, hasHistory: available, samplesEnabled: enabled }) => {
        if (controller.signal.aborted) return;
        setRecord(current);
        setSamples(history);
        setHasHistory(available);
        setSamplesEnabled(enabled);
      })
      .catch(() => {
        if (!controller.signal.aborted) setFailed(true);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [collectionId, recordId, page, range, refresh, retry]);
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
  const fields = useMemo(
    () => [...new Set((samples?.list || []).flatMap((sample) => numericPaths(sample.data)))],
    [samples]
  );
  const actualField = field && fields.includes(field) ? field : fields[0];
  const points = useMemo(
    () =>
      [...(samples?.list || [])]
        .sort((a, b) => a.time - b.time)
        .map((sample) => ({ time: sample.time, value: actualField ? numericValue(sample.data, actualField) : null })),
    [samples, actualField]
  );
  return (
    <section className={styles['sample-page']}>
      <div className={styles['history-toolbar']}>
        <Space wrap>
          <Select
            disabled={samplesEnabled === false}
            aria-label={t('timeRange')}
            value={mode}
            onChange={(time) => change({ time, samplePage: undefined })}
            options={[
              { value: '24h', label: t('last24h') },
              { value: 'all', label: t('allTime') },
              { value: 'custom', label: t('custom') },
            ]}
          />
          {mode === 'custom' && (
            <DatePicker.RangePicker
              disabled={samplesEnabled === false}
              showTime
              allowClear={false}
              value={[dayjs(range.from), dayjs(range.to)]}
              onChange={(value) => {
                if (value?.[0] && value[1])
                  change({ from: String(value[0].valueOf()), to: String(value[1].valueOf()), samplePage: undefined });
              }}
            />
          )}
        </Space>
        <ComButton onClick={() => setRetry((value) => value + 1)}>{t('refresh')}</ComButton>
      </div>
      {failed ? (
        <Alert
          type="error"
          message={t('loadFailed')}
          action={<ComButton onClick={() => setRetry((value) => value + 1)}>{t('retry')}</ComButton>}
        />
      ) : loading ? (
        <div className={styles['center-state']}>
          <Spin />
        </div>
      ) : (
        <>
          <details className={styles['current-record']} open>
            <summary>
              {t('current')} · {recordId}
            </summary>
            {record ? (
              <pre className={styles.code}>{JSON.stringify(record.data, null, 2)}</pre>
            ) : (
              <Alert type="info" message={t('noRecord')} />
            )}
          </details>
          <h3>{t('history')}</h3>
          {!samples?.list.length ? (
            <Empty description={t(hasHistory ? 'emptyRange' : 'noSamples')} />
          ) : (
            <>
              <div className={styles['sample-timeline']} aria-label={t('history')}>
                {samples.list.map((sample) => (
                  <button
                    key={sample.id}
                    onClick={() => setSelected(sample)}
                    title={`${displayTime(sample.time)} · ${sample.quality}`}
                    aria-label={`${t('sampleDetail')} ${displayTime(sample.time)}`}
                  >
                    <span />
                    <small>{dayjs(sample.time).format('HH:mm:ss')}</small>
                  </button>
                ))}
              </div>
              <Tabs
                activeKey={trend ? 'trend' : 'table'}
                onChange={(key) => setTrend(key === 'trend')}
                items={[
                  { key: 'table', label: t('data') },
                  { key: 'trend', label: t('trend'), disabled: !fields.length },
                ]}
              />
              {trend ? (
                <div className={styles['trend-panel']}>
                  <Space wrap>
                    <Select
                      aria-label={t('trendField')}
                      value={actualField}
                      options={fields.map((value) => ({ value, label: value }))}
                      onChange={setField}
                    />
                    <span>{t('pageTrend')}</span>
                  </Space>
                  <div className={styles['trend-chart']}>
                    <ResponsiveContainer width="100%" height="100%">
                      <LineChart data={points}>
                        <CartesianGrid stroke="var(--ui-table-tr-color)" strokeDasharray="3 3" />
                        <XAxis
                          dataKey="time"
                          type="number"
                          domain={['dataMin', 'dataMax']}
                          tickFormatter={(value: number) => dayjs(value).format('HH:mm:ss')}
                          stroke="var(--ui-text-color)"
                        />
                        <YAxis stroke="var(--ui-text-color)" />
                        <ChartTooltip
                          labelFormatter={(value) => displayTime(Number(value))}
                          contentStyle={{ background: 'var(--ui-bg-color)', borderColor: 'var(--ui-table-tr-color)' }}
                        />
                        <Line
                          type="linear"
                          dataKey="value"
                          name={actualField}
                          stroke="var(--ui-theme-color)"
                          isAnimationActive={false}
                          connectNulls={false}
                        />
                      </LineChart>
                    </ResponsiveContainer>
                  </div>
                </div>
              ) : (
                <ProTable
                  rowKey="id"
                  dataSource={samples.list}
                  size="small"
                  columnFit={false}
                  resizeable={false}
                  scroll={{ x: 760 }}
                  onRow={(row) => ({ onClick: () => setSelected(row) })}
                  columns={[
                    { title: t('dataTime'), width: 220, render: (_, row: UnsSample) => displayTime(row.time) },
                    { title: t('receivedAt'), width: 220, render: (_, row: UnsSample) => displayTime(row.receivedAt) },
                    { title: t('quality'), dataIndex: 'quality', width: 100 },
                    { title: t('data'), ellipsis: true, render: (_, row: UnsSample) => JSON.stringify(row.data) },
                  ]}
                  pagination={{
                    current: page,
                    pageSize: 50,
                    total: samples.total,
                    showSizeChanger: false,
                    onChange: (samplePage) => change({ samplePage: String(samplePage) }),
                  }}
                />
              )}
              {trend && (
                <div className={styles['trend-pagination']}>
                  <ComButton disabled={page <= 1} onClick={() => change({ samplePage: String(page - 1) })}>
                    {t('previous')}
                  </ComButton>
                  <span>
                    {t('shown', { shown: samples.list.length, total: samples.total })} · {page}
                  </span>
                  <ComButton
                    disabled={page * 50 >= samples.total}
                    onClick={() => change({ samplePage: String(page + 1) })}
                  >
                    {t('next')}
                  </ComButton>
                </div>
              )}
            </>
          )}
        </>
      )}
      <ComDrawer
        title={t('sampleDetail')}
        open={!!selected}
        onClose={() => setSelected(undefined)}
        width="min(560px, 100vw)"
      >
        {selected && (
          <>
            <p>
              {displayTime(selected.time)} · {selected.quality}
            </p>
            <pre className={styles.code}>{JSON.stringify(selected, null, 2)}</pre>
          </>
        )}
      </ComDrawer>
    </section>
  );
};
export default SampleHistory;
