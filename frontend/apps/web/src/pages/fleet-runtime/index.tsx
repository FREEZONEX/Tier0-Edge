import { useCallback, useEffect, useMemo, useState } from 'react';
import { App, Button, Dropdown, Space, Tag, Tooltip, Typography } from 'antd';
import type { MenuProps } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import dayjs from 'dayjs';
import { useNavigate } from 'react-router';
import ComBackButton from '@/components/com-back-button';
import ComEmpty from '@/components/com-empty';
import ComLayout from '@/components/com-layout';
import { ButtonPermission } from '@/common-types/button-permission';
import { Document, OverflowMenuHorizontal, Renew } from '@/components/lucide-icon/carbon';
import { openConfirmModal } from '@/components/confirm-modal';
import {
  getCurrentFleetContainerLogs,
  getCurrentFleetContainers,
  getCurrentFleetNode,
  restartCurrentFleetContainer,
  type FleetContainerSnapshot,
  type FleetContainerSummary,
  type FleetCurrentNode,
} from '@/apis/core-api/fleet';
import { useTranslate } from '@/hooks';
import { FleetContainerLogsDrawer } from '@/pages/fleet-shared/FleetContainerLogsDrawer';
import { FleetContainerTable, fleetContainerSorters } from '@/pages/fleet-shared/FleetContainerTable';
import { useFleetContainerLogs } from '@/pages/fleet-shared/useFleetContainerLogs';
import { hasPermission } from '@/utils/auth';
import { FleetHAPanel } from './FleetHAPanel';
import FleetCollaborationRegistration from '../fleet-management/FleetCollaborationRegistration';

import FleetControlGrants from '../fleet-management/FleetControlGrants';
import styles from './index.module.scss';

const formatTimestamp = (value?: number) => (value ? dayjs(value).format('YYYY-MM-DD HH:mm:ss') : '-');

const parseImageRef = (image?: string) => {
  const raw = String(image || '').trim();
  if (!raw) return { name: '-', tag: '' };

  const digestAt = raw.lastIndexOf('@');
  if (digestAt > 0) {
    return { name: raw.slice(0, digestAt), tag: raw.slice(digestAt + 1) };
  }

  const slash = raw.lastIndexOf('/');
  const colon = raw.lastIndexOf(':');
  if (colon > slash) {
    return { name: raw.slice(0, colon), tag: raw.slice(colon + 1) };
  }

  return { name: raw, tag: '' };
};

const containerStateColor = (state?: string) => {
  switch (String(state || '').toLowerCase()) {
    case 'running':
      return 'success';
    case 'restarting':
      return 'processing';
    case 'stopped':
    case 'exited':
      return 'default';
    default:
      return 'warning';
  }
};

