import ComLayout from '@/components/com-layout';
import { type FC, useCallback, useEffect, useMemo, useState } from 'react';
import type { PageProps } from '@/common-types';
import { Filter, GuiManagement, Search, Wikis } from '@carbon/icons-react';
import ComContent from '@/components/com-layout/ComContent.tsx';
import { App, Badge, Button, Flex, Form, Popover, Radio, Tag } from 'antd';
import ComLeft from '@/components/com-layout/ComLeft.tsx';
import { usePagination, useTranslate } from '@/hooks';
import ProTree from '@/components/pro-tree';
import ProSearch from '@/components/pro-search';
import ComSearch from '@/components/com-search';
import { AuthButton } from '@/components';
import { ButtonPermission } from '@/common-types/button-permission.ts';
import ProTable from '@/components/pro-table';
import ComTagFilter from '@/components/com-tag-filter';
import useLocalesSettings from '@/pages/localization/components/use-locales-settings';
import useNewEntry from '@/pages/localization/components/use-new-entry';
import './index.scss';
import {
  deleteResourcesApi,
  editResourcesApi,
  getLanguageRecordsApi,
  getModulesListApi,
  getResourcesListApi,
} from '@/apis/core-api/i18n.ts';
import useSimpleRequest from '@/hooks/useSimpleRequest.ts';
import { debounce } from 'lodash-es';
import { EditableCell, EditableRow } from '@/pages/localization/components/Editable';
import { useI18nStore } from '@/stores/i18n-store.ts';
import { useMount } from 'ahooks';
import { hasPermission } from '@/utils';
import useLangChange from '../../hooks/useLangChange.ts';

const TreeHeader = ({ request }: { request?: any }) => {
  const formatMessage = useTranslate();
  const popoverContent = (
    <Radio.Group
      defaultValue={0}
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 8,
      }}
      onChange={(e) => {
        const v = e.target?.value;
        request?.(
          {
            moduleType: v === 0 ? undefined : v,
          },
          false
        );
      }}
      options={[
        { value: 0, label: formatMessage('Localization.all') },
        { value: 1, label: formatMessage('Localization.builtIn') },
        { value: 2, label: formatMessage('Localization.custom') },
      ]}
    />
  );
  const handleDebouncedSearch = useCallback(
    // eslint-disable-next-line react-hooks/use-memo
    debounce((keyword) => {
      request?.({ keyword }, false);
    }, 300),
    [request] // 确保request变化时重新创建
  );

  return (
    <Flex gap={8} align="center" style={{ padding: '0 8px 8px 0' }}>
      <Popover placement="bottomLeft" title="" content={popoverContent} trigger="hover">
        <Button
          icon={<Filter />}
          style={{ flexShrink: 0, background: 'var(--ui-switchwrap-bg-color)' }}
          color="default"
          variant="filled"
        />
      </Popover>
      <ProSearch
        placeholder={formatMessage('common.commonPlaceholder')}
        size="sm"
        onChange={(e) => {
          handleDebouncedSearch(e.target.value);
        }}
      />
    </Flex>
  );
};

