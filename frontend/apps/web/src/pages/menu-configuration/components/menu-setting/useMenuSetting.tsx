import ProModal from '@/components/pro-modal';
import useTranslate from '@/hooks/useTranslate.ts';
import { type Key, useMemo, useState } from 'react';
import { useImmer } from 'use-immer';
import ProTree from '@/components/pro-tree/index.ts';
import type { TreeProps } from 'antd';
const defaultData: any[] = [
  {
    title: 'UNS',
    titleKey: 'menu.tag.uns',
    key: 'uns',
    children: [
      {
        title: 'Namespace',
        titleKey: 'Namespace',
        key: 'namespace',
        isLeaf: true,
        parentKey: 'uns',
      },
      {
        title: 'Edge Connection',
        titleKey: 'Edge Connection',
        key: 'mqtt-auth',
        isLeaf: true,
        parentKey: 'uns',
      },
    ],
  },
  {
    title: 'Project',
    titleKey: 'Project',
    key: 'project',
    isLeaf: true,
  },
  {
    title: 'Launchpad',
    titleKey: 'Launchpad',
    key: 'launchpad',
    isLeaf: true,
  },
  {
    title: 'System',
    titleKey: 'menu.tag.system',
    key: 'system',
    children: [
      {
        title: 'User Management',
        titleKey: 'UserManagement',
        key: 'user-management',
        isLeaf: true,
        parentKey: 'system',
      },
      {
        title: 'API Key',
        titleKey: 'API Key',
        key: 'api-key',
        isLeaf: true,
        parentKey: 'system',
      },
      {
        title: 'Audit Log',
        titleKey: 'AuditLog',
        key: 'audit-log',
        isLeaf: true,
        parentKey: 'system',
      },
      {
        title: 'OAuth Client',
        titleKey: 'menu.oauthClient',
        key: 'oauth-clients',
        isLeaf: true,
        parentKey: 'system',
      },
    ],
  },
];
const HOME_KEY = 'uns';
const SYSTEM_CONFIG_KEY = 'system';
const disableDND = [HOME_KEY, SYSTEM_CONFIG_KEY];

const loop = (data: any[], key: Key, callback: (node: any, i: number, data: any[]) => void) => {
  for (let i = 0; i < data.length; i++) {
    if (data[i].key === key) {
      return callback(data[i], i, data);
    }
    if (data[i].children) {
      loop(data[i].children!, key, callback);
    }
  }
};

const translateTreeData = (
  data: any[],
  formatMessage: (key: string, values?: any, defaultMessage?: string) => string
): any[] =>
  data.map((item) => ({
    ...item,
    title: item.titleKey ? formatMessage(item.titleKey, undefined, item.title) : item.title,
    children: item.children ? translateTreeData(item.children, formatMessage) : undefined,
  }));

const useMenuSetting = () => {
  const [open, setOpen] = useState(false);
  // const [gData, setData] = useState(defaultData);
  const [gData, setData] = useImmer(defaultData);
  const formatMessage = useTranslate();
  const treeData = useMemo(() => translateTreeData(gData, formatMessage), [formatMessage, gData]);

  const onMenuModalOpen = () => {
    setOpen(true);
  };

  const onDrop: TreeProps['onDrop'] = (info) => {
    const { node, dragNode, dropPosition, dropToGap } = info;
    const dropKey = node.key;
    const dragKey = dragNode.key;
    const dropPos = node.pos.split('-');
    const resolvedDropPosition = dropPosition - Number(dropPos[dropPos.length - 1]);
    setData((draft) => {
      // 1. 查找并从原位置删除拖拽节点
      let dragObj: any;
      loop(draft, dragKey, (item, index, arr) => {
        arr.splice(index, 1);
        dragObj = item;
      });

      // 2. 将拖拽节点插入到新位置
      if (dragObj) {
        if (!dropToGap) {
          // 拖拽到目标节点内部
          loop(draft, dropKey, (item) => {
            item.children = item.children || [];
            item.children.unshift(dragObj);
          });
        } else {
          // 拖拽到目标节点的间隙
          let ar: any[] = [];
          let i: number = 0;
          loop(draft, dropKey, (_item, index, arr) => {
            ar = arr;
            i = index;
          });
          const insertIndex = resolvedDropPosition === -1 ? i : i + 1;
          ar.splice(insertIndex, 0, dragObj);
        }
      }
    });
  };

  const MenuModal = (
    <ProModal title={formatMessage('account.menuSettings')} open={open} onCancel={() => setOpen(false)}>
      <div>
        <ProTree
          multiple
          onDrop={onDrop}
          treeData={treeData}
          draggable={{
            icon: false,
            nodeDraggable: (node: any) => {
              return !(disableDND.includes(node.key) || (node.parentKey && disableDND.includes(node.parentKey)));
            },
          }}
          allowDrop={({ dropNode, dropPosition, dragNode }: any) => {
            // 检查父节点是否在禁用列表中
            if (dropNode.parentKey && disableDND.includes(dropNode.parentKey)) return false;
            // 检查特定节点的放置限制
            if (dropNode.key === HOME_KEY) return ![0, -1].includes(dropPosition);
            if (dropNode.key === SYSTEM_CONFIG_KEY) return ![1, 0].includes(dropPosition);
            // 叶子节点不能作为父节点放置
            if (dropNode.isLeaf && dropPosition === 0) return false;
            if (!dragNode?.isLeaf) {
              // 文件夹不能拖到文件夹内
              if (dropNode?.isLeaf) {
                // 文件任何位置不行
                return false;
              } else if (dropPosition === 0) {
                // 文件夹内不行
                return false;
              } else {
                return true;
              }
            }
            return true;
          }}
        />
      </div>
    </ProModal>
  );

  return {
    onMenuModalOpen,
    MenuModal,
  };
};

export default useMenuSetting;
