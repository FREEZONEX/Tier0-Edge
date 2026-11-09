import { useState } from 'react';
import { Input, Empty } from 'antd';
import { Search, Table, Add } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import ProTable from '@/components/pro-table';
import { useTranslate } from '@/hooks';
import type { UnsMetadata } from '@/apis/core-api/uns-metadata';
import { displayTime } from './data-panel';
import styles from './index.module.scss';

// Cloud NamespaceOverview presentation; metadata continues to come from the local API.
export default function NamespaceOverview({
  items,
  loading,
  onSelect,
  onCreate,
}: {
  items: UnsMetadata[];
  loading: boolean;
  onSelect: (id: string) => void;
  onCreate?: () => void;
}) {
  const t = useTranslate();
  const [search, setSearch] = useState('');
  const location = (item: UnsMetadata) => {
    const path: string[] = [],
      seen = new Set<string>();
    let parent = item.data.parentId;
    while (parent && !seen.has(parent)) {
      seen.add(parent);
      const folder = items.find((node) => node.id === parent);
      if (!folder) break;
      path.unshift(folder.data.name);
      parent = folder.data.parentId;
    }
    return path.join(' / ') || t('NamespaceView.root');
  };
  const rows = items.filter(
    (item) =>
      item.kind === 'collection' &&
      [item.data.name, item.data.description, location(item)].some((value) =>
        value?.toLowerCase().includes(search.toLowerCase())
      )
  );
  return (
    <section className={styles.overview}>
      <h2>{t('NamespaceView.allData')}</h2>
      <Input
        prefix={<Search size={16} />}
        allowClear
        value={search}
        onChange={(event) => setSearch(event.target.value)}
        placeholder={t('SourceStream.search')}
        aria-label={t('NamespaceView.allData')}
      />
      <ProTable
        rowKey="id"
        loading={loading}
        dataSource={rows}
        rowNavigationIndicator={false}
        columnFit={false}
        pagination={{ pageSize: 20 }}
        scroll={{ x: 540 }}
        onRow={(item: UnsMetadata) => ({ onClick: () => onSelect(item.id) })}
        locale={{
          emptyText: (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('UnsUi.empty')}>
              {onCreate && (
                <ComButton icon={<Add />} onClick={onCreate}>
                  {t('NamespaceGuide.newDataset')}
                </ComButton>
              )}
            </Empty>
          ),
        }}
        columns={[
          {
            title: t('UnsMeta.name'),
            width: 240,
            render: (_, item: UnsMetadata) => (
              <ComButton
                type="text"
                className={styles['dataset-name']}
                icon={<Table size={16} />}
                onClick={(event) => {
                  event.stopPropagation();
                  onSelect(item.id);
                }}
              >
                {item.data.name}
              </ComButton>
            ),
          },
          { title: t('NamespaceView.location'), width: 160, render: (_, item: UnsMetadata) => location(item) },
          {
            title: t('NamespaceView.fields'),
            width: 80,
            render: (_, item: UnsMetadata) => Object.keys(item.data.recordSchema?.properties || {}).length,
          },
          { title: t('UnsMeta.updatedAt'), width: 170, dataIndex: 'updatedAt', render: displayTime },
        ]}
      />
    </section>
  );
}
