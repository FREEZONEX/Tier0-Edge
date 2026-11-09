import { useState } from 'react';
import { Button, Descriptions, Dropdown, Modal, Tabs, TreeSelect } from 'antd';
import { Edit, FolderMoveTo, Link, OverflowMenuHorizontal, Table as TableIcon, TrashCan } from '@carbon/icons-react';
import { metadataApi, type UnsMetadata } from '@/apis/core-api/uns-metadata';
import { useTranslate } from '@/hooks';
import { useIntegrationRoute } from '../source-stream/IntegrationTabs';
import { displayTime, SchemaView } from './data-panel';
import { metadataPath } from './collection-usage';
import type useConnections from './use-connections';
import CollectionRecords from './collection-records';
import CollectionRelations from './collection-relations';
import styles from './collection-detail.module.scss';

export default function CollectionDetail({
  collection,
  items,
  connections,
  canEdit,
  canDelete,
  onEdit,
  onArchive,
  onReference,
  onFlow,
  onSelect,
  onRefresh,
}: {
  collection: UnsMetadata;
  items: UnsMetadata[];
  connections: ReturnType<typeof useConnections>;
  canEdit: boolean;
  canDelete: boolean;
  onEdit: () => void;
  onArchive: () => void;
  onReference: () => void;
  onFlow?: () => void;
  onSelect: (id: string) => void;
  onRefresh: () => void;
}) {
  const t = useTranslate();
  const route = useIntegrationRoute();
  const [move, setMove] = useState(false);
  const [saving, setSaving] = useState(false);
  const [parent, setParent] = useState<string>();
  const go = (detail: string) => route.go({ view: 'namespace', metaId: collection.id, detail });
  const related = route.detail === 'relations' || route.detail === 'topology';
  const menu = [
    { key: 'details', label: t('common.detail'), onClick: () => go('details') },
    ...(canEdit
      ? [{ key: 'reference', label: t('UnsMeta.addReference'), icon: <Link size={16} />, onClick: onReference }]
      : []),
    ...(canDelete
      ? [{ key: 'delete', label: t('common.delete'), danger: true, icon: <TrashCan size={16} />, onClick: onArchive }]
      : []),
  ];
  return (
    <section className={styles.detail}>
      <header className={styles.heading}>
        <div>
          <h2>
            <span className={styles.icon}>
              <TableIcon size={18} />
            </span>
            {collection.data.name}
          </h2>
          {collection.data.description && <p>{collection.data.description}</p>}
        </div>
        <div className={styles.actions}>
          {canEdit && (
            <>
              <Button icon={<Edit size={16} />} onClick={onEdit}>
                {t('common.edit')}
              </Button>
              <Button
                icon={<FolderMoveTo size={16} />}
                onClick={() => {
                  setParent(collection.data.parentId || undefined);
                  setMove(true);
                }}
              >
                {t('NamespaceView.move')}
              </Button>
            </>
          )}
          <Dropdown trigger={['click']} menu={{ items: menu }}>
            <Button type="text" aria-label={t('common.more')} icon={<OverflowMenuHorizontal size={18} />} />
          </Dropdown>
        </div>
      </header>
      <Tabs
        className={styles.tabs}
        activeKey={related ? 'relations' : route.detail === 'schema' ? 'schema' : 'data'}
        onChange={go}
        items={[
          { key: 'data', label: t('NamespaceView.records') },
          { key: 'schema', label: t('NamespaceView.schema') },
        ]}
        tabBarExtraContent={
          <Button
            type={related ? 'default' : 'text'}
            aria-expanded={related}
            icon={<Link size={15} />}
            onClick={() => go(related ? 'data' : 'relations')}
          >
            {t('NamespaceView.viewRelations')}
          </Button>
        }
      />
      {related ? (
        <CollectionRelations
          collection={collection}
          items={items}
          connections={connections}
          onFlow={onFlow}
          onSelect={onSelect}
        />
      ) : route.detail === 'schema' ? (
        <div className={styles.schema}>
          <SchemaView collection={collection} />
        </div>
      ) : (
        <CollectionRecords key={collection.revision} collection={collection} />
      )}
      <Modal open={route.detail === 'details'} title={t('common.detail')} footer={null} onCancel={() => go('data')}>
        <Descriptions
          column={1}
          size="small"
          items={[
            { key: 'id', label: 'ID', children: collection.id },
            { key: 'path', label: t('UnsUi.path'), children: metadataPath(collection, items) },
            { key: 'created', label: t('UnsUi.createdAt'), children: displayTime(collection.createdAt) },
            { key: 'updated', label: t('UnsMeta.updatedAt'), children: displayTime(collection.updatedAt) },
          ]}
        />
      </Modal>
      <Modal
        open={canEdit && move}
        title={t('NamespaceView.move')}
        confirmLoading={saving}
        okButtonProps={{ disabled: parent === (collection.data.parentId || undefined) }}
        onCancel={() => !saving && setMove(false)}
        onOk={async () => {
          if (!canEdit || saving) return;
          setSaving(true);
          try {
            await metadataApi.save({
              id: collection.id,
              kind: 'collection',
              expectedRevision: collection.revision,
              document: { ...collection.data, parentId: parent },
            });
            setMove(false);
            onRefresh();
          } catch {
            // 接口已显示错误，保留目的目录供重试。
          } finally {
            setSaving(false);
          }
        }}
      >
        <TreeSelect
          allowClear
          showSearch
          treeDefaultExpandAll
          treeDataSimpleMode
          treeNodeFilterProp="title"
          disabled={saving}
          style={{ width: '100%' }}
          value={parent}
          onChange={setParent}
          aria-label={t('UnsMeta.parent')}
          placeholder={t('UnsMeta.root')}
          treeData={items
            .filter((item) => item.kind === 'folder' && !item.mountId)
            .map((item) => ({ id: item.id, pId: item.data.parentId, value: item.id, title: item.data.name }))}
        />
      </Modal>
    </section>
  );
}
