import { useTranslate } from '@/hooks';
import { isRemovedFeaturePath } from '@/utils/removed-feature-pages';
import { useEffect, useRef, useState } from 'react';
import { App, Button, ConfigProvider, Divider, Flex, Form, Input, Popover, Tabs } from 'antd';
import styles from './RoleSetting.module.scss';
import { getRoleList } from '@/apis/core-api/user-manage.ts';
import { Add, Close, UserAvatar } from '@carbon/icons-react';
import { produce } from 'immer';
import Permission from '@/pages/account-management/components/Permission.tsx';
import { addRole, deleteRole, putRole } from '@/apis/core-api/role.ts';
import { childrenRoutes } from '@/routers';
import { validSpecialCharacter } from '@/utils/pattern';
import Loading from '@/components/loading';
import ProModal from '@/components/pro-modal';
import { useBaseStore } from '@/stores/base';
import type { ResourceProps } from '@/stores/types.ts';
import ComSelect from '@/components/com-select';
import { formatShowName } from '@/utils';
import { createDeleteConfirmOptions } from '@/utils/modal-confirm';

export const BuilderRoleId = '7ca9f922-0d35-44cf-8747-8dcfd5e66f8e';
const operatorRoleId = '71dd6dc2-6b12-4273-9ec0-b44b86e5b500';
const disabledRoleList = [BuilderRoleId, operatorRoleId];

const isSystemRole = (role?: any) =>
  disabledRoleList.includes(role?.roleId) || ['admin', 'builder', 'operator'].includes(role?.roleCode || role?.code);

const AddRoleContent = ({ successBack, disabled }: { successBack: (data: any) => void; disabled?: boolean }) => {
  const formatMessage = useTranslate();
  const { message } = App.useApp();
  const [loading, setLoading] = useState(false);
  const [open, setOpen] = useState(false);
  const [form] = Form.useForm();

  useEffect(() => {
    if (!open) {
      form.resetFields();
    }
  }, [open]);

  const onSave = async () => {
    const info = await form.validateFields();
    setLoading(true);
    addRole({ name: info?.roleName })
      .then((data) => {
        setOpen(false);
        message.success(formatMessage('common.optsuccess'));
        successBack?.({
          roleId: data?.roleId,
          roleName: info?.roleName,
          roleCode: data?.roleCode,
          defaultHomePage: data?.defaultHomePage,
          resourceList: data?.resourceList,
        });
      })
      .finally(() => {
        setLoading(false);
      });
  };
  return (
    <Popover
      open={open}
      onOpenChange={(open) => !open && setOpen(false)}
      styles={{
        body: {
          padding: '12px 0',
        },
      }}
      content={
        <div>
          <Flex justify="space-between" align="center" gap={8} style={{ padding: '0 12px' }}>
            <UserAvatar size={24} />
            <Form form={form}>
              <Form.Item
                name="roleName"
                style={{ padding: 0, margin: 0 }}
                rules={[
                  {
                    required: true,
                    message: formatMessage('rule.required'),
                  },
                  {
                    type: 'string',
                    min: 1,
                    max: 10,
                    message: formatMessage('rule.customCharacterLimit', { length: 10 }),
                  },
                  {
                    pattern: validSpecialCharacter,
                    message: formatMessage('common.SpecialCharacterValidation', {
                      rule: '~ ` ! @ # $ % ^ & * ( ) _ + = { } [ ] \\ | ; : \' " , < > . / ?',
                    }),
                  },
                ]}
              >
                <Input
                  allowClear
                  style={{ width: 140 }}
                  size="small"
                  placeholder={formatMessage('account.addRoleName')}
                />
              </Form.Item>
            </Form>
          </Flex>

          <Divider
            style={{
              background: 'var(--ui-t-dividr-color)',
              margin: '14px auto',
            }}
          />
          <Flex justify="flex-end" align="center" style={{ padding: '0 12px' }}>
            <Button
              loading={loading}
              type="primary"
              size="small"
              style={{ width: 60 }}
              onClick={onSave}
              title={formatMessage('common.save')}
            >
              {formatMessage('common.save')}
            </Button>
          </Flex>
        </div>
      }
      arrow={false}
      placement={'bottom'}
      trigger={['click']}
    >
      <Button
        disabled={disabled}
        title={disabled ? formatMessage('account.addRoleMax') : formatMessage('account.addRole')}
        icon={<Add size={16} />}
        iconPosition="end"
        type="primary"
        onClick={() => setOpen(true)}
      >
        {formatMessage('account.addRole')}
      </Button>
    </Popover>
  );
};
export interface PermissionNode {
  id: string;
  showName: string;
  code?: string;
  url?: string;
  type: number;
  sort?: number;
  checked: boolean;
  locked?: boolean;
  pagePermissionChecked?: boolean;
  actionPermissionChecked?: boolean;
  actionPermissionCheckedDisabled?: boolean;
  resourceIds?: string[];
  skipAutoCheckChildren?: boolean;
  children?: PermissionNode[];
}

