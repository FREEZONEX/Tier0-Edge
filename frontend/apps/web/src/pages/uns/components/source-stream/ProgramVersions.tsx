import { useSourceStreamApi } from '@/apis/core-api/source-stream-scope';
import { useState, type FC } from 'react';
import { Alert, App, Space } from 'antd';
import { javascript } from '@codemirror/lang-javascript';
import { type Stream, type StreamVersion } from '@/apis/core-api/source-stream';
import { ButtonPermission } from '@/common-types/button-permission';
import { usePagination, useTranslate } from '@/hooks';
import ComButton from '@/components/com-button';
import ProTable from '@/components/pro-table';
import ProModal from '@/components/pro-modal';
import ProCodemirror from '@/components/pro-codemirror';
import { openConfirmModal } from '@/components/confirm-modal';
import styles from './index.module.scss';

const ProgramVersions: FC<{ stream: Stream; readOnly?: boolean; onChanged: (stream: Stream) => void }> = ({
  stream,
  readOnly,
  onChanged,
}) => {
  const sourceStreamApi = useSourceStreamApi();
  const t = useTranslate();
  const { modal, message } = App.useApp();
  const [failed, setFailed] = useState(false);
  const [selected, setSelected] = useState<StreamVersion>();
  const { data, loading, pagination } = usePagination<StreamVersion>({
    fetchApi: async ({ pageNo }: { pageNo: number }) => {
      try {
        const r = await sourceStreamApi.versions(stream.id, pageNo);
        setFailed(false);
        return { data: r.list, total: r.total, pageNo };
      } catch (error) {
        setFailed(true);
        throw error;
      }
    },
  });
  return (
    <div className={styles.section}>
      {failed && <Alert type="error" showIcon message={t('SourceStream.loadFailed')} />}
      <ProTable
        rowKey="id"
        dataSource={data}
        loading={loading}
        pagination={pagination}
        onRow={(version: StreamVersion) => ({ onClick: () => setSelected(version) })}
        columns={[
          { title: t('SourceStream.version'), dataIndex: 'id', width: 180 },
          {
            title: t('SourceStream.activatedAt'),
            dataIndex: 'activatedAt',
            width: 180,
            render: (time?: number) => (time ? new Date(time).toLocaleString() : t('SourceStream.notActivated')),
          },
          {
            title: t('SourceStream.createdAt'),
            dataIndex: 'createdAt',
            width: 180,
            render: (time: number) => new Date(time).toLocaleString(),
          },
        ]}
      />
      <ProModal
        open={!!selected}
        title={t('SourceStream.version')}
        width={760}
        onCancel={() => setSelected(undefined)}
        footer={
          <Space>
            <ComButton onClick={() => setSelected(undefined)}>{t('common.close')}</ComButton>
            {!readOnly && selected && (
              <ComButton
                className={styles['manage-button']}
                auth={ButtonPermission['uns.streamDeploy']}
                disabled={!selected.activatedAt || !!stream.pendingVersion}
                onClick={() =>
                  openConfirmModal(modal, {
                    title: t('SourceStream.rollback'),
                    content: t('SourceStream.rollbackHint'),
                    okText: t('common.confirm'),
                    cancelText: t('common.cancel'),
                    onOk: async () => {
                      const result = await sourceStreamApi.rollback(stream.id, selected.id, stream.revision, 0);
                      onChanged(result);
                      setSelected(undefined);
                      message.info(t(result.pendingVersion ? 'SourceStream.deployPending' : 'SourceStream.deployed'));
                    },
                  })
                }
              >
                {t('SourceStream.rollback')}
              </ComButton>
            )}
          </Space>
        }
      >
        {selected && (
          <ProCodemirror
            value={
              typeof selected.document.script === 'string'
                ? selected.document.script
                : JSON.stringify(selected.document, null, 2)
            }
            extensions={[javascript()]}
            readOnly
            height="440px"
            showHint={false}
          />
        )}
      </ProModal>
    </div>
  );
};
export default ProgramVersions;