const Index: FC<PageProps> = ({ title, location }) => {
  const formatMessage = useTranslate();
  const [searchForm] = Form.useForm();
  const [filterLang, setFilterLang] = useState<string[]>([]);
  const [moduleCode, setModuleCode] = useState();
  const { message } = App.useApp();
  const [exportRecords, setExportRecords] = useState([]);
  // 词条数据
  const {
    loading: resourceLoading,
    data,
    pagination,
    setSearchParams: setResourceParams,
    clearData,
    setData,
    refreshRequest,
  } = usePagination<any>({
    initPageSize: 100,
    fetchApi: getResourcesListApi,
    firstNotGetData: true,
  });

  // 模块数据
  const {
    data: modulesData,
    setSearchParams,
    loading,
  } = useSimpleRequest<any>({
    fetchApi: getModulesListApi,
  });

  useLangChange({ route: location?.pathname });

  const langData = useI18nStore((state) => state.langList);

  useMount(() => {
    setFilterLang(langData?.map((m: any) => m.languageCode));
  });

  const { onNewModalOpen, NewEntryModal } = useNewEntry({
    onSuccessBack: () => {
      refreshRequest?.();
    },
  });
  const { onLocalesModalOpen, LocalesModal } = useLocalesSettings({
    setButtonExportRecords: setExportRecords,
  });

  useEffect(() => {
    getLanguageRecordsApi().then((data) => {
      setExportRecords(data);
    });
  }, []);
  const handleSave = async (row: any) => {
    const index = data.findIndex((item) => row.i18nKey === item.i18nKey);
    if (index === -1) return;

    const originalItem = data[index];

    // 检查值是否相同，如果相同则不进行更新
    const valuesChanged = Object.keys(row.values || {}).some((key) => row.values[key] !== originalItem.values?.[key]);

    if (!valuesChanged) return;

    const updatedItem = { ...originalItem, ...row };

    // 乐观更新UI
    setData((prev) => prev.map((item, i) => (i === index ? updatedItem : item)));

    try {
      await editResourcesApi({
        key: row.i18nKey,
        values: row.values,
        moduleCode,
      });
      refreshRequest(false);
    } catch (error) {
      // 失败时回滚
      setData((prev) => prev.map((item, i) => (i === index ? originalItem : item)));
      console.log('Save failed:', error);
    }
  };

  const columns: any = useMemo(() => {
    return [
      {
        title: () => formatMessage('Localization.i18nMainKey'),
        dataIndex: 'i18nKey',
        width: 300,
        ellipsis: true,
      },
      ...filterLang.map((m) => {
        const cellProps = {
          dataIndex: ['values', m],
          editable: hasPermission(ButtonPermission['Localization.localesSetting']),
        };
        return {
          ...cellProps,
          title: langData?.find((f) => f.languageCode === m)?.languageName || m,
          width: 300,
          ellipsis: true,
          onCell: (record: any) => ({
            record,
            ...cellProps,
            handleSave,
          }),
        };
      }),
    ];
  }, [filterLang, moduleCode, handleSave]);

  const components = {
    body: {
      row: EditableRow,
      cell: EditableCell,
    },
  };

  return (
    <ComLayout>
      {NewEntryModal}
      {LocalesModal}
      <ComContent
        titleStyle={{ paddingLeft: 16 }}
        title={
          <Flex align="center" gap={8} style={{ lineHeight: 1 }}>
            <Wikis size={20} style={{ justifyContent: 'center', verticalAlign: 'middle' }} /> {title}
          </Flex>
        }
        extra={
          <Badge dot={exportRecords?.some((s: any) => !s.confirm)}>
            <AuthButton
              auth={ButtonPermission['Localization.localesSetting']}
              type="primary"
              onClick={() => {
                onLocalesModalOpen();
              }}
            >
              <Flex gap={8}>
                <GuiManagement />
                <span>{formatMessage('Localization.localesSetting')}</span>
              </Flex>
            </AuthButton>
          </Badge>
        }
        hasBack={false}
      >
        <ComLayout>
          <ComLeft title={formatMessage('common.model')} style={{ overflow: 'hidden' }} resize defaultWidth={360}>
            <ProTree
              loading={loading}
              treeNodeIcon={(node: any) => {
                const label = formatMessage(node?.moduleType === 1 ? 'Localization.builtIn' : 'Localization.custom');
                const color = node?.moduleType === 1 ? 'green' : undefined;
                return (
                  <Tag style={{ flexShrink: 0 }} color={color}>
                    {label}
                  </Tag>
                );
              }}
              header={<TreeHeader request={setSearchParams} />}
              showSwitcherIcon={false}
              wrapperStyle={{ padding: '8px 0 8px 8px' }}
              treeData={modulesData}
              height={0}
              onSelect={(selectKey, { node }: any) => {
                if (selectKey?.length === 1) {
                  setModuleCode(node?.moduleCode);
                  setResourceParams(
                    {
                      moduleCode: node?.moduleCode,
                    },
                    false
                  );
                } else {
                  setModuleCode(undefined);
                  clearData();
                }
              }}
            />
          </ComLeft>
          <ComContent
            titleStyle={{ paddingLeft: 16, overflow: 'hidden' }}
            title={
              <Flex gap={8} align="center">
                <span style={{ flexShrink: 0, lineHeight: 1 }}>{formatMessage('common.entry')}</span>
                <ComTagFilter
                  showTag
                  options={langData as any}
                  value={filterLang}
                  onChange={(v: any[]) => {
                    setFilterLang(v);
                  }}
                />
              </Flex>
            }
            extra={
              <Flex style={{ flexShrink: 0 }} gap={8}>
                <ComSearch
                  form={searchForm}
                  formItemOptions={[
                    {
                      name: 'keyword',
                      properties: {
                        prefix: <Search />,
                        placeholder: formatMessage('Localization.searchPlaceholder'),
                        style: { width: 300 },
                        allowClear: true,
                      },
                    },
                  ]}
                  formConfig={{
                    onFinish: () => {
                      setResourceParams(searchForm.getFieldsValue(), false);
                    },
                    disabled: !moduleCode,
                  }}
                  onSearch={() => {
                    setResourceParams(searchForm.getFieldsValue(), false);
                  }}
                />
                <AuthButton
                  disabled={!moduleCode}
                  auth={ButtonPermission['Localization.newEntry']}
                  onClick={() => {
                    onNewModalOpen({
                      filterLang,
                      moduleCode,
                    });
                  }}
                >
                  + {formatMessage('Localization.newEntry')}
                </AuthButton>
              </Flex>
            }
            hasBack={false}
          >
            <ProTable
              resizeable
              loading={resourceLoading}
              columns={columns}
              rowClassName={() => 'editable-row'}
              components={components}
              scroll={{ y: 'calc(100vh  - 260px)', x: '100%' }}
              dataSource={data}
              rowKey="i18nKey"
              pagination={{
                total: pagination?.total,
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
              operationOptions={{
                render: (record) => {
                  return [
                    {
                      type: 'Button',
                      key: 'editEntry',
                      auth: ButtonPermission['Localization.editEntry'],
                      label: formatMessage('common.edit'),
                      onClick: () => {
                        onNewModalOpen(
                          {
                            filterLang,
                            moduleCode,
                          },
                          record
                        );
                      },
                    },
                    {
                      type: 'Popconfirm',
                      key: 'delete',
                      auth: ButtonPermission['Localization.entryDelete'],
                      label: formatMessage('common.delete'),
                      disabled: modulesData?.find((m) => m.moduleCode === moduleCode)?.moduleType === 1,
                      onClick: () => {
                        deleteResourcesApi(moduleCode, encodeURIComponent(record.i18nKey)).then(() => {
                          message.success(formatMessage('common.deleteSuccessfully'));
                          refreshRequest?.();
                        });
                      },
                      popconfirm: {
                        title: formatMessage('common.deleteConfirm'),
                      },
                    },
                  ];
                },
              }}
            />
          </ComContent>
        </ComLayout>
      </ComContent>
    </ComLayout>
  );
};

export default Index;
