import { App, Button, Empty, Form, Input, Select, Space, Tooltip } from 'antd';
import { Pencil, Plus, Search, Trash2 } from 'lucide-react';
import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useState } from 'react';

import {
  createUserGroup,
  deleteUserGroup,
  getUserGroupMembers,
  getUserGroups,
  getUserManageList,
  replaceUserGroupMembers,
  updateUserGroup,
  type UserGroup,
  type UserGroupMember,
} from '@/apis/core-api/user-manage';
import { AuthButton } from '@/components/auth';
import { ButtonPermission } from '@/common-types/button-permission';
import { openConfirmModal } from '@/components/confirm-modal';
import { useNativeAppRoleAccess } from '@/hooks/use-native-app-roles';
import BusinessRoleAssignmentDialog from './BusinessRoleAssignmentDialog';
import ProModal from '@/components/pro-modal';
import ProTable from '@/components/pro-table';
import { useTranslate } from '@/hooks';
import { formatTimestamp } from '@/utils/format';
import { MAX_LENGTHS } from '@/utils/limits';

import pageStyles from '../index.module.scss';
import UserGroupMembersDrawer from './UserGroupMembersDrawer';
import styles from './UserGroupsPanel.module.scss';

type GroupFormValues = {
  groupName: string;
  description?: string;
};

type UserOption = {
  label: string;
  value: string;
};

const pageSizeOptions = [10, 20, 30, 50, 100];

export type UserGroupsPanelHandle = {
  openCreate: () => void;
};

