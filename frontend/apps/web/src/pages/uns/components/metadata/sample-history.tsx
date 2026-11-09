import { useEffect, useState } from 'react';
import { Alert, DatePicker, Empty, Select, Space, Spin, Tag } from 'antd';
import { Renew } from '@carbon/icons-react';
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import dayjs from 'dayjs';
import ComButton from '@/components/com-button';
import ProTable from '@/components/pro-table';
import { usePagination, useTranslate } from '@/hooks';
import { metadataApi, type UnsMetadata, type UnsSample } from '@/apis/core-api/uns-metadata';
import { DataValue, displayTime, FieldsTable } from './data-values';
import styles from './index.module.scss';

type Query = { pageNo: number; pageSize: number; startTime?: number; endTime?: number };
export default function SampleHistory({
  collectionId,
  recordId,
  collection,
  compact = false,
  readSamples = metadataApi.samples,
}: {
  collectionId: string;
  recordId: string;
  collection?: UnsMetadata;
  compact?: boolean;
  readSamples?: typeof metadataApi.samples;
}) {
  const t = useTranslate();
  const [failed, setFailed] = useState(false);
  const [field, setField] = useState<string>();
  const [refresh, setRefresh] = useState(0);
  const identity = `${collectionId}/${recordId}/${refresh}`;
  const [latest, setLatest] = useState<{ identity: string; sample?: UnsSample; failed?: boolean }>();
  useEffect(() => {
    const controller = new AbortController();
    readSamples(collectionId, { recordId, page: 1, size: 1 }, controller.signal)
      .then((page) => {
        if (!controller.signal.aborted) setLatest({ identity, sample: page.list[0] });
      })
      .catch(() => {
        if (!controller.signal.aborted) setLatest({ identity, failed: true });
      });
    return () => controller.abort();
  }, [collectionId, recordId, identity, readSamples]);
  const { data, loading, pagination, setSearchParams, refreshRequest } = usePagination<UnsSample>({
    fetchApi: async ({ pageNo, pageSize, ...params }: Query, { signal }: { signal: AbortSignal }) => {
      try {
        const result = await readSamples(collectionId, { ...params, recordId, page: pageNo, size: pageSize }, signal);
        if (!signal.aborted) setFailed(false);
        return { data: result.list, total: result.total, pageNo: result.page };
      } catch (error) {
        if (!signal.aborted) setFailed(true);
        throw error;
      }
    },
  });
  const properties = collection?.data.sampleSchema?.properties || {};
  const fields = [...new Set([...Object.keys(properties), ...data.flatMap((sample) => Object.keys(sample.data))])];
  const numeric = fields.filter(
    (name) =>
      ['number', 'integer'].includes(properties[name]?.type) ||
      data.some((sample) => typeof sample.data[name] === 'number')
  );
  const selectedField = field && numeric.includes(field) ? field : numeric[0];
  const unit = properties[selectedField]?.unit;
  const chartData = [...data]
    .sort((a, b) => a.time - b.time)
    .map((sample) => ({
      time: sample.time,
      value: typeof sample.data[selectedField] === 'number' ? sample.data[selectedField] : null,
    }));
  const current = latest?.identity === identity ? latest.sample : undefined;
  return (
    <section className={`${styles['sample-section']} ${compact ? styles['sample-compact'] : ''}`}>
      <div className={styles['data-heading']}>
        <h3>{t('UnsMeta.currentSamples')}</h3>
        <ComButton
          icon={<Renew />}
          onClick={() => {
            setRefresh((value) => value + 1);
            refreshRequest();
          }}
        >
          {t('UnsMeta.refresh')}
        </ComButton>
      </div>
      <Spin spinning={latest?.identity !== identity}>
        {latest?.identity === identity &&
          (latest.failed ? (
            <Alert type="error" message={t('UnsMeta.loadFailed')} />
          ) : current ? (
            <>
              <div className={styles['sample-metrics']}>
                {Object.entries(current.data)
                  .filter(([, value]) => compact || typeof value === 'number')
                  .slice(0, 4)
                  .map(([name, value]) => (
                    <div className={styles['sample-metric']} key={name}>
                      <span>{properties[name]?.title || name}</span>
                      <strong>
                        <DataValue value={value} unit={properties[name]?.unit} />
                      </strong>
                    </div>
                  ))}
              </div>
              <Space className={styles['sample-stamp']} wrap>
                <Tag>{current.quality || '—'}</Tag>
                <span>
                  {t('UnsMeta.eventTime')} · {displayTime(current.time)}
                </span>
                <span>{t('UnsMeta.latestUnfiltered')}</span>
              </Space>
              {!compact && (
                <FieldsTable
                  data={current.data}
                  schema={collection?.data.sampleSchema}
                  origin="Sample"
                  time={current.time}
                />
              )}
            </>
          ) : (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('UnsMeta.noSamples')} />
          ))}
      </Spin>
      <div className={styles['data-heading']}>
        <h3>{t('UnsMeta.recentSamples')}</h3>
        <span className={styles.hint}>{t('UnsMeta.sampleCount', { count: pagination.total })}</span>
      </div>
      <div className={styles['sample-filters']}>
        <DatePicker.RangePicker
          showTime
          onChange={(range) => setSearchParams({ startTime: range?.[0]?.valueOf(), endTime: range?.[1]?.valueOf() })}
        />
        {numeric.length > 0 && (
          <Select
            aria-label={t('UnsMeta.trendField')}
            value={selectedField}
            onChange={setField}
            options={numeric.map((name) => ({ value: name, label: properties[name]?.title || name }))}
          />
        )}
      </div>
      {failed && <Alert type="error" message={t('UnsMeta.loadFailed')} />}
      <div className={compact ? styles['sample-history-grid'] : undefined}>
        {!failed && selectedField && data.length > 0 && (
          <div className={styles['trend-panel']}>
            <div className={styles['data-heading']}>
              <strong>
                {properties[selectedField]?.title || selectedField}
                {unit ? ` (${unit})` : ''}
              </strong>
              <span className={styles.hint}>{t('UnsMeta.pageTrend')}</span>
            </div>
            <div className={styles['trend-chart']} role="img" aria-label={t('UnsMeta.trendField')}>
              <ResponsiveContainer width="100%" height="100%">
                <LineChart data={chartData} margin={{ top: 8, right: 20, left: 0, bottom: 4 }}>
                  <CartesianGrid stroke="var(--ui-line-color)" vertical={false} />
                  <XAxis
                    dataKey="time"
                    type="number"
                    domain={['dataMin', 'dataMax']}
                    tickFormatter={(value) => dayjs(value).format('HH:mm:ss')}
                    minTickGap={48}
                    stroke="var(--ui-text-color)"
                  />
                  <YAxis width={60} domain={['auto', 'auto']} stroke="var(--ui-text-color)" />
                  <Tooltip
                    labelFormatter={(value) => displayTime(Number(value))}
                    contentStyle={{ background: 'var(--ui-bg-color)', borderColor: 'var(--ui-line-color)' }}
                  />
                  <Line
                    dataKey="value"
                    name={properties[selectedField]?.title || selectedField}
                    unit={unit}
                    stroke="var(--ui-theme-color)"
                    strokeWidth={2}
                    dot={data.length < 2}
                    isAnimationActive={false}
                    connectNulls={false}
                  />
                </LineChart>
              </ResponsiveContainer>
            </div>
          </div>
        )}
        <ProTable
          rowKey="id"
          size="small"
          columnFit={false}
          dataSource={failed ? [] : data}
          loading={loading}
          pagination={{
            current: pagination.page,
            pageSize: pagination.pageSize,
            total: pagination.total,
            onChange: (page, pageSize) => pagination.onChange({ page, pageSize }),
            showSizeChanger: true,
          }}
          scroll={{ x: 500 + fields.length * 160, ...(compact ? { y: 280 } : {}) }}
          columns={[
            { title: t('UnsMeta.eventTime'), dataIndex: 'time', width: 220, render: displayTime },
            ...fields.map((name) => ({
              title: properties[name]?.title || name,
              key: name,
              width: 160,
              render: (_: unknown, sample: UnsSample) => (
                <DataValue value={sample.data[name]} unit={properties[name]?.unit} />
              ),
            })),
            {
              title: t('UnsMeta.quality'),
              dataIndex: 'quality',
              width: 100,
              render: (quality: string) => <Tag>{quality || '—'}</Tag>,
            },
            { title: t('UnsMeta.receivedAt'), dataIndex: 'receivedAt', width: 220, render: displayTime },
          ]}
          expandable={{
            expandedRowRender: (sample) => <pre className={styles.code}>{JSON.stringify(sample, null, 2)}</pre>,
          }}
        />
      </div>
    </section>
  );
}
