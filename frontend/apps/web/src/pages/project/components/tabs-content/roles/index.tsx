import { type FC, useCallback, useEffect, useMemo, useState } from 'react';
import { useParams } from 'react-router';

import { getProjectRoles } from '@/apis/core-api';
import type { PageProps } from '@/common-types';
import ProTable from '@/components/pro-table';
import { useActivate } from '@/contexts/tabs-lifecycle-context.ts';
import { useTranslate } from '@/hooks';
import type { ProjectRole } from '@/pages/project/types';

import TabLayout from '../../TabLayout';
import { createRolesColumns, type RoleItem } from './config.tsx';
import styles from './index.module.scss';

const Roles: FC<PageProps> = () => {
  const { projectId: id } = useParams();
  const formatMessage = useTranslate();
  const [roles, setRoles] = useState<RoleItem[]>([]);
  const [loading, setLoading] = useState(false);

  const projectId = Number(id);

  const getRoles = useCallback(async () => {
    if (!projectId || Number.isNaN(projectId)) {
      setRoles([]);
      return;
    }

    setLoading(true);
    try {
      // 数据统一通过接口层获取
      const response = await getProjectRoles(projectId);
      const roleList: ProjectRole[] = Array.isArray(response) ? response : [];

      // 按 roleId 聚合角色，兼容后端可能返回重复 roleId 的场景
      const roleMap = new Map<string, RoleItem>();

      roleList.forEach((role) => {
        const roleName = role.roleName || role.roleKey || '-';
        const roleId = String(role.roleId || roleName);
        const appList = Array.isArray(role.apps) ? role.apps : [];
        const appNames = appList.map((app) => app?.appName?.trim()).filter((name): name is string => Boolean(name));

        if (!roleMap.has(roleId)) {
          roleMap.set(roleId, {
            id: roleId,
            name: roleName,
            description: role.description || '-',
            memberCount: role.memberCount ?? '-',
            canAccess: appNames,
          });
          return;
        }

        const currentRole = roleMap.get(roleId);
        if (!currentRole) {
          return;
        }

        if (currentRole.description === '-' && role.description) {
          currentRole.description = role.description;
        }

        if (currentRole.memberCount === '-' && role.memberCount !== undefined) {
          currentRole.memberCount = role.memberCount;
        }

        appNames.forEach((appName) => {
          if (!currentRole.canAccess.includes(appName)) {
            currentRole.canAccess.push(appName);
          }
        });
      });

      setRoles(Array.from(roleMap.values()));
    } catch {
      setRoles([]);
    } finally {
      setLoading(false);
    }
  }, [projectId]);

  useEffect(() => {
    getRoles();
  }, [getRoles]);

  useActivate(() => {
    getRoles();
  });

  const columns: any = useMemo(() => {
    return createRolesColumns({
      formatMessage,
      styles,
    });
  }, [formatMessage]);

  return (
    <TabLayout style={{ padding: '0 24px' }}>
      <ProTable
        loading={loading}
        scroll={{ x: 'max-content', y: 'calc(100vh - 300px)' }}
        columns={columns}
        dataSource={roles as any}
        resizeable
        pagination={false}
      />
    </TabLayout>
  );
};

export default Roles;