// 定义子组件暴露的 ref 类型
export interface PermissionRefProps {
  getValue: () => PermissionNode[];
  setValue: (value: PermissionNode[]) => void;
}

const flowPageResourceIds = ['resource:flow.collection.page', 'resource:flow.event.page'];
const flowActionResourceIds = ['resource:flow.read', 'resource:flow.manage'];
const flowRootResourceId = 'resource:flow';
const flowPermissionRowId = 'permission:flow';
const launchpadResourceId = 'resource:launchpad.view';
const launchpadHomePage = '/launchpad';
const permissionFallbackExcludedPaths = new Set(['/routing-management', '/edge-connection']);
const permissionFallbackExcludedCodes = new Set(['route.routingManagement', 'menu.cloudSync']);
const homePageByResourceId: Record<string, string> = {
  'resource:home.page': '/home',
  'resource:uns.page': '/uns',
  'resource:flow.collection.page': '/flow',
  'resource:flow.event.page': '/flow',
  [flowPermissionRowId]: '/flow',
  'resource:project.view': '/project',
  'resource:notebook.view': '/notebook',
  [launchpadResourceId]: launchpadHomePage,
};

const getButtonGroup = (allButtonGroup: ResourceProps[], menuGroup: ResourceProps[]) => {
  const result: ResourceProps[] = [];
  allButtonGroup.forEach((item) => {
    if (item.parentId) {
      let parent = result.find((p) => String(p.id) === String(item.parentId));
      if (!parent) {
        const menuParent = menuGroup.find((p) => String(p.id) === String(item.parentId));
        if (!menuParent && String(item.parentId) !== flowRootResourceId) {
          return;
        }
        // 如果父项不存在，创建并添加到结果
        parent = {
          ...(menuParent || {
            id: flowRootResourceId,
            showName: 'common.flow',
            code: 'common.flow',
            type: 1,
            sort: 20,
          }),
          children: [],
        };
        result.push(parent);
      }
      const id = String(item.id || '').startsWith('resource:') ? item.id : 'button:' + item.code;
      parent.children?.push({ ...item, checked: false, id });
    }
  });
  return result;
};

const getAllMenuTree = (originMenu: ResourceProps[] = []) => {
  // 创建一个映射表，用于快速查找节点
  const map: { [key: string]: ResourceProps } = {};
  const tree: ResourceProps[] = [];
  const menu: ResourceProps[] = [];

  // 首先将所有节点存入映射表，以id为键
  originMenu.forEach((item) => {
    const parent = originMenu?.find((f) => f.id === item.parentId);
    if (!parent || parent?.type === 1) {
      map[item.id] = { ...item, children: [] };
      menu.push(item);
    }
  });
  // 遍历所有节点，根据parentId构建树
  menu.forEach((item) => {
    const node = map[item.id];

    if (!item.parentId) {
      // 如果没有parentId或parentId为0/null/undefined，则认为是根节点
      tree.push(node);
    } else {
      // 否则找到父节点，将当前节点添加到父节点的children中
      const parent = map[item.parentId];
      if (parent) {
        parent.children!.push(node);
        parent.children!.sort((a, b) => a.sort - b.sort);
      }
    }
  });
  return tree.filter((f) => f.type === 2 || (f.type === 1 && f?.children?.length)).sort((a, b) => a.sort - b.sort);
};

