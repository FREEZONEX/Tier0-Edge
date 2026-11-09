import { Search, TrashCan, UserFollow } from '@carbon/icons-react';
import { Button, Flex, Input, Space, Statistic, Table, Tag, Tooltip, type TableColumnsType } from 'antd';
import { useMemo, useState, type Key } from 'react';

import type { UserGroupMember } from '@/apis/core-api';
import ComDrawer from '@/components/com-drawer';
import { useTranslate } from '@/hooks';
import { formatTimestamp } from '@/utils/format';

import styles from './UserGroupMembersDrawer.module.scss';

type DrawerGroup = {
  groupName: string;
  description?: string;
  memberCount?: number;
  roleNames?: string[];
};

type UserGroupMembersDrawerProps = {
  mode: 'management' | 'project';
  open: boolean;
  group?: DrawerGroup;
  members: UserGroupMember[];
  loading?: boolean;
  saving?: boolean;
  onClose: () => void;
  onAddMembers?: () => void;
  onRemoveMember?: (member: UserGroupMember) => void;
  onRemoveSelected?: (userIds: string[]) => void;
};

const UserGroupMembersDrawer = ({
  mode,
  open,
  group,
  members,
  loading,
  saving,
  onClose,
  onAddMembers,
  onRemoveMember,
  onRemoveSelected,
}: UserGroupMembersDrawerProps) => {
  const formatMessage = useTranslate();
  const [keyword, setKeyword] = useState('');
  const [selectedKeys, setSelectedKeys] = useState<Key[]>([]);
  const managementMode = mode === 'management';

  const filteredMembers = useMemo(() => {
    const normalizedKeyword = keyword.trim().toLowerCase();
    if (!normalizedKeyword) return members;
    return members.filter((member) =>
      [member.userName, member.displayName, member.email].some((value) =>
        String(value || '')
          .toLowerCase()
          .includes(normalizedKeyword)
      )
    );
  }, [keyword, members]);

  const enabledCount = members.filter((member) => member.enabled).length;
  const disabledCount = members.length - enabledCount;

  const columns = useMemo<TableColumnsType<UserGroupMember>>(() => {
    const baseColumns: TableColumnsType<UserGroupMember> = [
      {
        key: 'user',
        title: formatMessage('account.group.userIdentity'),
        width: 210,
        render: (_, member) => (
          <div className={styles.identity}>
            <strong>{member.displayName || member.userName || '-'}</strong>
            <span>{member.userName || '-'}</span>
          </div>
        ),
      },
      {
        dataIndex: 'email',
        title: formatMessage('account.email'),
        ellipsis: true,
        render: (value: string) => value || '-',
      },
      {
        dataIndex: 'enabled',
        title: formatMessage('common.status'),
        width: 106,
        render: (enabled: boolean) => (
          <Tag bordered={false} className={enabled ? styles.available : styles.unavailable}>
            {formatMessage(enabled ? 'account.available' : 'account.unavailable')}
          </Tag>
        ),
      },
      {
        dataIndex: 'createdTime',
        title: formatMessage('account.group.joinedAt'),
        width: 150,
        render: (value: number) => (value ? formatTimestamp(value, 'YYYY/MM/DD HH:mm', true) : '-'),
      },
    ];

    if (managementMode && onRemoveMember) {
      baseColumns.push({
        key: 'operation',
        title: formatMessage('common.operation'),
        width: 96,
        align: 'center',
        render: (_, member) => (
          <Tooltip title={formatMessage('account.group.removeMember')}>
            <Button
              type="text"
              danger
              disabled={saving}
              icon={<TrashCan size={16} />}
              aria-label={formatMessage('account.group.removeMember')}
              onClick={() => onRemoveMember(member)}
            />
          </Tooltip>
        ),
      });
    }
    return baseColumns;
  }, [formatMessage, managementMode, onRemoveMember, saving]);

  return (
    <ComDrawer
      open={open}
      width={managementMode ? 920 : 820}
      destroyOnClose
      title={
        group
          ? managementMode
            ? formatMessage('account.group.membersTitle', { name: group.groupName })
            : group.groupName
          : undefined
      }
      afterOpenChange={(nextOpen) => {
        if (!nextOpen) return;
        setKeyword('');
        setSelectedKeys([]);
      }}
      onClose={onClose}
    >
      <div className={styles.drawerBody}>
        {group?.description ? <p className={styles.description}>{group.description}</p> : null}
        <div className={styles.stats}>
          <Statistic title={formatMessage('account.group.statsTotal')} value={members.length} />
          <Statistic title={formatMessage('account.group.statsEnabled')} value={enabledCount} />
          <Statistic title={formatMessage('account.group.statsDisabled')} value={disabledCount} />
        </div>

        {!managementMode ? (
          <div className={styles.projectContext}>
            <div>
              <span>{formatMessage('account.group.projectAccess')}</span>
              <Flex gap={6} wrap="wrap">
                {group?.roleNames?.length ? (
                  group.roleNames.map((role) => (
                    <Tag bordered={false} key={role}>
                      {role}
                    </Tag>
                  ))
                ) : (
                  <span className={styles.muted}>{formatMessage('account.group.noRoles')}</span>
                )}
              </Flex>
            </div>
            <p>{formatMessage('account.group.readOnlyHint')}</p>
          </div>
        ) : null}

        <div className={styles.toolbar}>
          <Input
            allowClear
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            prefix={<Search size={16} />}
            placeholder={formatMessage('account.group.memberSearchPlaceholder')}
            className={styles.search}
          />
          {managementMode ? (
            <Space>
              <Button
                danger
                disabled={!selectedKeys.length || saving}
                onClick={() => onRemoveSelected?.(selectedKeys.map(String))}
              >
                {formatMessage('account.group.removeSelected', { count: selectedKeys.length })}
              </Button>
              <Button type="primary" icon={<UserFollow size={16} />} onClick={onAddMembers}>
                {formatMessage('account.group.addMembers')}
              </Button>
            </Space>
          ) : null}
        </div>

        <Table
          rowKey="userId"
          size="middle"
          loading={loading || saving}
          columns={columns}
          dataSource={filteredMembers}
          rowSelection={managementMode ? { selectedRowKeys: selectedKeys, onChange: setSelectedKeys } : undefined}
          pagination={{
            pageSize: 10,
            pageSizeOptions: [10, 20, 30, 50, 100],
            showSizeChanger: true,
            showQuickJumper: true,
          }}
          locale={{ emptyText: formatMessage('account.group.memberEmpty') }}
          scroll={{ y: 'calc(100vh - 510px)' }}
        />
      </div>
    </ComDrawer>
  );
};

export default UserGroupMembersDrawer;
