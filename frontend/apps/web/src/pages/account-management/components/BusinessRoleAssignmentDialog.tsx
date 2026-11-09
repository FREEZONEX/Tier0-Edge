import { useState } from 'react';
import { Alert, Button, Form, Select, Space, Tag, Typography } from 'antd';
import { AuthButton } from '@/components/auth';
import { ButtonPermission } from '@/common-types/button-permission';
import ProModal from '@/components/pro-modal';
import { useTranslate } from '@/hooks';
import { useNativeAppRoleDirectory } from '@/hooks/use-native-app-roles';
import {
  replaceBusinessRoleAssignments,
  saveBusinessRole,
  type BusinessRoleDirectory,
  type RoleAssignmentTarget,
} from '@/apis/core-api/native-app';
import styles from './UserGroupsPanel.module.scss';

function AssignmentForm({
  target,
  directory: loaded,
  onClose,
  onBusy,
}: {
  target: RoleAssignmentTarget;
  directory: BusinessRoleDirectory;
  onClose: () => void;
  onBusy: (busy: boolean) => void;
}) {
  const t = useTranslate();
  // Keep the observed assignment set stable while editing.
  const [directory] = useState(loaded);
  const role = directory.roles.find((r) => r.key === target.id);
  const initial =
    target.kind === 'role'
      ? role?.userIds || []
      : directory.roles
          .filter((r) => (target.kind === 'user' ? r.userIds : r.groupIds)?.includes(target.id))
          .map((r) => r.key);
  const [selected, setSelected] = useState(initial);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const options =
    target.kind === 'role'
      ? directory.users.map((u) => ({ value: u.id, label: u.name }))
      : directory.roles.map((r) => ({ value: r.key, label: r.name }));
  const exists =
    target.kind === 'role'
      ? !!role
      : (target.kind === 'user' ? directory.users : directory.groups).some((entry) => entry.id === target.id);
  const missing = selected.filter((id) => !options.some((option) => option.value === id));
  const changed = selected.length !== initial.length || selected.some((id) => !initial.includes(id));
  const inherited =
    target.kind === 'user'
      ? directory.roles.flatMap((r) => {
          const groups = directory.groups.filter((g) => r.groupIds?.includes(g.id) && g.memberIds?.includes(target.id));
          return groups.length ? [{ id: r.key, name: r.name, groups: groups.map((g) => g.name) }] : [];
        })
      : target.kind === 'role'
        ? directory.users.flatMap((user) => {
            const groups = directory.groups.filter(
              (g) => role?.groupIds?.includes(g.id) && g.memberIds?.includes(user.id)
            );
            return groups.length ? [{ id: user.id, name: user.name, groups: groups.map((g) => g.name) }] : [];
          })
        : [];
  const save = async () => {
    if (!exists || !changed || saving || missing.length) return;
    setSaving(true);
    onBusy(true);
    setError('');
    try {
      if (target.kind === 'role' && role) {
        await saveBusinessRole({
          ...role,
          userIds: selected,
          // Deleted groups cannot grant access or be assigned again.
          groupIds: role.groupIds?.filter((id) => directory.groups.some((group) => group.id === id)),
        });
      } else await replaceBusinessRoleAssignments(target, selected, initial);
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('nativeApp.actionFailed'));
    } finally {
      setSaving(false);
      onBusy(false);
    }
  };
  const apps = target.kind === 'role' ? directory.bindings.filter((b) => b.key === target.id).map((b) => b.name) : [];
  return (
    <>
      {!exists && <Alert type="error" message={t('nativeApp.assignmentMissing')} />}
      {error && <Alert type="error" message={error} />}
      <Form layout="vertical" onFinish={() => void save()}>
        <Form.Item
          label={t(target.kind === 'role' ? 'nativeApp.directMembers' : 'nativeApp.directRoles')}
          htmlFor="business-role-assignment"
          tooltip={
            target.kind === 'role'
              ? t('nativeApp.workspaceRoleHint', { apps: apps.join(', ') || '—' })
              : t('nativeApp.directRolesHint')
          }
        >
          <Select
            id="business-role-assignment"
            mode="multiple"
            optionFilterProp="label"
            value={selected}
            onChange={setSelected}
            disabled={saving || !exists}
            options={[...options, ...missing.map((id) => ({ value: id, label: id }))]}
          />
        </Form.Item>
        {inherited.length > 0 && (
          <Form.Item label={t('nativeApp.inheritedRoles')} tooltip={t('nativeApp.inheritedHint')}>
            <Space direction="vertical" size={8}>
              {inherited.map((entry) => (
                <div key={entry.id}>
                  <Tag>{entry.name}</Tag>
                  <Typography.Text type="secondary">{entry.groups.join(', ')}</Typography.Text>
                </div>
              ))}
            </Space>
          </Form.Item>
        )}
        <div className={styles['modal-footer']}>
          <Button disabled={saving} onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <AuthButton
            auth={ButtonPermission['uns.appManage']}
            type="primary"
            htmlType="submit"
            loading={saving}
            disabled={!exists || !changed || !!missing.length}
          >
            {t('common.save')}
          </AuthButton>
        </div>
      </Form>
    </>
  );
}

export default function BusinessRoleAssignmentDialog({
  target,
  onClose,
}: {
  target: RoleAssignmentTarget;
  onClose: () => void;
}) {
  const t = useTranslate();
  const [busy, setBusy] = useState(false);
  const { directory, loading, error, reload } = useNativeAppRoleDirectory();
  return (
    <ProModal
      open
      width={544}
      fullScreenable={false}
      title={t('nativeApp.assignRoles', { name: target.name })}
      onCancel={() => {
        if (!busy) onClose();
      }}
      closable={!busy}
      styles={{ body: { maxHeight: '72vh', overflowY: 'auto' } }}
    >
      {error && !directory ? (
        <Alert
          type="error"
          message={t('nativeApp.roleLoadFailed')}
          action={<Button onClick={() => void reload()}>{t('common.refresh')}</Button>}
        />
      ) : directory ? (
        <AssignmentForm target={target} directory={directory} onClose={onClose} onBusy={setBusy} />
      ) : (
        <div aria-busy={loading}>{t('common.loading')}</div>
      )}
    </ProModal>
  );
}
