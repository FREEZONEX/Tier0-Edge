import { useEffect, useState } from 'react';
import { Alert, App, Breadcrumb, Dropdown, Empty, Input, Space, Tabs } from 'antd';
import { namespaceTree } from './namespace-tree';
import ProTree from '@/components/pro-tree';
import type { MenuProps } from 'antd';
import CopyDialog from './copy-dialog';
import ComLeft from '@/components/com-layout/ComLeft';
import ComContent from '@/components/com-layout/ComContent';
import NamespaceNodeIcon from './namespace-node-icon';
import {
  Add,
  Document,
  Edit,
  Folder,
  Renew,
  TrashCan,
  Link,
  OverflowMenuHorizontal,
  Search,
  Table,
  Flow,
} from '@carbon/icons-react';
import ComLayout from '@/components/com-layout';
import ComButton from '@/components/com-button';
import { openConfirmModal } from '@/components/confirm-modal';
import { useTranslate } from '@/hooks';
import { hasPermission } from '@/utils/auth';
import { ButtonPermission } from '@/common-types/button-permission';
import { useBaseStore } from '@/stores/base';
import { canReadUnsMetadata } from '../../metadata-permission';
import {
  loadMetadata,
  metadataApi,
  type MetaDocument,
  type MetaKind,
  type UnsMetadata,
} from '@/apis/core-api/uns-metadata';
import { useIntegrationRoute } from '../source-stream/IntegrationTabs';
import MetadataEditor from './metadata-editor';
import MetadataRecycleBin from './metadata-recycle-bin';
import TopicContent from './topic-content';
import CollectionDetail from './collection-detail';
import Topology from './topology';
import NamespaceConnections from './connections/NamespaceConnections';
import NamespaceOverview from './namespace-overview';
import useConnections from './use-connections';
import styles from './index.module.scss';
import registryStyles from '../source-stream/index.module.scss';

