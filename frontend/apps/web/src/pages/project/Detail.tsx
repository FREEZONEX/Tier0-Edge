import { useTabName, useTranslate } from '@/hooks';
import { type FC, useCallback, useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router';

import { getProject } from '@/apis/core-api';
import type { PageProps } from '@/common-types';
import { ComBtnTabs } from '@/components';
import ComDetailHeader from '@/components/com-detail-header';
import ComLayout from '@/components/com-layout';
import ComContent from '@/components/com-layout/ComContent';
import { getSearchParamsObj } from '@/utils/url-util';
import { Flex, message } from 'antd';
import { Folder } from 'lucide-react';
import AppsTab from './components/tabs-content/apps';
import MembersTab from './components/tabs-content/members';
import RolesTab from './components/tabs-content/roles';
import styles from './Detail.module.scss';
import type { ProjectItem } from './types';

const Detail: FC<PageProps> = ({ location }) => {
  const searchParams = getSearchParamsObj(location?.search) || {};
  const fromPath = searchParams.from;
  const { projectId } = useParams<{ projectId: string }>();
  const navigate = useNavigate();

  const formatMessage = useTranslate();
  const [apiLoading] = useState(false);
  const tabParam = searchParams.tab;
  const [activeTab, setActiveTab] = useState(tabParam === 'members' || tabParam === 'roles' ? tabParam : 'apps');
  const [projectDetail, setProjectDetail] = useState<ProjectItem>();
  useTabName(projectDetail?.name);

  const tabOptions = [
    { label: formatMessage('project.apps'), value: 'apps' },
    { label: formatMessage('project.tabMembers'), value: 'members' },
    { label: formatMessage('project.tabRoles'), value: 'roles' },
  ];

  // 根据 activeTab 渲染组件，组件在 src/pages/project/components/tabs-content 下
  const renderActiveTab = () => {
    switch (activeTab) {
      case 'apps':
        return <AppsTab projectId={projectId} />;
      case 'members':
        return <MembersTab />;
      case 'roles':
        return <RolesTab />;
      default:
        return null;
    }
  };

  const handleBack = () => {
    if (fromPath) {
      navigate(fromPath);
    } else {
      navigate(-1);
    }
  };

  // 根据projectId获取项目详情
  const getProjectDetail = useCallback(
    async (projectId: string) => {
      try {
        const data = await getProject(projectId);
        setProjectDetail(data);
      } catch (err: any) {
        if (err?.code === 403 || err?.code === 404) {
          message.error(
            formatMessage('project.accessDeniedOrNotFound', {}, 'Project not found or you have no access to it.')
          );
          navigate('/');
        } else {
          message.error(err?.msg || formatMessage('common.serverBusy', {}, 'Server is busy, please try again later'));
        }
      }
    },
    [formatMessage, navigate]
  );

  useEffect(() => {
    if (projectId) {
      getProjectDetail(projectId);
    }
  }, [projectId, getProjectDetail]);

  return (
    <ComLayout loading={apiLoading}>
      <ComContent mustHasBack={false} style={{ overflow: 'hidden' }} hasPadding border={false}>
        <Flex vertical style={{ height: '100%' }}>
          <div className={styles.projectHeader}>
            <ComDetailHeader
              showBack
              onBack={handleBack}
              showDesc
              titleNode={
                <Flex align="center" gap={8}>
                  <span className={styles.titleIcon} aria-hidden="true">
                    <Folder size={14} />
                  </span>
                  <h1 className={styles.title} title={projectDetail?.name}>
                    {projectDetail?.name}
                  </h1>
                  {projectDetail?.id != null && <span className={styles.projectId}>ID: {projectDetail.id}</span>}
                </Flex>
              }
              desc={projectDetail?.description || ''}
            />
          </div>
          {projectDetail && (
            <div className={styles.content}>
              <ComBtnTabs
                className={styles.toolbarRow}
                activeKey={activeTab}
                options={tabOptions}
                onSelect={(item) => setActiveTab(item.value)}
              />
              <div className={styles.tabContentWrapper}>{renderActiveTab()}</div>
            </div>
          )}
        </Flex>
      </ComContent>
    </ComLayout>
  );
};

export default Detail;
