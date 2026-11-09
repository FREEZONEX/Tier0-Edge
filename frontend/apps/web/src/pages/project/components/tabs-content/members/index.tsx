import { App, Button } from 'antd';
import { Plus } from 'lucide-react';
import { type FC, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useParams } from 'react-router';

import {
  deleteProjectMember,
  deleteProjectMemberGroup,
  getProjectMemberGroups,
  getProjectMembers,
  getUserGroupMembers,
  getUserGroups,
  type ProjectMemberGroup,
  type UserGroup,
  type UserGroupMember,
} from '@/apis/core-api';
import type { PageProps } from '@/common-types';
import ProTable from '@/components/pro-table';
import { useActivate } from '@/contexts/tabs-lifecycle-context.ts';
import { useTranslate } from '@/hooks';
import UserGroupMembersDrawer from '@/pages/account-management/components/UserGroupMembersDrawer';
import type { ProjectMember } from '@/pages/project/types';

import TabLayout from '../../TabLayout';
import MemberModal, { type MemberModalRef } from './MemberModal';
import { createMembersColumns, type MemberItem } from './config.tsx';
import styles from './index.module.scss';

const roleValues = (roles: ProjectMember['roles'] | ProjectMemberGroup['roles']) => {
  const ids = new Set<string>();
  const names = new Set<string>();
  (Array.isArray(roles) ? roles : []).forEach((role) => {
    const roleId = role?.roleId == null ? '' : String(role.roleId);
    if (!roleId) return;
    ids.add(roleId);
    if (role?.roleName || role?.roleKey) names.add(role?.roleName || role?.roleKey || '');
  });
  return { ids: Array.from(ids), names: Array.from(names) };
};

