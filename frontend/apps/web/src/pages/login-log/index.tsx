import { useEffect, useRef, useState, type FC } from 'react';
import { Empty, Typography } from 'antd';
import { Renew } from '@carbon/icons-react';
import { getLoginLogPage, type LoginLogItem } from '@/apis/core-api/login-log';
import ComButton from '@/components/com-button';
import ComLayout from '@/components/com-layout';
import ComContent from '@/components/com-layout/ComContent';
import HelpTooltip from '@/components/help-tooltip';
import ProTable from '@/components/pro-table';
import { useTranslate } from '@/hooks';
import { formatTimestamp } from '@/utils/format';
import LoginLogDetailDrawer from './components/LoginLogDetailDrawer';
import LoginLogFilterBar from './components/LoginLogFilters';
import LoginLogStatus from './components/LoginLogStatus';
import { cleanLoginFilters, hasLoginFilters, type LoginLogFilters } from './filters';
import { getLoginAccountLabel, getLoginEventLabel, getLoginIpLabel, getLoginMessageLabel } from './presentation';
import styles from './index.module.scss';

const { Text } = Typography;
const initialPageSize = 20;

const LoginLogPage: FC = () => {
  const t = useTranslate();
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);
  const [data, setData] = useState<LoginLogItem[]>([]);
  const [total, setTotal] = useState(0);
  const [pageNo, setPageNo] = useState(1);
  const [pageSize, setPageSize] = useState(initialPageSize);
  const [applied, setApplied] = useState<LoginLogFilters>({});
  const [draft, setDraft] = useState<LoginLogFilters>({});
  const [detail, setDetail] = useState<LoginLogItem | null>(null);
  const requestSequence = useRef(0);

  const fetchList = async (nextPage = pageNo, nextSize = pageSize, filters = applied) => {
    const request = ++requestSequence.current;
    setLoading(true);
    setFailed(false);
    try {
      const response = await getLoginLogPage(
        { ...filters, pageNo: nextPage, pageSize: nextSize },
        { _noMessage: true }
      );
      // 只接受最后一次查询结果，避免较慢的旧请求覆盖当前筛选和分页。
      if (request !== requestSequence.current) return;
      setData(response?.data || []);
      setTotal(response?.total || 0);
      setPageNo(response?.pageNo || nextPage);
      setPageSize(response?.pageSize || nextSize);
    } catch {
      if (request !== requestSequence.current) return;
      setData([]);
      setTotal(0);
      setFailed(true);
    } finally {
      if (request === requestSequence.current) setLoading(false);
    }
  };

  const applyFilters = (value: LoginLogFilters) => {
    const next = cleanLoginFilters(value);
    setApplied(next);
    setDraft(next);
    setPageNo(1);
    void fetchList(1, pageSize, next);
  };

  useEffect(() => {
    void fetchList(1, initialPageSize, {});
    return () => {
      requestSequence.current++;
    };
    // 初次加载一次；后续查询由筛选、分页和刷新显式触发。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const renderText = (value: string) => (
    <Text className={styles['ellipsis-cell']} ellipsis={{ tooltip: value }}>
      {value}
    </Text>
  );

  return (
    <ComLayout>
      <ComContent
        className={styles['login-log-content']}
        title={
          <span className={styles['page-title']}>
            {t('loginLog.title', {}, 'Login Log')}
            <HelpTooltip
              title={t('loginLog.desc', {}, 'Track platform login and logout activities, including failed attempts.')}
            />
          </span>
        }
        hasBack={false}
        extra={
          <ComButton icon={<Renew size={16} />} loading={loading} onClick={() => fetchList()}>
            {t('common.refresh', {}, 'Refresh')}
          </ComButton>
        }
      >
        <LoginLogFilterBar value={draft} applied={applied} onChange={setDraft} onApply={applyFilters} />
        <div className={styles['table-summary']} aria-live="polite">
          {t('common.total', {}, 'Total')}: {total}
        </div>
        <ProTable
          className={styles['login-log-table']}
          rowKey="id"
          dataSource={data}
          loading={loading}
          tableLayout="fixed"
          columnFit={false}
          scroll={{ x: 1128, y: 'max(240px, calc(100vh - 460px))' }}
          columns={[
            {
              title: t('loginLog.time', {}, 'Time'),
              dataIndex: 'createdAt',
              key: 'createdAt',
              width: 180,
              render: (value: number) => renderText(formatTimestamp(value) || '-'),
            },
            {
              title: t('loginLog.event', {}, 'Event'),
              key: 'eventType',
              width: 96,
              render: (_: unknown, record: LoginLogItem) => renderText(getLoginEventLabel(record, t)),
            },
            {
              title: t('loginLog.account', {}, 'Account'),
              key: 'userName',
              width: 150,
              render: (_: unknown, record: LoginLogItem) => renderText(getLoginAccountLabel(record)),
            },
            {
              title: t('loginLog.ip', {}, 'IP Address'),
              key: 'ipAddr',
              width: 160,
              render: (_: unknown, record: LoginLogItem) => renderText(getLoginIpLabel(record)),
            },
            {
              title: `${t('loginLog.browser', {}, 'Browser')} / ${t('loginLog.os', {}, 'OS')}`,
              key: 'client',
              width: 170,
              render: (_: unknown, record: LoginLogItem) => (
                <div className={styles['client-cell']}>
                  {renderText(record.browser || '-')}
                  <span className={styles['secondary-text']}>{renderText(record.os || '-')}</span>
                </div>
              ),
            },
            {
              title: t('loginLog.status', {}, 'Status'),
              dataIndex: 'code',
              key: 'code',
              width: 104,
              render: (value: number) => <LoginLogStatus code={value} />,
            },
            {
              title: t('loginLog.message', {}, 'Message'),
              key: 'msg',
              width: 180,
              render: (_: unknown, record: LoginLogItem) => renderText(getLoginMessageLabel(record, t)),
            },
            {
              title: t('common.operation', {}, 'Operation'),
              key: 'operation',
              width: 88,
              fixed: 'right',
              render: (_: unknown, record: LoginLogItem) => (
                <ComButton type="link" size="small" onClick={() => setDetail(record)}>
                  {t('common.detail', {}, 'Details')}
                </ComButton>
              ),
            },
          ]}
          pagination={{
            total,
            current: pageNo,
            pageSize,
            showSizeChanger: true,
            pageSizeOptions: [10, 20, 50, 100],
            onChange: (current, size) => {
              const nextPage = size !== pageSize ? 1 : current;
              setPageNo(nextPage);
              setPageSize(size);
              void fetchList(nextPage, size);
            },
          }}
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={
                  failed
                    ? t('loginLog.loadFailed', {}, 'Failed to load login logs.')
                    : hasLoginFilters(applied)
                      ? t('auditLog.noMatches', {}, 'No logs match these filters')
                      : t('loginLog.empty', {}, 'No login logs yet')
                }
              >
                {failed ? (
                  <ComButton onClick={() => fetchList()}>{t('common.refresh', {}, 'Refresh')}</ComButton>
                ) : null}
                {!failed && hasLoginFilters(applied) ? (
                  <ComButton onClick={() => applyFilters({})}>{t('common.reset', {}, 'Reset')}</ComButton>
                ) : null}
              </Empty>
            ),
          }}
        />
        <LoginLogDetailDrawer record={detail} onClose={() => setDetail(null)} />
      </ComContent>
    </ComLayout>
  );
};

export default LoginLogPage;
