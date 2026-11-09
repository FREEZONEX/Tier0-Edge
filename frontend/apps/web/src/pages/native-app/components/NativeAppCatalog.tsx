import { useState } from 'react';
import { Button, Empty, Flex, Input, Spin, Tabs, Tooltip } from 'antd';
import {
  CheckCircle2,
  Circle,
  CircleAlert,
  CirclePause,
  ExternalLink,
  LoaderCircle,
  Pencil,
  RotateCcw,
  Search,
  Settings,
  Shield,
  Trash2,
  Upload,
} from 'lucide-react';
import type { NativeAppInstallation } from '@/apis/core-api/native-app';
import ViewModeSegmented from '@/components/lucide-icon/ViewModeSegmented';
import ProTable from '@/components/pro-table';
import { useTranslate } from '@/hooks';
import AppIcon from '@/pages/launchpad/components/AppIcon';
import empty from '@/assets/common/empty.svg';
import searchEmpty from '@/assets/common/search.svg';
import grid from '@/pages/launchpad/components/AppGrid.module.scss';
import card from '@/pages/launchpad/components/AppCard.module.scss';
import list from '@/pages/project/components/tabs-content/apps/index.module.scss';
import styles from './NativeAppPanel.module.scss';

const statuses = {
  running: [CheckCircle2, list.statusActive],
  starting: [LoaderCircle, list.statusDeploying],
  stopped: [CirclePause, list.statusInactive],
  failed: [CircleAlert, list.statusFailed],
} as const;

export function NativeAppStatus({ status }: { status: string }) {
  const t = useTranslate();
  const [Icon, color] = statuses[status as keyof typeof statuses] || [Circle, styles.neutralStatus];
  return (
    <span className={`${list.tag} ${color}`}>
      <Icon size={14} />
      {t(`nativeApp.status.${status}`)}
    </span>
  );
}

interface CatalogProps {
  apps: NativeAppInstallation[];
  canManage: boolean;
  archived: boolean;
  launchpad: boolean;
  loading: boolean;
  busy: boolean;
  onEdit: (app: NativeAppInstallation) => void;
  onAccess: (app: NativeAppInstallation) => void;
  onManage: (app: NativeAppInstallation) => void;
  onImport: (app: NativeAppInstallation) => void;
  onTrash: (app: NativeAppInstallation) => void;
  onRestore: (app: NativeAppInstallation) => void;
  onArchiveChange: (archived: boolean) => void;
  onRefresh: () => void;
}