const clonePermissionData = (data: PermissionNode[] = []) => JSON.parse(JSON.stringify(data || [])) as PermissionNode[];

const enforceMandatoryPermissions = (data: PermissionNode[] = []) => {
  const next = clonePermissionData(data);
  const refreshGroupState = (group: PermissionNode) => {
    const menuNodes = group.children?.filter((child) => child.type === 2) || [];
    const buttonNodes =
      group.children?.flatMap((child) => child.children || []).filter((child) => child.type === 3) || [];
    const allMenusChecked = menuNodes.length > 0 && menuNodes.every((menu) => menu.checked);
    const allButtonsChecked = buttonNodes.length > 0 && buttonNodes.every((button) => button.checked);
    group.pagePermissionChecked = allMenusChecked;
    group.actionPermissionChecked = buttonNodes.length > 0 ? allButtonsChecked : false;
    group.actionPermissionCheckedDisabled = buttonNodes.length === 0;
    group.checked = buttonNodes.length > 0 ? allMenusChecked && allButtonsChecked : allMenusChecked;
    group.locked = Boolean(group.children?.length) && (group.children || []).every((child) => child.locked);
  };
  const walk = (nodes: PermissionNode[]) => {
    nodes.forEach((node) => {
      if (node.id === launchpadResourceId) {
        node.checked = true;
        node.locked = true;
      }
      if (node.children?.length) {
        walk(node.children);
      }
      if (node.type === 1) {
        refreshGroupState(node);
      }
    });
  };
  walk(next);
  return next;
};

const pageRouteForPermission = (node: PermissionNode) => {
  if (node.url) return node.url;
  if (homePageByResourceId[node.id]) return homePageByResourceId[node.id];
  const resourceId = node.resourceIds?.find((id) => homePageByResourceId[id]);
  return resourceId ? homePageByResourceId[resourceId] : '';
};

const getDefaultHomePageOptions = (data: PermissionNode[] = [], formatMessage: (id: string) => string) => {
  const options: { label: string; value: string }[] = [];
  const addOption = (node: PermissionNode, value: string) => {
    if (!value || isRemovedFeaturePath(value) || options.some((option) => option.value === value)) return;
    options.push({
      value,
      label: formatShowName({
        code: node.code,
        formatMessage,
        showName: node.showName,
      }),
    });
  };
  const walk = (nodes: PermissionNode[]) => {
    nodes.forEach((node) => {
      if (node.type === 2 && node.checked) {
        addOption(node, pageRouteForPermission(node));
      }
      if (node.children?.length) {
        walk(node.children);
      }
    });
  };
  walk(data);
  if (!options.some((option) => option.value === launchpadHomePage)) {
    options.push({ value: launchpadHomePage, label: formatMessage('Launchpad.title') });
  }
  return options;
};

const ensureDefaultHomePage = (
  value: string | undefined,
  data: PermissionNode[] = [],
  formatMessage: (id: string) => string
) => {
  const options = getDefaultHomePageOptions(data, formatMessage);
  if (value && options.some((option) => option.value === value)) {
    return value;
  }
  return options.find((option) => option.value === launchpadHomePage)?.value || options[0]?.value || launchpadHomePage;
};

