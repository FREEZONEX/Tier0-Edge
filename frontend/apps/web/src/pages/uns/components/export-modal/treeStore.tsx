import { createContext, type Key, type ReactNode, useContext, useState } from 'react';
import { createStore } from 'zustand/vanilla';
import type { UnsTreeNode } from '../../types';
import { enableMapSet } from 'immer';
import { immer } from 'zustand/middleware/immer';
import useTranslate from '@/hooks/useTranslate.ts';
import { useStoreWithEqualityFn } from 'zustand/traditional';
import { shallow } from 'zustand/vanilla/shallow';
import { getTreeData, getUnsLazyTree } from '@/apis/core-api/uns';
import {
  appendTreeData,
  createLoadMoreNode,
  formatNodeData,
  handlerTreeData,
  hasMoreData,
} from '@/pages/uns/store/utils.ts';
import { CustomAxiosConfigEnum } from '@/utils/request';

enableMapSet(); // immer需要开启支持map和set, 必须在创建 store 前调用

export interface NodePaginationStateType {
  currentPage: number; // 当前页码
  hasMore: boolean; // 是否有更多数据
  isLoading: boolean; // 是否正在加载
}

export type NodePaginationStateProps = Record<string | number, NodePaginationStateType>;

export type TreeStoreState = {
  loadMoreText?: string;
  loading: boolean;
  // 搜索框值
  searchValue: string;
  // 树数据
  treeData: UnsTreeNode[];
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
  checkedKeys: Key[];
  scrollTreeNode?: any;
  allChecked: boolean;
  jsonData?: any;
  lazyTree: boolean;
  smallFile: boolean;
  filePath?: string;
  params?: any;
};

export type TreeStoreActions = {
  loadData: (
    options?: {
      key?: Key; // 既parentId
      page?: number; // 分页
      reset?: boolean; // 是否重置
      parentInfo?: UnsTreeNode; // 父级信息
      clearSelect?: boolean;
      clearExpanded?: boolean; // 清空展开态
      clearLoadedKeys?: boolean; // 清空请求
      startLoading?: boolean; // 启动loading
    },
    // cb 回调函数
    cb?: (data?: TreeStoreState['treeData'], selectData?: UnsTreeNode) => void
  ) => void;
  // 设置树数据
  setTreeData: (
    newTreeData:
      ((treeData: TreeStoreState['treeData']) => TreeStoreState['treeData'] | void) | TreeStoreState['treeData']
  ) => void;
  setCheckedKeys: (newCheckedKeys: ((checkedKeys: Key[]) => Key[] | void) | Key[]) => void;
  setLoading: (value: TreeStoreState['loading']) => void;
  setJsonData: (value: TreeStoreState['jsonData']) => void;
  setParams: (value: TreeStoreState['params']) => void;
  setFilePath: (value: TreeStoreState['filePath']) => void;
  setSmallFile: (value: TreeStoreState['smallFile']) => void;
  // 设置异步加载的key
  setLoadedKeys: (newLoadedKeys: ((loadedKeys: Key[]) => Key[] | void) | Key[]) => void;
  // loadingKeys相关操作
  addLoadingKey: (key: Key) => void;
  removeLoadingKey: (key: Key) => void;
  clearLoadingKeys: () => void;
  // 设置节点懒加载的配置
  setNodePaginationState: (
    newNodePaginationState:
      ((nodePaginationState: NodePaginationStateProps) => void | NodePaginationStateProps) | NodePaginationStateProps
  ) => void;
  // 设置展开收起key
  setExpandedKeys: (newExpandedKeys: ((expandedKeys: Key[]) => Key[] | void) | Key[]) => void;
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
  // 设置是否是懒加载树
  setLazyTree: (value: TreeStoreState['lazyTree']) => void;
  setScrollTreeNode: (scrollTreeNode: any) => void;
  setAllChecked: (newAllChecked: ((allChecked: boolean) => boolean | void) | boolean) => void;
};
export type TreeStoreProps = TreeStoreState & TreeStoreActions;

const initialState = {
  loading: false,
  allChecked: false,
  searchValue: '',
  treeData: [],
  expandedKeys: [],
  loadedKeys: [],
  nodePaginationState: {},
  loadingKeys: new Set<Key>(),
  // 初始化为空Map
  abortControllers: new Map<Key, AbortController>(),
  selectedNode: undefined,
  checkedKeys: [],
  lazyTree: false,
  smallFile: true,
};

// 根节点的特殊ID
export const ROOT_NODE_ID = '0';
// 页码
export const PAGE_SIZE = 100;

