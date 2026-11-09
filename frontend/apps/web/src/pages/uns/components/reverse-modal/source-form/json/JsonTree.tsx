import {
  useState,
  useEffect,
  forwardRef,
  useImperativeHandle,
  type Dispatch,
  type SetStateAction,
  type RefObject,
} from 'react';
import { Form } from 'antd';
import type { TreeProps, TreeDataNode } from 'antd';
import { cloneDeep } from 'lodash-es';
import ComTree from '@/components/com-tree';
import { generateAlias } from '@/utils/uns';

export interface JsonTreeRefProps {
  checkedKeys: string[];
}
export interface JsonTreeProps {
  ref: RefObject<JsonTreeRefProps>;
  treeData: TreeNode[];
  setTreeData: Dispatch<SetStateAction<TreeNode[]>>;
  selectedInfo?: TreeNode;
  setSelectedInfo: Dispatch<SetStateAction<TreeNode | undefined>>;
}

export interface FieldItem {
  name: string;
  type: string;
  displayName?: string;
  remark?: string;
  unique?: boolean;
  systemField?: boolean;
}

export interface TreeNode extends TreeDataNode {
  name: string;
  dataPath: string;
  newDataPath?: string;
  children?: TreeNode[];
  // type: number;
  pathType: number;
  description?: string;
  tags?: string[];
  persistence?: boolean;
  addFlow?: boolean;
  mainKey?: number;
  fields?: FieldItem[];
  parentDataPath?: string;
  alias?: string;
  parentAlias?: string;
  [key: string]: any;
}

const JsonForm = forwardRef<JsonTreeRefProps, JsonTreeProps>(
  ({ treeData, setTreeData, selectedInfo, setSelectedInfo }, ref) => {
    const form = Form.useFormInstance();
    const [checkedKeys, setCheckedKeys] = useState<string[]>([]);
    const [selectedKeys, setSelectedKeys] = useState<string[]>([]);

    const currentNode = Form.useWatch('currentNode', form) || form.getFieldValue('currentNode');

    useImperativeHandle(ref, () => ({
      checkedKeys: checkedKeys,
    }));

    const onSelect: TreeProps['onSelect'] = (selectedKeys, info) => {
      form.setFieldsValue({
        currentNode: undefined,
      });
      setSelectedKeys(selectedKeys as string[]);
      setSelectedInfo((selectedKeys.length ? info?.selectedNodes[0] : undefined) as TreeNode);
    };

    useEffect(() => {
      form.setFieldsValue({
        currentNode: selectedInfo || undefined,
      });
      if (!selectedInfo) setSelectedKeys([]);
    }, [selectedInfo]);

    const onCheck: TreeProps['onCheck'] = (checkedKeysValue) => {
      setCheckedKeys(checkedKeysValue as string[]);
    };

    const updateChildrenNewDataPaths = (node: TreeNode, parentNames: string[], parentAlias?: string) => {
      node.newDataPath = [...parentNames, node.name].join('.');
      node.parentAlias = parentAlias || node.parentAlias;
      if (node.children && node.children.length > 0) {
        node.children.forEach((child) => updateChildrenNewDataPaths(child, [...parentNames, node.name], node.alias));
      }
    };

    const replaceNodeInTree = (tree: TreeNode[], targetDataPath: string, newNode: TreeNode): TreeNode[] => {
      const traverse = (nodes: TreeNode[], parentNames: string[] = []): boolean => {
        for (let i = 0; i < nodes.length; i++) {
          const children = nodes[i].children;
          if (nodes[i].dataPath === targetDataPath) {
            // 更新新节点的 children 以保持原有结构，并更新 newDataPath
            newNode.children = nodes[i].children || [];
            newNode.alias = generateAlias(newNode.name);

            // 使用扩展运算符合并对象，但不覆盖 children，以便后续更新 newDataPath
            nodes[i] = { ...nodes[i], ...newNode, children: newNode.children };

            // 更新替换后的节点及其子节点的 newDataPath
            updateChildrenNewDataPaths(nodes[i], parentNames);

            return true;
          } else if (children && children.length > 0) {
            // 显式检查 nodes[i].children 是否存在且为非空数组
            if (traverse(children, [...parentNames, nodes[i].name])) {
              return true;
            }
          }
        }
        return false;
      };

      // 创建树的副本以避免直接修改原始数据
      const treeCopy = cloneDeep(tree) as TreeNode[];

      // 开始从根节点进行遍历和替换操作
      traverse(treeCopy);

      return treeCopy;
    };

    useEffect(() => {
      if (currentNode?.name) {
        const newTreeData = replaceNodeInTree(treeData, selectedKeys?.[0], currentNode);
        setTreeData(newTreeData);
      }
    }, [currentNode, selectedKeys]);

    return (
      <div className="json-tree">
        <ComTree
          ibmStyle={false}
          defaultExpandAll
          checkable
          blockNode
          showIcon
          treeData={treeData}
          fieldNames={{ title: 'name', key: 'dataPath' }}
          selectedKeys={selectedKeys}
          checkedKeys={checkedKeys}
          onSelect={onSelect}
          onCheck={onCheck}
        />
      </div>
    );
  }
);
export default JsonForm;