const FleetRuntimePage = () => {
  const formatMessage = useTranslate();
  const navigate = useNavigate();
  const { message, modal } = App.useApp();
  const [loading, setLoading] = useState(false);
  const [current, setCurrent] = useState<FleetCurrentNode>();
  const [snapshot, setSnapshot] = useState<FleetContainerSnapshot>();
  const [restartingID, setRestartingID] = useState('');
  const [logsDrawerHeight, setLogsDrawerHeight] = useState(0);

  const formatUptime = useCallback(
    (startedAt?: number) => {
      if (!startedAt) return '-';
      const seconds = Math.max(0, dayjs().diff(dayjs(startedAt), 'second'));
      const days = Math.floor(seconds / 86400);
      const hours = Math.floor((seconds % 86400) / 3600);
      const minutes = Math.floor((seconds % 3600) / 60);
      return [
        days ? formatMessage('fleet.runtime.durationDays', { count: days }) : '',
        hours ? formatMessage('fleet.runtime.durationHours', { count: hours }) : '',
        formatMessage('fleet.runtime.durationMinutes', { count: minutes }),
      ]
        .filter(Boolean)
        .join(' ');
    },
    [formatMessage]
  );

  const formatContainerState = useCallback(
    (state?: string) => {
      switch (String(state || '').toLowerCase()) {
        case 'running':
          return formatMessage('fleet.runtime.stateRunning');
        case 'restarting':
          return formatMessage('fleet.runtime.stateRestarting');
        case 'exited':
          return formatMessage('fleet.runtime.stateExited');
        case 'stopped':
          return formatMessage('fleet.runtime.stateStopped');
        default:
          return formatMessage('fleet.runtime.stateUnknown');
      }
    },
    [formatMessage]
  );

  const loadRuntime = useCallback(async () => {
    setLoading(true);
    try {
      const node = await getCurrentFleetNode();
      setCurrent(node);
      if (node.agent.availability === 'available') {
        setSnapshot(await getCurrentFleetContainers());
      } else {
        setSnapshot(undefined);
      }
    } catch {
      message.error(formatMessage('fleet.runtime.loadFailed'));
    } finally {
      setLoading(false);
    }
  }, [formatMessage, message]);

  useEffect(() => {
    const timer = window.setTimeout(() => void loadRuntime(), 0);
    return () => window.clearTimeout(timer);
  }, [loadRuntime]);

  const fetchCurrentContainerLogs = useCallback(
    (container: FleetContainerSummary, request: Parameters<typeof getCurrentFleetContainerLogs>[1]) =>
      getCurrentFleetContainerLogs(container.containerID, request),
    []
  );
  const handleLogsError = useCallback(
    () => message.error(formatMessage('fleet.runtime.logsFailed')),
    [formatMessage, message]
  );
  const {
    open: logsOpen,
    loading: logsLoading,
    downloading: logsDownloading,
    logs,
    container: logsContainer,
    live: logsLive,
    follow: logsFollow,
    filterText: logsFilterText,
    tabs: logTabs,
    activeContainerID,
    logsRef,
    openLogs,
    closeLogs,
    refreshLogs,
    downloadLogs,
    clearLogs,
    closeLogTab,
    reorderTabs,
    setActiveTab,
    setLive: setLogsLive,
    setFollow: setLogsFollow,
    setFilterText: setLogsFilterText,
    handleScroll: handleLogsScroll,
  } = useFleetContainerLogs({ fetchLogs: fetchCurrentContainerLogs, onError: handleLogsError });

  const confirmRestart = useCallback(
    (container: FleetContainerSummary) => {
      openConfirmModal(modal, {
        title: formatMessage('fleet.runtime.restartConfirmTitle'),
        content: formatMessage('fleet.runtime.restartConfirmContent', {
          name: container.displayName || container.serviceName,
        }),
        okText: formatMessage('fleet.runtime.restart'),
        cancelText: formatMessage('common.cancel'),
        onOk: async () => {
          setRestartingID(container.containerID);
          try {
            await restartCurrentFleetContainer(container.containerID);
            message.success(formatMessage('fleet.runtime.restartSucceeded'));
            await loadRuntime();
          } catch {
            message.error(formatMessage('fleet.runtime.restartFailed'));
          } finally {
            setRestartingID('');
          }
        },
      });
    },
    [formatMessage, loadRuntime, message, modal]
  );

  const columns = useMemo<ColumnsType<FleetContainerSummary>>(
    () => [
      {
        title: <span className={styles.tableHeaderSingle}>{formatMessage('fleet.runtime.service')}</span>,
        dataIndex: 'displayName',
        width: 168,
        ellipsis: true,
        sorter: fleetContainerSorters.service,
        onHeaderCell: () => ({ className: styles.tableHeaderCellSingle }),
        render: (_, record) => (
          <span className={styles.serviceName} title={record.displayName || record.serviceName}>
            {record.supersededBy
              ? record.containerName
              : record.displayName || record.serviceName || record.containerID.slice(0, 12)}
          </span>
        ),
      },
      {
        title: <span className={styles.tableHeaderSingle}>{formatMessage('common.status')}</span>,
        dataIndex: 'state',
        width: 156,
        sorter: fleetContainerSorters.state,
        onHeaderCell: () => ({ className: styles.tableHeaderCellSingle }),
        render: (value: string) => (
          <Tag className={styles.statusTag} color={containerStateColor(value)}>
            {formatContainerState(value)}
          </Tag>
        ),
      },
      {
        title: <span className={styles.tableHeaderSingle}>{formatMessage('fleet.runtime.uptime')}</span>,
        dataIndex: 'startedAt',
        width: 148,
        sorter: fleetContainerSorters.startedAt,
        onHeaderCell: () => ({ className: styles.tableHeaderCellSingle }),
        render: (value: number, record) => (record.state === 'running' ? formatUptime(value) : '-'),
      },
      {
        title: <span className={styles.tableHeaderSingle}>{formatMessage('fleet.runtime.restartCount')}</span>,
        dataIndex: 'restartCount',
        width: 148,
        sorter: fleetContainerSorters.restartCount,
        onHeaderCell: () => ({ className: styles.tableHeaderCellSingle }),
      },
      {
        title: <span className={styles.tableHeaderSingle}>{formatMessage('fleet.runtime.image')}</span>,
        dataIndex: 'image',
        width: 240,
        ellipsis: true,
        sorter: fleetContainerSorters.image,
        onHeaderCell: () => ({ className: styles.tableHeaderCellSingle }),
        render: (value: string) => {
          const { name, tag } = parseImageRef(value);
          return (
            <Tooltip title={value || undefined}>
              <div className={styles.imageCell}>
                <span className={styles.imageName}>{name}</span>
                {tag ? <span className={styles.imageTag}>{tag}</span> : null}
              </div>
            </Tooltip>
          );
        },
      },
      {
        title: <span className={styles.operationHeader}>{formatMessage('common.operation')}</span>,
        key: 'operation',
        width: 96,
        onHeaderCell: () => ({ className: styles.operationHeaderCell }),
        render: (_, record) => {
          const items: MenuProps['items'] = [];
          if (hasPermission(ButtonPermission['FleetContainer.logs'])) {
            items.push({
              key: 'logs',
              label: formatMessage('fleet.runtime.logs'),
              icon: <Document size={16} />,
              onClick: () => void openLogs(record),
            });
          }
          if (hasPermission(ButtonPermission['FleetContainer.restart'])) {
            items.push({
              key: 'restart',
              label: formatMessage('fleet.runtime.restart'),
              icon: <Renew size={16} />,
              danger: true,
              disabled: !record.restartable || Boolean(restartingID),
              title: !record.restartable ? formatMessage('fleet.runtime.restartProtected') : undefined,
              onClick: () => confirmRestart(record),
            });
          }
          if (!items.length) return null;
          return (
            <div className="custom-operation">
              <Dropdown
                overlayClassName="pro-table-operation-menu"
                menu={{ items }}
                trigger={['click']}
                placement="bottomRight"
              >
                <Button type="text" icon={<OverflowMenuHorizontal size={16} />} />
              </Dropdown>
            </div>
          );
        },
      },
    ],
    [confirmRestart, formatContainerState, formatMessage, formatUptime, openLogs, restartingID]
  );

  const agent = current?.agent;
  const host = agent?.host;
  const roleLabel = formatMessage(current?.role === 'center' ? 'fleet.role.center' : 'fleet.role.edge');

  return (
    <ComLayout loading={loading} className={styles.page}>
      <div className={styles.content} style={{ paddingBottom: logsDrawerHeight ? logsDrawerHeight + 16 : 0 }}>
        <div className={styles.detailHeader}>
          <div className={styles.detailHeaderLeft}>
            <ComBackButton onClick={() => navigate('/home')} />
            <div className={styles.detailHeaderInfo}>
              <div className={styles.detailTitleRow}>
                <Typography.Title level={3} className={styles.detailTitle}>
                  {formatMessage('fleet.runtime.title')}
                </Typography.Title>
                <Tag className={styles.statusTag} color={current?.role === 'center' ? 'success' : 'processing'}>
                  {roleLabel}
                </Tag>
              </div>
              <div className={styles.meta}>
                <span>{host?.hostname || '-'}</span>
                <span>{host ? `${host.os} / ${host.arch}` : '-'}</span>
                <span>
                  {formatMessage('fleet.runtime.collectedAt', { time: formatTimestamp(snapshot?.observedAt) })}
                </span>
              </div>
            </div>
          </div>
          <Space size={8} className={styles.detailHeaderActions}>
            <Tooltip title={formatMessage('common.refresh')}>
              <Button
                className={styles.refreshBtn}
                aria-label={formatMessage('common.refresh')}
                icon={<Renew size={16} />}
                onClick={() => void loadRuntime()}
              />
            </Tooltip>
          </Space>
        </div>

        <div className={styles.body}>
          <FleetCollaborationRegistration local />
          <FleetControlGrants />
          {agent?.availability !== 'available' ? (
            <div className={styles.unavailable}>
              <ComEmpty description={formatMessage('fleet.runtime.agentUnavailable')} />
            </div>
          ) : (
            <Space className={styles.runtimeContent} direction="vertical" size={16}>
              <FleetHAPanel ha={current?.ha} />
              <FleetContainerTable
                columns={columns}
                containers={snapshot?.containers}
                emptyText={<ComEmpty description={formatMessage('fleet.runtime.empty')} />}
              />
            </Space>
          )}
        </div>
      </div>

      <FleetContainerLogsDrawer
        open={logsOpen}
        loading={logsLoading}
        downloading={logsDownloading}
        logs={logs}
        container={logsContainer}
        live={logsLive}
        follow={logsFollow}
        filterText={logsFilterText}
        tabs={logTabs}
        activeContainerID={activeContainerID}
        logsRef={logsRef}
        closeLogs={closeLogs}
        refreshLogs={refreshLogs}
        downloadLogs={downloadLogs}
        clearLogs={clearLogs}
        closeLogTab={closeLogTab}
        reorderTabs={reorderTabs}
        setActiveTab={setActiveTab}
        setLive={setLogsLive}
        setFollow={setLogsFollow}
        setFilterText={setLogsFilterText}
        handleScroll={handleLogsScroll}
        onHeightChange={setLogsDrawerHeight}
      />
    </ComLayout>
  );
};

export default FleetRuntimePage;
