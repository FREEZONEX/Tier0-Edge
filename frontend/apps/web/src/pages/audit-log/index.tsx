import {
  getAuditLogDetail,
  getAuditLogPage,
  getAuditLogOptions,
  exportAuditLogs,
  type AuditLogItem,
  type AuditFilterOption,
} from '@/apis/core-api/audit-log';
import ComLayout from '@/components/com-layout';
import ComContent from '@/components/com-layout/ComContent';
import ProTable from '@/components/pro-table';
import { useTranslate } from '@/hooks';
import {
  getAuditActionLabel,
  getAuditModuleLabel,
  getAuditOperatorLabel,
  getAuditResultLabel,
  getAuditResourceLabel,
  getAuditScopeLabel,
} from './presentation';
import { formatTimestamp } from '@/utils/format';
import { App, Button, Empty, Space, Tag, Typography } from 'antd';
import { useSearchParams } from 'react-router';
import { Renew, Download } from '@carbon/icons-react';
import { AuthButton } from '@/components/auth';
import { ButtonPermission } from '@/common-types/button-permission';
import { downloadFn } from '@/utils/blob';
import { AuditFilterBar } from './AuditFilters';
import { type AuditFilters, cleanFilters, defaultFilters, readFilters, writeFilters } from './filters';
import { useEffect, useRef, useState } from 'react';
import { AuditLogDetailDrawer } from './AuditLogDetailDrawer';
import styles from './index.module.scss';

const { Text } = Typography;

const fixedColumnWidth = {
  time: 166,
  operation: 86,
};
const tableScrollbarReserve = 12;
const auditMinimumTableWidth = 1120;
const flexibleColumnConfig = {
  scope: { min: 88, max: 150, padding: 46 },
  module: { min: 112, max: 180, padding: 46 },
  action: { min: 96, max: 150, padding: 46 },
  resource: { min: 110, max: 180, padding: 46 },
  operator: { min: 120, max: 210, padding: 46 },
  result: { min: 96, max: 140, padding: 46 },
};

const clampWidth = (width: number, min: number, max: number) => Math.min(max, Math.max(min, width));

const estimateTextWidth = (value: string, config: { min: number; max: number; padding: number }) =>
  clampWidth(config.padding + value.length * 7, config.min, config.max);

const getElementContentWidth = (element: HTMLElement) => {
  const style = window.getComputedStyle(element);
  const horizontalPadding = (Number.parseFloat(style.paddingLeft) || 0) + (Number.parseFloat(style.paddingRight) || 0);
  return Math.max(0, element.getBoundingClientRect().width - horizontalPadding);
};

const renderEllipsisText = (value: string) => (
  <Text className={styles.ellipsisCell} ellipsis={{ tooltip: value }}>
    {value || '-'}
  </Text>
);

const renderTimeText = (value: number) => {
  const formatted = formatTimestamp(value) || '-';
  return (
    <Text className={styles.timeCell} ellipsis={{ tooltip: formatted }}>
      {formatted}
    </Text>
  );
};

const flexibleColumnKeys = ['scope', 'module', 'action', 'resource', 'operator', 'result'] as const;
type FlexibleColumnKey = (typeof flexibleColumnKeys)[number];

const distributeExtraWidth = (widths: Record<FlexibleColumnKey, number>, extraWidth: number) => {
  let restWidth = extraWidth;
  while (restWidth > 0.5) {
    const expandableColumns = flexibleColumnKeys.filter((key) => widths[key] < flexibleColumnConfig[key].max);
    if (!expandableColumns.length) break;
    const widthPerColumn = restWidth / expandableColumns.length;
    let usedWidth = 0;
    expandableColumns.forEach((key) => {
      const addedWidth = Math.min(widthPerColumn, flexibleColumnConfig[key].max - widths[key]);
      widths[key] += addedWidth;
      usedWidth += addedWidth;
    });
    if (usedWidth <= 0) break;
    restWidth -= usedWidth;
  }
};

