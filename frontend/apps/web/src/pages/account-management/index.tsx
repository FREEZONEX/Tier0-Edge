import { Add, ChevronDown, Download, Search, Upload } from '@carbon/icons-react';
import { App, Button, Dropdown, Flex, Input, Select, Tabs, Tag, Tooltip, message, type MenuProps } from 'antd';
import { useTranslate, usePagination } from '@/hooks';
import { UserRound, UsersRound } from 'lucide-react';
import { Edit, FlashOff, Password, PlayOutline, TrashCan } from '@/components/lucide-icon/carbon';
import {
  batchUpdateUsers,
  deleteUser,
  getRoleList,
  getUserGroupMembers,
  getUserGroups,
  getUserManageList,
  replaceUserGroupMembers,
  updateUser,
  type UserGroup,
  type UserGroupMember,
} from '@/apis/core-api/user-manage';
import useResetPassword from '@/pages/account-management/components/useResetPassword';
import useAddUser from '@/pages/account-management/components/useAddUser';
import { cloneElement, useEffect, useMemo, useRef, useState, type FC, type Key, type ReactElement } from 'react';
import type { PageProps } from '@/common-types';
import { ButtonPermission } from '@/common-types/button-permission';
import useRoleSetting from './components/useRoleSetting';
import BulkUserOperations, { type BulkUserOperationsHandle } from './components/BulkUserOperations';
import BatchEditUsersModal, { type BatchEditFormValues } from './components/BatchEditUsersModal';
import BusinessRoleAssignmentDialog from './components/BusinessRoleAssignmentDialog';
import { useNativeAppRoleAccess } from '@/hooks/use-native-app-roles';
import type { RoleAssignmentTarget } from '@/apis/core-api/native-app';
import BusinessRolesPanel from './components/BusinessRolesPanel';
import UserGroupsPanel, { type UserGroupsPanelHandle } from './components/UserGroupsPanel';
import { AuthButton, AuthWrapper } from '@/components/auth';
import ComLayout from '@/components/com-layout';
import ComContent from '@/components/com-layout/ComContent';
import ProTable from '@/components/pro-table';
import ProModal from '@/components/pro-modal';
import { useBaseStore } from '@/stores/base';
import { hasPermission } from '@/utils/auth';
import { createDeleteConfirmOptions } from '@/utils/modal-confirm';
import { formatTimestamp } from '@/utils/format';
import styles from './index.module.scss';

const apiObj: any = {
  updateUser,
};

const fixedColumnWidth = {
  status: 110,
  operation: 160,
};
const timeColumnWidth = 180;
const tableScrollbarReserve = 12;

const flexibleColumnConfig = {
  account: { min: 110, max: 220, padding: 46 },
  displayName: { min: 130, max: 220, padding: 46 },
  phone: { min: 130, max: 180, padding: 46 },
  email: { min: 220, max: 320, padding: 46 },
  role: { min: 140, max: 220, padding: 46 },
  userGroup: { min: 220, max: 320, padding: 46 },
};
const flexibleColumnKeys = ['account', 'displayName', 'phone', 'email', 'role', 'userGroup'] as const;
type FlexibleColumnKey = (typeof flexibleColumnKeys)[number];
const minFlexibleWidth = flexibleColumnKeys.reduce((sum, key) => sum + flexibleColumnConfig[key].min, 0);
const absoluteFlexibleMin = 72;

const clampWidth = (width: number, min: number, max: number) => Math.min(max, Math.max(min, width));

const estimateTextWidth = (value: string, config: { min: number; max: number; padding: number }) =>
  clampWidth(config.padding + value.length * 7, config.min, config.max);

const getElementContentWidth = (element: HTMLElement) => {
  const style = window.getComputedStyle(element);
  const horizontalPadding = (Number.parseFloat(style.paddingLeft) || 0) + (Number.parseFloat(style.paddingRight) || 0);
  return Math.max(0, element.getBoundingClientRect().width - horizontalPadding);
};

const roleText = (record: any) => record?.roleList?.map((item: any) => item.roleName)?.join(',') || '';
const userGroupText = (record: any) => record?.groupList?.map((item: any) => item.groupName)?.join(',') || '';

const distributeExtraWidth = (widths: Record<FlexibleColumnKey, number>, extraWidth: number) => {
  let restWidth = extraWidth;
  while (restWidth > 0.5) {
    const expandableColumns = flexibleColumnKeys.filter((key) => widths[key] < flexibleColumnConfig[key].max);
    if (!expandableColumns.length) break;
    const widthPerColumn = restWidth / expandableColumns.length;
    let usedWidth = 0;
    expandableColumns.forEach((key) => {
      const addedWidth = Math.min(widthPerColumn, flexibleColumnConfig[key].max - widths[key]);
      widths[key] += addedWidth;
      usedWidth += addedWidth;
    });
    if (usedWidth <= 0) break;
    restWidth -= usedWidth;
  }
  if (restWidth > 0) {
    widths.userGroup += restWidth;
  }
};