const Members: FC<PageProps> = () => {
  const { message, modal } = App.useApp();
  const { projectId: id } = useParams();
  const formatMessage = useTranslate();
  const memberModalRef = useRef<MemberModalRef>(null);
  const [editingMember, setEditingMember] = useState<MemberItem | null>(null);
  const [rows, setRows] = useState<MemberItem[]>([]);
  const [groupOptions, setGroupOptions] = useState<Array<{ label: string; value: string }>>([]);
  const [projectGroups, setProjectGroups] = useState<ProjectMemberGroup[]>([]);
  const [loading, setLoading] = useState(false);
  const [selectedGroup, setSelectedGroup] = useState<MemberItem>();
  const [groupMembers, setGroupMembers] = useState<UserGroupMember[]>([]);
  const [groupMembersLoading, setGroupMembersLoading] = useState(false);

  const projectId = Number(id);

  const loadGroupOptions = useCallback(async () => {
    const response = await getUserGroups({ pageNo: 1, pageSize: 1_000_000 });
    setGroupOptions(
      response.list.map((group: UserGroup) => ({
        value: `group:${group.groupId}`,
        label: `${group.groupName} · ${formatMessage('account.group.memberUnit', { count: group.memberCount })}`,
      }))
    );
  }, [formatMessage]);

  const getMembers = useCallback(async () => {
    if (!projectId || Number.isNaN(projectId)) {
      setRows([]);
      setProjectGroups([]);
      return;
    }

    setLoading(true);
    try {
      const [memberResponse, groupResponse] = await Promise.all([
        getProjectMembers(projectId),
        getProjectMemberGroups(projectId),
      ]);
      const members: ProjectMember[] = Array.isArray(memberResponse) ? memberResponse : [];
      const groups: ProjectMemberGroup[] = Array.isArray(groupResponse) ? groupResponse : [];

      const memberRows: MemberItem[] = members.map((member) => {
        const roles = roleValues(member.roles);
        return {
          id: String(member.memberId || member.userId || ''),
          rowType: 'user',
          name: member.userName || member.userId || '-',
          userId: member.userId || '',
          email: member.email || '',
          roles: roles.ids,
          roleNames: roles.names,
          updatedAt: member.updatedAt,
        };
      });

      const groupRows: MemberItem[] = groups.map((group) => {
        const roles = roleValues(group.roles);
        return {
          id: group.projectGroupId,
          rowType: 'group',
          groupId: group.groupId,
          description: group.description,
          memberCount: group.memberCount,
          name: group.groupName,
          userId: `group:${group.groupId}`,
          email: '',
          roles: roles.ids,
          roleNames: roles.names,
          updatedAt: group.updatedTime,
        };
      });

      setProjectGroups(groups);
      setRows([...groupRows, ...memberRows]);
    } finally {
      setLoading(false);
    }
  }, [projectId]);

  const refresh = useCallback(async () => {
    await Promise.all([getMembers(), loadGroupOptions()]);
  }, [getMembers, loadGroupOptions]);

  useEffect(() => {
    const requestTimer = window.setTimeout(() => void refresh(), 0);
    return () => window.clearTimeout(requestTimer);
  }, [refresh]);

  useActivate(() => {
    void refresh();
  });

  useEffect(() => {
    if (editingMember) memberModalRef.current?.onOpen(editingMember);
  }, [editingMember]);

  const handleEditMember = useCallback((record: MemberItem) => {
    setEditingMember({ ...record });
  }, []);

  const handleDelete = useCallback(
    async (record: MemberItem) => {
      if (!projectId) return;
      if (record.rowType === 'group') {
        await deleteProjectMemberGroup(projectId, record.id);
      } else {
        await deleteProjectMember(projectId, record.id);
      }
      message.success(formatMessage('common.deleteSuccessfully'));
      await getMembers();
    },
    [formatMessage, getMembers, message, projectId]
  );

  const viewGroupMembers = useCallback(async (record: MemberItem) => {
    if (!record.groupId) return;
    setSelectedGroup(record);
    setGroupMembers([]);
    setGroupMembersLoading(true);
    try {
      const response = await getUserGroupMembers(record.groupId);
      setGroupMembers(response.list);
    } finally {
      setGroupMembersLoading(false);
    }
  }, []);

  const columns: any = useMemo(
    () =>
      createMembersColumns({
        formatMessage,
        styles,
        onEdit: handleEditMember,
        onDelete: handleDelete,
        onViewGroup: (record) => void viewGroupMembers(record),
        modal,
      }),
    [formatMessage, handleDelete, handleEditMember, modal, viewGroupMembers]
  );

  return (
    <>
      <TabLayout
        style={{ padding: '0 24px' }}
        toolbarActions={
          <Button type="primary" icon={<Plus size={16} />} onClick={() => memberModalRef.current?.onOpen()}>
            {formatMessage('project.addMember')}
          </Button>
        }
      >
        <ProTable
          loading={loading}
          rowKey={(record: MemberItem) => `${record.rowType}:${record.id}`}
          scroll={{ x: 'max-content', y: 'calc(100vh - 300px)' }}
          columns={columns}
          dataSource={rows as any}
          onRow={(record: MemberItem) =>
            record.rowType === 'group'
              ? {
                  onClick: () => void viewGroupMembers(record),
                }
              : {}
          }
          rowNavigationIndicator={false}
          resizeable
          pagination={false}
        />
      </TabLayout>
      <MemberModal
        ref={memberModalRef}
        refreshRequest={refresh}
        existingMemberUserIds={rows.filter((row) => row.rowType === 'user').map((row) => row.userId)}
        groupOptions={groupOptions}
        existingGroupIds={projectGroups.map((group) => group.groupId)}
      />
      <UserGroupMembersDrawer
        mode="project"
        open={Boolean(selectedGroup)}
        group={
          selectedGroup
            ? {
                groupName: selectedGroup.name,
                description: selectedGroup.description,
                memberCount: selectedGroup.memberCount,
                roleNames: selectedGroup.roleNames,
              }
            : undefined
        }
        members={groupMembers}
        loading={groupMembersLoading}
        onClose={() => setSelectedGroup(undefined)}
      />
    </>
  );
};

export default Members;
