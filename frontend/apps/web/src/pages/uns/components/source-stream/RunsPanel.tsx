import { useSourceStreamApi } from '@/apis/core-api/source-stream-scope';
import { useState, type FC } from 'react';
import { Alert, Input, Select, Space, Tag } from 'antd';
import { Renew } from '@carbon/icons-react';
import ProTable from '@/components/pro-table';
import ComDrawer from '@/components/com-drawer';
import ComButton from '@/components/com-button';
import { usePagination, useTranslate } from '@/hooks';
import { type RunFilter, type StreamRun } from '@/apis/core-api/source-stream';
import { useIntegrationRoute } from './IntegrationTabs';
import styles from './index.module.scss';

const RunsPanel: FC<{ sourceId?: number; streamId?: number; topicId?: number }> = (scope) => {
  const sourceStreamApi = useSourceStreamApi();
  const t = useTranslate();
  const { go } = useIntegrationRoute();
  const [failed, setFailed] = useState(false);
  const [detail, setDetail] = useState<StreamRun>();
  const { data, loading, pagination, setSearchParams, refreshRequest } = usePagination<StreamRun>({
    fetchApi: async ({ pageNo, pageSize, ...filters }: RunFilter & { pageNo: number; pageSize: number }) => {
      try {
        const result = await sourceStreamApi.runs({ ...scope, ...filters, page: pageNo, size: pageSize });
        setFailed(false);
        return { data: result.list, total: result.total, pageNo };
      } catch (error) {
        setFailed(true);
        throw error;
      }
    },
  });
  return (
    <div className={styles.section}>
      <Space wrap>
        <Select
          allowClear
          placeholder={t('SourceStream.status')}
          style={{ width: 180 }}
          onChange={(status) => setSearchParams({ status })}
          options={['succeeded', 'accepted', 'failed', 'result-unknown', 'dropped', 'partial', 'partial-failure'].map(
            (value) => ({
              value,
              label: t(`SourceStream.status.${value}`),
            })
          )}
        />
        <Input.Search
          placeholder={t('SourceStream.runId')}
          onSearch={async (id) => {
            if (id.trim()) setDetail(await sourceStreamApi.run(id.trim()));
          }}
        />
        <ComButton icon={<Renew />} onClick={() => refreshRequest()}>
          {t('SourceStream.refresh')}
        </ComButton>
      </Space>
      {failed && <Alert type="error" showIcon message={t('SourceStream.loadFailed')} />}
      <ProTable
        rowKey="id"
        loading={loading}
        dataSource={data}
        pagination={pagination}
        onRow={(row: StreamRun) => ({ onClick: async () => setDetail(await sourceStreamApi.run(row.id)) })}
        columns={[
          { title: t('SourceStream.runId'), dataIndex: 'id', width: 180, ellipsis: true },
          {
            title: t('SourceStream.model'),
            dataIndex: 'model',
            width: 130,
            render: (model: string) => t(`SourceStream.model.${model}`),
          },
          { title: t('SourceStream.entryId'), dataIndex: 'entryId', width: 160, ellipsis: true },
          { title: t('SourceStream.version'), dataIndex: 'versionId', width: 150, ellipsis: true },
          {
            title: t('SourceStream.status'),
            dataIndex: 'status',
            width: 140,
            render: (status: string) => <Tag>{t(`SourceStream.status.${status}`)}</Tag>,
          },
          {
            title: t('SourceStream.duration'),
            dataIndex: 'durationMs',
            width: 100,
            render: (ms: number) => `${ms} ms`,
          },
          {
            title: t('SourceStream.createdAt'),
            dataIndex: 'createdAt',
            width: 170,
            render: (time: number) => new Date(time).toLocaleString(),
          },
        ]}
      />
      <ComDrawer
        open={Boolean(detail)}
        title={t('SourceStream.runDetail')}
        width={640}
        onClose={() => setDetail(undefined)}
      >
        {detail && (
          <div className={styles.section}>
            <Alert type="info" showIcon message={t('SourceStream.redactedRunHint')} />
            <Space wrap>
              {detail.sourceId > 0 && (
                <ComButton
                  onClick={() => {
                    setDetail(undefined);
                    go({ view: 'source', sourceId: detail.sourceId });
                  }}
                >
                  {t('SourceStream.openSource')}
                </ComButton>
              )}
              <ComButton
                onClick={() => {
                  setDetail(undefined);
                  go({ view: 'stream', sourceId: detail.sourceId, streamId: detail.streamId });
                }}
              >
                {t('SourceStream.openStream')}
              </ComButton>
            </Space>
            <pre className={styles.code}>{JSON.stringify(detail, null, 2)}</pre>
          </div>
        )}
      </ComDrawer>
    </div>
  );
};

export default RunsPanel;