const resolveFlexibleColumnWidths = (
  records: AuditLogItem[],
  containerWidth: number,
  translate: (id: string, opt?: any, defaultMessage?: string) => string
) => {
  const widths = {
    scope: flexibleColumnConfig.scope.min,
    module: flexibleColumnConfig.module.min,
    action: flexibleColumnConfig.action.min,
    resource: flexibleColumnConfig.resource.min,
    operator: flexibleColumnConfig.operator.min,
    result: flexibleColumnConfig.result.min,
  };

  records.forEach((record) => {
    widths.scope = Math.max(
      widths.scope,
      estimateTextWidth(getAuditScopeLabel(record, translate), flexibleColumnConfig.scope)
    );
    widths.module = Math.max(
      widths.module,
      estimateTextWidth(
        getAuditModuleLabel(record.scopeType, record.scopeName, record.resType, translate),
        flexibleColumnConfig.module
      )
    );
    widths.action = Math.max(
      widths.action,
      estimateTextWidth(getAuditActionLabel(record.businessType, translate), flexibleColumnConfig.action)
    );
    widths.resource = Math.max(
      widths.resource,
      estimateTextWidth(getAuditResourceLabel(record, translate), flexibleColumnConfig.resource)
    );
    widths.operator = Math.max(
      widths.operator,
      estimateTextWidth(getAuditOperatorLabel(record), flexibleColumnConfig.operator)
    );
    widths.result = Math.max(
      widths.result,
      estimateTextWidth(getAuditResultLabel(record, translate), flexibleColumnConfig.result)
    );
  });

  const fixedWidth = fixedColumnWidth.time + fixedColumnWidth.operation;
  const availableWidth = Math.max(
    containerWidth ? containerWidth - fixedWidth : 0,
    flexibleColumnConfig.scope.min +
      flexibleColumnConfig.module.min +
      flexibleColumnConfig.action.min +
      flexibleColumnConfig.resource.min +
      flexibleColumnConfig.operator.min +
      flexibleColumnConfig.result.min
  );
  const flexibleWidth =
    widths.scope + widths.module + widths.action + widths.resource + widths.operator + widths.result;

  if (flexibleWidth > availableWidth) {
    const shrinkableWidth =
      widths.scope -
      flexibleColumnConfig.scope.min +
      widths.module -
      flexibleColumnConfig.module.min +
      widths.action -
      flexibleColumnConfig.action.min +
      widths.resource -
      flexibleColumnConfig.resource.min +
      widths.operator -
      flexibleColumnConfig.operator.min +
      widths.result -
      flexibleColumnConfig.result.min;
    const overflow = flexibleWidth - availableWidth;
    if (shrinkableWidth > 0) {
      const shrinkRatio = Math.min(1, overflow / shrinkableWidth);
      widths.scope -= (widths.scope - flexibleColumnConfig.scope.min) * shrinkRatio;
      widths.module -= (widths.module - flexibleColumnConfig.module.min) * shrinkRatio;
      widths.action -= (widths.action - flexibleColumnConfig.action.min) * shrinkRatio;
      widths.resource -= (widths.resource - flexibleColumnConfig.resource.min) * shrinkRatio;
      widths.operator -= (widths.operator - flexibleColumnConfig.operator.min) * shrinkRatio;
      widths.result -= (widths.result - flexibleColumnConfig.result.min) * shrinkRatio;
    }
  } else if (containerWidth && flexibleWidth < availableWidth) {
    distributeExtraWidth(widths, availableWidth - flexibleWidth);
  }

  return {
    scope: Math.round(widths.scope),
    module: Math.round(widths.module),
    action: Math.round(widths.action),
    resource: Math.round(widths.resource),
    operator: Math.round(widths.operator),
    result: Math.round(widths.result),
  };
};