const useRoleSetting = ({ onSaveBack }: any) => {
  const formatMessage = useTranslate();
  const { originMenu, allButtonGroup } = useBaseStore((state) => ({
    allButtonGroup: state.allButtonGroup,
    originMenu: state.originMenu,
  }));
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<any[]>([]);
  const { message } = App.useApp();
  // 初始数据
  const initItems = useRef<any[]>([]);
  // 初始的菜单按钮配置
  const initialRolePermissionData = useRef<any[]>([]);
  // 跟踪每个标签页的保存状态
  const unsavedChanges = useRef<Map<string, boolean>>(new Map());
  const [loading, setLoading] = useState(false);
  const [activeKey, setActiveKey] = useState('');
  const permissionRefs = useRef<Map<string, PermissionRefProps | null>>(new Map());

  const { modal } = App.useApp();
  const onRoleModalOpen = () => {
    setOpen(true);
  };

  const onClose = () => {
    const hasChanges = [...unsavedChanges.current.values()].some(Boolean);
    if (hasChanges) {
      modal.confirm({
        title: formatMessage('common.unsavedChanges'),
        okText: formatMessage('common.save'),
        cancelText: formatMessage('common.unSave'),
        onOk: () => {
          onSave();
          setOpen(false);
        },
        onCancel: () => {
          setOpen(false);
        },
        okButtonProps: {
          title: formatMessage('common.save'),
        },
        cancelButtonProps: {
          title: formatMessage('common.unSave'),
        },
      });
    } else {
      setOpen(false);
    }
  };
  useEffect(() => {
    if (open) {
      const menuGroup = originMenu?.filter((i) => i.type !== 3 && i.enable);
      const menuTree = getAllMenuTree(menuGroup);
      getRoleList().then((role) => {
        const buttons = getButtonGroup(
          allButtonGroup.filter((item) => String(item.id || '').startsWith('resource:')),
          menuGroup
        );
        initialRolePermissionData.current = mapInitialRolePermissionData(menuTree, buttons);
        const info =
          role?.map?.((i: any) => {
            const permissionData = updatePermissionData(
              initialRolePermissionData.current,
              i?.resourceList?.map((item: any) => item.uri) ?? []
            );
            return {
              ...i,
              defaultHomePage: ensureDefaultHomePage(i?.defaultHomePage, permissionData, formatMessage),
              resourceList: permissionData,
            };
          }) || [];
        setItems(info);
        initItems.current = info;
        setActiveKey(role?.[0]?.roleId);
      });
    }
  }, [open, originMenu]);

  const onSave = () => {
    setLoading(true);
    const newValue = permissionRefs.current.get(activeKey)?.getValue?.();
    const roleItem = items?.find((i: any) => i.roleId === activeKey);
    const defaultHomePage = ensureDefaultHomePage(roleItem?.defaultHomePage, newValue, formatMessage);
    const { checkedResources } = filterMenuAndButtonItems(newValue);
    putRole({
      id: activeKey,
      name: roleItem?.roleName,
      defaultHomePage,
      denyResourceList: [],
      allowResourceList: checkedResources?.map?.((item) => ({ uri: item })) ?? [],
    })
      .then(() => {
        message.success(formatMessage('common.optsuccess'));
        setItems(
          produce(items, (draft) => {
            const info = draft.find((todo) => todo.roleId === activeKey);
            if (info) {
              info['resourceList'] = enforceMandatoryPermissions(newValue);
              info['defaultHomePage'] = defaultHomePage;
            }
          })
        );
        initItems.current = initItems.current.map((item) => {
          if (item.roleId === activeKey) {
            return {
              ...item,
              defaultHomePage,
              resourceList: enforceMandatoryPermissions(newValue),
            };
          } else {
            return item;
          }
        });
        unsavedChanges.current.set(activeKey, false);
      })
      .finally(() => {
        setLoading(false);
      });
  };

  const onChange = (key: string) => {
    if (unsavedChanges.current.get(activeKey)) {
      modal.confirm({
        title: formatMessage('common.unsavedChanges'),
        okText: formatMessage('common.save'),
        cancelText: formatMessage('common.unSave'),
        onOk: () => {
          onSave();
          setActiveKey(key);
        },
        onCancel: () => {
          const initPermission = initItems.current.find((item) => item.roleId === activeKey);
          permissionRefs.current.get(activeKey)?.setValue(initPermission?.resourceList);
          unsavedChanges.current.set(activeKey, false);
          setActiveKey(key);
        },
        okButtonProps: {
          title: formatMessage('common.save'),
        },
        cancelButtonProps: {
          title: formatMessage('common.unSave'),
        },
      });
    } else {
      setActiveKey(key);
    }
  };

  const RoleModal = (
    <ProModal
      afterClose={() => {
        unsavedChanges.current.clear();
        permissionRefs.current.clear();
        setItems([]);
        initItems.current = [];
      }}
      className={styles['role-setting']}
      width={720}
      open={open}
      maskClosable={false}
      onCancel={onClose}
      title={
        <Flex justify="space-between" align="center" gap={24} className={styles['role-modal-title']}>
          <span>{formatMessage('account.roleSettings')}</span>
          <AddRoleContent
            successBack={(data) => {
              setItems((items) => {
                const resourceUris = data?.resourceList?.map?.((item: any) => item.uri)?.filter(Boolean) || [
                  launchpadResourceId,
                ];
                const permissionData = updatePermissionData(initialRolePermissionData.current, resourceUris);
                const newItems = [
                  ...items,
                  {
                    ...data,
                    defaultHomePage: ensureDefaultHomePage(data?.defaultHomePage, permissionData, formatMessage),
                    resourceList: permissionData,
                  },
                ];
                initItems.current = newItems;
                return newItems;
              });
              setActiveKey(data?.roleId);
            }}
            disabled={items?.length >= 10}
          />
        </Flex>
      }
    >
      <ConfigProvider
        theme={{
          components: {
            Tabs: {
              itemSelectedColor: 'var(--ui-theme-color)',
              zIndexPopup: 9999,
              horizontalMargin: '0 0 0 0',
            },
            Dropdown: {
              colorText: '#000',
            },
          },
        }}
      >
        <Loading spinning={loading}>
          <Tabs
            more={{
              overlayStyle: { '--ui-text-color': '#000' },
            }}
            onChange={onChange}
            activeKey={activeKey}
            items={items?.map((item: any) => {
              return {
                label: (
                  <Flex justify="space-between" align="center" gap={8}>
                    {item.roleName}
                    {!isSystemRole(item) && (
                      <Close
                        className={styles['role-tab-close']}
                        size={14}
                        style={{ cursor: 'pointer' }}
                        onClick={(e: any) => {
                          e.stopPropagation();
                          modal.confirm({
                            ...createDeleteConfirmOptions({
                              title: formatMessage('common.deleteConfirm'),
                              name: item?.roleName,
                              formatMessage,
                            }),
                            onOk: async () => {
                              return await deleteRole(item?.roleId).then(() => {
                                message.success(formatMessage('common.deleteSuccessfully'));
                                onSaveBack?.();
                                setItems(
                                  produce(items, (draft) => {
                                    const index = draft.findIndex((todo) => todo.roleId === item.roleId);
                                    if (index !== -1) {
                                      draft.splice(index, 1);
                                      if (activeKey === item.roleId) {
                                        setActiveKey(draft.filter((todo) => todo.roleId !== item.roleId)?.[0]?.roleId);
                                      }
                                    }
                                  })
                                );
                              });
                            },
                          });
                        }}
                      />
                    )}
                  </Flex>
                ),
                key: item.roleId,
                children: (() => {
                  const roleDisabled = isSystemRole(item);
                  const homePageOptions = getDefaultHomePageOptions(item.resourceList, formatMessage);
                  return (
                    <Flex vertical gap={12}>
                      <Flex align="center" gap={12} className={styles['role-homepage']}>
                        <span>{formatMessage('account.defaultHomePage')}</span>
                        <ComSelect
                          value={item.defaultHomePage}
                          disabled={roleDisabled}
                          options={homePageOptions}
                          style={{ width: 260 }}
                          onChange={(value) => {
                            setItems((current) =>
                              produce(current, (draft) => {
                                const info = draft.find((todo) => todo.roleId === item.roleId);
                                if (info) {
                                  info.defaultHomePage = value;
                                }
                              })
                            );
                            const initItem = initItems.current.find((i) => i.roleId === item.roleId);
                            unsavedChanges.current.set(
                              item.roleId,
                              value !== initItem?.defaultHomePage ||
                                JSON.stringify(item.resourceList) !== JSON.stringify(initItem?.resourceList)
                            );
                          }}
                        />
                      </Flex>
                      <Permission
                        disabled={roleDisabled}
                        ref={(el) => permissionRefs.current.set(item.roleId, el)}
                        initValue={item.resourceList}
                        onChange={(pre) => {
                          const nextPermission = enforceMandatoryPermissions(pre);
                          const nextHomePage = ensureDefaultHomePage(
                            item.defaultHomePage,
                            nextPermission,
                            formatMessage
                          );
                          setItems((current) =>
                            produce(current, (draft) => {
                              const info = draft.find((todo) => todo.roleId === item.roleId);
                              if (info) {
                                info.resourceList = nextPermission;
                                info.defaultHomePage = nextHomePage;
                              }
                            })
                          );
                          const initItem = initItems.current.find((i) => i.roleId === item.roleId);
                          const hasChanges =
                            JSON.stringify(nextPermission) !== JSON.stringify(initItem?.resourceList) ||
                            nextHomePage !== initItem?.defaultHomePage;
                          unsavedChanges.current.set(item.roleId, hasChanges);
                        }}
                      />
                    </Flex>
                  );
                })(),
              };
            })}
          />
          <Button
            disabled={isSystemRole(items?.find((item: any) => item.roleId === activeKey))}
            onClick={onSave}
            style={{ height: 32, marginTop: 20 }}
            block
            type="primary"
            loading={loading}
            title={formatMessage('common.save')}
          >
            {formatMessage('common.save')}
          </Button>
        </Loading>
      </ConfigProvider>
    </ProModal>
  );

  return {
    RoleModal,
    onRoleModalOpen,
  };
};

