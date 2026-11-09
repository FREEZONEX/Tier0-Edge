import { Renew } from '@carbon/icons-react';
import { Alert, App, Empty, Select, Space, Tag, Typography } from 'antd';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router';

import {
  acknowledgeDataQualityIssue,
  getDataQualityStatus,
  listDataQualityIssues,
  type DataQualityIssue,
  type DataQualityIssueState,
  type DataQualityNodeStatus,
  type DataQualityStatus,
  type PersistedDataQualityRuleCode,
} from '@/apis/core-api/data-quality';
import { getModelInfo } from '@/apis/core-api/uns';
import ComButton from '@/components/com-button';
import ComContent from '@/components/com-layout/ComContent';
import ComLayout from '@/components/com-layout';
import ProTable from '@/components/pro-table';
import type { ProTableColumns } from '@/components/pro-table/types';
import { useTranslate } from '@/hooks';
import { useBaseStore } from '@/stores/base';
import { formatTimestamp } from '@/utils/format';
import { copyToClipboard } from '@/utils/common';

import IssueDetailDrawer from './components/IssueDetailDrawer';
import { buildIssueDiagnosis, issueNodeLabel, nodeLabel } from './dashboard-utils';
import styles from './index.module.scss';

const issueStates: DataQualityIssueState[] = ['', 'open', 'ack', 'resolved'];
const severities: Array<DataQualityIssue['severity'] | ''> = ['', 'critical', 'warning', 'info'];
const rules: Array<PersistedDataQualityRuleCode | ''> = [
  '',
  'connectorFailure',
  'collectionInterrupted',
  'duplicate',
  'lateArrival',
  'timestampGap',
  'outOfOrder',
  'missingTelemetry',
  'ingestionDrop',
  'flatSignal',
  'frequencyDeviation',
  'mandatoryFieldMissing',
  'valueRange',
  'invalidValue',
  'staleSignal',
];
const refreshIntervals = [15_000, 30_000, 60_000];

interface NodeMetadata {
  nodeName?: string;
  displayName?: string;
  namespace?: string;
}

const withNodeMetadata = <T extends DataQualityNodeStatus | DataQualityIssue>(
  item: T,
  metadata: Map<number, NodeMetadata>
): T => {
  const node = metadata.get(item.unsId);
  if (!node) return item;
  return {
    ...item,
    nodeName: item.nodeName || node.nodeName,
    displayName: item.displayName || node.displayName,
    namespace: item.namespace || node.namespace,
  };
};

const stateColor = (state: DataQualityIssue['state']) => {
  if (state === 'resolved') return 'success';
  if (state === 'ack') return 'warning';
  return 'error';
};

const severityColor = (severity: DataQualityIssue['severity']) => {
  if (severity === 'critical') return 'error';
  if (severity === 'warning') return 'warning';
  return 'processing';
};