const resolveFlexibleColumnWidths = (records: any[], containerWidth: number) => {
  const widths: Record<FlexibleColumnKey, number> = {
    account: flexibleColumnConfig.account.min,
    displayName: flexibleColumnConfig.displayName.min,
    phone: flexibleColumnConfig.phone.min,
    email: flexibleColumnConfig.email.min,
    role: flexibleColumnConfig.role.min,
    userGroup: flexibleColumnConfig.userGroup.min,
  };

  records.forEach((record) => {
    widths.account = Math.max(
      widths.account,
      estimateTextWidth(record?.preferredUsername || '', flexibleColumnConfig.account)
    );
    widths.displayName = Math.max(
      widths.displayName,
      estimateTextWidth(record?.firstName || '', flexibleColumnConfig.displayName)
    );
    widths.phone = Math.max(widths.phone, estimateTextWidth(record?.phone || '', flexibleColumnConfig.phone));
    widths.email = Math.max(widths.email, estimateTextWidth(record?.email || '', flexibleColumnConfig.email));
    widths.role = Math.max(widths.role, estimateTextWidth(roleText(record), flexibleColumnConfig.role));
    widths.userGroup = Math.max(
      widths.userGroup,
      estimateTextWidth(userGroupText(record), flexibleColumnConfig.userGroup)
    );
  });

  const fixedWidth = fixedColumnWidth.status + fixedColumnWidth.operation + timeColumnWidth * 2;
  const availableWidth = containerWidth > 0 ? Math.max(0, containerWidth - fixedWidth) : minFlexibleWidth;
  const flexibleWidth =
    widths.account + widths.displayName + widths.phone + widths.email + widths.role + widths.userGroup;

  if (flexibleWidth > availableWidth) {
    const shrinkableWidth =
      widths.account -
      flexibleColumnConfig.account.min +
      widths.displayName -
      flexibleColumnConfig.displayName.min +
      widths.phone -
      flexibleColumnConfig.phone.min +
      widths.email -
      flexibleColumnConfig.email.min +
      widths.role -
      flexibleColumnConfig.role.min +
      widths.userGroup -
      flexibleColumnConfig.userGroup.min;
    const overflow = flexibleWidth - availableWidth;
    if (shrinkableWidth > 0) {
      const shrinkRatio = Math.min(1, overflow / shrinkableWidth);
      widths.account -= (widths.account - flexibleColumnConfig.account.min) * shrinkRatio;
      widths.displayName -= (widths.displayName - flexibleColumnConfig.displayName.min) * shrinkRatio;
      widths.phone -= (widths.phone - flexibleColumnConfig.phone.min) * shrinkRatio;
      widths.email -= (widths.email - flexibleColumnConfig.email.min) * shrinkRatio;
      widths.role -= (widths.role - flexibleColumnConfig.role.min) * shrinkRatio;
      widths.userGroup -= (widths.userGroup - flexibleColumnConfig.userGroup.min) * shrinkRatio;
    } else if (availableWidth > 0) {
      const scale = availableWidth / flexibleWidth;
      flexibleColumnKeys.forEach((key) => {
        widths[key] = Math.max(absoluteFlexibleMin, Math.round(widths[key] * scale));
      });
      const currentSum = flexibleColumnKeys.reduce((sum, key) => sum + widths[key], 0);
      widths.userGroup = Math.max(absoluteFlexibleMin, widths.userGroup + (availableWidth - currentSum));
    }
  } else if (containerWidth && flexibleWidth < availableWidth) {
    distributeExtraWidth(widths, availableWidth - flexibleWidth);
  }

  return {
    account: Math.round(widths.account),
    displayName: Math.round(widths.displayName),
    phone: Math.round(widths.phone),
    email: Math.round(widths.email),
    role: Math.round(widths.role),
    userGroup: Math.round(widths.userGroup),
  };
};

const getTableScrollX = (widths: ReturnType<typeof resolveFlexibleColumnWidths>) =>
  widths.account +
  widths.displayName +
  widths.phone +
  widths.email +
  widths.role +
  widths.userGroup +
  timeColumnWidth * 2 +
  fixedColumnWidth.status +
  fixedColumnWidth.operation;

type AccountSection = 'users' | 'groups' | 'roles';

