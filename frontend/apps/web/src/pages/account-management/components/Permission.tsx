import { Flex } from 'antd';
import styles from './Permission.module.scss';
import { type CSSProperties, forwardRef, useCallback, useImperativeHandle } from 'react';
import { usePropsValue, useTranslate } from '@/hooks';
import type { PermissionNode, PermissionRefProps } from './useRoleSetting';
import ComCheckbox from '@/components/com-checkbox';
import { MenuLucideIcon } from '@/components/lucide-icon';
import type { ResourceProps } from '@/stores/types';
import { formatShowName } from '@/utils';

interface PermissionProps {
  onChange?: (permissionData: PermissionNode[]) => void;
  value?: PermissionNode[];
  initValue?: PermissionNode[];
  style?: CSSProperties;
  // 是否都禁用
  disabled?: boolean;
}

const Permission = forwardRef<PermissionRefProps, PermissionProps>(
  ({ value, initValue, onChange, style, disabled }, ref) => {
    const formatMessage = useTranslate();
    // 使用状态管理权限数据
    const [permissionData, setPermissionData] = usePropsValue<PermissionNode[]>({
      value,
      defaultValue: initValue,
      onChange,
    });
    const enforceLockedNodes = useCallback((data: PermissionNode[] = []) => {
      const next = JSON.parse(JSON.stringify(data || [])) as PermissionNode[];
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
          if (node.locked) {
            node.checked = true;
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
    }, []);
    useImperativeHandle(ref, () => {
      return {
        getValue: () => enforceLockedNodes(permissionData),
        setValue: (p) => setPermissionData(enforceLockedNodes(p)),
      };
    }, [enforceLockedNodes, permissionData, setPermissionData]);

    // 更新节点及其子节点的选中状态
    const updateNodeAndChildren = useCallback(
      (nodes: PermissionNode[], nodeId: string, checked: boolean): PermissionNode[] => {
        return nodes.map((node) => {
          if (node.id === nodeId) {
            // 更新当前节点
            const updatedNode = { ...node, checked };

            // 如果有子节点，递归更新子节点
            if (node.children && node.children.length > 0) {
              updatedNode.children = node.children.map((child) => ({
                ...child,
                checked,
                // 如果子节点还有子节点，递归更新
                children: child.children
                  ? child.children.map((grandChild) => ({
                      ...grandChild,
                      checked,
                    }))
                  : undefined,
              }));
            }
            return updatedNode;
          } else if (node.children && node.children.length > 0) {
            // 递归查找并更新子节点
            return {
              ...node,
              children: updateNodeAndChildren(node.children, nodeId, checked),
            };
          }
          return node;
        });
      },
      []
    );

    // 更新页面权限
    const updatePagePermissions = useCallback(
      (nodes: PermissionNode[], groupId: string, checked: boolean): PermissionNode[] => {
        return nodes.map((node) => {
          if (node.id === groupId) {
            // 更新当前组的页面权限状态
            const updatedNode = { ...node, pagePermissionChecked: checked };

            // 更新子菜单的选中状态（页面权限）
            if (node.children && node.children.length > 0) {
              updatedNode.children = node.children.map((child) => {
                if (child.type === 2) {
                  return { ...child, checked };
                }
                return child;
              });
            }
            return updatedNode;
          } else if (node.children && node.children.length > 0) {
            return {
              ...node,
              children: updatePagePermissions(node.children, groupId, checked),
            };
          }
          return node;
        });
      },
      []
    );

    // 更新操作权限
    const updateActionPermissions = useCallback(
      (nodes: PermissionNode[], groupId: string, checked: boolean): PermissionNode[] => {
        return nodes.map((node) => {
          if (node.id === groupId) {
            // 更新当前组的操作权限状态
            const updatedNode = { ...node, actionPermissionChecked: checked };

            // 更新所有按钮的选中状态
            if (node.children && node.children.length > 0) {
              updatedNode.children = node.children.map((menu) => {
                if (menu.children && menu.children.length > 0) {
                  return {
                    ...menu,
                    children: menu.children.map((button) => {
                      if (button.type === 3) {
                        return { ...button, checked };
                      }
                      return button;
                    }),
                  };
                }
                return menu;
              });
            }
            return updatedNode;
          } else if (node.children && node.children.length > 0) {
            return {
              ...node,
              children: updateActionPermissions(node.children, groupId, checked),
            };
          }
          return node;
        });
      },
      []
    );

    // 检查组的状态并更新
    const checkAndUpdateGroupState = useCallback((data: PermissionNode[]): PermissionNode[] => {
      return data.map((group) => {
        if (group.type === 1 && group.children && group.children.length > 0) {
          // 检查所有菜单是否都被选中
          const allMenusChecked = group.children.every((menu) => (menu.type === 2 ? menu.checked : true));

          // 检查所有按钮是否都被选中
          let allButtonsChecked = true;
          group.children.forEach((menu) => {
            if (menu.children && menu.children.length > 0) {
              if (!menu.children.every((button) => button.checked)) {
                allButtonsChecked = false;
              }
            }
          });

          // 更新组的状态 - 菜单和按钮状态独立判断
          return {
            ...group,
            // 页面权限状态只取决于菜单是否全选
            pagePermissionChecked: allMenusChecked,
            // 操作权限状态只取决于按钮是否全选
            actionPermissionChecked: allButtonsChecked,
            // 组的选中状态取决于所有菜单和所有按钮是否都被选中
            checked: allMenusChecked && allButtonsChecked,
            children: group.children,
          };
        }
        return group;
      });
    }, []);

    // 处理组级别复选框变化
    const handleGroupCheckChange = useCallback(
      (groupId: string, checked: boolean) => {
        setPermissionData((prev: PermissionNode[]) => {
          // 更新组及其所有子节点
          const updated = updateNodeAndChildren(prev, groupId, checked);
          // 同时更新组的pagePermissionChecked和actionPermissionChecked状态
          return enforceLockedNodes(
            updated.map((node) => {
              if (node.id === groupId) {
                return {
                  ...node,
                  pagePermissionChecked: checked,
                  actionPermissionChecked: checked,
                };
              }
              return node;
            })
          );
        });
      },
      [enforceLockedNodes, updateNodeAndChildren]
    );

    // 处理页面权限复选框变化
    const handlePagePermissionChange = useCallback(
      (groupId: string, checked: boolean) => {
        setPermissionData((prev: PermissionNode[]) => {
          // 更新组的页面权限
          const updated = updatePagePermissions(prev, groupId, checked);
          // 检查并更新组的状态
          return enforceLockedNodes(checkAndUpdateGroupState(updated));
        });
      },
      [enforceLockedNodes, updatePagePermissions, checkAndUpdateGroupState]
    );

    // 处理操作权限复选框变化
    const handleActionPermissionChange = useCallback(
      (groupId: string, checked: boolean) => {
        setPermissionData((prev: PermissionNode[]) => {
          // 更新组的操作权限
          const updated = updateActionPermissions(prev, groupId, checked);
          // 检查并更新组的状态
          return enforceLockedNodes(checkAndUpdateGroupState(updated));
        });
      },
      [enforceLockedNodes, updateActionPermissions, checkAndUpdateGroupState]
    );

    // 处理菜单复选框变化
    const handleMenuCheckChange = useCallback(
      (menuId: string, checked: boolean) => {
        setPermissionData((prev: PermissionNode[]) => {
          // 只更新菜单节点本身，不影响按钮
          const updated = prev.map((node) => {
            if (node.children && node.children.length > 0) {
              return {
                ...node,
                children: node.children.map((child) => {
                  if (child.id === menuId) {
                    // 只更新当前菜单的选中状态，不影响其子节点
                    return { ...child, checked };
                  }
                  return child;
                }),
              };
            }
            return node;
          });
          // 检查并更新组的状态
          return enforceLockedNodes(checkAndUpdateGroupState(updated));
        });
      },
      [enforceLockedNodes, checkAndUpdateGroupState]
    );

    // 处理按钮复选框变化
    const handleButtonCheckChange = useCallback(
      (buttonId: string, checked: boolean) => {
        setPermissionData((prev: PermissionNode[]) => {
          // 更新按钮
          const updated = updateNodeAndChildren(prev, buttonId, checked);
          // 检查并更新组的状态
          return enforceLockedNodes(checkAndUpdateGroupState(updated));
        });
      },
      [enforceLockedNodes, updateNodeAndChildren, checkAndUpdateGroupState]
    );

    return (
      <div className={styles['permission']} style={style}>
        {permissionData?.map((item: PermissionNode) => {
          return (
            <div key={item.id} className={styles['permission-group']}>
              <Flex
                align="center"
                style={{
                  height: 40,
                  padding: '0 16px',
                  background: 'var(--ui-table-head-color)',
                }}
              >
                {/*控制所有菜单和按钮的全选反选*/}
                <ComCheckbox
                  className={styles['menu-bar']}
                  label={
                    item.code === 'menu.uns'
                      ? formatMessage('sidebar.unifiedNamespace')
                      : formatShowName({
                          code: item.code,
                          formatMessage: formatMessage,
                          showName: item.showName,
                        })
                  }
                  checked={item.checked}
                  disabled={disabled || item.locked}
                  onChange={(e) => handleGroupCheckChange(item.id, e.target.checked)}
                />
              </Flex>
              {/* 快捷操作 */}
              <Flex
                style={{
                  height: 40,
                  background: 'var(--ui-uns-button-color)',
                  borderBottom: '1px solid var(--ui-t-dividr-color)',
                  borderTop: '1px solid var(--ui-t-dividr-color)',
                }}
              >
                <Flex
                  align="center"
                  style={{
                    flex: '1 1 30%',
                    padding: '0 16px',
                    borderRight: '1px solid var(--ui-t-dividr-color)',
                  }}
                >
                  {/*pagePermissionChecked控制children下面所有的页面全选反选*/}
                  <ComCheckbox
                    rootStyle={{
                      '--custom-border-color': 'var(--ui-table-tr-color)',
                    }}
                    className={styles['operation-bar']}
                    label={formatMessage('common.pagePermission')}
                    checked={item.pagePermissionChecked}
                    disabled={disabled || item.locked}
                    onChange={(e) => handlePagePermissionChange(item.id, e.target.checked)}
                  />
                </Flex>
                <Flex
                  align="center"
                  style={{
                    flex: '1 1 70%',
                    padding: '0 16px',
                  }}
                >
                  {/*actionPermissionChecked控制children下面所有的按钮全选反选*/}
                  <ComCheckbox
                    rootStyle={{
                      '--custom-border-color': 'var(--ui-table-tr-color)',
                    }}
                    className={styles['operation-bar']}
                    label={formatMessage('common.actionPermission')}
                    checked={item.actionPermissionCheckedDisabled ? false : item.actionPermissionChecked}
                    disabled={disabled || item.locked || item.actionPermissionCheckedDisabled}
                    onChange={(e) => handleActionPermissionChange(item.id, e.target.checked)}
                  />
                </Flex>
              </Flex>
              {/* 菜单和按钮 */}
              {item?.children?.map((menu, index) => {
                return (
                  <Flex
                    key={menu.id}
                    style={{
                      borderBottom:
                        item?.children?.length === index + 1 ? undefined : '1px solid var(--ui-t-dividr-color)',
                    }}
                  >
                    <Flex
                      style={{
                        flex: '1 1 30%',
                        overflow: 'hidden',
                        borderRight: '1px solid var(--ui-t-dividr-color)',
                        padding: '8px 16px',
                      }}
                    >
                      {/*控制自己的cheked*/}
                      <ComCheckbox
                        rootStyle={{
                          '--custom-border-color': 'var(--ui-table-tr-color)',
                        }}
                        label={
                          <Flex gap={6} align="center">
                            <MenuLucideIcon item={menu as ResourceProps} size={16} />
                            {formatShowName({
                              code: menu.code,
                              formatMessage: formatMessage,
                              showName: menu.showName,
                            })}
                          </Flex>
                        }
                        checked={menu.checked}
                        disabled={disabled || menu.locked}
                        onChange={(e) => handleMenuCheckChange(menu.id, e.target.checked)}
                      />
                    </Flex>
                    <Flex
                      style={{
                        overflow: 'hidden',
                        // flex: 1,
                        flex: '1 1 70%',
                        padding: '8px 16px',
                      }}
                      wrap
                      gap={8}
                    >
                      {menu?.children?.length ? (
                        menu?.children?.map((button) => {
                          return (
                            <Flex key={button.id}>
                              {/*控制自己的cheked*/}
                              <ComCheckbox
                                disabled={disabled || button.locked}
                                rootStyle={{
                                  '--custom-border-color': 'var(--ui-table-tr-color)',
                                }}
                                label={formatShowName({
                                  code: button.code,
                                  formatMessage: formatMessage,
                                  showName: button.showName,
                                })}
                                checked={button.checked}
                                onChange={(e) => handleButtonCheckChange(button.id, e.target.checked)}
                              />
                            </Flex>
                          );
                        })
                      ) : (
                        <div></div>
                      )}
                    </Flex>
                  </Flex>
                );
              })}
            </div>
          );
        })}
      </div>
    );
  }
);

export default Permission;