const AuditLogPage = () => {
  const formatMessage = useTranslate();
  const { message } = App.useApp();
  const translate = (id: string, opt?: any, defaultMessage?: string) => formatMessage(id, opt, defaultMessage);
  const t = (id: string, fallback: string, opt?: Record<string, unknown>) => translate(id, opt, fallback);
  const [loading, setLoading] = useState(false);
  const [listFailed, setListFailed] = useState(false);
  const [detailLoading, setDetailLoading] = useState(false);
  const [data, setData] = useState<AuditLogItem[]>([]);
  const [total, setTotal] = useState(0);
  const [searchParams, setSearchParams] = useSearchParams();
  const [pageNo, setPageNo] = useState(Math.max(1, Number(searchParams.get('page')) || 1));
  const [pageSize, setPageSize] = useState(Math.min(200, Math.max(1, Number(searchParams.get('size')) || 20)));
  const [applied, setApplied] = useState<AuditFilters>(() => readFilters(searchParams));
  const [draft, setDraft] = useState<AuditFilters>(applied);
  const [options, setOptions] = useState<AuditFilterOption[]>([]);
  const [exporting, setExporting] = useState(false);
  const listRequest = useRef(0);
  const detailRequest = useRef(0);
  const [detail, setDetail] = useState<AuditLogItem | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);
  const [tableShellWidth, setTableShellWidth] = useState(0);
  const tableShellRef = useRef<HTMLDivElement>(null);
  const tableAvailableWidth = tableShellWidth ? Math.max(0, tableShellWidth - tableScrollbarReserve) : 0;
  const auditTableWidth = Math.max(tableAvailableWidth, auditMinimumTableWidth);
  const flexibleColumnWidth = resolveFlexibleColumnWidths(data, auditTableWidth, translate);

  const fetchList = async (nextPageNo = pageNo, nextPageSize = pageSize, nextFilters = applied) => {
    const request = ++listRequest.current;
    setLoading(true);
    setListFailed(false);
    try {
      const resp = await getAuditLogPage({
        pageNo: nextPageNo,
        pageSize: nextPageSize,
        ...nextFilters,
      });
      if (request !== listRequest.current) return;
      setData(resp?.data || []);
      setTotal(resp?.total || 0);
      setPageNo(resp?.pageNo || nextPageNo);
      setPageSize(resp?.pageSize || nextPageSize);
    } catch {
      if (request !== listRequest.current) return;
      setData([]);
      setTotal(0);
      setListFailed(true);
      message.error(t('auditLog.loadFailed', 'Failed to load audit logs'));
    } finally {
      if (request === listRequest.current) setLoading(false);
    }
  };

  const applyFilters = (value: AuditFilters) => {
    const next = cleanFilters(value);
    setApplied(next);
    setDraft(next);
    setPageNo(1);
    setSearchParams(writeFilters(searchParams, next, 1, pageSize), { replace: true });
    void fetchList(1, pageSize, next);
  };
  const exportLogs = async () => {
    setExporting(true);
    try {
      const result = await exportAuditLogs(applied);
      downloadFn({ data: result.content, name: result.fileName });
    } catch {
      message.error(t('auditLog.exportFailed', 'Export failed'));
    } finally {
      setExporting(false);
    }
  };
  useEffect(() => {
    let active = true;
    void getAuditLogOptions({ startTime: applied.startTime, endTime: applied.endTime })
      .then((items) => {
        if (active) setOptions(items || []);
      })
      .catch(() => {
        if (active) message.error(t('auditLog.optionsFailed', 'Failed to load filters'));
      });
    return () => {
      active = false;
    };
  }, [applied.startTime, applied.endTime]);
  // 首次加载恢复当前 URL 的已生效条件，后续查询由显式操作触发。

  useEffect(() => {
    void fetchList(pageNo, pageSize, applied);
    return () => {
      listRequest.current++;
      detailRequest.current++;
    };
  }, []);

  useEffect(() => {
    const element = tableShellRef.current;
    if (!element) return undefined;
    const updateWidth = () => setTableShellWidth(Math.floor(getElementContentWidth(element)));
    updateWidth();
    const resizeObserver = new ResizeObserver(updateWidth);
    resizeObserver.observe(element);
    return () => resizeObserver.disconnect();
  }, []);

  const openDetail = async (record: AuditLogItem) => {
    const request = ++detailRequest.current;
    setDetail(null);
    setDetailOpen(true);
    setDetailLoading(true);
    try {
      const resp = await getAuditLogDetail(record.id);
      if (request === detailRequest.current) setDetail(resp);
    } catch {
      if (request === detailRequest.current)
        message.error(t('auditLog.detailFailed', 'Failed to load audit log detail'));
    } finally {
      if (request === detailRequest.current) setDetailLoading(false);
    }
  };

  return (
    <ComLayout>
      <ComContent
        className={styles.auditContent}
        title={t('auditLog.title', 'Audit Log')}
        hasBack={false}
        style={{ display: 'flex', flexDirection: 'column' }}
        extra={
          <Space>
            <Button icon={<Renew size={16} />} onClick={() => void fetchList()}>
              {t('auditLog.refresh', 'Refresh')}
            </Button>
            <AuthButton
              auth={ButtonPermission['AuditLog.export']}
              icon={<Download size={16} />}
              loading={exporting}
              onClick={() => void exportLogs()}
            >
              {t('auditLog.export', 'Export')}
            </AuthButton>
          </Space>
        }
      >
        <AuditFilterBar
          value={draft}
          applied={applied}
          options={options}
          onChange={setDraft}
          onApply={applyFilters}
          onReset={() => applyFilters(defaultFilters())}
        />
        <div ref={tableShellRef} className={styles.tableShell}>
          <ProTable
            key={auditTableWidth}
            className={styles.auditTable}
            rowKey="id"
            loading={loading}
            locale={{
              emptyText: (
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  description={t(
                    listFailed
                      ? 'auditLog.loadFailed'
                      : Object.keys(applied).length
                        ? 'auditLog.noMatches'
                        : 'auditLog.noLogs',
                    'No audit logs'
                  )}
                >
                  {listFailed ? (
                    <Button onClick={() => void fetchList()}>{t('auditLog.refresh', 'Refresh')}</Button>
                  ) : (
                    Object.keys(applied).length > 0 && (
                      <Button onClick={() => applyFilters({})}>{t('auditLog.clearFilters', 'Clear filters')}</Button>
                    )
                  )}
                </Empty>
              ),
            }}
            dataSource={data}
            tableLayout="fixed"
            columnFit={false}
            scroll={{ y: 'calc(100vh - 440px)', x: auditTableWidth }}
            columns={[
              {
                title: () => t('auditLog.time', 'Time'),
                dataIndex: 'createdAt',
                key: 'createdAt',
                ellipsis: true,
                width: fixedColumnWidth.time,
                render: (value: number) => renderTimeText(value),
              },
              {
                title: () => t('auditLog.scope', 'Scope'),
                dataIndex: 'scopeName',
                key: 'scopeName',
                ellipsis: true,
                width: flexibleColumnWidth.scope,
                render: (_: unknown, record: any) =>
                  renderEllipsisText(getAuditScopeLabel(record as AuditLogItem, translate)),
              },
              {
                title: () => t('auditLog.module', 'Module'),
                dataIndex: 'resType',
                key: 'resType',
                ellipsis: true,
                width: flexibleColumnWidth.module,
                render: (_: unknown, record: any) =>
                  renderEllipsisText(
                    getAuditModuleLabel(
                      (record as AuditLogItem).scopeType,
                      (record as AuditLogItem).scopeName,
                      (record as AuditLogItem).resType,
                      translate
                    )
                  ),
              },
              {
                title: () => t('auditLog.action', 'Action'),
                dataIndex: 'businessType',
                key: 'businessType',
                ellipsis: true,
                width: flexibleColumnWidth.action,
                render: (value: string) => renderEllipsisText(getAuditActionLabel(value, translate)),
              },
              {
                title: () => t('auditLog.resource', 'Resource'),
                dataIndex: 'resName',
                key: 'resName',
                ellipsis: true,
                width: flexibleColumnWidth.resource,
                render: (_: unknown, record: any) =>
                  renderEllipsisText(getAuditResourceLabel(record as AuditLogItem, translate)),
              },
              {
                title: () => t('auditLog.operator', 'Operator'),
                dataIndex: 'operatorName',
                key: 'operatorName',
                ellipsis: true,
                width: flexibleColumnWidth.operator,
                render: (_: unknown, record: any) => renderEllipsisText(getAuditOperatorLabel(record as AuditLogItem)),
              },
              {
                title: () => t('auditLog.result', 'Result'),
                dataIndex: 'code',
                key: 'code',
                ellipsis: true,
                width: flexibleColumnWidth.result,
                render: (_: unknown, record: any) => (
                  <Tag
                    color={
                      record.result === 'accepted' || record.code === 202
                        ? 'processing'
                        : record.code >= 200 && record.code < 400
                          ? 'success'
                          : 'error'
                    }
                  >
                    {getAuditResultLabel(record as AuditLogItem, translate)}
                  </Tag>
                ),
              },
              {
                title: () => t('common.operation', 'Operation'),
                key: 'operation',
                width: fixedColumnWidth.operation,
                fixed: 'right',
                render: (_: unknown, record: any) => (
                  <a className={styles.operationLink} onClick={() => void openDetail(record)}>
                    {t('auditLog.detail', 'Detail')}
                  </a>
                ),
              },
            ]}
            pagination={{
              total,
              current: pageNo,
              pageSize,
              onChange: (current, size) => {
                setSearchParams(writeFilters(searchParams, applied, current, size), { replace: true });
                void fetchList(current, size, applied);
              },
            }}
          />
        </div>
        <AuditLogDetailDrawer
          open={detailOpen}
          loading={detailLoading}
          detail={detail}
          onClose={() => {
            detailRequest.current++;
            setDetailOpen(false);
          }}
          onRelated={(record) => {
            detailRequest.current++;
            setDetailOpen(false);
            applyFilters({ resType: record.resType, resId: record.resId });
          }}
        />
      </ComContent>
    </ComLayout>
  );
};

export default AuditLogPage;