// 根据前端维护的路由
const getOtherRoutes = () => {
  return childrenRoutes
    ?.filter((route) => {
      const path = String(route?.path || '');
      const code = String(route?.handle?.code || '');
      return (
        route?.handle?.parentPath === '/_common' &&
        !['dev', 'all'].includes(route?.handle?.type || '') &&
        !permissionFallbackExcludedPaths.has(path) &&
        !permissionFallbackExcludedCodes.has(code)
      );
    })
    ?.map((route) => {
      const children = [
        {
          showName: route?.handle?.showName,
          code: route?.handle?.code,
          url: route?.path,
          isFrontend: true,
          isRemote: false,
          type: 2,
          urlType: 1,
        },
      ];
      return {
        ...children[0],
        children,
      };
    });
};

// 根据前端维护的button、路由以及kong维护的路由进行初始化数据整合
const mapInitialRolePermissionData = (routes: any, buttonGroup: ResourceProps[]) => {
  const permissionData = [...routes, ...getOtherRoutes()]?.map((group: any) => {
    // id就是路由 keycloke配置的路由
    return {
      ...group,
      id: 'group' + group?.code,
      type: 1,
      checked: false,
      pagePermissionChecked: false,
      actionPermissionChecked: false,
      children:
        group?.children?.length > 0
          ? group?.children?.map((menu: any) => {
              const buttonList = buttonGroup?.find((f) => f.id === menu.id)?.children;
              return {
                ...menu,
                id: menu.id,
                checked: false,
                children: buttonList,
              };
            })
          : [
              {
                ...group,
                id: group.id,
                checked: false,
                children: buttonGroup?.find((f) => f.id === group.id)?.children,
              },
            ],
    };
  });
  return mergeFlowPermissionRows(permissionData, buttonGroup);
};

