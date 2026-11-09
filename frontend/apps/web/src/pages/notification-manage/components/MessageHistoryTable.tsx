import { type FC, useEffect, useMemo, useState } from 'react';
import { App, Descriptions, Space, Tag } from 'antd';
import ProTable from '@/components/pro-table';
import MessageHistoryFilters from './MessageHistoryFilters';

import {
  getMessageHistory,
  getNotificationRecipientOptions,
  type MessageHistoryItem,
  type MessageHistoryListParams,
  type MessageHistorySource,
  type NotificationRecipientUser,
} from '@/apis/core-api/notify';
import { formatTimestamp } from '@/utils/format';

import styles from './MessageHistoryTable.module.scss';

const PAGE_SIZE = 20;

type Translate = (key: string, values?: Record<string, unknown>, fallback?: string) => string;

type Props = {
  t: Translate;
};

const MessageHistoryTable: FC<Props> = ({ t }) => {
  const { message } = App.useApp();
  const [list, setList] = useState<MessageHistoryItem[]>([]);
  const [sources, setSources] = useState<MessageHistorySource[]>([]);
  const [users, setUsers] = useState<NotificationRecipientUser[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [filters, setFilters] = useState<MessageHistoryListParams>({});
  const [refreshToken, setRefreshToken] = useState(0);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let active = true;
    void getMessageHistory({ ...filters, page, size: PAGE_SIZE })
      .then((response) => {
        if (!active) return;
        setList(response.list || []);
        setTotal(response.total || 0);
        setSources(response.sources || []);
      })
      .catch(() => {
        if (active) message.error(t('historyLoadFailed'));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [filters, message, page, refreshToken, t]);

  useEffect(() => {
    let active = true;
    void getNotificationRecipientOptions()
      .then((response) => {
        if (active) setUsers(response.users || []);
      })
      .catch(() => {
        if (active) setUsers([]);
      });
    return () => {
      active = false;
    };
  }, []);

  const applyFilters = (next: MessageHistoryListParams) => {
    setLoading(true);
    setPage(1);
    setFilters(next);
    setRefreshToken((value) => value + 1);
  };

  const columns = useMemo(
    () => [
      {
        title: t('colCreatedAt'),
        dataIndex: 'createdTime',
        width: 180,
        render: (value: number) => formatTimestamp(value) || '-',
      },
      {
        title: t('historySource'),
        key: 'source',
        width: 210,
        render: (_: unknown, row: MessageHistoryItem) => (
          <Space size={6}>
            <Tag color={row.sourceType === 'app' ? 'blue' : 'default'}>
              {row.sourceType === 'app' ? t('sourceApp') : t('sourcePlatform')}
            </Tag>
            <span className={styles['ellipsis-cell']}>
              {row.sourceType === 'app' ? row.sourceName || row.sourceId : t('sourcePlatform')}
            </span>
          </Space>
        ),
      },
      { title: t('colTitle'), dataIndex: 'title', ellipsis: true },
      {
        title: t('colType'),
        dataIndex: 'type',
        width: 100,
        render: (value: string) => t(`type${value.charAt(0).toUpperCase()}${value.slice(1)}`, undefined, value),
      },
      { title: t('recipientCount'), dataIndex: 'recipientCount', width: 100 },
      {
        title: t('readProgress'),
        key: 'readProgress',
        width: 110,
        render: (_: unknown, row: MessageHistoryItem) => `${row.readCount}/${row.recipientCount}`,
      },
      {
        title: t('colStatus'),
        dataIndex: 'status',
        width: 100,
        render: (value: MessageHistoryItem['status']) => (
          <Tag color={value === 'sent' ? 'green' : 'default'}>
            {value === 'sent' ? t('statusSent') : t('statusRevoked')}
          </Tag>
        ),
      },
    ],
    [t]
  );

  const originLabel = (origin: string) => {
    const labels: Record<string, string> = {
      admin: t('originAdmin'),
      openapi: t('originOpenAPI'),
      system: t('originSystem'),
    };
    return labels[origin] || origin || '-';
  };

  return (
    <div className={styles['history-panel']}>
      <MessageHistoryFilters
        value={filters}
        sources={sources}
        users={users}
        loading={loading}
        onApply={applyFilters}
        onRefresh={() => {
          setLoading(true);
          setRefreshToken((value) => value + 1);
        }}
      />
      <div className={styles['table-summary']} aria-live="polite">
        {t('historyTotal', { count: total })}
      </div>

      <ProTable
        rowKey="messageId"
        loading={loading}
        dataSource={list}
        columns={columns}
        scroll={{ x: 1080 }}
        expandable={{
          expandedRowRender: (row) => (
            <Descriptions size="small" column={2} className={styles['history-detail']}>
              <Descriptions.Item label={t('historyContent')} span={2}>
                <span className={styles['message-content']}>{row.content}</span>
              </Descriptions.Item>
              <Descriptions.Item label={t('sourceId')}>{row.sourceId || '-'}</Descriptions.Item>
              <Descriptions.Item label={t('origin')}>{originLabel(row.origin)}</Descriptions.Item>
              <Descriptions.Item label={t('messageId')}>{row.messageId}</Descriptions.Item>
              <Descriptions.Item label={t('requestId')}>{row.notifyRequestId || '-'}</Descriptions.Item>
              <Descriptions.Item label={t('formLink')} span={2}>
                {row.link || '-'}
              </Descriptions.Item>
            </Descriptions>
          ),
        }}
        pagination={{
          current: page,
          pageSize: PAGE_SIZE,
          total,
          showSizeChanger: false,
          showTotal: (count) => t('historyTotal', { count }),
          onChange: (nextPage) => {
            setLoading(true);
            setPage(nextPage);
          },
        }}
      />
    </div>
  );
};

export default MessageHistoryTable;
