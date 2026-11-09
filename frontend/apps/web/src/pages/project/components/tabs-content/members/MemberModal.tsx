import {
  addProjectMember,
  addProjectMemberGroup,
  getUserGroupMembers,
  getProjectRoles,
  type UserGroupMember,
  updateProjectMember,
  updateProjectMemberGroup,
} from '@/apis/core-api';
import ProModal from '@/components/pro-modal';
import { useTranslate } from '@/hooks';
import {
  MAX_CONCURRENT_PAGE_REQUESTS,
  PAGE_SIZE,
  useProjectCommonStore,
} from '@/pages/project/store/projectCommonStore';
import { type ProjectRole } from '@/pages/project/types';
import { fetchAllPagesWithConcurrency } from '@/utils/common';
import { App, Button, Flex, Form, Select, Tag, TreeSelect } from 'antd';
import type { TreeSelectProps } from 'antd';
import { UserRound, UsersRound } from 'lucide-react';
import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react';
import { useParams } from 'react-router';

import type { MemberItem } from './config';
import styles from './MemberModal.module.scss';

export interface MemberModalRef {
  onOpen: (member?: MemberItem) => void;
  onClose: () => void;
}

type SelectOption = {
  label: string;
  value: string | number;
};

export interface MemberModalProps {
  refreshRequest?: () => void | Promise<void>;
  existingMemberUserIds?: string[];
  groupOptions?: SelectOption[];
  existingGroupIds?: string[];
}

type MemberFormValues = {
  subjectIds?: Array<string | { value: string }>;
  roles?: string[];
};

type SubjectTreeNode = NonNullable<TreeSelectProps['treeData']>[number] & {
  groupId?: string;
  searchText?: string;
};