const mergeFlowPermissionRows = (permissionData: PermissionNode[], buttonGroup: ResourceProps[]) => {
  const flowActions =
    buttonGroup
      ?.find((item) => item.id === flowRootResourceId)
      ?.children?.filter((item) => flowActionResourceIds.includes(String(item.id)))
      ?.sort((a, b) => a.sort - b.sort)
      ?.map((item): PermissionNode => ({
        ...item,
        showName: item.showName || item.code || item.id,
        checked: false,
        children: undefined,
      })) || [];

  return permissionData.map((group) => {
    if (group.code !== 'menu.uns' || !group.children?.length) {
      return group;
    }

    const flowMenus = group.children.filter((menu) => flowPageResourceIds.includes(menu.id));
    if (!flowMenus.length) {
      return group;
    }

    const otherMenus = group.children.filter((menu) => !flowPageResourceIds.includes(menu.id));
    const mergedResourceIds = flowPageResourceIds.filter((id) => flowMenus.some((menu) => menu.id === id));
    const firstFlowMenu = flowMenus[0];
    const flowMenu: PermissionNode = {
      ...firstFlowMenu,
      id: flowPermissionRowId,
      showName: 'common.flow',
      code: 'common.flow',
      checked: false,
      resourceIds: mergedResourceIds,
      skipAutoCheckChildren: true,
      sort: Math.min(...flowMenus.map((menu) => Number(menu.sort || 0))),
      children: flowActions,
    };

    return {
      ...group,
      children: [...otherMenus, flowMenu].sort((a, b) => Number(a.sort || 0) - Number(b.sort || 0)),
    };
  });
};

