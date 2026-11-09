import { deleteProject, getProjectList } from '@/apis/core-api';
import { ComSearch, ComEmptyState } from '@/components';
import { AuthButton, AuthWrapper } from '@/components/auth';
import ComLayout from '@/components/com-layout';
import ComContent from '@/components/com-layout/ComContent';
import { ButtonPermission } from '@/common-types/button-permission';
import ProTable from '@/components/pro-table';
import { useActivate } from '@/contexts/tabs-lifecycle-context.ts';
import { useMediaSize, usePagination, useTranslate } from '@/hooks';
import { formatTimestamp } from '@/utils/format';
import { getSearchParamsString } from '@/utils/url-util';
import { createDeleteConfirmOptions } from '@/utils/modal-confirm';
import { PageTitleRow } from '@/components/lucide-icon';
import { Edit, Folder, Search, TrashCan } from '@/components/lucide-icon/carbon';
import type { PaginationProps } from 'antd';
import { App, Button, Flex, Form, Tooltip } from 'antd';
import { useCallback, useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import ProjectFormModal from './components/ProjectFormModal';
import { AppStatus } from './components/tabs-content/apps';
import styles from './index.module.scss';
import type { ProjectItem } from './types';

const Project = () => {
  const navigate = useNavigate();
  const formatMessage = useTranslate();
  const { modal, message } = App.useApp();
  const { isH5 } = useMediaSize();
  const [searchForm] = Form.useForm();
  const createProjectModalRef = useRef<any>(null);

  const [type, setType] = useState('');

  const { loading, data, pagination, setSearchParams, reload, refreshRequest } = usePagination<ProjectItem>({
    initPageSize: 20,
    fetchApi: getProjectList,
    defaultParams: {
      k: '',
    },
  });

  const onDeleteHandle = (item: any) => {
    return deleteProject(item.id)
      .then(() => {
        message.success(formatMessage('common.deleteSuccessfully'));
        reload();
      })
      .catch((error: any) => {
        // 后端校验失败（如项目下仍有应用 project.hasApps）时映射为友好提示，与云端行为对齐
        if (error?.msg === 'project.hasApps') {
          message.warning(formatMessage('project.hasApps'));
          return;
        }
        message.error(error?.msg || formatMessage('project.deleteFailed'));
      });
  };

  const showTotal: PaginationProps['showTotal'] = (total) =>
    isH5 ? null : `${formatMessage('common.total')}  ${total}  ${formatMessage('common.items')}`;

  useActivate(() => {
    refreshRequest?.();
  });

  const hasList = data.length > 0;

  const handleSearch = useCallback(() => {
    setSearchParams(searchForm.getFieldsValue());
  }, [searchForm, setSearchParams]);

  return (
    <ComLayout loading={loading}>
      <ComContent
        mustHasBack={false}
        hasBack={false}
        title={
          <PageTitleRow resourceKey="project.view">
            <span>{formatMessage('common.project')}</span>
          </PageTitleRow>
        }
        extra={
          <Flex style={{ flexShrink: 0 }} gap={8}>
            <ComSearch
              form={searchForm}
              formItemOptions={[
                {
                  name: 'k',
                  properties: {
                    prefix: <Search />,
                    placeholder: formatMessage('common.searchPlaceholder'),
                    style: { width: 300 },
                    allowClear: true,
                  },
                },
              ]}
              formConfig={{
                onFinish: handleSearch,
              }}
              onSearch={handleSearch}
            />
            <AuthButton
              auth={ButtonPermission['Project.add']}
              type="primary"
              onClick={() => {
                setType('create');
                createProjectModalRef?.current?.onOpen?.();
              }}
            >
              + {formatMessage('project.createProject')}
            </AuthButton>
          </Flex>
        }
      >
        {hasList ? (
          <div className={styles['table-wrap']}>
            <ProTable
              scroll={{ x: 'max-content', y: 'calc(100vh - 260px)' }}
              columns={[
                {
                  title: () => formatMessage('common.name'),
                  dataIndex: 'name',
                  key: 'name',
                  width: '15%',
                  render: (text: string, record: any) => {
                    return (
                      <Button
                        type="link"
                        className={`table-link-button ${styles.nameWrap}`}
                        onClick={() => {
                          navigate(
                            `/project/${record.id}?${getSearchParamsString({
                              from: location.pathname,
                            })}`,
                            { state: { tabName: text } }
                          );
                        }}
                        title={text}
                      >
                        <span className={styles.nameIcon}>
                          <Folder size={16} />
                        </span>
                        {text}
                      </Button>
                    );
                  },
                },
                {
                  title: () => formatMessage('common.description'),
                  dataIndex: 'description',
                  key: 'description',
                  width: '20%',
                  ellipsis: true,
                },
                {
                  title: () => formatMessage('project.apps'),
                  dataIndex: 'appNums',
                  key: 'appNums',
                  width: '10%',
                },
                {
                  title: () => formatMessage('common.creator'),
                  dataIndex: 'createdBy',
                  key: 'createdBy',
                  width: '15%',
                  ellipsis: true,
                },
                {
                  title: () => formatMessage('project.lastUpdated'),
                  dataIndex: 'updatedAt',
                  key: 'updatedAt',
                  width: '15%',
                  render: (text: number) => formatTimestamp(text, 'YYYY/MM/DD HH:mm', true),
                },
                {
                  title: () => formatMessage('common.operation'),
                  key: 'operation',
                  fixed: 'right',
                  width: 120,
                  render: (_: any, record: any) => (
                    <span className={styles['operation']}>
                      <AuthWrapper auth={ButtonPermission['Project.edit']}>
                        <Tooltip title={formatMessage('common.edit')}>
                          <Edit
                            className="custom-operation"
                            style={{ cursor: 'pointer' }}
                            onClick={() => {
                              setType('edit');
                              createProjectModalRef?.current?.onOpen?.({
                                id: record.id,
                                name: record.name,
                                description: record.description,
                              });
                            }}
                          />
                        </Tooltip>
                      </AuthWrapper>
                      <AuthWrapper auth={ButtonPermission['Project.delete']}>
                        <Tooltip title={formatMessage('common.delete')}>
                          <TrashCan
                            className="custom-operation"
                            style={{ cursor: 'pointer' }}
                            onClick={() => {
                              if (record.status === AppStatus.Active) {
                                message.warning(formatMessage('project.disabledDeleteProject'));
                                return;
                              }
                              if (record.appNums > 0) {
                                message.warning(formatMessage('project.deleteHasApps'));
                                return;
                              }
                              setType('delete');
                              modal.confirm({
                                ...createDeleteConfirmOptions({
                                  title: formatMessage('common.deleteConfirm'),
                                  name: record?.name,
                                  formatMessage,
                                }),
                                onOk: () => onDeleteHandle(record),
                              });
                            }}
                          />
                        </Tooltip>
                      </AuthWrapper>
                    </span>
                  ),
                },
              ]}
              dataSource={data as any}
              onRow={(record: ProjectItem) => ({
                onClick: () => {
                  navigate(
                    `/project/${record.id}?${getSearchParamsString({
                      from: location.pathname,
                    })}`,
                    { state: { tabName: record.name } }
                  );
                },
              })}
              resizeable
              pagination={{
                total: pagination?.total,
                showTotal,
                style: { display: 'flex', justifyContent: 'flex-end', padding: '10px 0' },
                pageSize: pagination?.pageSize || 20,
                current: pagination?.page,
                showQuickJumper: true,
                pageSizeOptions: pagination?.pageSizes,
                onChange: pagination.onChange,
                onShowSizeChange: (current, size) => {
                  pagination.onChange({ page: current, pageSize: size });
                },
              }}
            />
          </div>
        ) : (
          <ComEmptyState description={formatMessage('project.emptyTitle')} />
        )}

        <ProjectFormModal
          ref={createProjectModalRef}
          title={type === 'edit' ? formatMessage('project.editProject') : formatMessage('project.createProject')}
          refreshRequest={refreshRequest}
        />
      </ComContent>
    </ComLayout>
  );
};

export default Project;
