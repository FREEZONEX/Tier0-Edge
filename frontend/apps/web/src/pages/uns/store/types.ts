import type { Key } from 'react';
import type { UnsTreeNode } from '@/pages/uns/types';

export type TSearchType = 1 | 2 | 3;
export type TTreeType = 'uns';
// all-内部topic，unusedTopic-外部topic
export type TCurrentTreeMapType = 'all' | 'unusedTopic';

export interface NodePaginationStateType {
  currentPage: number; // 当前页码
  hasMore: boolean; // 是否有更多数据
  isLoading: boolean; // 是否正在加载
}

export type NodePaginationStateProps = Record<string | number, NodePaginationStateType>;

export type TreeStoreState = {
  loadMoreText?: string;
  loadDataDefaultParams?: Record<string, unknown>;
  loading: boolean;
  // 首次无搜索条件的根节点加载成功后，允许 URL 定位；失败后刷新也可恢复。
  rootInitialized: boolean;
  // 搜索框值
  searchValue: string;
  // 树类型
  treeType: TTreeType;
  // uns 树搜索的类型
  searchType: TSearchType;
  // 树数据
  treeData: UnsTreeNode[];
  // 是否是懒加载树
  lazyTree: boolean;
  // 树的展开key
  expandedKeys: Key[];
  // 异步加载的key
  loadedKeys: Key[];
  // 节点的页码集合
  nodePaginationState: NodePaginationStateProps;
  // 正在加载的key
  loadingKeys: Set<Key>;
  // AbortController实例
  abortControllers: Map<Key, AbortController>;
  // 选中的节点
  selectedNode?: UnsTreeNode;
  // 面包屑数据
  breadcrumbList: UnsTreeNode[];
  // 方法合集 Todo: 完善ts
  operationFns: any;
  // 黏贴的节点
  pasteNode: UnsTreeNode | null;
  // 是否显示treeMap
  treeMap: boolean;
  currentTreeMapType: TCurrentTreeMapType;
  // 跳转 Todo: 完善ts
  scrollTreeNode?: any;
  // 树上行走高亮（agent 过程可视化，@tier0/uns-agent-ui 树上行走注入缝驱动）：agent 工具命中某
  // UNS 节点时临时高亮对应树节点。与 selectedNode 完全独立——只做视觉高亮，不改变选中态。
  walkHighlightKey?: Key;
};

export type TreeStoreActions = {
  setLoading: (value: TreeStoreState['loading']) => void;
  // 设置搜索值
  setSearchValue: (value: TreeStoreState['searchValue']) => void;
  // 设置树类型
  setTreeType: (value: TreeStoreState['treeType']) => void;
  // 设置树搜索类型
  setSearchType: (value: TreeStoreState['searchType']) => void;
  // 设置树数据
  setTreeData: (
    newTreeData:
      ((treeData: TreeStoreState['treeData']) => TreeStoreState['treeData'] | void) | TreeStoreState['treeData']
  ) => void;
  // 重置
  reset: (value: Partial<TreeStoreState>) => void;
  // 设置是否是懒加载树
  setLazyTree: (
    value: ((treeData: TreeStoreState['lazyTree']) => TreeStoreState['lazyTree'] | void) | TreeStoreState['lazyTree']
  ) => void;
  /**
   * 数据请求
   * */
  loadData: (
    options?: {
      key?: Key; // 既parentId
      page?: number; // 分页
      reset?: boolean; // 是否重置
      parentInfo?: UnsTreeNode; // 父级信息
      // search: 搜索 addFile 新增文件 addFolder 新增文件夹
      queryType?: string; // 请求类型，需要特殊处理
      newNodeKey?: Key; // 创建成功后的id
      clearSelect?: boolean;
      clearExpanded?: boolean; // 清空展开态
      clearLoadedKeys?: boolean; // 清空请求
      startLoading?: boolean; // 启动loading
      nodeDetail?: UnsTreeNode; // 当前操作节点信息
    },
    // cb 回调函数
    cb?: (
      data?: TreeStoreState['treeData'],
      selectData?: UnsTreeNode,
      opt?: { scrollTreeNode: TreeStoreState['scrollTreeNode'] }
    ) => void
  ) => void;
  recursiveLoadData: TreeStoreActions['loadData'];
  // 设置展开收起key
  setExpandedKeys: (newExpandedKeys: ((expandedKeys: Key[]) => Key[] | void) | Key[]) => void;
  // 设置异步加载的key
  setLoadedKeys: (newLoadedKeys: ((loadedKeys: Key[]) => Key[] | void) | Key[]) => void;
  // 设置节点懒加载的配置
  setNodePaginationState: (
    newNodePaginationState:
      ((nodePaginationState: NodePaginationStateProps) => void | NodePaginationStateProps) | NodePaginationStateProps
  ) => void;
  // loadingKeys相关操作
  addLoadingKey: (key: Key) => void;
  removeLoadingKey: (key: Key) => void;
  clearLoadingKeys: () => void;
  // AbortController相关操作
  setAbortController: (key: string, controller: AbortController) => void;
  removeAbortController: (key: string) => void;
  clearAbortControllers: () => void;
  abortRequest: (key: string) => void;
  abortAllRequests: () => void;
  // 设置根节点的方法
  handleRootNodeData: (
    data: any[],
    restResponse: { total: number; pageNo: number; pageSize: number },
    parentInfo?: UnsTreeNode
  ) => UnsTreeNode[];
  setSelectedNode: (node?: UnsTreeNode, quick?: boolean) => void;
  setBreadcrumbList: (node?: UnsTreeNode, quick?: boolean) => void;
  // 新增设置方法
  setOperationFns: (fns: any) => void;
  setPasteNode: (node: UnsTreeNode | null) => void;
  // 刷新
  onRefresh: (node: UnsTreeNode, expandedBySelf?: boolean) => void;
  setTreeMap: (treeMap: boolean) => void;
  setCurrentTreeMapType: (currentTreeMapType: TCurrentTreeMapType) => void;
  setScrollTreeNode: (scrollTreeNode: any) => void;
  // 请求失败后处理数据
  resetTreeData: () => void;
  // 展开\收起
  handleExpandNode: (expand: boolean, node: UnsTreeNode) => void;
  // 设置树上行走高亮节点（与 selectedNode 完全独立）
  setWalkHighlightKey: (value?: Key) => void;
};

export type TreeStoreProps = TreeStoreState & TreeStoreActions;
