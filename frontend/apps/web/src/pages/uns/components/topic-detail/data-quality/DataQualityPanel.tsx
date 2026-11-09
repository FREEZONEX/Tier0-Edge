import { type FC, useCallback, useEffect, useRef, useState } from 'react';
import { Alert, App, Empty, Flex, Form, InputNumber, Select, Space, Spin, Tag, Typography } from 'antd';

import {
  acknowledgeDataQualityIssue,
  getDataQualityPolicy,
  getDataQualityStatus,
  listDataQualityIssues,
  saveDataQualityPolicy,
  type DataQualityIssue,
  type DataQualityIssueState,
  type DataQualityPolicy,
  type DataQualityRuleCode,
  type PersistedDataQualityRuleCode,
  type SaveDataQualityPolicyInput,
} from '@/apis/core-api/data-quality';
import ComButton from '@/components/com-button';
import HelpTooltip from '@/components/help-tooltip';
import ProTable from '@/components/pro-table';
import type { ProTableColumns } from '@/components/pro-table/types';
import { useTranslate } from '@/hooks';
import { useBaseStore } from '@/stores/base';
import type { FieldItem } from '@/pages/uns/types';
import { formatTimestamp } from '@/utils/format';
import ValueRangeSettings, { type ValueRangePatch } from './ValueRangeSettings';
import styles from './DataQualityPanel.module.scss';

interface DataQualityPanelProps {
  unsId: string | number;
  readOnly?: boolean;
  schemaFields: FieldItem[];
  schemaEditable: boolean;
  onSchemaRangeChange: (patch: ValueRangePatch) => Promise<FieldItem[]>;
  onUnavailable: () => void;
}

interface DataQualityFormValues {
  expectedIntervalSeconds?: number;
  missingGraceSeconds?: number;
}

type AutoSaveState = 'idle' | 'saving' | 'saved' | 'error' | 'invalid';

const nonZeroNumber = (value: unknown) =>
  typeof value === 'number' && Number.isFinite(value) && value !== 0 ? value : undefined;

const defaultPolicy = (unsId: string | number): DataQualityPolicy => ({
  unsId: Number(unsId),
  enabled: false,
  notifyOnIssue: false,
  expectedIntervalMs: 0,
  sequenceField: '',
  missingGraceMs: 0,
  lateThresholdMs: 5000,
  clockSkewToleranceMs: 0,
  gapToleranceMs: 0,
  outOfOrderToleranceMs: 0,
  frequencyTolerancePercent: 20,
  minSamples: 3,
  flatMinSamples: 3,
  flatMinDurationMs: 2000,
  mandatoryFields: [],
  flatFields: [],
  staleFields: [],
  rules: [],
  severityOverrides: {},
  samplingPriority: 0,
  cooldownMs: 60000,
});

const normalizePolicy = (unsId: string | number, policy?: DataQualityPolicy): DataQualityPolicy => ({
  ...defaultPolicy(unsId),
  ...policy,
  unsId: Number(policy?.unsId ?? unsId),
  mandatoryFields: Array.isArray(policy?.mandatoryFields) ? policy.mandatoryFields : [],
  flatFields: Array.isArray(policy?.flatFields) ? policy.flatFields : [],
  staleFields: Array.isArray(policy?.staleFields) ? policy.staleFields : [],
  rules: Array.isArray(policy?.rules) ? policy.rules : [],
  severityOverrides: policy?.severityOverrides || {},
});

const formValuesFromPolicy = (policy: DataQualityPolicy): DataQualityFormValues => ({
  expectedIntervalSeconds: policy.expectedIntervalMs > 0 ? policy.expectedIntervalMs / 1000 : undefined,
  missingGraceSeconds: policy.missingGraceMs > 0 ? policy.missingGraceMs / 1000 : undefined,
});