export default function MetadataWorkspace() {
  const t = useTranslate();
  const { modal, message } = App.useApp();
  const route = useIntegrationRoute();
  const user = useBaseStore((state) => state.currentUserInfo);
  const authEnable = useBaseStore((state) => state.systemInfo?.authEnable);
  useBaseStore((state) => state.buttonList);
  const readable = canReadUnsMetadata(user, authEnable);
  const [items, setItems] = useState<UnsMetadata[]>([]);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const [search, setSearch] = useState('');
  const [namespaceView, setNamespaceView] = useState('data');
  const [flowCollection, setFlowCollection] = useState<string>();
  const [expandedKeys, setExpandedKeys] = useState<string[]>([]);
  const [copiedId, setCopiedId] = useState<string>();
  const [pasting, setPasting] = useState(false);
  const [copyDialog, setCopyDialog] = useState<{ source: UnsMetadata; parentId?: string }>();
  const [editor, setEditor] = useState<{
    kind: MetaKind;
    current?: UnsMetadata;
    parentId?: string;
    initialData?: Partial<MetaDocument>;
  }>();
  const connections = useConnections(refresh, readable);
  const reload = () => setRefresh((value) => value + 1);
  useEffect(() => {
    let alive = true;
    if (!readable) {
      setItems([]);
      setLoading(false);
      setFailed(false);
      return;
    }
    setLoading(true);
    loadMetadata()
      .then((active) => {
        if (alive) {
          setItems(active);
          setFailed(false);
        }
      })
      .catch(() => {
        if (alive) setFailed(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [refresh, readable, user?.sub]);
  const trashOpen = route.detail === 'trash';
  const selected = readable && !trashOpen ? items.find((item) => item.id === route.metaId) : undefined;
  useEffect(() => {
    const ancestors = new Set<string>();
    let parent = items.find((item) => item.id === route.metaId)?.data.parentId;
    while (parent && !ancestors.has(parent)) {
      ancestors.add(parent);
      parent = items.find((item) => item.id === parent)?.data.parentId;
    }
    if (ancestors.size) setExpandedKeys((keys) => [...new Set([...keys, ...ancestors])]);
  }, [items, route.metaId]);
  const collection =
    selected?.kind === 'collection' ? selected : items.find((item) => item.id === selected?.data.collectionId);
  const select = (id?: string) => {
    route.go({ view: 'namespace', metaId: id, detail: trashOpen ? 'data' : route.detail });
  };
  const create = (parent?: UnsMetadata, kind: MetaKind = 'collection', useSelection = true) => {
    if (loading || failed) return;
    const target = parent || (useSelection ? selected : undefined);
    if (target?.mountId) return;
    setEditor({ kind, parentId: target?.kind === 'folder' ? target.id : target?.data.parentId });
  };
  const canWrite = hasPermission(ButtonPermission['uns.metadataWrite']);
  const createMenu = (parent?: UnsMetadata) => ({
    items: [
      { key: 'collection', label: t('NamespaceGuide.newDataset'), icon: <Document />, disabled: !canWrite },
      { key: 'folder', label: t('UnsUi.createFolder'), icon: <Folder />, disabled: !canWrite },
      { key: 'topic', label: t('UnsMeta.addReference'), icon: <Link />, disabled: !canWrite },
    ],
    onClick: ({ key }: { key: string }) => create(parent, key as MetaKind),
  });
  const tree = namespaceTree(items, search);
  const breadcrumb: UnsMetadata[] = [];
  let node = selected;
  const visited = new Set<string>();
  while (node && !visited.has(node.id)) {
    visited.add(node.id);
    breadcrumb.unshift(node);
    node = items.find((item) => item.id === node?.data.parentId);
  }
  const deleteNode = (meta: UnsMetadata) =>
    openConfirmModal(modal, {
      title: t('common.delete'),
      content: t('UnsMeta.archiveHint'),
      okText: t('common.delete'),
      cancelText: t('common.cancel'),
      danger: true,
      onOk: async () => {
        await metadataApi.state(meta, false);
        message.success(t('common.deleteSuccessfully'));
        reload();
        route.go({ view: 'namespace' });
      },
    });
  const list = items.filter(
    (item) =>
      (item.data.parentId || '') === (selected?.kind === 'folder' ? selected.id : '') &&
      (!search || item.data.name.toLowerCase().includes(search.toLowerCase()))
  );
  const topology = selected && (
    <Topology meta={selected} items={items} connections={connections} onSelect={select} onRefresh={reload} />
  );
  const subtreeFolders = (id: string) => {
    const descendants = new Set([id]);
    for (const parentId of descendants) {
      items.forEach((item) => {
        if (item.kind === 'folder' && item.data.parentId === parentId) descendants.add(item.id);
      });
    }
    return descendants;
  };
  const copyNode = (meta: UnsMetadata) => {
    if (meta.kind === 'collection') return;
    setCopiedId(meta.id);
    message.success(t('common.copySuccess'));
  };
  const onCopied = (saved: UnsMetadata) => {
    setCopyDialog(undefined);
    reload();
    route.go({ view: 'namespace', metaId: saved.id, detail: route.detail }, true);
  };
  const pasteNode = async (source: UnsMetadata, parentId?: string) => {
    if (pasting) return;
    setPasting(true);
    try {
      const saved = await metadataApi.copy(source, { parentId });
      message.success(t('uns.pasteSuccess'));
      onCopied(saved);
    } catch {
      // The API interceptor reports failed requests; never show a successful paste.
    } finally {
      setPasting(false);
    }
  };
  const contextMenu = (meta?: UnsMetadata): NonNullable<MenuProps['items']> => {
    if (meta?.mountId) return [{ key: 'refresh', label: t('common.refresh'), onClick: reload }];
    const copied = items.find((item) => item.id === copiedId);
    const parentId = meta?.kind === 'folder' ? meta.id : meta?.data.parentId;
    const blocked =
      !canWrite ||
      pasting ||
      !copied ||
      (copied.kind === 'folder' && !!parentId && subtreeFolders(copied.id).has(parentId));
    return [
      ...(meta?.kind === 'folder' ? [{ key: 'refresh', label: t('common.refresh'), onClick: reload }] : []),
      ...(meta && meta.kind !== 'collection'
        ? [{ key: 'copy', label: t('common.copy'), onClick: () => copyNode(meta) }]
        : []),
      {
        key: 'paste',
        label: t('common.paste'),
        disabled: blocked,
        onClick: () => copied && void pasteNode(copied, parentId),
      },
      {
        key: 'pasteAndEdit',
        label: t('common.pasteAndEdit'),
        disabled: blocked,
        onClick: () => copied && setCopyDialog({ source: copied, parentId }),
      },
      ...createMenu(meta).items.map((item) => ({
        key: item.key,
        label: item.label,
        disabled: item.disabled,
        onClick: () => create(meta, item.key as MetaKind, false),
      })),
      ...(meta?.kind === 'folder'
        ? [
            {
              key: 'expandFolder',
              label: t('common.expandFolder'),
              onClick: () => {
                setSearch('');
                setExpandedKeys((keys) => [...new Set([...keys, ...subtreeFolders(meta.id)])]);
              },
            },
            {
              key: 'collapseFolder',
              label: t('common.collapseFolder'),
              onClick: () => {
                setSearch('');
                const collapsed = subtreeFolders(meta.id);
                setExpandedKeys((keys) => keys.filter((key) => !collapsed.has(key)));
              },
            },
          ]
        : []),
      ...(meta
        ? [
            { type: 'divider' as const },
            {
              key: 'delete',
              label: t('common.delete'),
              danger: true,
              disabled: !hasPermission(ButtonPermission['uns.metadataDelete']),
              onClick: () => deleteNode(meta),
            },
          ]
        : []),
    ];
  };
  const sidebar = (
    <div className={registryStyles.sidebar}>
      <div className={styles['tree-toolbar']}>
        <Input
          allowClear
          prefix={<Search size={16} />}
          value={search}
          aria-label={t('SourceStream.search')}
          placeholder={t('SourceStream.search')}
          onChange={(event) => setSearch(event.target.value)}
        />
        <ComButton
          type="text"
          size="small"
          icon={<Renew size={16} />}
          aria-label={t('UnsMeta.refresh')}
          onClick={reload}
        />
        <Dropdown trigger={['click']} menu={createMenu()}>
          <ComButton
            type="text"
            size="small"
            auth={ButtonPermission['uns.metadataWrite']}
            icon={<Add size={16} />}
            aria-label={t('UnsMeta.create')}
            disabled={trashOpen || loading || failed || !!selected?.mountId}
          />
        </Dropdown>
        <Dropdown
          trigger={['click']}
          menu={{
            items: [
              {
                key: 'expand',
                label: t('NamespaceView.expand'),
                onClick: () => setExpandedKeys(items.filter((item) => item.kind === 'folder').map((item) => item.id)),
              },
              { key: 'collapse', label: t('NamespaceView.collapse'), onClick: () => setExpandedKeys([]) },
            ],
          }}
        >
          <ComButton
            type="text"
            size="small"
            icon={<OverflowMenuHorizontal size={16} />}
            aria-label={t('SourceStream.manage')}
          />
        </Dropdown>
      </div>
      <div className={styles['namespace-tree']}>
        <ProTree
          height={0}
          wrapperStyle={{ padding: '0 14px' }}
          loading={loading || pasting}
          treeNodeIcon={(node) => <NamespaceNodeIcon node={node} items={items} />}
          matchHighlightValue={search}
          rightClickMenuItems={({ node }) => contextMenu(items.find((item) => item.id === node?.key))}
          treeNodeExtra={(node) => {
            const meta = items.find((item) => item.id === node.key);
            if (!meta || meta.mountId) return null;
            return (
              <Dropdown trigger={['click']} menu={{ items: contextMenu(meta) }}>
                <ComButton
                  type="text"
                  size="small"
                  icon={<OverflowMenuHorizontal size={16} />}
                  aria-label={t('SourceStream.manage')}
                  onClick={(event) => event.stopPropagation()}
                />
              </Dropdown>
            );
          }}
          expandedKeys={search ? items.filter((item) => item.kind === 'folder').map((item) => item.id) : expandedKeys}
          onExpand={(keys) => setExpandedKeys(keys.map(String))}
          treeData={tree}
          selectedKeys={selected ? [selected.id] : []}
          onSelect={(keys) => select(keys[0]?.toString())}
          empty={<Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('UnsUi.empty')} />}
        />
      </div>
      <div className={styles['recycle-toggle']}>
        <ComButton
          type={trashOpen ? 'primary' : 'text'}
          icon={<TrashCan />}
          onClick={() => route.go({ view: 'namespace', detail: trashOpen ? 'data' : 'trash' })}
        >
          {t('UnsUi.recycleBin')}
        </ComButton>
      </div>
    </div>
  );
  const content =
    trashOpen && readable ? (
      <MetadataRecycleBin
        key={String(user?.sub)}
        identity={String(user?.sub || 'local')}
        canRestore={canWrite}
        canDelete={hasPermission(ButtonPermission['uns.metadataDelete'])}
        onChanged={reload}
      />
    ) : readable && !failed && selected?.kind === 'collection' ? (
      <CollectionDetail
        key={selected.id}
        collection={selected}
        items={items}
        connections={connections}
        canEdit={canWrite && !selected.mountId}
        canDelete={hasPermission(ButtonPermission['uns.metadataDelete']) && !selected.mountId}
        onEdit={() => setEditor({ kind: 'collection', current: selected })}
        onArchive={() => deleteNode(selected)}
        onReference={() =>
          setEditor({
            kind: 'topic',
            parentId: selected.data.parentId,
            initialData: { collectionId: selected.id, name: selected.data.name },
          })
        }
        onFlow={
          hasPermission(ButtonPermission['uns.streams'])
            ? () => {
                setFlowCollection(selected.id);
                setNamespaceView('flow');
              }
            : undefined
        }
        onSelect={select}
        onRefresh={reload}
      />
    ) : (
      <div className={styles.content}>
        <div className={`${styles.header} ${!selected ? styles['root-header'] : ''}`}>
          <div>
            <Breadcrumb
              items={[
                { title: <a onClick={() => select()}>{t('UnsMeta.namespace')}</a> },
                ...breadcrumb.map((item) => ({ title: <a onClick={() => select(item.id)}>{item.data.name}</a> })),
              ]}
            />
            {selected && (
              <h2>
                <NamespaceNodeIcon
                  node={{ key: selected.id, title: selected.data.name, pathType: selected.kind === 'folder' ? 0 : 2 }}
                  items={items}
                />{' '}
                {selected.data.name}
              </h2>
            )}
          </div>
          <Space>
            {selected?.kind === 'folder' && canWrite && (
              <Dropdown trigger={['click']} menu={createMenu(selected)}>
                <ComButton icon={<Add size={16} />}>{t('UnsMeta.create')}</ComButton>
              </Dropdown>
            )}
            {selected && !selected.mountId && (
              <Dropdown
                trigger={['click']}
                menu={{
                  items: [
                    ...(selected.kind === 'folder'
                      ? createMenu(selected).items.map((item) => ({
                          ...item,
                          onClick: () => create(selected, item.key as MetaKind),
                        }))
                      : []),
                    {
                      key: 'edit',
                      label: t('UnsMeta.nodeSettings'),
                      icon: <Edit />,
                      disabled: !canWrite,
                      onClick: () => setEditor({ kind: selected.kind, current: selected }),
                    },
                    ...(collection && selected.kind !== 'collection'
                      ? [
                          {
                            key: 'schema',
                            label: t('UnsUi.editSchema'),
                            icon: <Edit />,
                            disabled: !canWrite || !!collection.mountId,
                            onClick: () => setEditor({ kind: 'collection', current: collection }),
                          },
                        ]
                      : []),
                    { type: 'divider' },
                    {
                      key: 'delete',
                      label: t('common.delete'),
                      icon: <TrashCan />,
                      danger: true,
                      disabled: !hasPermission(ButtonPermission['uns.metadataDelete']),
                      onClick: () => deleteNode(selected),
                    },
                  ],
                }}
              >
                <ComButton
                  type="text"
                  icon={<OverflowMenuHorizontal size={18} />}
                  aria-label={t('SourceStream.manage')}
                />
              </Dropdown>
            )}
          </Space>
        </div>
        {!readable ? (
          <Alert type="warning" message={t('common.noPermission')} />
        ) : failed ? (
          <Alert
            type="error"
            message={t('UnsMeta.loadFailed')}
            action={<ComButton onClick={reload}>{t('UnsMeta.retry')}</ComButton>}
          />
        ) : (
          <>
            {route.metaId && !selected && !loading && <Alert type="warning" message={t('UnsMeta.notFound')} />}
            {!selected ? (
              <NamespaceOverview
                items={items}
                loading={loading}
                onSelect={select}
                onCreate={canWrite ? () => create(undefined, 'collection', false) : undefined}
              />
            ) : selected.kind === 'folder' ? (
              <div className={styles['folder-content']}>
                <div className={styles['folder-grid']}>
                  {list.map((item) => (
                    <ComButton key={item.id} onClick={() => select(item.id)}>
                      <NamespaceNodeIcon
                        node={{ key: item.id, title: item.data.name, pathType: item.kind === 'folder' ? 0 : 2 }}
                        items={items}
                      />
                      <span>{item.data.name}</span>
                    </ComButton>
                  ))}
                </div>
                {!list.length && (
                  <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('NamespaceView.folderEmpty')}>
                    {canWrite && (
                      <Space>
                        <ComButton onClick={() => create(selected, 'collection')}>
                          {t('NamespaceGuide.newDataset')}
                        </ComButton>
                        <ComButton onClick={() => create(selected, 'folder')}>{t('UnsUi.createFolder')}</ComButton>
                      </Space>
                    )}
                  </Empty>
                )}
              </div>
            ) : (
              <TopicContent
                meta={selected}
                collection={collection}
                items={items}
                path={breadcrumb.map((item) => item.data.name).join(' / ')}
                topology={topology}
                onSelect={select}
                onEditSchema={() => collection && setEditor({ kind: 'collection', current: collection })}
              />
            )}
          </>
        )}
      </div>
    );
  return (
    <ComLayout className={`unsContainer ${styles.workspace}`}>
      <Tabs
        className={styles['view-tabs']}
        activeKey={namespaceView}
        onChange={(view) => {
          setFlowCollection(undefined);
          setNamespaceView(view);
        }}
        items={[
          { key: 'data', label: t('NamespaceView.data'), icon: <Table size={15} /> },
          ...(hasPermission(ButtonPermission['uns.streams'])
            ? [{ key: 'flow', label: t('NamespaceView.connections'), icon: <Flow size={15} /> }]
            : []),
        ]}
      />
      <div className={styles['workspace-body']}>
        {namespaceView === 'flow' && hasPermission(ButtonPermission['uns.streams']) ? (
          <NamespaceConnections
            collectionId={flowCollection}
            sourceAllowed={hasPermission(ButtonPermission['uns.sources'])}
            metadataAllowed={readable}
            refreshKey={refresh}
            onOpenResource={(id) => {
              setNamespaceView('data');
              select(id);
            }}
          />
        ) : (
          <>
            <ComLeft defaultWidth={300} resize style={{ height: '100%', overflow: 'hidden' }}>
              {sidebar}
            </ComLeft>
            <ComContent wrapperStyle={{ minWidth: 0 }} style={{ padding: 0, display: 'flex', minHeight: 0 }}>
              {content}
            </ComContent>
          </>
        )}
      </div>
      {copyDialog && (
        <CopyDialog {...copyDialog} items={items} onClose={() => setCopyDialog(undefined)} onSaved={onCopied} />
      )}
      {editor && (
        <MetadataEditor
          {...editor}
          items={items}
          connections={connections}
          standalone={editor.current?.kind === 'collection' && selected?.kind === 'collection'}
          onClose={() => setEditor(undefined)}
          onSaved={(saved) => {
            setEditor(undefined);
            reload();
            route.go(
              {
                view: 'namespace',
                metaId: saved.kind === 'collection' && selected?.kind === 'topic' ? selected.id : saved.id,
                detail: 'data',
              },
              true
            );
          }}
        />
      )}
    </ComLayout>
  );
}
