import { useState, type FC } from 'react';
import { Alert, Input, Space } from 'antd';
import ComButton from '@/components/com-button';
import ProModal from '@/components/pro-modal';
import ProTable from '@/components/pro-table';
import HelpTooltip from '@/components/help-tooltip';
import { ButtonPermission } from '@/common-types/button-permission';
import { sourceStreamApi, type Source, type NativeResource, type AccessModel } from '@/apis/core-api/source-stream';
import { useTranslate } from '@/hooks';
import styles from './index.module.scss';

const StreamResourceBrowser: FC<{ source: Source; model: AccessModel; onSelect: (id: string) => void }> = ({
  source,
  model,
  onSelect,
}) => {
  const t = useTranslate();
  const [open, setOpen] = useState(false);
  const [parent, setParent] = useState('');
  const [resources, setResources] = useState(source.resources || []);
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);
  const discover = async () => {
    setLoading(true);
    setFailed(false);
    try {
      const result = await sourceStreamApi.discover(source.id, source.revision, parent);
      setResources(result.list);
    } catch {
      setFailed(true);
    } finally {
      setLoading(false);
    }
  };
  return (
    <>
      <ComButton auth={ButtonPermission['uns.sourceManage']} onClick={() => setOpen(true)}>
        {t('SourceStream.browseInputs')}
      </ComButton>
      <ProModal
        open={open}
        title={t('SourceStream.browseInputs')}
        width={720}
        footer={null}
        onCancel={() => setOpen(false)}
      >
        <div className={styles.section}>
          <Space wrap>
            {source.protocol === 'opcua' && (
              <Input
                aria-label={t('SourceStream.parentNode')}
                placeholder={t('SourceStream.parentNode')}
                value={parent}
                onChange={(e) => setParent(e.target.value)}
              />
            )}
            <ComButton auth={ButtonPermission['uns.sourceManage']} loading={loading} onClick={discover}>
              {t('SourceStream.discover')}
            </ComButton>
            <HelpTooltip title={t('SourceStream.discoveryHint')} />
          </Space>
          {failed && <Alert type="error" showIcon message={t('SourceStream.loadFailed')} />}
          <ProTable
            rowKey="id"
            loading={loading}
            dataSource={resources}
            pagination={false}
            locale={{ emptyText: t('SourceStream.noResources') }}
            columns={[
              {
                title: t('SourceStream.name'),
                dataIndex: 'name',
                width: 200,
                render: (name: string, item: NativeResource) => (
                  <ComButton
                    type="link"
                    disabled={!item.models.includes(model)}
                    onClick={() => {
                      onSelect(item.id);
                      setOpen(false);
                    }}
                  >
                    {name}
                  </ComButton>
                ),
              },
              { title: t('SourceStream.resourceId'), dataIndex: 'id', width: 280, ellipsis: true },
            ]}
          />
        </div>
      </ProModal>
    </>
  );
};
export default StreamResourceBrowser;