const AccountManagement: FC<PageProps> = ({ title }) => {
  const appRolesEnabled = useNativeAppRoleAccess();
  const [roleTarget, setRoleTarget] = useState<RoleAssignmentTarget>();
  const [activeSection, setActiveSection] = useState<AccountSection>('users');
  const [exporting, setExporting] = useState(false);
  const [userKeyword, setUserKeyword] = useState('');
  const [statusFilter, setStatusFilter] = useState<string[]>([]);
  const [roleFilter, setRoleFilter] = useState<string[]>([]);
  const [groupFilter, setGroupFilter] = useState<string[]>([]);
  const [roleOptions, setRoleOptions] = useState<Array<{ label: string; value: string }>>([]);
  const [groupOptions, setGroupOptions] = useState<Array<{ label: string; value: string }>>([]);
  const [selectedUserKeys, setSelectedUserKeys] = useState<Key[]>([]);
  const [batchGroupOpen, setBatchGroupOpen] = useState(false);
  const [batchGroupIds, setBatchGroupIds] = useState<string[]>([]);
  const [batchGroupSaving, setBatchGroupSaving] = useState(false);
  const [batchEditOpen, setBatchEditOpen] = useState(false);
  const [batchEditSaving, setBatchEditSaving] = useState(false);
  const [tableShellWidth, setTableShellWidth] = useState(0);
  const tableShellRef = useRef<HTMLDivElement>(null);
  const userGroupsRef = useRef<UserGroupsPanelHandle>(null);
  const bulkOperationsRef = useRef<BulkUserOperationsHandle>(null);
  const formatMessage = useTranslate();
  const ldapEnable = useBaseStore((state) => state?.systemInfo?.ldapEnable);
  const currentUserName = useBaseStore((state) => state.currentUserInfo?.preferredUsername);
  const menuGroupNoSub = useBaseStore((state) => state.menuGroup?.filter((item) => !item.subMenu) || []);
  const { modal } = App.useApp();
  const { data, pagination, setLoading, loading, refreshRequest } = usePagination({
    initPageSize: 100,
    fetchApi: getUserManageList,
  });
  const records = useMemo(() => (Array.isArray(data) ? data : []), [data]);
  const filteredRecords = useMemo(() => {
    const keyword = userKeyword.trim().toLowerCase();
    return records.filter((record: any) => {
      const matchesKeyword =
        !keyword ||
        String(record?.preferredUsername || '')
          .toLowerCase()
          .includes(keyword) ||
        String(record?.firstName || '')
          .toLowerCase()
          .includes(keyword);
      const matchesStatus = !statusFilter.length || statusFilter.includes(String(Boolean(record?.enabled)));
      const matchesRole =
        !roleFilter.length ||
        record?.roleList?.some((role: any) => roleFilter.includes(String(role.roleId || role.roleName)));
      const matchesGroup =
        !groupFilter.length || record?.groupList?.some((group: any) => groupFilter.includes(String(group.groupId)));
      return matchesKeyword && matchesStatus && matchesRole && matchesGroup;
    });
  }, [groupFilter, records, roleFilter, statusFilter, userKeyword]);
  const systemUserKey = useMemo(() => {
    const systemUser: any = records.find(
      (record: any) =>
        String(record?.preferredUsername || '')
          .trim()
          .toLowerCase() === 'tier0'
    );
    return systemUser ? String(systemUser?.userId || systemUser?.id || '') : '';
  }, [records]);
  const protectedSystemUserKey =
    String(currentUserName || '')
      .trim()
      .toLowerCase() === 'tier0'
      ? ''
      : systemUserKey;
  const batchEditableUserKeys = useMemo(
    () =>
      protectedSystemUserKey
        ? selectedUserKeys.filter((key) => String(key) !== protectedSystemUserKey)
        : selectedUserKeys,
    [protectedSystemUserKey, selectedUserKeys]
  );
  const userLabelsById = useMemo(
    () =>
      new Map(
        records.map((record: any) => [
          String(record?.userId || record?.id || ''),
          String(record?.preferredUsername || record?.firstName || record?.userId || record?.id || ''),
        ])
      ),
    [records]
  );
  const tableAvailableWidth = tableShellWidth ? Math.max(0, tableShellWidth - tableScrollbarReserve) : 0;
  const flexibleColumnWidth = resolveFlexibleColumnWidths(filteredRecords, tableAvailableWidth);
  const tableScrollX = getTableScrollX(flexibleColumnWidth);

  useEffect(() => {
    const element = tableShellRef.current;
    if (!element) return undefined;
    const updateWidth = () => setTableShellWidth(Math.floor(getElementContentWidth(element)));
    updateWidth();
    const resizeObserver = new ResizeObserver(updateWidth);
    resizeObserver.observe(element);
    return () => resizeObserver.disconnect();
  }, [activeSection]);

  useEffect(() => {
    if (activeSection !== 'users') return undefined;
    let canceled = false;
    void Promise.all([getRoleList(), getUserGroups({ pageNo: 1, pageSize: 1_000_000 })])
      .then(([roles, groups]) => {
        if (canceled) return;
        setRoleOptions(
          roles.map((role: any) => ({ label: role.roleName || role.roleCode, value: String(role.roleId) }))
        );
        setGroupOptions(groups.list.map((group: UserGroup) => ({ label: group.groupName, value: group.groupId })));
      })
      .catch(() => {
        if (canceled) return;
        setRoleOptions([]);
        setGroupOptions([]);
      });
    return () => {
      canceled = true;
    };
  }, [activeSection]);

  const handle = (params: any, apiKey: string) => {
    setLoading(true);
    apiObj?.[apiKey]?.(params)
      .then(() => {
        message.success(formatMessage('common.optsuccess'));
      })
      .finally(() => {
        refreshRequest();
        setLoading(false);
      });
  };

  const { ModalDom, onOpen } = useResetPassword({
    onSaveBack: refreshRequest,
  });
  const { ModalAddDom, onAddOpen } = useAddUser({
    onSaveBack: refreshRequest,
  });
  const onAddHandle = () => {
    onAddOpen();
  };
  const { onRoleModalOpen, RoleModal } = useRoleSetting({
    onSaveBack: refreshRequest,
  });
  const renderStatusTag = (enabled: boolean) =>
    enabled ? (
      <Tag bordered={false} className={`${styles.statusPill} ${styles.statusAvailable}`}>
        {formatMessage('account.available')}
      </Tag>
    ) : (
      <Tag bordered={false} className={`${styles.statusPill} ${styles.statusUnavailable}`}>
        {formatMessage('account.unavailable')}
      </Tag>
    );

  const renderOperationIcon = ({
    auth,
    disabled,
    label,
    icon,
    onClick,
  }: {
    auth: string;
    disabled?: boolean;
    label: string;
    icon: ReactElement<any>;
    onClick: () => void;
  }) => (
    <AuthWrapper auth={auth}>
      <Tooltip title={label}>
        <span className={styles.operationIcon}>
          {cloneElement(icon, {
            className: `${icon.props.className ? `${icon.props.className} ` : ''}custom-operation ${
              disabled ? styles.operationDisabled : ''
            }`,
            style: { cursor: disabled ? 'not-allowed' : 'pointer', ...(icon.props.style || {}) },
            onClick: disabled ? undefined : onClick,
            'aria-label': label,
            role: 'button',
          })}
        </span>
      </Tooltip>
    </AuthWrapper>
  );

  const renderStatusToggleIcon = (record: any) => {
    if (record?.preferredUsername === 'tier0') return null;
    const statusDisabled = ldapEnable || record?.source === 'external';
    if (record?.enabled) {
      return renderOperationIcon({
        auth: ButtonPermission['UserManagement.disable'],
        disabled: statusDisabled,
        label: formatMessage('account.disable'),
        icon: <FlashOff size={16} />,
        onClick: () => {
          handle(
            {
              userId: record.id,
              enabled: false,
              roleList: record.roleList,
            },
            'updateUser'
          );
        },
      });
    }
    return renderOperationIcon({
      auth: ButtonPermission['UserManagement.enable'],
      disabled: statusDisabled,
      label: formatMessage('account.enable'),
      icon: <PlayOutline size={16} />,
      onClick: () => {
        handle(
          {
            userId: record.id,
            enabled: true,
            roleList: record.roleList,
          },
          'updateUser'
        );
      },
    });
  };

  const addSelectedUsersToGroups = async () => {
    if (!selectedUserKeys.length || !batchGroupIds.length) return;
    setBatchGroupSaving(true);
    try {
      for (const groupId of batchGroupIds) {
        const response = await getUserGroupMembers(groupId);
        const nextUserIds = Array.from(
          new Set([...response.list.map((member: UserGroupMember) => member.userId), ...selectedUserKeys.map(String)])
        );
        await replaceUserGroupMembers(groupId, nextUserIds);
      }
      setBatchGroupOpen(false);
      setBatchGroupIds([]);
      setSelectedUserKeys([]);
      message.success(formatMessage('account.bulk.groupAdded', { count: selectedUserKeys.length }));
      refreshRequest();
    } finally {
      setBatchGroupSaving(false);
    }
  };

  const saveBatchEdit = async ({ homePage, roleId }: BatchEditFormValues) => {
    const targetUserKeys = roleId
      ? batchEditableUserKeys.filter((key) => String(key) !== systemUserKey)
      : batchEditableUserKeys;
    if (targetUserKeys.length !== batchEditableUserKeys.length) {
      message.warning(formatMessage('account.bulk.protectedSkipped'));
    }
    if (!targetUserKeys.length) return;
    setBatchEditSaving(true);
    try {
      const response = await batchUpdateUsers({
        userIds: targetUserKeys.map(String),
        homePage,
        roleList: roleId ? [{ roleId }] : undefined,
      });
      const requestedHomePage = homePage?.trim().toLowerCase();
      const adjustedUsers = requestedHomePage
        ? response.users.filter((user) => user.homePage?.trim().toLowerCase() !== requestedHomePage)
        : [];
      setBatchEditOpen(false);
      setSelectedUserKeys([]);
      if (adjustedUsers.length) {
        const users = adjustedUsers
          .map((user) => `${userLabelsById.get(user.userId) || user.userId} → ${user.homePage}`)
          .join(', ');
        message.warning(
          formatMessage('account.bulk.homePageAdjusted', {
            count: response.updatedCount,
            users,
          })
        );
      } else {
        message.success(formatMessage('account.bulk.updateSuccess', { count: response.updatedCount }));
      }
      refreshRequest();
    } catch {
      // 请求封装会统一提示接口错误，这里只负责保持弹窗状态。
    } finally {
      setBatchEditSaving(false);
    }
  };

  const openBatchEdit = () => {
    const skippedCount = selectedUserKeys.length - batchEditableUserKeys.length;
    if (skippedCount > 0) {
      message.warning(formatMessage('account.bulk.protectedSkipped'));
    }
    if (batchEditableUserKeys.length) {
      setBatchEditOpen(true);
    }
  };

  const columns: any = [
    {
      dataIndex: 'preferredUsername',
      ellipsis: true,
      titleIntlId: 'account.account',
      width: flexibleColumnWidth.account,
      minWidth: flexibleColumnConfig.account.min,
      maxWidth: flexibleColumnConfig.account.max,
    },
    {
      dataIndex: 'firstName',
      ellipsis: true,
      titleIntlId: 'account.displayName',
      width: flexibleColumnWidth.displayName,
      minWidth: flexibleColumnConfig.displayName.min,
      maxWidth: flexibleColumnConfig.displayName.max,
    },
    {
      dataIndex: 'phone',
      ellipsis: true,
      titleIntlId: 'account.phone',
      width: flexibleColumnWidth.phone,
      minWidth: flexibleColumnConfig.phone.min,
      maxWidth: flexibleColumnConfig.phone.max,
    },
    {
      dataIndex: 'email',
      ellipsis: true,
      titleIntlId: 'account.email',
      width: flexibleColumnWidth.email,
      minWidth: flexibleColumnConfig.email.min,
      maxWidth: flexibleColumnConfig.email.max,
    },
    {
      dataIndex: 'roleList',
      ellipsis: true,
      titleIntlId: 'account.role',
      render: (text: any) => text?.map((i: any) => i.roleName)?.join(','),
      width: flexibleColumnWidth.role,
      minWidth: flexibleColumnConfig.role.min,
      maxWidth: flexibleColumnConfig.role.max,
    },
    ...(appRolesEnabled
      ? [
          {
            key: 'businessRoles',
            titleIntlId: 'nativeApp.businessRoles',
            width: 140,
            render: (_: unknown, record: any) => (
              <AuthButton
                auth={ButtonPermission['uns.appManage']}
                type="text"
                disabled={!record.enabled}
                onClick={() =>
                  setRoleTarget({
                    kind: 'user',
                    id: String(record.id),
                    name: record.firstName || record.preferredUsername,
                  })
                }
              >
                {formatMessage('nativeApp.assign')}
              </AuthButton>
            ),
          },
        ]
      : []),
    {
      dataIndex: 'groupList',
      titleIntlId: 'account.groupsTab',
      width: flexibleColumnWidth.userGroup,
      minWidth: flexibleColumnConfig.userGroup.min,
      maxWidth: flexibleColumnConfig.userGroup.max,
      render: (groups: Array<{ groupId: string; groupName: string }> = []) =>
        groups.length ? (
          <Flex gap={4} wrap="wrap">
            {groups.slice(0, 2).map((group) => (
              <Tag bordered={false} key={group.groupId}>
                {group.groupName}
              </Tag>
            ))}
            {groups.length > 2 ? (
              <Tooltip title={groups.map((group) => group.groupName).join(', ')}>
                <Tag bordered={false}>+{groups.length - 2}</Tag>
              </Tooltip>
            ) : null}
          </Flex>
        ) : (
          '-'
        ),
    },
    {
      dataIndex: 'enabled',
      titleIntlId: 'common.status',
      render: (enabled: boolean) => renderStatusTag(enabled),
      width: fixedColumnWidth.status,
      minWidth: fixedColumnWidth.status,
    },
    {
      dataIndex: 'lastLoginAt',
      titleIntlId: 'account.lastLoginAt',
      width: timeColumnWidth,
      render: (value: number) => (value ? formatTimestamp(value, 'YYYY/MM/DD HH:mm', true) : '-'),
    },
    {
      dataIndex: 'createdAt',
      titleIntlId: 'common.creationTime',
      width: timeColumnWidth,
      render: (value: number) => (value ? formatTimestamp(value, 'YYYY/MM/DD HH:mm', true) : '-'),
    },
    {
      dataIndex: 'operation',
      titleIntlId: 'common.operation',
      fixed: 'right',
      width: fixedColumnWidth.operation,
      minWidth: fixedColumnWidth.operation,
      render: (_: any, record: any) => (
        <span className={styles.operation}>
          {renderOperationIcon({
            auth: ButtonPermission['UserManagement.edit'],
            disabled: record?.source === 'external',
            label: formatMessage('common.edit'),
            icon: <Edit size={16} />,
            onClick: () => onAddOpen?.(record),
          })}
          {renderOperationIcon({
            auth: ButtonPermission['UserManagement.resetPassword'],
            disabled: ldapEnable && record?.preferredUsername !== 'tier0',
            label: formatMessage('account.resetpassword'),
            icon: <Password size={16} />,
            onClick: () => onOpen?.(record),
          })}
          {renderStatusToggleIcon(record)}
          {record?.preferredUsername !== 'tier0'
            ? renderOperationIcon({
                auth: ButtonPermission['UserManagement.delete'],
                disabled: (ldapEnable && record?.preferredUsername !== 'tier0') || record?.source === 'external',
                label: formatMessage('common.delete'),
                icon: <TrashCan size={16} />,
                onClick: () => {
                  modal.confirm({
                    ...createDeleteConfirmOptions({
                      title: formatMessage('common.deleteConfirm'),
                      name: record?.preferredUsername,
                      formatMessage,
                    }),
                    onOk: () => {
                      setLoading(true);
                      deleteUser(record.id)
                        .then(() => {
                          message.success(formatMessage('common.optsuccess'));
                          refreshRequest();
                        })
                        .finally(() => {
                          setLoading(false);
                        });
                    },
                  });
                },
              })
            : null}
        </span>
      ),
    },
  ];
  const sectionItems = [
    {
      key: 'users',
      label: (
        <span className={styles['section-tab-label']}>
          <UserRound size={16} strokeWidth={1.75} />
          {formatMessage('account.usersTab')}
        </span>
      ),
    },
    {
      key: 'groups',
      label: (
        <span className={styles['section-tab-label']}>
          <UsersRound size={16} strokeWidth={1.75} />
          {formatMessage('account.groupsTab')}
        </span>
      ),
    },
  ];

  if (appRolesEnabled)
    sectionItems.push({
      key: 'roles',
      label: <span className={styles['section-tab-label']}>{formatMessage('nativeApp.businessRoles')}</span>,
    });
  const startExport = async () => {
    if (exporting) return;
    setExporting(true);
    try {
      await bulkOperationsRef.current?.startExport();
    } finally {
      setExporting(false);
    }
  };

  const openGroupCreateFromBatch = () => {
    setBatchGroupOpen(false);
    setBatchGroupIds([]);
    setActiveSection('groups');
    window.setTimeout(() => userGroupsRef.current?.openCreate(), 0);
  };

  const handleBulkActionClick: MenuProps['onClick'] = ({ key }) => {
    if (key === 'export') void startExport();
  };

  const handleSelectedActionClick: MenuProps['onClick'] = ({ key }) => {
    if (key === 'add-to-groups') {
      setBatchGroupOpen(true);
    }
  };

  const canImportUsers = hasPermission(ButtonPermission['UserManagement.import']);
  const canExportUsers = hasPermission(ButtonPermission['UserManagement.export']);
  const canEditUsers = hasPermission(ButtonPermission['UserManagement.edit']);
  const canAddUsersToGroups = hasPermission(ButtonPermission['UserManagement.userGroups']);

  const bulkActionItems: NonNullable<MenuProps['items']> = [];
  if (canExportUsers) {
    bulkActionItems.push({
      key: 'export',
      icon: <Download size={16} />,
      label: formatMessage('common.export'),
      disabled: exporting,
    });
  }

  const selectedActionItems: NonNullable<MenuProps['items']> = [];
  if (canAddUsersToGroups) {
    selectedActionItems.push({
      key: 'add-to-groups',
      icon: <UsersRound size={16} strokeWidth={1.75} />,
      label: formatMessage('account.bulk.addToGroups', { count: selectedUserKeys.length }),
    });
  }

  return (
    <ComLayout loading={activeSection === 'users' && loading}>
      <ComContent
        className={styles.accountContent}
        hasBack={false}
        title={title}
        extra={
          activeSection === 'users' ? (
            <Flex align="center" gap={8} className={styles.titleActions}>
              <AuthButton auth={ButtonPermission['UserManagement.roleSettings']} onClick={onRoleModalOpen}>
                {formatMessage('account.roleSettings')}
              </AuthButton>
              {canImportUsers ? (
                bulkActionItems.length ? (
                  <Dropdown.Button
                    menu={{ items: bulkActionItems, onClick: handleBulkActionClick }}
                    placement="bottomRight"
                    trigger={['click']}
                    icon={<ChevronDown size={14} />}
                    onClick={() => bulkOperationsRef.current?.openImport()}
                  >
                    <Flex align="center" gap={6}>
                      <Upload size={16} />
                      {formatMessage('common.import')}
                    </Flex>
                  </Dropdown.Button>
                ) : (
                  <Button icon={<Upload size={16} />} onClick={() => bulkOperationsRef.current?.openImport()}>
                    {formatMessage('common.import')}
                  </Button>
                )
              ) : canExportUsers ? (
                <Button icon={<Download size={16} />} loading={exporting} onClick={() => void startExport()}>
                  {formatMessage('common.export')}
                </Button>
              ) : null}
              <AuthButton
                auth={ButtonPermission['UserManagement.add']}
                icon={<Add size={16} />}
                onClick={onAddHandle}
                type="primary"
                disabled={ldapEnable}
              >
                {formatMessage('account.newUsers')}
              </AuthButton>
            </Flex>
          ) : activeSection === 'groups' ? (
            <AuthButton
              auth={ButtonPermission['UserManagement.userGroups']}
              type="primary"
              icon={<Add size={16} />}
              className={styles.titleActions}
              onClick={() => userGroupsRef.current?.openCreate()}
            >
              {formatMessage('account.group.create')}
            </AuthButton>
          ) : null
        }
      >
        <Tabs
          activeKey={activeSection}
          items={sectionItems}
          className={styles.managementTabs}
          onChange={(key) => setActiveSection(key as AccountSection)}
        />
        {activeSection === 'users' ? (
          <>
            <div className={styles['user-toolbar']}>
              <Input
                allowClear
                value={userKeyword}
                onChange={(event) => setUserKeyword(event.target.value)}
                prefix={<Search size={16} />}
                placeholder={formatMessage('account.bulk.searchUsers')}
                className={styles['user-search']}
              />
              <Select
                mode="multiple"
                allowClear
                maxTagCount="responsive"
                className={styles['filter-select']}
                value={statusFilter}
                onChange={setStatusFilter}
                placeholder={formatMessage('account.bulk.filterStatus')}
                options={[
                  { value: 'true', label: formatMessage('account.available') },
                  { value: 'false', label: formatMessage('account.unavailable') },
                ]}
              />
              <Select
                mode="multiple"
                allowClear
                showSearch
                maxTagCount="responsive"
                className={styles['filter-select']}
                value={roleFilter}
                onChange={setRoleFilter}
                placeholder={formatMessage('account.bulk.filterRole')}
                options={roleOptions}
                optionFilterProp="label"
              />
              <Select
                mode="multiple"
                allowClear
                showSearch
                maxTagCount="responsive"
                className={styles['group-filter-select']}
                value={groupFilter}
                onChange={setGroupFilter}
                placeholder={formatMessage('account.bulk.filterGroup')}
                options={groupOptions}
                optionFilterProp="label"
              />
              <div className={styles['toolbar-spacer']} />
              {canEditUsers ? (
                selectedActionItems.length ? (
                  <Dropdown.Button
                    menu={{ items: selectedActionItems, onClick: handleSelectedActionClick }}
                    placement="bottomRight"
                    trigger={['click']}
                    disabled={!selectedUserKeys.length}
                    icon={<ChevronDown size={14} />}
                    onClick={openBatchEdit}
                  >
                    <Flex align="center" gap={6}>
                      <Edit size={16} />
                      {formatMessage('account.bulk.editUsers')}
                    </Flex>
                  </Dropdown.Button>
                ) : (
                  <Button icon={<Edit size={16} />} disabled={!selectedUserKeys.length} onClick={openBatchEdit}>
                    {formatMessage('account.bulk.editUsers')}
                  </Button>
                )
              ) : canAddUsersToGroups ? (
                <Button
                  icon={<UsersRound size={16} strokeWidth={1.75} />}
                  disabled={!selectedUserKeys.length}
                  onClick={() => setBatchGroupOpen(true)}
                >
                  {formatMessage('account.bulk.addToGroups', { count: selectedUserKeys.length })}
                </Button>
              ) : null}
            </div>
            <div ref={tableShellRef} className={styles['table-shell']}>
              <ProTable
                key={tableAvailableWidth || 'auto'}
                resizeable
                columnFit={false}
                className={styles.accountTable}
                style={{ height: '100%' }}
                scroll={{ y: 'calc(100vh  - 350px)', x: tableAvailableWidth || tableScrollX || '100%' }}
                dataSource={filteredRecords as any}
                columns={columns}
                rowKey={(record: any) => String(record?.userId || record?.id || '')}
                rowSelection={{ selectedRowKeys: selectedUserKeys, onChange: setSelectedUserKeys }}
                pagination={{
                  total: filteredRecords.length,
                  pageSize: pagination?.pageSize || 20,
                  current: pagination?.page,
                  showSizeChanger: true,
                  showQuickJumper: true,
                  pageSizeOptions: pagination?.pageSizes,
                  onChange: (page, pageSize) => pagination.onChange({ page, pageSize }),
                }}
              />
            </div>
            {ModalDom}
            {ModalAddDom}
            {RoleModal}
            <BulkUserOperations
              ref={bulkOperationsRef}
              filters={{
                keyword: userKeyword,
                statuses: statusFilter,
                roleIds: roleFilter,
                groupIds: groupFilter,
              }}
            />
            <BatchEditUsersModal
              open={batchEditOpen}
              loading={batchEditSaving}
              selectedCount={batchEditableUserKeys.length}
              resources={menuGroupNoSub}
              roleOptions={roleOptions}
              onCancel={() => setBatchEditOpen(false)}
              onSubmit={saveBatchEdit}
            />
            <ProModal
              open={batchGroupOpen}
              title={formatMessage('account.bulk.addToGroupsTitle')}
              width={600}
              fullScreenable={false}
              destroyOnHidden
              maskClosable={false}
              onCancel={() => setBatchGroupOpen(false)}
            >
              <div className={styles['batch-summary']}>
                <div>
                  <span>{formatMessage('account.bulk.selectedUsers')}</span>
                  <strong>{selectedUserKeys.length}</strong>
                </div>
                <div>
                  <span>{formatMessage('account.bulk.selectedGroups')}</span>
                  <strong>{batchGroupIds.length}</strong>
                </div>
              </div>
              {groupOptions.length ? (
                <>
                  <div className={styles['batch-field-label']}>{formatMessage('account.bulk.selectGroups')}</div>
                  <Select
                    mode="multiple"
                    showSearch
                    value={batchGroupIds}
                    onChange={setBatchGroupIds}
                    options={groupOptions}
                    optionFilterProp="label"
                    placeholder={formatMessage('account.bulk.selectGroupsPlaceholder')}
                  />
                </>
              ) : (
                <div className={styles['empty-group-state']}>
                  <div>
                    <div className={styles['empty-group-title']}>{formatMessage('account.bulk.noGroupsTitle')}</div>
                    <div className={styles['empty-group-desc']}>{formatMessage('account.bulk.noGroupsDesc')}</div>
                  </div>
                  <AuthButton
                    auth={ButtonPermission['UserManagement.userGroups']}
                    type="primary"
                    icon={<Add size={16} />}
                    onClick={openGroupCreateFromBatch}
                  >
                    {formatMessage('account.group.create')}
                  </AuthButton>
                </div>
              )}
              <div className={styles['modal-footer']}>
                <Button disabled={batchGroupSaving} onClick={() => setBatchGroupOpen(false)}>
                  {formatMessage('common.cancel')}
                </Button>
                {groupOptions.length ? (
                  <Button
                    type="primary"
                    loading={batchGroupSaving}
                    disabled={!batchGroupIds.length}
                    onClick={() => void addSelectedUsersToGroups()}
                  >
                    {formatMessage('account.bulk.confirmAdd')}
                  </Button>
                ) : null}
              </div>
            </ProModal>
          </>
        ) : activeSection === 'roles' && appRolesEnabled ? (
          <BusinessRolesPanel />
        ) : (
          <UserGroupsPanel ref={userGroupsRef} />
        )}
        {appRolesEnabled && roleTarget && (
          <BusinessRoleAssignmentDialog target={roleTarget} onClose={() => setRoleTarget(undefined)} />
        )}
      </ComContent>
    </ComLayout>
  );
};

export default AccountManagement;