const UserGroupsPanel = forwardRef<UserGroupsPanelHandle>((_, ref) => {
  const formatMessage = useTranslate();
  const { message, modal } = App.useApp();
  const [form] = Form.useForm<GroupFormValues>();
  const [groups, setGroups] = useState<UserGroup[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);
  const [keywordInput, setKeywordInput] = useState('');
  const [keyword, setKeyword] = useState('');
  const appRolesEnabled = useNativeAppRoleAccess();
  const [roleGroup, setRoleGroup] = useState<UserGroup>();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [formOpen, setFormOpen] = useState(false);
  const [editingGroup, setEditingGroup] = useState<UserGroup>();
  const [selectedGroup, setSelectedGroup] = useState<UserGroup>();
  const [members, setMembers] = useState<UserGroupMember[]>([]);
  const [membersLoading, setMembersLoading] = useState(false);
  const [addMemberOpen, setAddMemberOpen] = useState(false);
  const [allUserOptions, setAllUserOptions] = useState<UserOption[]>([]);
  const [selectedUserIds, setSelectedUserIds] = useState<string[]>([]);
  const [membersSaving, setMembersSaving] = useState(false);

  const loadGroups = useCallback(async () => {
    setLoading(true);
    try {
      const response = await getUserGroups({ keyword, pageNo: page, pageSize: size });
      setGroups(response.list);
      setTotal(response.total);
    } finally {
      setLoading(false);
    }
  }, [keyword, page, size]);

  useEffect(() => {
    void loadGroups();
  }, [loadGroups]);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setPage(1);
      setKeyword(keywordInput.trim());
    }, 300);
    return () => window.clearTimeout(timer);
  }, [keywordInput]);

  const loadMembers = useCallback(async (group: UserGroup) => {
    setMembersLoading(true);
    try {
      const response = await getUserGroupMembers(group.groupId);
      setMembers(response.list);
      setSelectedGroup((current) => (current ? { ...current, memberCount: response.total } : current));
    } finally {
      setMembersLoading(false);
    }
  }, []);

  const openCreate = () => {
    setEditingGroup(undefined);
    form.resetFields();
    setFormOpen(true);
  };

  useImperativeHandle(ref, () => ({ openCreate }));

  const openEdit = (group: UserGroup) => {
    setEditingGroup(group);
    form.setFieldsValue({ groupName: group.groupName, description: group.description });
    setFormOpen(true);
  };

  const closeGroupForm = () => {
    if (saving) return;
    setFormOpen(false);
    setEditingGroup(undefined);
    form.resetFields();
  };

  const saveGroup = async () => {
    const values = await form.validateFields();
    setSaving(true);
    try {
      const payload = {
        groupName: values.groupName.trim(),
        description: values.description?.trim() || '',
      };
      if (editingGroup) {
        await updateUserGroup(editingGroup.groupId, payload);
      } else {
        await createUserGroup(payload);
      }
      setFormOpen(false);
      setEditingGroup(undefined);
      form.resetFields();
      message.success(formatMessage('common.optsuccess'));
      await loadGroups();
    } finally {
      setSaving(false);
    }
  };

  const confirmDeleteGroup = (group: UserGroup) => {
    openConfirmModal(modal, {
      title: formatMessage('account.group.deleteTitle'),
      content: formatMessage('account.group.deleteCascadeDesc', {
        name: group.groupName,
        count: group.memberCount,
      }),
      okText: formatMessage('common.delete'),
      cancelText: formatMessage('common.cancel'),
      danger: true,
      onOk: async () => {
        await deleteUserGroup(group.groupId);
        if (selectedGroup?.groupId === group.groupId) setSelectedGroup(undefined);
        message.success(formatMessage('common.deleteSuccessfully'));
        await loadGroups();
      },
    });
  };

  const openMembers = async (group: UserGroup) => {
    setSelectedGroup(group);
    setMembers([]);
    await loadMembers(group);
  };

  const replaceMembers = async (userIds: string[]) => {
    if (!selectedGroup) return;
    setMembersSaving(true);
    try {
      await replaceUserGroupMembers(selectedGroup.groupId, userIds);
      message.success(formatMessage('common.optsuccess'));
      await Promise.all([loadMembers(selectedGroup), loadGroups()]);
    } finally {
      setMembersSaving(false);
    }
  };

  const confirmRemoveMember = (member: UserGroupMember) => {
    if (!selectedGroup) return;
    openConfirmModal(modal, {
      title: formatMessage('account.group.removeMemberTitle'),
      content: formatMessage('account.group.removeMemberDesc', {
        user: member.displayName || member.userName,
        group: selectedGroup.groupName,
      }),
      okText: formatMessage('account.group.removeMember'),
      cancelText: formatMessage('common.cancel'),
      danger: true,
      onOk: () => replaceMembers(members.filter((item) => item.userId !== member.userId).map((item) => item.userId)),
    });
  };

  const confirmRemoveSelected = (userIds: string[]) => {
    if (!selectedGroup || !userIds.length) return;
    const selectedSet = new Set(userIds);
    openConfirmModal(modal, {
      title: formatMessage('account.group.removeSelectedTitle'),
      content: formatMessage('account.group.removeSelectedDesc', {
        count: userIds.length,
        group: selectedGroup.groupName,
      }),
      okText: formatMessage('account.group.removeFromGroup'),
      cancelText: formatMessage('common.cancel'),
      danger: true,
      onOk: () => replaceMembers(members.filter((item) => !selectedSet.has(item.userId)).map((item) => item.userId)),
    });
  };

  const openAddMembers = async () => {
    const response = await getUserManageList({ pageNo: 1, pageSize: 1_000_000 });
    setAllUserOptions(
      (response.data || []).map((user: any) => ({
        value: String(user.userId || user.id),
        label:
          user.firstName && user.firstName !== user.preferredUsername
            ? `${user.firstName} (${user.preferredUsername})`
            : user.preferredUsername,
      }))
    );
    setSelectedUserIds([]);
    setAddMemberOpen(true);
  };

  const availableUserOptions = useMemo(() => {
    const memberIdSet = new Set(members.map((member) => member.userId));
    return allUserOptions.filter((option) => !memberIdSet.has(option.value));
  }, [allUserOptions, members]);

  const addSelectedMembers = async () => {
    const nextIds = Array.from(new Set([...members.map((member) => member.userId), ...selectedUserIds]));
    await replaceMembers(nextIds);
    setAddMemberOpen(false);
    setSelectedUserIds([]);
  };

  const columns = [
    {
      dataIndex: 'groupName',
      titleIntlId: 'account.group.name',
      ellipsis: true,
      width: 220,
      minWidth: 180,
      maxWidth: 280,
      render: (name: string, group: UserGroup) => (
        <Tooltip title={name} placement="topLeft">
          <button type="button" className={styles['group-name']} onClick={() => void openMembers(group)}>
            <span>{name}</span>
          </button>
        </Tooltip>
      ),
    },
    {
      dataIndex: 'description',
      titleIntlId: 'common.description',
      ellipsis: true,
      width: 320,
      minWidth: 240,
      maxWidth: 520,
      render: (value: string) => (
        <Tooltip title={value || undefined} placement="topLeft">
          <span className={styles['cell-ellipsis']}>{value || '-'}</span>
        </Tooltip>
      ),
    },
    {
      dataIndex: 'memberCount',
      titleIntlId: 'account.group.memberCount',
      width: 130,
      minWidth: 120,
      maxWidth: 150,
      render: (count: number, group: UserGroup) => (
        <button type="button" className={styles['member-count']} onClick={() => void openMembers(group)}>
          {formatMessage('account.group.memberUnit', { count })}
        </button>
      ),
    },
    {
      dataIndex: 'updatedTime',
      titleIntlId: 'account.group.updatedAt',
      width: 180,
      minWidth: 160,
      maxWidth: 200,
      render: (value: number) => (value ? formatTimestamp(value, 'YYYY/MM/DD HH:mm', true) : '-'),
    },
    ...(appRolesEnabled
      ? [
          {
            key: 'businessRoles',
            titleIntlId: 'nativeApp.businessRoles',
            width: 140,
            render: (_: unknown, group: UserGroup) => (
              <AuthButton
                auth={ButtonPermission['uns.appManage']}
                type="text"
                onClick={(event) => {
                  event.stopPropagation();
                  setRoleGroup(group);
                }}
              >
                {formatMessage('nativeApp.assign')}
              </AuthButton>
            ),
          },
        ]
      : []),
    {
      key: 'operation',
      titleIntlId: 'common.operation',
      fixed: 'right',
      width: 108,
      render: (_: unknown, group: UserGroup) => (
        <Space size={2} className={styles['operation-buttons']}>
          <Tooltip title={formatMessage('common.edit')}>
            <Button
              type="text"
              icon={<Pencil size={16} strokeWidth={1.75} />}
              aria-label={formatMessage('common.edit')}
              onClick={() => openEdit(group)}
            />
          </Tooltip>
          <Tooltip title={formatMessage('common.delete')}>
            <Button
              type="text"
              danger
              icon={<Trash2 size={16} strokeWidth={1.75} />}
              aria-label={formatMessage('common.delete')}
              onClick={() => confirmDeleteGroup(group)}
            />
          </Tooltip>
        </Space>
      ),
    },
  ];

  return (
    <div className={styles['groups-panel']}>
      <div className={styles.toolbar}>
        <Input
          allowClear
          value={keywordInput}
          onChange={(event) => setKeywordInput(event.target.value)}
          prefix={<Search size={16} strokeWidth={1.75} />}
          placeholder={formatMessage('account.group.searchPlaceholder')}
          className={styles.search}
        />
        <span className={styles['result-count']}>{formatMessage('account.group.resultCount', { count: total })}</span>
      </div>

      <ProTable
        loading={loading}
        className={`${pageStyles.accountTable} ${styles['groups-table']}`}
        rowKey="groupId"
        dataSource={groups}
        columns={columns as any}
        onRow={(group: UserGroup) => ({
          onClick: () => void openMembers(group),
        })}
        resizeable
        scroll={{ x: 1000, y: 'calc(100vh - 350px)' }}
        locale={{ emptyText: <Empty description={formatMessage('account.group.empty')} /> }}
        pagination={{
          current: page,
          pageSize: size,
          total,
          showQuickJumper: true,
          showSizeChanger: true,
          pageSizeOptions,
          onChange: (nextPage, nextSize) => {
            setPage(nextPage);
            setSize(nextSize);
          },
        }}
      />

      {appRolesEnabled && roleGroup && (
        <BusinessRoleAssignmentDialog
          target={{ kind: 'group', id: String(roleGroup.groupId), name: roleGroup.groupName }}
          onClose={() => setRoleGroup(undefined)}
        />
      )}
      <ProModal
        open={formOpen}
        title={formatMessage(editingGroup ? 'account.group.editTitle' : 'account.group.createTitle')}
        width={520}
        fullScreenable={false}
        forceRender
        destroyOnHidden
        maskClosable={false}
        onCancel={closeGroupForm}
      >
        <Form form={form} layout="vertical" preserve={false} requiredMark={false}>
          <Form.Item
            name="groupName"
            label={formatMessage('account.group.name')}
            rules={[{ required: true, whitespace: true, message: formatMessage('account.group.nameRequired') }]}
          >
            <Input
              maxLength={MAX_LENGTHS.name}
              showCount
              placeholder={formatMessage('account.group.namePlaceholder')}
            />
          </Form.Item>
          <Form.Item name="description" label={formatMessage('common.description')}>
            <Input.TextArea
              rows={4}
              maxLength={MAX_LENGTHS.description}
              showCount
              placeholder={formatMessage('account.group.descriptionPlaceholder')}
            />
          </Form.Item>
        </Form>
        <div className={styles['modal-footer']}>
          <Button disabled={saving} onClick={closeGroupForm}>
            {formatMessage('common.cancel')}
          </Button>
          <Button type="primary" loading={saving} onClick={() => void saveGroup()}>
            {formatMessage('common.save')}
          </Button>
        </div>
      </ProModal>

      <UserGroupMembersDrawer
        mode="management"
        open={Boolean(selectedGroup)}
        group={selectedGroup}
        members={members}
        loading={membersLoading}
        saving={membersSaving}
        onClose={() => setSelectedGroup(undefined)}
        onAddMembers={() => void openAddMembers()}
        onRemoveMember={confirmRemoveMember}
        onRemoveSelected={confirmRemoveSelected}
      />

      <ProModal
        open={addMemberOpen}
        title={formatMessage('account.group.addMembers')}
        width={560}
        fullScreenable={false}
        destroyOnHidden
        maskClosable={false}
        onCancel={() => setAddMemberOpen(false)}
      >
        <div className={styles['add-member-content']}>
          <div className={styles['field-label']}>{formatMessage('account.group.selectMembers')}</div>
          <Select
            mode="multiple"
            showSearch
            value={selectedUserIds}
            onChange={setSelectedUserIds}
            placeholder={formatMessage('account.group.selectMembersPlaceholder')}
            optionFilterProp="label"
            options={availableUserOptions}
          />
          <div className={styles['selection-hint']}>
            {formatMessage('account.group.selectedCount', { count: selectedUserIds.length })}
          </div>
        </div>
        <div className={styles['modal-footer']}>
          <Button disabled={membersSaving} onClick={() => setAddMemberOpen(false)}>
            {formatMessage('common.cancel')}
          </Button>
          <Button
            type="primary"
            icon={<Plus size={16} strokeWidth={1.75} />}
            loading={membersSaving}
            disabled={!selectedUserIds.length}
            onClick={() => void addSelectedMembers()}
          >
            {formatMessage('account.group.addSelected')}
          </Button>
        </div>
      </ProModal>
    </div>
  );
});

UserGroupsPanel.displayName = 'UserGroupsPanel';

export default UserGroupsPanel;
