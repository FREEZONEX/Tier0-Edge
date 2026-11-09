import { useMemo, useState, type ReactNode } from 'react';
import { Input, Select } from 'antd';
import type { ColumnsType, TablePaginationConfig } from 'antd/es/table';
import type { FleetContainerSummary } from '@/apis/core-api/fleet';
import ProTable from '@/components/pro-table';
import { useTranslate } from '@/hooks';
import styles from './FleetContainerTable.module.scss';

const DEFAULT_PAGE_SIZE = 10;
const PAGE_SIZE_OPTIONS = [10, 20, 50];
const CONTAINER_STATE_OPTIONS = [
  { value: 'running', labelKey: 'fleet.runtime.stateRunning' },
  { value: 'restarting', labelKey: 'fleet.runtime.stateRestarting' },
  { value: 'exited', labelKey: 'fleet.runtime.stateExited' },
  { value: 'stopped', labelKey: 'fleet.runtime.stateStopped' },
  { value: 'unknown', labelKey: 'fleet.runtime.stateUnknown' },
] as const;
const CONTAINER_STATES = CONTAINER_STATE_OPTIONS.map(({ value }) => value);

const normalizeText = (value?: string) =>
  String(value || '')
    .trim()
    .toLocaleLowerCase();

const normalizeState = (value?: string) => {
  const state = normalizeText(value);
  return CONTAINER_STATES.includes(state as (typeof CONTAINER_STATES)[number]) ? state : 'unknown';
};

const containerDisplayName = (container: FleetContainerSummary) =>
  container.displayName || container.serviceName || container.containerID;

export const fleetContainerSorters = {
  service: (left: FleetContainerSummary, right: FleetContainerSummary) =>
    containerDisplayName(left).localeCompare(containerDisplayName(right)),
  image: (left: FleetContainerSummary, right: FleetContainerSummary) =>
    String(left.image || '').localeCompare(String(right.image || '')),
  state: (left: FleetContainerSummary, right: FleetContainerSummary) =>
    normalizeState(left.state).localeCompare(normalizeState(right.state)),
  startedAt: (left: FleetContainerSummary, right: FleetContainerSummary) =>
    Number(left.startedAt || 0) - Number(right.startedAt || 0),
  restartCount: (left: FleetContainerSummary, right: FleetContainerSummary) =>
    Number(left.restartCount || 0) - Number(right.restartCount || 0),
};

type FleetContainerTableProps = {
  columns: ColumnsType<FleetContainerSummary>;
  containers?: FleetContainerSummary[];
  emptyText: ReactNode;
  scrollX?: number;
};

export const FleetContainerTable = ({
  columns,
  containers = [],
  emptyText,
  scrollX = 920,
}: FleetContainerTableProps) => {
  const formatMessage = useTranslate();
  const [keyword, setKeyword] = useState('');
  const [state, setState] = useState('');
  const [deployment, setDeployment] = useState('current');
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE);
  const searchPlaceholder = formatMessage('fleet.runtime.searchPlaceholder', undefined, formatMessage('common.search'));
  const stateFilterLabel = formatMessage('fleet.runtime.stateFilter', undefined, formatMessage('common.status'));

  const filteredContainers = useMemo(() => {
    const normalizedKeyword = normalizeText(keyword);
    return containers.filter((container) => {
      if ((deployment === 'history') !== !!container.supersededBy) return false;
      if (state && normalizeState(container.state) !== state) {
        return false;
      }
      if (!normalizedKeyword) {
        return true;
      }
      return [
        container.containerName,
        container.displayName,
        container.serviceName,
        container.containerID,
        container.image,
        container.composeProject,
        container.composeService,
      ].some((value) => normalizeText(value).includes(normalizedKeyword));
    });
  }, [containers, keyword, state, deployment]);

  const lastPage = Math.max(1, Math.ceil(filteredContainers.length / pageSize));
  const currentPage = Math.min(page, lastPage);

  const handlePaginationChange = (pagination: TablePaginationConfig) => {
    const nextPageSize = pagination.pageSize || DEFAULT_PAGE_SIZE;
    setPageSize(nextPageSize);
    setPage(nextPageSize === pageSize ? pagination.current || 1 : 1);
  };

  return (
    <div className={styles.containerList}>
      <div className={styles.toolbar}>
        <Select
          className={styles.deploymentFilter}
          aria-label={formatMessage('fleet.runtime.deployment')}
          value={deployment}
          options={[
            {
              value: 'current',
              label: `${formatMessage('fleet.runtime.currentDeployment')} (${containers.filter((item) => !item.supersededBy).length})`,
            },
            {
              value: 'history',
              label: `${formatMessage('fleet.runtime.historyDeployment')} (${containers.filter((item) => item.supersededBy).length})`,
            },
          ]}
          onChange={(value) => {
            setDeployment(value);
            setPage(1);
          }}
        />
        <Input.Search
          allowClear
          className={styles.search}
          aria-label={searchPlaceholder}
          placeholder={searchPlaceholder}
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value);
            setPage(1);
          }}
        />
        <Select
          className={styles.stateFilter}
          aria-label={stateFilterLabel}
          value={state}
          options={[
            { value: '', label: formatMessage('common.all') },
            ...CONTAINER_STATE_OPTIONS.map(({ value, labelKey }) => ({
              value,
              label: formatMessage(labelKey),
            })),
          ]}
          onChange={(value) => {
            setState(value);
            setPage(1);
          }}
        />
      </div>
      <ProTable
        rowKey="containerID"
        columns={columns}
        dataSource={filteredContainers}
        locale={{ emptyText }}
        pagination={{
          current: currentPage,
          pageSize,
          total: filteredContainers.length,
          pageSizeOptions: PAGE_SIZE_OPTIONS,
          showSizeChanger: true,
          showTotal: (total) => `${formatMessage('common.total')} ${total} ${formatMessage('common.items')}`,
        }}
        scroll={{ x: scrollX }}
        resizeable
        onChange={handlePaginationChange}
      />
    </div>
  );
};