const createTreeStore = (initProps?: Partial<TreeStoreProps>) => {
  return createStore<TreeStoreProps>()(
    immer((set, get) => ({
      ...initialState,
      ...initProps,
      loadData: async (options) => {
        const {
          key = ROOT_NODE_ID,
          page = 1,
          reset = false,
          parentInfo = { key: ROOT_NODE_ID, path: '' },
        } = options || {};
        const {
          setTreeData,
          setLoading,
          loadingKeys,
          addLoadingKey,
          setNodePaginationState,
          setAbortController,
          handleRootNodeData,
          loadMoreText,
          removeLoadingKey,
          lazyTree,
          setLoadedKeys,
          abortAllRequests,
          setExpandedKeys,
          scrollTreeNode,
        } = get();

        if (reset) {
          // 重置异步加载key
          setLoadedKeys([]);
          // 取消所有请求
          abortAllRequests();
          // 重置请求页
          setNodePaginationState({});
          // 重置展开key
          setExpandedKeys([]);
          // 设置为true
          setLoading(true);
        }

        if (lazyTree) {
          if (!reset && loadingKeys.has(key)) {
            console.log(`节点正在请求：${key};  page: ${page}`);
            return;
          }
          addLoadingKey(key);
          setNodePaginationState((pre) => {
            pre[key as string] = { ...pre[key as string], isLoading: true };
          });
          // 创建AbortController用于取消请求
          const controller = new AbortController();
          const keyStr = key.toString();
          setAbortController(keyStr, controller);
          try {
            const pageSize = PAGE_SIZE;
            const parentId = keyStr;
            const params = {
              parentId,
              pageNo: page,
              pageSize: pageSize,
            };
            const { data, ...restResponse } = await getUnsLazyTree(params, {
              signal: controller.signal,
              [CustomAxiosConfigEnum.BusinessResponse]: true,
            });

            if (key === ROOT_NODE_ID) {
              if (page === 1) {
                // 如果是第一页，直接设置树数据
                const rootNodes = handleRootNodeData(data, restResponse, parentInfo);
                setTreeData(rootNodes);
                if (reset) {
                  scrollTreeNode?.(rootNodes?.[0]?.id);
                }
              } else {
                let hasMore = false;
                hasMore = hasMoreData(restResponse);
                // 追加到现有树数据
                setTreeData((pre) => {
                  // 过滤掉之前的加载更多节点
                  const filteredData = pre.filter((node) => !node.isLoadMoreNode);
                  const newNodes = formatNodeData(
                    data,
                    parentInfo?.path ?? '',
                    filteredData?.[filteredData.length - 1]?.id
                  );
                  return hasMore
                    ? [...filteredData, ...newNodes, createLoadMoreNode(ROOT_NODE_ID, page, parentInfo, loadMoreText)]
                    : [...filteredData, ...newNodes];
                });
                setNodePaginationState((pre) => {
                  pre[key as string] = { currentPage: page, hasMore, isLoading: false };
                });
                if (reset) {
                  scrollTreeNode?.(get().treeData?.[0]?.id);
                }
              }
              return;
            }

            // 子节点的处理逻辑
            let hasMore = false;
            if (data && Array.isArray(data)) {
              const newChildren = formatNodeData(data, parentInfo?.path ?? '');
              // 判断是否有更多数据
              hasMore = hasMoreData(restResponse);
              setTreeData((pre) => {
                appendTreeData(
                  pre,
                  key,
                  hasMore
                    ? [...newChildren, createLoadMoreNode(key as string, page, parentInfo, loadMoreText)]
                    : newChildren
                );
              });
              setNodePaginationState((pre) => {
                pre[key as string] = { currentPage: page, hasMore, isLoading: false };
              });
              if (reset) {
                scrollTreeNode?.(data?.[0]?.id);
              }
            }
          } catch (e) {
            console.log(e);
            setNodePaginationState((pre) => {
              pre[key as string] = { ...pre[key as string], isLoading: false };
            });
          } finally {
            removeLoadingKey(key);
            setLoading(false);
          }
        } else {
          setLoading(true);
          getTreeData()
            .then((res) => {
              if (res?.length) {
                setTreeData(handlerTreeData(res));
              } else {
                setTreeData([]);
              }
            })
            .finally(() => {
              setLoading(false);
            });
        }
      },
      setScrollTreeNode: (scrollTreeNode) => set({ scrollTreeNode }),
      setLoading: (loading) => set({ loading }),
      setParams: (params) => set({ params }),
      setJsonData: (jsonData) => set({ jsonData }),
      setFilePath: (filePath) => set({ filePath }),
      setSmallFile: (smallFile) => set({ smallFile }),
      setCheckedKeys: (next) =>
        set((state) => {
          if (typeof next === 'function') {
            const newValue = next(state.checkedKeys);
            if (newValue !== undefined) {
              state.checkedKeys = newValue;
            }
          } else {
            state.checkedKeys = next;
          }
        }),
      setLoadedKeys: (next) =>
        set((state) => {
          if (typeof next === 'function') {
            const newValue = next(state.loadedKeys);
            if (newValue !== undefined) {
              state.loadedKeys = newValue;
            }
          } else {
            state.loadedKeys = next;
          }
        }),
      setAllChecked: (next) =>
        set((state) => {
          if (typeof next === 'function') {
            const newValue = next(state.allChecked);
            if (newValue !== undefined) {
              state.allChecked = newValue;
            }
          } else {
            state.allChecked = next;
          }
        }),
      // === loadingKeys相关操作
      addLoadingKey: (key) =>
        set((state) => {
          const newLoadingKeys = new Set(state.loadingKeys);
          newLoadingKeys.add(key);
          return { loadingKeys: newLoadingKeys };
        }),
      removeLoadingKey: (key) =>
        set((state) => {
          const newLoadingKeys = new Set(state.loadingKeys);
          newLoadingKeys.delete(key);
          return { loadingKeys: newLoadingKeys };
        }),
      clearLoadingKeys: () => set({ loadingKeys: new Set<Key>() }),
      setNodePaginationState: (next) =>
        set((state) => {
          if (typeof next === 'function') {
            const newValue = next(state.nodePaginationState);
            if (newValue !== undefined) {
              state.nodePaginationState = newValue;
            }
          } else {
            state.nodePaginationState = next;
          }
        }),
      setExpandedKeys: (next) =>
        set((state) => {
          if (typeof next === 'function') {
            const newValue = next(state.expandedKeys);
            if (newValue !== undefined) {
              state.expandedKeys = newValue;
            }
          } else {
            state.expandedKeys = next;
          }
        }),
      // === 实现AbortController相关方法
      setAbortController: (key, controller) =>
        set((state) => {
          const newAbortControllers = new Map(state.abortControllers);
          newAbortControllers.set(key, controller);
          return { abortControllers: newAbortControllers };
        }),
      removeAbortController: (key) =>
        set((state) => {
          const newAbortControllers = new Map(state.abortControllers);
          newAbortControllers.delete(key);
          return { abortControllers: newAbortControllers };
        }),
      clearAbortControllers: () => set({ abortControllers: new Map<string, AbortController>() }),
      abortRequest: (key) => {
        const { abortControllers } = get();
        if (abortControllers.has(key)) {
          abortControllers.get(key)?.abort();
          get().removeAbortController(key);
        }
      },
      abortAllRequests: () => {
        const { abortControllers } = get();
        abortControllers.forEach((controller) => {
          controller.abort();
        });
        set({ abortControllers: new Map<string, AbortController>() });
      },
      handleRootNodeData: (data, restResponse, parentInfo) => {
        const { setNodePaginationState, loadMoreText } = get();
        if (data && Array.isArray(data)) {
          const rootNodes = formatNodeData(data, parentInfo?.path ?? '');

          // 判断是否有更多数据
          const hasMore = hasMoreData(restResponse);

          // 设置根节点分页状态
          setNodePaginationState((pre) => {
            pre[ROOT_NODE_ID] = { currentPage: restResponse.pageNo, hasMore, isLoading: false };
          });

          // 如果有更多数据，添加"加载更多"节点
          if (hasMore) {
            return [...rootNodes, createLoadMoreNode(ROOT_NODE_ID, 1, parentInfo, loadMoreText)];
          }

          return rootNodes;
        }
        return [];
      },
      setLazyTree: (lazyTree) => set({ lazyTree: lazyTree }),
      setTreeData: (newTreeData) =>
        set((state) => {
          if (typeof newTreeData === 'function') {
            const newValue = newTreeData(state.treeData);
            if (newValue !== undefined) {
              state.treeData = newValue;
            }
          } else {
            state.treeData = newTreeData;
          }
        }),
    }))
  );
};

const TreeStoreContext = createContext<ReturnType<typeof createTreeStore> | null>(null);

export function TreeStoreProvider({ children }: { children: ReactNode }) {
  const formatMessage = useTranslate();
  const [TreeStoreProps] = useState(() => createTreeStore({ loadMoreText: formatMessage('common.loadMore') }));

  return <TreeStoreContext.Provider value={TreeStoreProps}>{children}</TreeStoreContext.Provider>;
}

export function useTreeStore<U>(selector: (state: TreeStoreProps) => U) {
  const store = useContext(TreeStoreContext);

  if (store === null) {
    throw new Error('useTreeStore must be used within TreeStoreProvider');
  }

  return useStoreWithEqualityFn(store, selector, shallow);
}