export default function NativeAppCatalog({
  apps,
  canManage,
  archived,
  launchpad,
  loading,
  busy,
  onEdit,
  onAccess,
  onManage,
  onImport,
  onTrash,
  onRestore,
  onRefresh,
  onArchiveChange,
}: CatalogProps) {
  const t = useTranslate();
  const [view, setView] = useState(launchpad ? 'card' : 'list');
  const [search, setSearch] = useState('');
  const keyword = search.trim().toLowerCase();
  const visible = apps.filter((app) => (app.name + ' ' + (app.description || '')).toLowerCase().includes(keyword));
  const emptyState = (
    <Empty
      image={<img src={keyword ? searchEmpty : empty} alt="" />}
      description={t(keyword ? 'Launchpad.searchEmptyTitle' : 'common.noData')}
    />
  );

  const actions = (app: NativeAppInstallation, inCard = false) => {
    const iconAction = (label: string, Icon: typeof Pencil, action: CatalogProps['onEdit'], danger = false) => (
      <Tooltip title={t(label)}>
        <button
          type="button"
          className={`${list.operationButton} ${danger ? list.operationButtonDanger : ''}`}
          disabled={busy}
          aria-label={t(label)}
          onClick={() => action(app)}
        >
          <Icon size={16} strokeWidth={1.75} />
        </button>
      </Tooltip>
    );
    return (
      <div className={inCard ? styles.cardActions : `${list.operation} ${styles.rowActions}`}>
        {!archived && !inCard && app.canOpen && (
          <Tooltip title={t('nativeApp.open')}>
            <a
              className={list.operationButton}
              aria-label={t('nativeApp.open')}
              href={app.entryPath}
              target="_blank"
              rel="noopener noreferrer"
            >
              <ExternalLink size={16} strokeWidth={1.75} />
            </a>
          </Tooltip>
        )}
        {canManage &&
          (archived ? (
            iconAction('nativeApp.restore', RotateCcw, onRestore)
          ) : (
            <>
              {iconAction('common.edit', Pencil, onEdit)}
              {iconAction('nativeApp.access', Shield, onAccess)}
              {app.status === 'notInstalled'
                ? iconAction('project.importApp', Upload, onImport)
                : iconAction('nativeApp.manage', Settings, onManage)}
              {iconAction('nativeApp.moveToTrash', Trash2, onTrash, true)}
            </>
          ))}
      </div>
    );
  };

  const content = (
    <>
      <div className={styles.toolbar}>
        <Input
          allowClear
          aria-label={t('common.search')}
          placeholder={t('common.searchPlaceholder')}
          prefix={<Search size={16} strokeWidth={1.75} />}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <Flex align="center" gap={8}>
          <ViewModeSegmented
            value={view}
            onChange={setView}
            listTitle={t('nativeApp.listView')}
            cardTitle={t('nativeApp.gridView')}
          />
          <Tooltip title={t('common.refresh')}>
            <Button
              type="text"
              aria-label={t('common.refresh')}
              icon={<RotateCcw size={16} />}
              loading={loading}
              onClick={onRefresh}
            />
          </Tooltip>
        </Flex>
      </div>
      {view === 'card' ? (
        <Spin spinning={loading && visible.length === 0}>
          {visible.length ? (
            <div className={grid.appGrid}>
              {visible.map((app) => (
                <article key={app.appId} className={`${grid.appCard} ${card.appCard} ${styles.catalogCard}`}>
                  <div className={card.thumbnail}>
                    <span className={card.appIcon}>
                      <AppIcon size={28} />
                    </span>
                  </div>
                  <div className={card.info}>
                    <div className={card.appName} title={app.name}>
                      {app.canOpen && !archived ? (
                        <a className={styles.cardLink} href={app.entryPath} target="_blank" rel="noopener noreferrer">
                          {app.name}
                        </a>
                      ) : (
                        app.name
                      )}
                    </div>
                    {app.description && (
                      <span className={list.descriptionCell} title={app.description}>
                        {app.description}
                      </span>
                    )}
                    <div className={card.footer}>
                      <NativeAppStatus status={app.status} />
                      {canManage ? actions(app, true) : app.version}
                    </div>
                  </div>
                </article>
              ))}
            </div>
          ) : (
            emptyState
          )}
        </Spin>
      ) : (
        <ProTable
          rowKey="appId"
          loading={loading}
          dataSource={visible}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          scroll={{ x: 760 }}
          locale={{ emptyText: emptyState }}
          columns={[
            {
              title: t('common.name'),
              dataIndex: 'name',
              width: 220,
              sorter: (a: NativeAppInstallation, b: NativeAppInstallation) => a.name.localeCompare(b.name),
              render: (name) => (
                <div className={list.nameCell}>
                  <span className={list.nameIcon}>
                    <AppIcon size={14} />
                  </span>
                  <span className={list.nameText} title={name}>
                    {name}
                  </span>
                </div>
              ),
            },
            {
              title: t('common.description'),
              dataIndex: 'description',
              ellipsis: true,
              render: (value) => (
                <span className={list.descriptionCell} title={value}>
                  {value || '—'}
                </span>
              ),
            },
            { title: t('nativeApp.version'), dataIndex: 'version', width: 100, render: (value) => value || '—' },
            {
              title: t('common.status'),
              dataIndex: 'status',
              width: 132,
              render: (status) => <NativeAppStatus status={status} />,
            },
            {
              title: t('common.operation'),
              key: 'action',
              width: canManage && !archived ? 180 : 100,
              render: (_, app: NativeAppInstallation) => actions(app),
            },
          ]}
        />
      )}
    </>
  );
  return canManage ? (
    <Tabs
      activeKey={archived ? 'trash' : 'all'}
      onChange={(key) => onArchiveChange(key === 'trash')}
      items={[
        { key: 'all', label: t('nativeApp.allApps'), children: !archived && content },
        { key: 'trash', label: t('nativeApp.trash'), children: archived && content },
      ]}
    />
  ) : (
    content
  );
}