const MemberModal = forwardRef<MemberModalRef, MemberModalProps>((props, ref) => {
  const { refreshRequest, existingMemberUserIds = [], groupOptions = [], existingGroupIds = [] } = props;
  const { projectId: routeProjectId } = useParams();
  const projectId = Number(routeProjectId);
  const formatMessage = useTranslate();
  const { message } = App.useApp();
  const [form] = Form.useForm<MemberFormValues>();
  const [visible, setVisible] = useState(false);
  const [saving, setSaving] = useState(false);
  const [editingMember, setEditingMember] = useState<MemberItem>();
  const [roleOptions, setRoleOptions] = useState<SelectOption[]>([]);
  const [groupMemberMap, setGroupMemberMap] = useState<Record<string, UserGroupMember[]>>({});
  const savingRef = useRef(false);
  const roleOptionsRef = useRef<SelectOption[]>([]);
  const roleOptionsLoadedProjectIdRef = useRef<number | null>(null);
  const memberOptions = useProjectCommonStore((state) => state.memberOptions);
  const memberOptionsLoading = useProjectCommonStore((state) => state.memberOptionsLoading);
  const fetchMemberOptions = useProjectCommonStore((state) => state.fetchMemberOptions);
  const resetMemberOptions = useProjectCommonStore((state) => state.resetMemberOptions);
  const watchedSubjectIds = Form.useWatch('subjectIds', { form, preserve: true });
  const selectedSubjectIds = useMemo(() => watchedSubjectIds || [], [watchedSubjectIds]);

  useEffect(() => {
    resetMemberOptions();
  }, [resetMemberOptions]);

  const loadRoleOptions = useCallback(async () => {
    if (roleOptionsLoadedProjectIdRef.current === projectId) return roleOptionsRef.current;
    if (!projectId || Number.isNaN(projectId)) {
      roleOptionsLoadedProjectIdRef.current = null;
      roleOptionsRef.current = [];
      return [];
    }

    const result = await fetchAllPagesWithConcurrency<ProjectRole>(
      async ({ pageNo, pageSize }) => {
        const response: any = await getProjectRoles(projectId, { pageNo, pageSize });
        if (Array.isArray(response)) return { total: response.length, data: response };
        const data = (Array.isArray(response?.data) ? response.data : []) as ProjectRole[];
        return { total: Number(response?.total ?? data.length), data };
      },
      { pageSize: PAGE_SIZE, maxConcurrency: MAX_CONCURRENT_PAGE_REQUESTS }
    );

    const roleMap = new Map<number, string>();
    result.data.forEach((role) => {
      if (role?.roleId) roleMap.set(role.roleId, role.roleName || role.roleKey);
    });
    const options = Array.from(roleMap.entries()).map(([roleId, roleName]) => ({
      label: roleName,
      value: String(roleId),
    }));
    roleOptionsLoadedProjectIdRef.current = projectId;
    roleOptionsRef.current = options;
    return options;
  }, [projectId]);

  const groupSelectionOptions = useMemo(() => {
    return groupOptions.map((option) => ({ ...option, value: String(option.value).replace(/^group:/, '') }));
  }, [groupOptions]);

  const existingGroupSubjectValues = useMemo(
    () => new Set(existingGroupIds.map((groupId) => `group:${groupId}`)),
    [existingGroupIds]
  );

  const existingUserSubjectValues = useMemo(
    () => new Set(existingMemberUserIds.map((userId) => `user:${userId}`)),
    [existingMemberUserIds]
  );

  const existingSubjectValues = useMemo(
    () => new Set([...existingGroupSubjectValues, ...existingUserSubjectValues]),
    [existingGroupSubjectValues, existingUserSubjectValues]
  );

  const existingGroupSubjects = useMemo(
    () =>
      groupSelectionOptions
        .filter((option) => existingGroupSubjectValues.has(`group:${option.value}`))
        .map((option) => ({ value: `group:${option.value}`, label: option.label })),
    [existingGroupSubjectValues, groupSelectionOptions]
  );

  const existingUserSubjects = useMemo(
    () =>
      memberOptions
        .filter((option) => existingUserSubjectValues.has(`user:${option.value}`))
        .map((option) => ({ value: `user:${option.value}`, label: option.label })),
    [existingUserSubjectValues, memberOptions]
  );

  const normalizeSubjectValues = useCallback(
    (values: MemberFormValues['subjectIds']) => {
      const subjectValues = Array.isArray(values) ? values : values ? [values] : [];
      return subjectValues.filter((subject) => {
        const subjectValue = typeof subject === 'object' && subject ? String(subject.value) : String(subject);
        return !existingSubjectValues.has(subjectValue);
      });
    },
    [existingSubjectValues]
  );

  const renderSubjectTitle = useCallback(
    (label: string, type: 'group-section' | 'user-section' | 'group' | 'user', alreadyMember = false) => {
      if (type === 'group-section' || type === 'user-section') {
        return <span className={styles['subject-option-name']}>{label}</span>;
      }
      const SubjectIcon = type === 'group' ? UsersRound : UserRound;
      return (
        <span className={styles['subject-option']}>
          <SubjectIcon size={16} strokeWidth={1.75} aria-hidden="true" />
          <span className={styles['subject-option-name']}>{label}</span>
          {alreadyMember ? (
            <Tag bordered={false} className={styles['subject-existing-tag']}>
              {formatMessage(type === 'group' ? 'project.groupAlreadyAdded' : 'project.userAlreadyAdded')}
            </Tag>
          ) : null}
        </span>
      );
    },
    [formatMessage]
  );

  const subjectTreeData = useMemo<SubjectTreeNode[]>(() => {
    const groupNodes: SubjectTreeNode[] = groupSelectionOptions.map((option) => {
      const groupId = String(option.value);
      const loadedMembers = groupMemberMap[groupId];
      const alreadyGroupMember = existingGroupSubjectValues.has(`group:${groupId}`);
      return {
        title: renderSubjectTitle(String(option.label), 'group', alreadyGroupMember),
        label: option.label,
        searchText: String(option.label),
        value: `group:${groupId}`,
        key: `group:${groupId}`,
        groupId,
        isLeaf: Array.isArray(loadedMembers) && loadedMembers.length === 0,
        selectable: !alreadyGroupMember,
        disableCheckbox: alreadyGroupMember,
        children: loadedMembers?.map((member) => ({
          title: renderSubjectTitle(member.displayName || member.userName, 'user'),
          label: member.displayName || member.userName,
          searchText: `${member.displayName || ''} ${member.userName || ''} ${member.email || ''}`,
          value: `group-user:${groupId}:${member.userId}`,
          key: `group-user:${groupId}:${member.userId}`,
          isLeaf: true,
          selectable: false,
          checkable: false,
        })),
      };
    });

    const userNodes: SubjectTreeNode[] = memberOptions.map((option) => {
      const userId = String(option.value);
      const alreadyUserMember = existingUserSubjectValues.has(`user:${userId}`);
      return {
        title: renderSubjectTitle(String(option.label), 'user', alreadyUserMember),
        label: option.label,
        searchText: String(option.label),
        value: `user:${userId}`,
        key: `user:${userId}`,
        isLeaf: true,
        selectable: !alreadyUserMember,
        disableCheckbox: alreadyUserMember,
      };
    });

    return [
      {
        title: renderSubjectTitle(formatMessage('project.subjectGroups'), 'group-section'),
        value: 'subject-root-groups',
        key: 'subject-root-groups',
        className: styles['subject-tree-section'],
        selectable: false,
        checkable: false,
        children: groupNodes,
      },
      {
        title: renderSubjectTitle(formatMessage('project.subjectUsers'), 'user-section'),
        value: 'subject-root-users',
        key: 'subject-root-users',
        className: styles['subject-tree-section'],
        selectable: false,
        checkable: false,
        children: userNodes,
      },
    ];
  }, [
    existingGroupSubjectValues,
    existingUserSubjectValues,
    formatMessage,
    groupSelectionOptions,
    groupMemberMap,
    memberOptions,
    renderSubjectTitle,
  ]);

  const loadGroupMembers = useCallback(
    async (node: SubjectTreeNode) => {
      const groupId = node.groupId;
      if (!groupId || groupMemberMap[groupId]) return;
      const response = await getUserGroupMembers(groupId);
      setGroupMemberMap((current) => ({ ...current, [groupId]: response.list }));
    },
    [groupMemberMap]
  );

  const onOpen = (member?: MemberItem) => {
    setEditingMember(member);
    setGroupMemberMap({});
    form.resetFields();
    if (member) form.setFieldsValue({ roles: member.roles || [] });
    setVisible(true);
    void Promise.all([fetchMemberOptions(), loadRoleOptions()])
      .then(([, roles]) => setRoleOptions(roles))
      .catch(() => setRoleOptions([]));
  };

  const onClose = () => {
    if (savingRef.current) return;
    form.resetFields();
    setEditingMember(undefined);
    setVisible(false);
  };

  const onSave = async () => {
    if (savingRef.current) return;
    savingRef.current = true;
    setSaving(true);

    try {
      const values = await form.validateFields();
      if (!projectId || Number.isNaN(projectId)) return;
      if (!editingMember && !values.subjectIds?.length) {
        message.warning(formatMessage('project.batchSubjectsRequired'));
        return;
      }

      const roles = values.roles || [];
      const subjectIds = (values.subjectIds || []).map((subject) =>
        typeof subject === 'string' ? subject : subject.value
      );
      if (editingMember?.rowType === 'group') {
        await updateProjectMemberGroup(projectId, editingMember.id, { roleIds: roles });
      } else if (editingMember) {
        const memberId = Number(editingMember.id);
        if (!memberId || Number.isNaN(memberId)) throw new Error('member id is required');
        await updateProjectMember(projectId, memberId, {
          userId: editingMember.userId,
          userName: editingMember.name,
          roleIds: roles.map(Number),
        });
      } else {
        const userIds = Array.from(
          new Set(
            subjectIds.flatMap((subjectId) => {
              if (subjectId.startsWith('user:')) return [subjectId.replace(/^user:/, '')];
              if (subjectId.startsWith('group-user:')) return [subjectId.split(':').at(-1) || ''];
              return [];
            })
          )
        ).filter(Boolean);
        const groupIds = subjectIds
          .filter((subjectId) => subjectId.startsWith('group:'))
          .map((subjectId) => subjectId.replace(/^group:/, ''));
        await Promise.all([
          ...userIds.map((userId) =>
            addProjectMember(projectId, {
              userId,
              userName:
                memberOptions.find((option) => String(option.value) === userId)?.label ||
                Object.values(groupMemberMap)
                  .flat()
                  .find((member) => String(member.userId) === userId)?.userName ||
                '',
              roleIds: roles.map(Number),
            })
          ),
          ...groupIds.map((groupId) => addProjectMemberGroup(projectId, { groupId, roleIds: roles })),
        ]);
      }
      await refreshRequest?.();
      form.resetFields();
      setEditingMember(undefined);
      setVisible(false);
      message.success(formatMessage('common.optsuccess'));
    } catch (error: any) {
      if (error?.code == 100010) {
        message.error(formatMessage('project.memberAlreadyExists'));
      } else {
        message.error(
          (error as Error)?.message || formatMessage(editingMember ? 'project.memberEditFail' : 'project.memberAddFail')
        );
      }
    } finally {
      savingRef.current = false;
      setSaving(false);
    }
  };

  useImperativeHandle(ref, () => ({ onOpen, onClose }));

  return (
    <ProModal
      open={visible}
      onCancel={onClose}
      maskClosable={false}
      title={formatMessage(editingMember ? 'project.editAssignment' : 'project.batchMemberTitle')}
      width={editingMember ? 500 : 520}
      fullScreenable={false}
      destroyOnHidden
    >
      <Form form={form} layout="vertical" requiredMark={false} className={styles.form}>
        {editingMember ? (
          <div className={styles.subjectSummary}>
            <div>
              <span>{formatMessage('project.member')}</span>
              <strong>{editingMember.name}</strong>
            </div>
            <Tag bordered={false}>
              {formatMessage(editingMember.rowType === 'group' ? 'project.subjectGroups' : 'project.subjectUsers')}
            </Tag>
          </div>
        ) : (
          <Form.Item
            name="subjectIds"
            label={formatMessage('project.selectMembers')}
            getValueProps={(values: MemberFormValues['subjectIds'] = []) => ({
              value: [...existingGroupSubjects, ...existingUserSubjects, ...values],
            })}
            getValueFromEvent={normalizeSubjectValues}
          >
            <TreeSelect
              treeCheckable
              treeCheckStrictly
              allowClear
              showSearch
              maxTagCount="responsive"
              loading={memberOptionsLoading}
              treeData={subjectTreeData}
              loadData={(node) => loadGroupMembers(node as SubjectTreeNode)}
              treeDefaultExpandedKeys={['subject-root-groups', 'subject-root-users']}
              treeNodeFilterProp="searchText"
              treeNodeLabelProp="label"
              showCheckedStrategy={TreeSelect.SHOW_PARENT}
              popupClassName={styles['subject-tree-popup']}
              placeholder={formatMessage('project.selectMembersPlaceholder')}
              tagRender={({ value, label, closable, onClose }) => {
                // antd 的 tagRender 要求返回 ReactElement，返回 null 过不了类型检查
                // （tsc -b 在 CI 上直接失败）；不渲染标签时给一个空片段
                if (existingSubjectValues.has(String(value))) return <></>;
                return (
                  <Tag closable={closable} onClose={onClose} className={styles['subject-selection-tag']}>
                    {label}
                  </Tag>
                );
              }}
            />
          </Form.Item>
        )}

        <Form.Item name="roles" label={formatMessage('project.role')}>
          <Select
            mode="multiple"
            allowClear
            showSearch
            maxTagCount="responsive"
            options={roleOptions}
            optionFilterProp="label"
            placeholder={formatMessage('project.selectRole')}
          />
        </Form.Item>
        <div className={styles.roleHint}>{formatMessage('project.batchRolesHint')}</div>

        <Flex justify="flex-end" gap={8} className={styles.footer}>
          <Button disabled={saving} onClick={onClose}>
            {formatMessage('common.cancel')}
          </Button>
          <Button
            type="primary"
            loading={saving}
            disabled={!editingMember && !selectedSubjectIds.length}
            onClick={() => void onSave()}
          >
            {formatMessage(editingMember ? 'common.save' : 'project.addSelectedSubjects')}
          </Button>
        </Flex>
      </Form>
    </ProModal>
  );
});

MemberModal.displayName = 'MemberModal';

export default MemberModal;
