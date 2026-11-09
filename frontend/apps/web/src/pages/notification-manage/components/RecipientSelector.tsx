import { User, UserMultiple } from '@carbon/icons-react';
import { Spin, Tag, TreeSelect, type TreeSelectProps } from 'antd';
import { useCallback, useEffect, useMemo, useState } from 'react';

import {
  getNotificationRecipientOptions,
  type NotificationRecipientGroup,
  type NotificationRecipientUser,
} from '@/apis/core-api/notify';
import { useTranslate } from '@/hooks';

import styles from './RecipientSelector.module.scss';

export type RecipientSubjectValue = string | { value: string };

type RecipientSelectorProps = {
  value?: RecipientSubjectValue[];
  onChange?: (value: RecipientSubjectValue[]) => void;
  onSelectionChange?: () => void;
  disabled?: boolean;
};

type RecipientTreeNode = NonNullable<TreeSelectProps['treeData']>[number] & {
  searchText?: string;
};

const normalizeSubjectIds = (subjects: RecipientSubjectValue[] = []) =>
  subjects.map((subject) => (typeof subject === 'string' ? subject : String(subject.value)));

const buildUserLabel = (displayName?: string, userName?: string, fallback?: string) => {
  const display = String(displayName || '').trim();
  const account = String(userName || '').trim();
  if (display && account && display !== account) return `${display} (${account})`;
  return display || account || String(fallback || '');
};

export const resolveRecipientUserIds = async (subjects: RecipientSubjectValue[] = []) => {
  const subjectIds = normalizeSubjectIds(subjects);
  const userIds = new Set(
    subjectIds
      .filter((subjectId) => subjectId.startsWith('user:'))
      .map((subjectId) => Number(subjectId.replace(/^user:/, '')))
      .filter((userId) => Number.isInteger(userId) && userId > 0)
  );
  const groupIds = Array.from(
    new Set(
      subjectIds
        .filter((subjectId) => subjectId.startsWith('group:'))
        .map((subjectId) => Number(subjectId.replace(/^group:/, '')))
        .filter((groupId) => Number.isInteger(groupId) && groupId > 0)
    )
  );
  if (groupIds.length > 0) {
    const options = await getNotificationRecipientOptions();
    const selectedGroupIds = new Set(groupIds);
    options.groups.forEach((group) => {
      if (!selectedGroupIds.has(group.groupId)) return;
      group.userIds.forEach((userId) => {
        if (Number.isInteger(userId) && userId > 0) userIds.add(userId);
      });
    });
  }

  return Array.from(userIds);
};

const RecipientSelector = ({ value, onChange, onSelectionChange, disabled }: RecipientSelectorProps) => {
  const formatMessage = useTranslate();
  const [users, setUsers] = useState<NotificationRecipientUser[]>([]);
  const [groups, setGroups] = useState<NotificationRecipientGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const t = useCallback(
    (key: string, values?: Record<string, unknown>) => formatMessage(`notificationManage.${key}`, values),
    [formatMessage]
  );

  useEffect(() => {
    let active = true;

    void getNotificationRecipientOptions()
      .then((response) => {
        if (!active) return;
        setUsers(response.users || []);
        setGroups(response.groups || []);
      })
      .catch(() => {
        if (!active) return;
        setUsers([]);
        setGroups([]);
        setLoadFailed(true);
      })
      .finally(() => {
        if (active) setLoading(false);
      });

    return () => {
      active = false;
    };
  }, []);

  const renderOption = useCallback(
    (label: string, type: 'section' | 'group' | 'user', count?: number) => {
      if (type === 'section') return <span className={styles['recipient-option-name']}>{label}</span>;
      const RecipientIcon = type === 'group' ? UserMultiple : User;
      return (
        <span className={styles['recipient-option']}>
          <RecipientIcon size={16} aria-hidden="true" />
          <span className={styles['recipient-option-name']}>{label}</span>
          {type === 'group' ? (
            <Tag bordered={false} className={styles['recipient-count-tag']}>
              {t('groupMemberCount', { count: count ?? 0 })}
            </Tag>
          ) : null}
        </span>
      );
    },
    [t]
  );

  const treeData = useMemo<RecipientTreeNode[]>(() => {
    const userById = new Map(users.map((user) => [user.userId, user]));
    const groupNodes = groups.map((group) => {
      return {
        title: renderOption(group.groupName, 'group', group.memberCount),
        label: group.groupName,
        searchText: `${group.groupName} ${group.description || ''}`,
        value: `group:${group.groupId}`,
        key: `group:${group.groupId}`,
        isLeaf: group.userIds.length === 0,
        children: group.userIds
          .map((userId) => userById.get(userId))
          .filter((user): user is NotificationRecipientUser => Boolean(user))
          .map((user) => {
            const label = buildUserLabel(user.displayName, user.userName, String(user.userId));
            return {
              title: renderOption(label, 'user'),
              label,
              searchText: `${user.displayName || ''} ${user.userName || ''}`,
              value: `group-user:${group.groupId}:${user.userId}`,
              key: `group-user:${group.groupId}:${user.userId}`,
              isLeaf: true,
              selectable: false,
              checkable: false,
            };
          }),
      };
    });
    const seenUserIds = new Set<string>();
    const userNodes = users.flatMap((user) => {
      const userId = String(user.userId || '');
      if (!userId || seenUserIds.has(userId)) return [];
      seenUserIds.add(userId);
      const label = buildUserLabel(user.displayName, user.userName, userId);
      return [
        {
          title: renderOption(label, 'user'),
          label,
          searchText: `${user.displayName || ''} ${user.userName || ''}`,
          value: `user:${userId}`,
          key: `user:${userId}`,
          isLeaf: true,
        },
      ];
    });

    return [
      {
        title: renderOption(t('recipientGroups'), 'section'),
        value: 'recipient-root-groups',
        key: 'recipient-root-groups',
        className: styles['recipient-tree-section'],
        selectable: false,
        checkable: false,
        children: groupNodes,
      },
      {
        title: renderOption(t('recipientUsers'), 'section'),
        value: 'recipient-root-users',
        key: 'recipient-root-users',
        className: styles['recipient-tree-section'],
        selectable: false,
        checkable: false,
        children: userNodes,
      },
    ];
  }, [groups, renderOption, t, users]);

  return (
    <TreeSelect
      treeCheckable
      treeCheckStrictly
      allowClear
      showSearch
      maxTagCount="responsive"
      value={value}
      disabled={disabled}
      loading={loading}
      treeData={loadFailed ? [] : treeData}
      treeDefaultExpandedKeys={['recipient-root-groups', 'recipient-root-users']}
      treeNodeFilterProp="searchText"
      treeNodeLabelProp="label"
      showCheckedStrategy={TreeSelect.SHOW_PARENT}
      popupClassName={styles['recipient-tree-popup']}
      placeholder={t('formRecipientsPlaceholder')}
      notFoundContent={
        loading ? <Spin size="small" /> : t(loadFailed ? 'recipientLoadFailed' : 'recipientEmptyOptions')
      }
      onChange={(subjects) => {
        onChange?.(subjects as RecipientSubjectValue[]);
        onSelectionChange?.();
      }}
    />
  );
};

export default RecipientSelector;