// 角色只保存 resource 节点，button/API 由 resource_action 绑定。
function filterMenuAndButtonItems(data: PermissionNode[] = []) {
  const result = {
    checkedResources: [] as string[],
  };
  const addResource = (id: string) => {
    if (String(id || '').startsWith('resource:') && !result.checkedResources.includes(id)) {
      result.checkedResources.push(id);
    }
  };

  function traverse(items: PermissionNode[]) {
    items.forEach((item: PermissionNode) => {
      if ((item.type === 2 || item.type === 3) && item.checked) {
        if (item.resourceIds?.length) {
          item.resourceIds.forEach(addResource);
        } else {
          addResource(item.id);
        }
      }

      // 递归处理子项
      if (item.children && item.children.length) {
        traverse(item.children);
      }
    });
  }

  traverse(data);
  addResource(launchpadResourceId);
  return result;
}

// 回显值
function updatePermissionData(data: any, idArray: string[]) {
  const newData = JSON.parse(JSON.stringify(data));
  function updateChecked(items: any) {
    if (!items || !Array.isArray(items)) return;

    items.forEach((item: any) => {
      if (idArray?.includes(item.id) || item.resourceIds?.some((id: string) => idArray?.includes(id))) {
        item.checked = true;
      }
      if (item.type === 2 && item.checked && item.children?.length && !item.skipAutoCheckChildren) {
        item.children = item.children.map((child: any) => ({ ...child, checked: true }));
      }

      if (item.children && item.children.length) {
        updateChecked(item.children);
      }
    });

    items.forEach((item: any) => {
      // 如果是group类型，检查并更新其状态
      if (item.type === 1) {
        const menuNodes = item.children?.filter((child: any) => child.type === 2) || [];
        const buttonNodes =
          item.children?.flatMap((child: any) => child.children || []).filter((child: any) => child.type === 3) || [];

        // 检查并更新pagePermissionChecked - 只受menu类型节点影响
        const allMenuChecked = menuNodes.length > 0 && menuNodes.every((menu: any) => menu.checked === true);
        // 如果任何菜单被选中，则页面权限部分选中
        item.pagePermissionChecked = allMenuChecked;

        // 检查并更新actionPermissionChecked - 只受button类型节点影响
        if (buttonNodes.length === 0) {
          item.actionPermissionChecked = false; // 设置默认值为false，确保禁用状态下始终为未选中
          item.actionPermissionCheckedDisabled = true; // 添加禁用标志
        } else {
          const allButtonsChecked = buttonNodes.every((button: any) => button.checked === true);
          item.actionPermissionChecked = allButtonsChecked;
          item.actionPermissionCheckedDisabled = false; // 有按钮时不禁用
        }
        item.checked =
          (buttonNodes.length === 0 && allMenuChecked) ||
          (menuNodes.length === 0 &&
            buttonNodes.length > 0 &&
            buttonNodes.every((button: any) => button.checked === true)) ||
          (menuNodes.length > 0 &&
            buttonNodes.length > 0 &&
            allMenuChecked &&
            buttonNodes.every((button: any) => button.checked === true));
      }
    });
  }

  updateChecked(newData);
  return enforceMandatoryPermissions(newData);
}

export default useRoleSetting;
