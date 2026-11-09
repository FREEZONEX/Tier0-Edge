import { Button, Flex, Tag } from 'antd';
import type { HookAPI } from 'antd/es/modal/useModal';
import { ChevronRight, UserRound, UsersRound } from 'lucide-react';

import { openConfirmModal } from '@/components/confirm-modal';
import { Edit, TrashCan } from '@/components/lucide-icon/carbon';
import { formatTimestamp } from '@/utils/format';

export type MemberItem = {
  id: string;
  rowType: 'user' | 'group';
  groupId?: string;
  description?: string;
  memberCount?: number;
  /** 后端返回的用户唯一标识，用于添加成员时过滤已存在成员 */
  userId: string;
  name: string;
  email: string;
  roles: string[];
  roleNames: string[];
  updatedAt: string | number;
};

type FormatMessage = (...args: any[]) => string;
type MembersStyles = Record<string, string>;

export const createMembersColumns = ({
  formatMessage,
  styles,
  onEdit,
  onDelete,
  onViewGroup,
  modal,
}: {
  formatMessage: FormatMessage;
  styles: MembersStyles;
  onEdit: (record: MemberItem) => void;
  onDelete: (record: MemberItem) => void;
  onViewGroup: (record: MemberItem) => void;
  modal: HookAPI;
}) => [
  {
    title: () => formatMessage('project.member'),
    dataIndex: 'name',
    key: 'name',
    width: '30%',
    render: (value: string, record: MemberItem) => (
      <div className={styles.memberCell}>
        <div>
          <div className={styles.memberIdentity}>
            {record.rowType === 'group' ? (
              <UsersRound className={styles.memberIcon} size={16} strokeWidth={1.75} aria-hidden="true" />
            ) : (
              <UserRound className={styles.memberIcon} size={16} strokeWidth={1.75} aria-hidden="true" />
            )}
            {record.rowType === 'group' ? (
              <Button
                type="link"
                className={styles.groupNameButton}
                onClick={(event) => {
                  event.stopPropagation();
                  onViewGroup(record);
                }}
              >
                <span>{value}</span>
                <ChevronRight className={styles.rowDisclosure} size={14} strokeWidth={1.75} aria-hidden="true" />
              </Button>
            ) : (
              <span className={styles.memberName}>{value}</span>
            )}
          </div>
          <div className={styles.memberMeta}>
            {record.rowType === 'group'
              ? formatMessage('account.group.memberUnit', { count: record.memberCount || 0 })
              : record.email || '-'}
          </div>
        </div>
      </div>
    ),
  },
  {
    title: () => formatMessage('project.role'),
    dataIndex: 'roleNames',
    key: 'roleNames',
    width: '30%',
    render: (roleNames: string[]) =>
      roleNames.length ? (
        <Flex gap={6} wrap="wrap">
          {roleNames.map((role) => (
            <div key={role} className={styles.roleTag} title={role}>
              {role}
            </div>
          ))}
        </Flex>
      ) : (
        '-'
      ),
  },
  {
    title: () => formatMessage('project.memberType'),
    dataIndex: 'rowType',
    key: 'rowType',
    width: '18%',
    render: (rowType: MemberItem['rowType']) => (
      <Tag
        className={`${styles.memberTypeTag} ${rowType === 'group' ? styles.groupTypeTag : styles.directMemberTypeTag}`}
      >
        {formatMessage(rowType === 'group' ? 'project.subjectGroups' : 'project.subjectUsers')}
      </Tag>
    ),
  },
  {
    title: () => formatMessage('project.lastUpdated'),
    dataIndex: 'updatedAt',
    key: 'updatedAt',
    width: '18%',
    render: (value: string | number) => (value ? formatTimestamp(value, 'YYYY/MM/DD HH:mm', true) : '-'),
  },
  {
    title: () => formatMessage('project.operation'),
    key: 'operation',
    width: 120,
    fixed: 'right',
    render: (_: unknown, record: MemberItem) => (
      <span className={styles.operation} data-row-action="true">
        <i title={formatMessage('common.edit')}>
          <Edit className="custom-operation" style={{ cursor: 'pointer' }} onClick={() => onEdit(record)} />
        </i>
        <i title={formatMessage('common.delete')}>
          <TrashCan
            className="custom-operation"
            style={{ cursor: 'pointer' }}
            onClick={() => {
              openConfirmModal(modal, {
                title: formatMessage(
                  record.rowType === 'group' ? 'project.groupRemoveTitle' : 'project.confirmDeleteMemberTitle'
                ),
                content: formatMessage(
                  record.rowType === 'group' ? 'project.groupRemoveDesc' : 'project.confirmDeleteMemberDesc',
                  record.rowType === 'group' ? { name: record.name } : undefined
                ),
                okText: formatMessage('common.delete'),
                cancelText: formatMessage('common.cancel'),
                danger: true,
                onOk: () => onDelete(record),
              });
            }}
          />
        </i>
      </span>
    ),
  },
];
