import { useState } from 'react';
import { Alert, App, Button, Input, Space, Tag, Tooltip } from 'antd';
import { Search, Trash2 } from 'lucide-react';
import { AuthButton } from '@/components/auth';
import { ButtonPermission } from '@/common-types/button-permission';
import { openConfirmModal } from '@/components/confirm-modal';
import ProTable from '@/components/pro-table';
import {
  deleteBusinessRole,
  getBusinessRoles,
  getEffectiveRoleMembers,
  type WorkspaceRole,
} from '@/apis/core-api/native-app';
import { useTranslate } from '@/hooks';
import { useNativeAppRoleAccess, useNativeAppRoleDirectory } from '@/hooks/use-native-app-roles';
import BusinessRoleAssignmentDialog from './BusinessRoleAssignmentDialog';
import styles from './UserGroupsPanel.module.scss';
import pageStyles from '../index.module.scss';

export default function BusinessRolesPanel({ appId }: { appId?: string }) {
  const t = useTranslate();
  const { modal, message } = App.useApp();
  const allowed = useNativeAppRoleAccess();
  const { directory, loading, error, reload } = useNativeAppRoleDirectory(allowed);
  const [search, setSearch] = useState('');
  const [editing, setEditing] = useState<WorkspaceRole>();
  const [deleting, setDeleting] = useState(false);
  const remove = async (role: WorkspaceRole) => {
    setDeleting(true);
    try {
      const latest = await getBusinessRoles();
      const current = latest.roles.find((r) => r.key === role.key);
      if (!current?.canDelete) {
        message.error(t('nativeApp.roleInUse'));
        void reload();
        return;
      }
      const users = getEffectiveRoleMembers(latest, current);
      const groups = latest.groups.filter((g) => current.groupIds?.includes(g.id));
      openConfirmModal(modal, {
        title: t('nativeApp.removeRole', { name: current.name }),
        danger: true,
        content: (
          <Space direction="vertical">
            <span>{t('nativeApp.deleteRoleImpact')}</span>
            <span>
              {t('nativeApp.members')}
              {' · '}
              {users.map((u) => u.name).join(', ') || '—'}
            </span>
            <span>
              {t('account.groupsTab')}
              {' · '}
              {groups.map((g) => g.name).join(', ') || '—'}
            </span>
          </Space>
        ),
        okText: t('common.delete'),
        cancelText: t('common.cancel'),
        onOk: async () => {
          try {
            await deleteBusinessRole(current);
          } catch (e) {
            message.error(e instanceof Error ? e.message : t('nativeApp.actionFailed'));
            void reload();
            throw e;
          }
        },
      });
    } catch {
      message.error(t('nativeApp.roleLoadFailed'));
    } finally {
      setDeleting(false);
    }
  };
  if (!allowed) return null;
  return (
    <div className={styles['groups-panel']}>
      <div className={styles.toolbar}>
        <Input
          className={styles.search}
          prefix={<Search size={16} />}
          allowClear
          aria-label={t('common.search')}
          placeholder={t('common.search')}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </div>
      {error && (
        <Alert
          type="error"
          message={t('nativeApp.roleLoadFailed')}
          action={<Button onClick={() => void reload()}>{t('common.refresh')}</Button>}
        />
      )}
      {appId && directory && !loading && !error && !directory.bindings.some((b) => b.id === appId) && (
        <Alert type="info" message={t('nativeApp.noAppRoles')} />
      )}
      <ProTable
        rowKey="key"
        className={`${pageStyles.accountTable} ${styles['groups-table']}`}
        loading={loading}
        scroll={{ x: 760 }}
        dataSource={directory?.roles.filter(
          (r) =>
            (!appId || directory.bindings.some((b) => b.key === r.key && b.id === appId)) &&
            (r.name + ' ' + r.key + ' ' + r.description).toLowerCase().includes(search.trim().toLowerCase())
        )}
        pagination={{ pageSize: 10, hideOnSinglePage: true }}
        columns={[
          { title: t('common.name'), dataIndex: 'name', width: 170, ellipsis: true },
          { title: t('nativeApp.roleKey'), dataIndex: 'key', width: 150, ellipsis: true },
          { title: t('common.description'), dataIndex: 'description', ellipsis: true },
          {
            title: t('nativeApp.members'),
            width: 100,
            render: (_: unknown, r: WorkspaceRole) => (
              <AuthButton
                auth={ButtonPermission['uns.appManage']}
                type="text"
                aria-label={t('nativeApp.assignRoles', { name: r.name })}
                disabled={loading || error}
                onClick={() => setEditing(r)}
              >
                {directory ? getEffectiveRoleMembers(directory, r).length : 0}
              </AuthButton>
            ),
          },
          {
            title: t('nativeApp.appliedApps'),
            render: (_: unknown, r: WorkspaceRole) => (
              <Space size={[4, 4]} wrap>
                {directory?.bindings
                  .filter((b) => b.key === r.key)
                  .map((b) => (
                    <Tag key={b.id} icon={b.trashed ? <Trash2 size={12} /> : undefined}>
                      {b.name}
                      {b.trashed ? ` · ${t('nativeApp.trash')}` : ''}
                    </Tag>
                  ))}
              </Space>
            ),
          },
          {
            title: t('common.operation'),
            width: 70,
            render: (_: unknown, r: WorkspaceRole) =>
              r.canDelete &&
              !appId && (
                <Tooltip title={t('common.delete')}>
                  <AuthButton
                    auth={ButtonPermission['uns.appManage']}
                    danger
                    type="text"
                    aria-label={t('common.delete')}
                    icon={<Trash2 size={16} />}
                    disabled={loading || error || deleting}
                    onClick={() => void remove(r)}
                  />
                </Tooltip>
              ),
          },
        ]}
      />
      {editing && (
        <BusinessRoleAssignmentDialog
          target={{ kind: 'role', id: editing.key, name: editing.name }}
          onClose={() => setEditing(undefined)}
        />
      )}
    </div>
  );
}
