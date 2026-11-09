import { useState } from 'react';
import { Alert, DatePicker, Input } from 'antd';
import dayjs from 'dayjs';
import { Renew } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import ProTable from '@/components/pro-table';
import { usePagination, useTranslate } from '@/hooks';
import { type UnsMetadata, type UnsSample } from '@/apis/core-api/uns-metadata';
import { sampleHistory } from '@/apis/core-api/uns-v2';
import { DataValue, displayTime } from '@/pages/uns/components/metadata/data-values';
import styles from '../index.module.scss';
type PageQuery = { pageNo: number; pageSize: number; search?: string; timeRange: { from: number; to: number } };
export default function Samples({ collection, inspect = false }: { collection: UnsMetadata; inspect?: boolean }) {
  const t = useTranslate();
  const [recordId, setRecordId] = useState('');
  const [failed, setFailed] = useState(false);
  const [range, setRange] = useState<[dayjs.Dayjs, dayjs.Dayjs]>(() => [dayjs().subtract(1, 'day'), dayjs()]);
  const { data, loading, pagination, setSearchParams, refreshRequest } = usePagination<UnsSample>({
    defaultParams: { timeRange: { from: range[0].valueOf(), to: range[1].valueOf() } },
    fetchApi: async ({ pageNo, pageSize, search, timeRange }: PageQuery, { signal }: { signal: AbortSignal }) => {
      try {
        const response = await sampleHistory(
          collection.id,
          search?.trim() || '',
          {
            startTime: timeRange.from,
            endTime: timeRange.to,
            page: pageNo,
            size: pageSize,
          },
          signal
        );
        if (!signal.aborted) setFailed(false);
        return { data: response.list, total: response.total, pageNo };
      } catch (error) {
        if (!signal.aborted) setFailed(true);
        throw error;
      }
    },
  });
  return (
    <>
      <div className={styles.toolbar}>
        <Input.Search
          allowClear
          value={recordId}
          onChange={(event) => setRecordId(event.target.value)}
          placeholder={t('Collections.filterRecord')}
          onSearch={(search) =>
            setSearchParams({ search, timeRange: { from: range[0].valueOf(), to: range[1].valueOf() } })
          }
        />
        <DatePicker.RangePicker
          showTime
          allowClear={false}
          presets={[
            { label: t('auditLog.filter.last24h'), value: () => [dayjs().subtract(24, 'hour'), dayjs()] },
            { label: t('auditLog.filter.last7d'), value: () => [dayjs().subtract(7, 'day'), dayjs()] },
            { label: t('auditLog.filter.last30d'), value: () => [dayjs().subtract(30, 'day'), dayjs()] },
          ]}
          value={range}
          onChange={(value) => {
            if (value?.[0] && value[1]) {
              setRange([value[0], value[1]]);
              setSearchParams({ search: recordId, timeRange: { from: value[0].valueOf(), to: value[1].valueOf() } });
            }
          }}
        />
        <ComButton icon={<Renew />} onClick={() => refreshRequest()} aria-label={t('UnsMeta.refresh')} />
      </div>
      {failed && <Alert type="error" message={t('UnsMeta.loadFailed')} />}
      <ProTable
        rowKey={(row) => `${row.recordId}/${row.id}`}
        expandable={
          inspect
            ? {
                expandedRowRender: (row) => <pre className={styles.code}>{JSON.stringify(row, null, 2)}</pre>,
              }
            : undefined
        }
        dataSource={failed ? [] : data}
        size="small"
        columnFit={false}
        scroll={{ x: 700 + Object.keys(collection.data.sampleSchema?.properties || {}).length * 160 }}
        loading={loading}
        pagination={{
          current: pagination.page,
          pageSize: pagination.pageSize,
          total: pagination.total,
          onChange: (page, pageSize) => pagination.onChange({ page, pageSize }),
          showSizeChanger: true,
        }}
        columns={[
          { title: t('UnsMeta.recordId'), dataIndex: 'recordId' },
          { title: t('UnsMeta.dataTime'), render: (_, row) => displayTime(row.time) },
          ...Object.entries(collection.data.sampleSchema?.properties || {}).map(([name, field]) => ({
            title: field.title || name,
            render: (_: unknown, row: UnsSample) => <DataValue value={row.data[name]} unit={field.unit} />,
          })),
          { title: t('UnsMeta.quality'), dataIndex: 'quality' },
          { title: t('Collections.receivedAt'), render: (_, row) => displayTime(row.receivedAt) },
        ]}
      />
    </>
  );
}