const DataQualityAlarmsPage = () => {
  const t = useTranslate();
  const navigate = useNavigate();
  const { message } = App.useApp();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const [status, setStatus] = useState<DataQualityStatus>();
  const [items, setItems] = useState<DataQualityIssue[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);
  const [filterUnsId, setFilterUnsId] = useState<number>();
  const [state, setState] = useState<DataQualityIssueState>('');
  const [severity, setSeverity] = useState<DataQualityIssue['severity'] | ''>('');
  const [ruleCode, setRuleCode] = useState<PersistedDataQualityRuleCode | ''>('');
  const [selectedIssue, setSelectedIssue] = useState<DataQualityIssue>();
  const [refreshInterval, setRefreshInterval] = useState(30_000);
  const [lastUpdatedAt, setLastUpdatedAt] = useState<Date>();

  const { currentUserInfo, systemInfo } = useBaseStore((store) => ({
    currentUserInfo: store.currentUserInfo,
    systemInfo: store.systemInfo,
  }));
  const resourceKeys = (currentUserInfo as (typeof currentUserInfo & { resourceKeys?: string[] }) | undefined)
    ?.resourceKeys;
  const canManage =
    systemInfo?.authEnable === false ||
    currentUserInfo?.superAdmin === true ||
    resourceKeys?.includes('uns.dataQuality.manage') === true;

  const load = useCallback(
    async (nextPage = page, nextSize = size, silent = false) => {
      if (!silent) setLoading(true);
      try {
        const [nextStatus, issuePage] = await Promise.all([
          getDataQualityStatus(0),
          listDataQualityIssues({
            unsId: filterUnsId,
            state,
            severity,
            ruleCode,
            page: nextPage,
            size: nextSize,
          }),
        ]);

        const rawNodes = nextStatus.items || [];
        const rawIssues = issuePage.list || [];
        const missingMetadataIds = Array.from(
          new Set(
            [...rawNodes, ...rawIssues].filter((item) => !item.nodeName || !item.namespace).map((item) => item.unsId)
          )
        );
        const metadata = new Map<number, NodeMetadata>();
        await Promise.all(
          missingMetadataIds.map(async (unsId) => {
            try {
              const node = await getModelInfo({ id: unsId });
              metadata.set(unsId, {
                nodeName: node?.name,
                displayName: node?.displayName,
                namespace: node?.namespace || node?.path || node?.alias,
              });
            } catch {
              // Deleted or permission-filtered nodes retain the stable UNS ID fallback.
            }
          })
        );

        const nodes = rawNodes.map((node) => withNodeMetadata(node, metadata));
        const issues = rawIssues.map((issue) => withNodeMetadata(issue, metadata));
        const normalizedStatus: DataQualityStatus = {
          ...nextStatus,
          items: nodes,
        };

        setStatus(normalizedStatus);
        setItems(issues);
        setSelectedIssue((current) => {
          if (!current) return current;
          return issues.find((issue) => issue.id === current.id) || current;
        });
        setTotal(issuePage.total || 0);
        setPage(issuePage.page || nextPage);
        setSize(issuePage.size || nextSize);
        setLastUpdatedAt(new Date());
        setError(false);
      } catch {
        setError(true);
        if (!silent) message.error(t('dataQuality.alarmLoadFailed'));
      } finally {
        if (!silent) setLoading(false);
      }
    },
    [filterUnsId, message, page, ruleCode, severity, size, state, t]
  );

  useEffect(() => {
    const initial = window.setTimeout(() => void load(page, size), 0);
    const timer = window.setInterval(() => void load(page, size, true), refreshInterval);
    return () => {
      window.clearTimeout(initial);
      window.clearInterval(timer);
    };
  }, [load, page, refreshInterval, size]);

  const acknowledge = useCallback(
    async (issue: DataQualityIssue) => {
      try {
        const acknowledged = await acknowledgeDataQualityIssue(issue.id);
        message[acknowledged.state === 'resolved' ? 'info' : 'success'](
          t(acknowledged.state === 'resolved' ? 'dataQuality.alarmAlreadyResolved' : 'dataQuality.alarmAckSucceeded')
        );
        setSelectedIssue((current) =>
          current?.id === issue.id
            ? {
                ...issue,
                ...acknowledged,
                nodeName: issue.nodeName,
                displayName: issue.displayName,
                namespace: issue.namespace,
              }
            : current
        );
        await load(page, size, true);
      } catch {
        await load(page, size, true);
        message.error(t('dataQuality.alarmAckFailed'));
      }
    },
    [load, message, page, size, t]
  );

  const copyEvidence = useCallback(
    (content: string) => {
      copyToClipboard(content, (success) => {
        message[success ? 'success' : 'error'](
          t(success ? 'dataQuality.diagnosis.copySucceeded' : 'dataQuality.diagnosis.copyFailed')
        );
      });
    },
    [message, t]
  );

  const openUnsNode = useCallback(
    (node: DataQualityNodeStatus | DataQualityIssue) => {
      navigate(`/uns?namespaceId=${node.unsId}`);
    },
    [navigate]
  );

  const columns = useMemo<ProTableColumns<DataQualityIssue>>(
    () => [
      {
        key: 'severity',
        dataIndex: 'severity',
        title: t('dataQuality.severity'),
        width: 105,
        render: (value: DataQualityIssue['severity']) => (
          <Tag color={severityColor(value)}>{t(`dataQuality.severity.${value}`)}</Tag>
        ),
      },
      {
        key: 'nodeName',
        title: t('dataQuality.dashboard.nodeName'),
        width: 160,
        render: (_: unknown, issue: DataQualityIssue) => (
          <Typography.Text ellipsis={{ tooltip: issueNodeLabel(issue) }}>{issueNodeLabel(issue)}</Typography.Text>
        ),
      },
      {
        key: 'namespace',
        dataIndex: 'namespace',
        title: t('dataQuality.dashboard.namespace'),
        width: 260,
        render: (value: string) => <Typography.Text ellipsis={{ tooltip: value }}>{value || '-'}</Typography.Text>,
      },
      {
        key: 'ruleCode',
        dataIndex: 'ruleCode',
        title: t('dataQuality.issueRule'),
        width: 175,
        render: (value: PersistedDataQualityRuleCode) => t(`dataQuality.rule.${value}`, undefined, value),
      },
      {
        key: 'diagnosis',
        title: t('dataQuality.diagnosis.hitReason'),
        width: 360,
        render: (_: unknown, issue: DataQualityIssue) => {
          const diagnosis = buildIssueDiagnosis(issue, t);
          return (
            <div className={styles['reason-cell']}>
              <Typography.Text ellipsis={{ tooltip: diagnosis.summary }}>{diagnosis.summary}</Typography.Text>
              {diagnosis.isLegacyEvidence && <span>{t('dataQuality.diagnosis.legacyEvidence')}</span>}
            </div>
          );
        },
      },
      {
        key: 'lastObservedAt',
        dataIndex: 'lastObservedAt',
        title: t('dataQuality.lastObserved'),
        width: 175,
        render: (value: string) => formatTimestamp(value) || '-',
      },
      {
        key: 'state',
        dataIndex: 'state',
        title: t('common.status'),
        width: 105,
        render: (value: DataQualityIssue['state']) => (
          <Tag color={stateColor(value)}>{t(`dataQuality.state.${value}`)}</Tag>
        ),
      },
      {
        key: 'operation',
        title: t('common.operation'),
        fixed: 'right',
        width: 150,
        render: (_: unknown, issue: DataQualityIssue) => (
          <Space size={4}>
            <ComButton
              size="small"
              type="link"
              onClick={(event) => {
                event.stopPropagation();
                setSelectedIssue(issue);
              }}
            >
              {t('dataQuality.dashboard.viewDetail')}
            </ComButton>
            {issue.state === 'open' && canManage && (
              <ComButton
                size="small"
                type="link"
                onClick={(event) => {
                  event.stopPropagation();
                  void acknowledge(issue);
                }}
              >
                {t('dataQuality.ack')}
              </ComButton>
            )}
          </Space>
        ),
      },
    ],
    [acknowledge, canManage, t]
  );

  const nodeOptions = (status?.items || []).map((node) => ({ value: node.unsId, label: nodeLabel(node) }));
  const selectedIssueIndex = selectedIssue ? items.findIndex((issue) => issue.id === selectedIssue.id) : -1;

  return (
    <ComLayout loading={loading && !status} className={styles.page}>
      <ComContent
        hasBack={false}
        title={t('dataQuality.alarmWorkbench')}
        style={{ display: 'flex', flexDirection: 'column' }}
        extra={
          <Space>
            {lastUpdatedAt && (
              <span className={styles['last-updated']}>
                {t('dataQuality.dashboard.updatedAt', { time: formatTimestamp(lastUpdatedAt) })}
              </span>
            )}
            <Select
              className={styles['refresh-select']}
              value={refreshInterval}
              options={refreshIntervals.map((value) => ({
                value,
                label: t('dataQuality.dashboard.autoRefresh', { seconds: value / 1000 }),
              }))}
              onChange={setRefreshInterval}
            />
            <ComButton loading={loading} icon={<Renew size={16} />} onClick={() => void load(page, size)}>
              {t('common.refresh')}
            </ComButton>
          </Space>
        }
      >
        <main className={styles.dashboard}>
          {error && (
            <Alert
              showIcon
              type="error"
              message={t('dataQuality.dashboard.loadError')}
              action={<ComButton onClick={() => void load(page, size)}>{t('dataQuality.dashboard.retry')}</ComButton>}
            />
          )}

          {!error || status ? (
            <section className={`${styles.panel} ${styles['issues-panel']}`}>
              <div className={styles['panel-heading']}>
                <div>
                  <h2>{t('dataQuality.dashboard.alarmList')}</h2>
                  <p>{t('dataQuality.dashboard.alarmListDesc')}</p>
                </div>
                <Space wrap className={styles.filters}>
                  <Select<number | undefined>
                    className={styles['node-filter']}
                    value={filterUnsId}
                    allowClear
                    showSearch
                    optionFilterProp="label"
                    placeholder={t('dataQuality.dashboard.allNodes')}
                    options={nodeOptions}
                    onChange={(value) => {
                      setFilterUnsId(value);
                      setPage(1);
                    }}
                  />
                  <Select<DataQualityIssueState>
                    className={styles.filter}
                    value={state}
                    options={issueStates.map((value) => ({
                      value,
                      label: value ? t(`dataQuality.state.${value}`) : t('dataQuality.allStates'),
                    }))}
                    onChange={(value) => {
                      setState(value);
                      setPage(1);
                    }}
                  />
                  <Select<DataQualityIssue['severity'] | ''>
                    className={styles.filter}
                    value={severity}
                    options={severities.map((value) => ({
                      value,
                      label: value ? t(`dataQuality.severity.${value}`) : t('dataQuality.allSeverities'),
                    }))}
                    onChange={(value) => {
                      setSeverity(value);
                      setPage(1);
                    }}
                  />
                  <Select<PersistedDataQualityRuleCode | ''>
                    className={styles['rule-filter']}
                    value={ruleCode}
                    showSearch
                    optionFilterProp="label"
                    options={rules.map((value) => ({
                      value,
                      label: value ? t(`dataQuality.rule.${value}`, undefined, value) : t('dataQuality.allRules'),
                    }))}
                    onChange={(value) => {
                      setRuleCode(value);
                      setPage(1);
                    }}
                  />
                </Space>
              </div>

              <ProTable
                rowKey="id"
                className={styles['issues-table']}
                columns={columns}
                dataSource={items}
                loading={loading}
                scroll={{ x: 1450 }}
                locale={{
                  emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('dataQuality.noIssues')} />,
                }}
                onRow={(issue) => ({
                  className: styles['issue-row'],
                  onClick: () => setSelectedIssue(issue),
                })}
                pagination={{
                  current: page,
                  pageSize: size,
                  total,
                  showSizeChanger: true,
                  onChange: (nextPage, nextSize) => void load(nextPage, nextSize),
                }}
              />
            </section>
          ) : null}
        </main>
      </ComContent>

      <IssueDetailDrawer
        issue={selectedIssue}
        canManage={canManage}
        onClose={() => setSelectedIssue(undefined)}
        onAcknowledge={acknowledge}
        onCopyEvidence={copyEvidence}
        onOpenUns={openUnsNode}
        onPrevious={selectedIssueIndex > 0 ? () => setSelectedIssue(items[selectedIssueIndex - 1]) : undefined}
        onNext={
          selectedIssueIndex >= 0 && selectedIssueIndex < items.length - 1
            ? () => setSelectedIssue(items[selectedIssueIndex + 1])
            : undefined
        }
        t={t}
      />
    </ComLayout>
  );
};

export default DataQualityAlarmsPage;