const policyInputFromValues = (
  policy: DataQualityPolicy,
  values: DataQualityFormValues,
  fields: FieldItem[]
): SaveDataQualityPolicyInput => {
  const expectedIntervalMs = Math.round(Number(values.expectedIntervalSeconds || 0) * 1000);
  const missingGraceMs = Math.round(Number(values.missingGraceSeconds || 0) * 1000);
  const rules: DataQualityRuleCode[] = [];
  if (
    fields.some(
      (field) => nonZeroNumber(field.lowerLimit) !== undefined || nonZeroNumber(field.upperLimit) !== undefined
    )
  ) {
    rules.push('valueRange');
  }
  if (expectedIntervalMs > 0) rules.push('missingTelemetry');
  return {
    ...(Object.fromEntries(Object.entries(policy).filter(([key]) => key !== 'unsId')) as SaveDataQualityPolicyInput),
    enabled: rules.length > 0,
    notifyOnIssue: false,
    rules,
    severityOverrides: {},
    sequenceField: '',
    mandatoryFields: [],
    flatFields: [],
    staleFields: [],
    expectedIntervalMs,
    missingGraceMs,
  };
};

const errorCode = (error: unknown) => Number((error as { code?: number | string })?.code || 0);

const DataQualityPanel: FC<DataQualityPanelProps> = ({
  unsId,
  readOnly = false,
  schemaFields,
  schemaEditable,
  onSchemaRangeChange,
  onUnavailable,
}) => {
  const formatMessage = useTranslate();
  const { message } = App.useApp();
  const [form] = Form.useForm<DataQualityFormValues>();
  const [policy, setPolicy] = useState<DataQualityPolicy>(() => defaultPolicy(unsId));
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState(false);
  const [issues, setIssues] = useState<DataQualityIssue[]>([]);
  const [issueTotal, setIssueTotal] = useState(0);
  const [issueLoading, setIssueLoading] = useState(false);
  const [issueError, setIssueError] = useState(false);
  const [issueState, setIssueState] = useState<DataQualityIssueState>('');
  const [issuePage, setIssuePage] = useState(1);
  const [issueSize, setIssueSize] = useState(10);
  const [autoSaveState, setAutoSaveState] = useState<AutoSaveState>('idle');
  const policyRef = useRef(policy);
  const [confirmedValues, setConfirmedValues] = useState<DataQualityFormValues>(() => formValuesFromPolicy(policy));
  const confirmedValuesRef = useRef<DataQualityFormValues>(confirmedValues);
  const pendingValuesRef = useRef<{ values: DataQualityFormValues; fields: FieldItem[] } | undefined>(undefined);
  const saveRunningRef = useRef(false);
  const mountedRef = useRef(true);
  const currentUserInfo = useBaseStore((state) => state.currentUserInfo);
  const systemInfo = useBaseStore((state) => state.systemInfo);
  const resourceKeys = (currentUserInfo as (typeof currentUserInfo & { resourceKeys?: string[] }) | undefined)
    ?.resourceKeys;
  const canManage = Boolean(
    !readOnly &&
    (systemInfo?.authEnable === false ||
      currentUserInfo?.superAdmin === true ||
      resourceKeys?.includes('uns.dataQuality.manage'))
  );
  const expectedIntervalSeconds = Form.useWatch('expectedIntervalSeconds', form) ?? 0;
  const missingGraceSeconds = Form.useWatch('missingGraceSeconds', form) ?? 0;
  const timingDirty =
    expectedIntervalSeconds !== confirmedValues.expectedIntervalSeconds ||
    missingGraceSeconds !== confirmedValues.missingGraceSeconds;
  const ruleLabel = useCallback(
    (rule: PersistedDataQualityRuleCode) => formatMessage(`dataQuality.rule.${rule}`),
    [formatMessage]
  );

  const loadStatusAndPolicy = useCallback(async () => {
    setLoading(true);
    setLoadError(false);
    try {
      try {
        const nextStatus = await getDataQualityStatus(unsId);
        if (!nextStatus?.enabled) {
          onUnavailable();
          return;
        }
      } catch (error) {
        if ([403, 404, 503].includes(errorCode(error))) {
          onUnavailable();
          return;
        }
        throw error;
      }
      let nextPolicy = defaultPolicy(unsId);
      try {
        nextPolicy = normalizePolicy(unsId, await getDataQualityPolicy(unsId));
      } catch (error) {
        const code = errorCode(error);
        if ([403, 503].includes(code)) {
          onUnavailable();
          return;
        }
        throw error;
      }
      setPolicy(nextPolicy);
      policyRef.current = nextPolicy;
      const nextValues = formValuesFromPolicy(nextPolicy);
      confirmedValuesRef.current = nextValues;
      setConfirmedValues(nextValues);
      form.setFieldsValue(nextValues);
      setAutoSaveState('idle');
    } catch {
      setLoadError(true);
    } finally {
      setLoading(false);
    }
  }, [form, onUnavailable, unsId]);

  const loadIssues = useCallback(
    async (silent = false) => {
      if (!silent) {
        setIssueLoading(true);
        setIssueError(false);
      }
      try {
        const page = await listDataQualityIssues({
          unsId,
          state: issueState || undefined,
          page: issuePage,
          size: issueSize,
        });
        setIssues(page?.list || []);
        setIssueTotal(page?.total || 0);
        setIssueError(false);
      } catch (error) {
        if ([403, 404, 503].includes(errorCode(error))) {
          onUnavailable();
          return;
        }
        if (!silent) {
          setIssueError(true);
          setIssues([]);
          setIssueTotal(0);
        }
      } finally {
        if (!silent) setIssueLoading(false);
      }
    },
    [issuePage, issueSize, issueState, onUnavailable, unsId]
  );

  useEffect(() => {
    const timer = window.setTimeout(() => void loadStatusAndPolicy(), 0);
    return () => window.clearTimeout(timer);
  }, [loadStatusAndPolicy]);

  useEffect(() => {
    const initialTimer = window.setTimeout(() => void loadIssues(), 0);
    const refreshTimer = window.setInterval(() => void loadIssues(true), 15_000);
    return () => {
      window.clearTimeout(initialTimer);
      window.clearInterval(refreshTimer);
    };
  }, [loadIssues]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const persistQueuedPolicy = useCallback(async () => {
    if (saveRunningRef.current) return;
    saveRunningRef.current = true;

    while (pendingValuesRef.current) {
      const pending = pendingValuesRef.current;
      pendingValuesRef.current = undefined;
      try {
        await form.validateFields();
      } catch {
        if (!pendingValuesRef.current) pendingValuesRef.current = pending;
        if (mountedRef.current) setAutoSaveState('invalid');
        break;
      }

      setAutoSaveState('saving');
      try {
        const saved = normalizePolicy(
          unsId,
          await saveDataQualityPolicy(unsId, policyInputFromValues(policyRef.current, pending.values, pending.fields))
        );
        policyRef.current = saved;
        confirmedValuesRef.current = formValuesFromPolicy(saved);
        if (!mountedRef.current) continue;

        setPolicy(saved);
        setConfirmedValues(confirmedValuesRef.current);
        if (!pendingValuesRef.current) {
          form.setFieldsValue(confirmedValuesRef.current);
          setAutoSaveState('saved');
        }
        void loadIssues();
      } catch (error) {
        if ([403, 404, 503].includes(errorCode(error))) {
          onUnavailable();
          break;
        }
        if (!pendingValuesRef.current) pendingValuesRef.current = pending;
        if (mountedRef.current) setAutoSaveState('error');
        break;
      }
    }

    saveRunningRef.current = false;
  }, [form, loadIssues, onUnavailable, unsId]);

  const queuePolicySave = useCallback(
    (fields: FieldItem[] = schemaFields) => {
      pendingValuesRef.current = {
        values: form.getFieldsValue(true) as DataQualityFormValues,
        fields,
      };
      setAutoSaveState('saving');
      void persistQueuedPolicy();
    },
    [form, persistQueuedPolicy, schemaFields]
  );

  const reloadPersistedPolicy = async () => {
    pendingValuesRef.current = undefined;
    setAutoSaveState('saving');
    try {
      let persisted = defaultPolicy(unsId);
      try {
        persisted = normalizePolicy(unsId, await getDataQualityPolicy(unsId));
      } catch (error) {
        const code = errorCode(error);
        if ([403, 503].includes(code)) {
          onUnavailable();
          return;
        }
        throw error;
      }
      policyRef.current = persisted;
      setPolicy(persisted);
      confirmedValuesRef.current = formValuesFromPolicy(persisted);
      setConfirmedValues(confirmedValuesRef.current);
      form.setFieldsValue(confirmedValuesRef.current);
      setAutoSaveState('saved');
      await loadIssues();
    } catch {
      setAutoSaveState('error');
    }
  };

  const handleSchemaRangeChange = async (patch: ValueRangePatch) => {
    const savedFields = await onSchemaRangeChange(patch);
    queuePolicySave(savedFields);
  };

  const acknowledgeIssue = async (id: number) => {
    try {
      const acknowledged = await acknowledgeDataQualityIssue(id);
      message[acknowledged.state === 'resolved' ? 'info' : 'success'](
        formatMessage(acknowledged.state === 'resolved' ? 'dataQuality.alarmAlreadyResolved' : 'dataQuality.ackSuccess')
      );
      await loadIssues(true);
    } catch (error) {
      const code = errorCode(error);
      if ([403, 404, 503].includes(code)) {
        onUnavailable();
        return;
      }
      await loadIssues(true);
      message.error(formatMessage('dataQuality.alarmAckFailed'));
    }
  };

  const issueStateLabel = (state: DataQualityIssue['state']) => formatMessage(`dataQuality.state.${state}`);

  const issueStateColor = (state: DataQualityIssue['state']) => {
    if (state === 'resolved') return 'success';
    if (state === 'ack') return 'warning';
    return 'error';
  };

  const severityColor = (severity: DataQualityIssue['severity']) => {
    if (severity === 'critical') return 'error';
    if (severity === 'warning') return 'warning';
    return 'processing';
  };

  const issueColumns: ProTableColumns<DataQualityIssue> = [
    {
      key: 'ruleCode',
      dataIndex: 'ruleCode',
      title: formatMessage('dataQuality.issueRule'),
      width: 180,
      render: (value: PersistedDataQualityRuleCode) => ruleLabel(value),
    },
    {
      key: 'severity',
      dataIndex: 'severity',
      title: formatMessage('dataQuality.severity'),
      width: 110,
      render: (value: DataQualityIssue['severity']) => (
        <Tag color={severityColor(value)}>{formatMessage(`dataQuality.severity.${value}`)}</Tag>
      ),
    },
    {
      key: 'state',
      dataIndex: 'state',
      title: formatMessage('common.status'),
      width: 110,
      render: (value: DataQualityIssue['state']) => <Tag color={issueStateColor(value)}>{issueStateLabel(value)}</Tag>,
    },
    {
      key: 'occurrences',
      dataIndex: 'occurrences',
      title: formatMessage('dataQuality.occurrences'),
      width: 110,
    },
    {
      key: 'lastObservedAt',
      dataIndex: 'lastObservedAt',
      title: formatMessage('dataQuality.lastObserved'),
      width: 190,
      render: (value: string) => formatTimestamp(value) || '-',
    },
    {
      key: 'operation',
      title: formatMessage('common.operation'),
      fixed: 'right',
      width: 100,
      render: (_value: unknown, issue: DataQualityIssue) =>
        issue.state === 'open' && canManage ? (
          <ComButton size="small" type="link" onClick={() => acknowledgeIssue(issue.id)}>
            {formatMessage('dataQuality.ack')}
          </ComButton>
        ) : (
          '-'
        ),
    },
  ];

  if (loading) {
    return (
      <div className={styles['loading-state']}>
        <Spin />
      </div>
    );
  }

  if (loadError) {
    return (
      <Alert
        type="error"
        showIcon
        message={formatMessage('dataQuality.loadFailed')}
        action={<ComButton onClick={loadStatusAndPolicy}>{formatMessage('common.refresh')}</ComButton>}
      />
    );
  }

  return (
    <div className={styles.panel}>
      <section className={styles.section}>
        {autoSaveState === 'error' && (
          <Alert
            className={styles['save-error']}
            type="error"
            showIcon
            message={formatMessage('dataQuality.autoSave.failed')}
            action={
              <Space>
                <ComButton size="small" onClick={() => void persistQueuedPolicy()}>
                  {formatMessage('dataQuality.autoSave.retry')}
                </ComButton>
                <ComButton size="small" onClick={() => void reloadPersistedPolicy()}>
                  {formatMessage('dataQuality.autoSave.reload')}
                </ComButton>
              </Space>
            }
          />
        )}

        <Form
          form={form}
          layout="vertical"
          disabled={!canManage}
          className={styles.form}
          initialValues={formValuesFromPolicy(policy)}
          onValuesChange={() => queuePolicySave()}
        >
          <div className={styles['rule-list']}>
            <div className={styles['rule-row']}>
              <div className={styles['rule-heading']}>
                <div className={styles['setting-label']}>
                  <span>{formatMessage('dataQuality.configCore.valueTitle')}</span>
                  <HelpTooltip title={formatMessage('dataQuality.ruleHelp.valueRange')} />
                </div>
              </div>
              <ValueRangeSettings
                fields={schemaFields}
                editable={canManage && schemaEditable}
                onSave={handleSchemaRangeChange}
              />
            </div>

            <div className={styles['rule-row']}>
              <div className={styles['rule-heading']}>
                <div className={styles['setting-label']}>
                  <span>{formatMessage('dataQuality.configCore.continuityTitle')}</span>
                  <HelpTooltip title={formatMessage('dataQuality.ruleHelp.missingTelemetry')} />
                </div>
              </div>
              <div className={styles['continuity-settings']}>
                <Form.Item
                  name="expectedIntervalSeconds"
                  label={formatMessage('dataQuality.expectedSeconds')}
                  rules={[
                    {
                      validator: (_rule, value) =>
                        value === undefined || value === null || Number(value) >= 0
                          ? Promise.resolve()
                          : Promise.reject(new Error(formatMessage('dataQuality.nonNegativeRequired'))),
                    },
                  ]}
                >
                  <InputNumber
                    min={0}
                    precision={3}
                    addonAfter={formatMessage('dataQuality.secondsUnit')}
                    placeholder={formatMessage('dataQuality.secondsPlaceholder')}
                    className={styles['full-width']}
                    disabled={!canManage}
                  />
                </Form.Item>
                <Form.Item
                  name="missingGraceSeconds"
                  label={formatMessage('dataQuality.missingGraceSeconds')}
                  rules={[
                    {
                      validator: (_rule, value) =>
                        value === undefined || value === null || Number(value) >= 0
                          ? Promise.resolve()
                          : Promise.reject(new Error(formatMessage('dataQuality.nonNegativeRequired'))),
                    },
                  ]}
                >
                  <InputNumber
                    min={0}
                    precision={3}
                    addonAfter={formatMessage('dataQuality.secondsUnit')}
                    placeholder={formatMessage('dataQuality.secondsPlaceholder')}
                    className={styles['full-width']}
                    disabled={!canManage}
                  />
                </Form.Item>
              </div>
              {autoSaveState === 'error' && timingDirty && (
                <Typography.Text type="danger" className={styles['continuity-summary']}>
                  {formatMessage('dataQuality.autoSave.unsaved')}
                </Typography.Text>
              )}
            </div>
          </div>
        </Form>
      </section>

      <section className={styles.section}>
        <Flex justify="space-between" align="center" gap={12} wrap="wrap" className={styles['issue-header']}>
          <Typography.Title level={5} className={styles['section-title']}>
            {formatMessage('dataQuality.issues')}
          </Typography.Title>
          <Space>
            <Select<DataQualityIssueState>
              value={issueState}
              className={styles['state-filter']}
              options={[
                { value: '', label: formatMessage('common.all') },
                { value: 'open', label: formatMessage('dataQuality.state.open') },
                { value: 'ack', label: formatMessage('dataQuality.state.ack') },
                { value: 'resolved', label: formatMessage('dataQuality.state.resolved') },
              ]}
              onChange={(value) => {
                setIssueState(value);
                setIssuePage(1);
              }}
            />
            <ComButton onClick={loadIssues}>{formatMessage('common.refresh')}</ComButton>
          </Space>
        </Flex>
        {issueError ? (
          <Alert type="error" showIcon message={formatMessage('dataQuality.issueLoadFailed')} />
        ) : (
          <ProTable
            rowKey="id"
            loading={issueLoading}
            columns={issueColumns}
            dataSource={issues}
            rowNavigationIndicator={false}
            scroll={{ x: 800 }}
            locale={{ emptyText: <Empty description={formatMessage('dataQuality.noIssues')} /> }}
            pagination={{
              current: issuePage,
              pageSize: issueSize,
              total: issueTotal,
              showSizeChanger: true,
              pageSizeOptions: [10, 20, 50],
              onChange: (page, size) => {
                setIssuePage(page);
                setIssueSize(size);
              },
            }}
          />
        )}
      </section>
    </div>
  );
};

export default DataQualityPanel;
