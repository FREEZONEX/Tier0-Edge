import { createContext, type ReactNode, useContext, useState, type Key } from 'react';
import { createStore } from 'zustand/vanilla';
import { useStoreWithEqualityFn } from 'zustand/traditional';
import { shallow } from 'zustand/shallow';
import { getTreeData, getUnsLazyTree } from '@/apis/core-api/uns';
import { immer } from 'zustand/middleware/immer';
import { persist, subscribeWithSelector } from 'zustand/middleware';
import { enableMapSet } from 'immer';
import {
  appendTreeData,
  createLoadMoreNode,
  findNodeInfoById,
  formatNodeData,
  getDescendantKeys,
  getParentNodes,
  handlerTreeData,
  hasMoreData,
  setTreeDescendantChildren,
  uniqueArr,
} from './utils';
import type { TreeStoreProps, TreeStoreState } from './types.ts';
import { useTranslate } from '@/hooks';
import { APP_UNS_TREE } from '@/common-types/constans.ts';
import type { UnsTreeNode } from '@/pages/uns/types';
import { CustomAxiosConfigEnum } from '@/utils/request.ts';
import { collectChildrenIds, findParentIds } from '@/utils/uns.ts';
enableMapSet(); // immer需要开启支持map和set, 必须在创建 store 前调用

const initialState: TreeStoreState = {
  loading: false,
  rootInitialized: false,
  searchValue: '',
  treeType: 'uns',
  searchType: 1,
  treeData: [],
  lazyTree: true,
  expandedKeys: [],
  loadedKeys: [],
  nodePaginationState: {},
  loadingKeys: new Set<Key>(),
  // 初始化为空Map
  abortControllers: new Map<Key, AbortController>(),
  selectedNode: undefined,
  breadcrumbList: [],
  operationFns: {},
  pasteNode: null,
  treeMap: false,
  currentTreeMapType: 'all',
  walkHighlightKey: undefined,
};

// 根节点的特殊ID
export const ROOT_NODE_ID = '0';
// 页码
export const PAGE_SIZE = 100;

export const createTreeStore = (initProps?: Partial<TreeStoreProps>) => {
  return createStore<TreeStoreProps>()(
    subscribeWithSelector(
      persist(
        immer((set, get) => ({
          ...initialState,
          ...initProps,
          setLoading: (loading) => set({ loading }),
          setSearchValue: (searchValue) => set({ searchValue }),
          setTreeType: (treeType) => set({ treeType }),
          setSearchType: (searchType) => set({ searchType }),
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
          reset: (value) => set({ ...initialState, ...value }),
          setLazyTree: (next) =>
            set((state) => {
              if (typeof next === 'function') {
                const newValue = next(state.lazyTree);
                if (newValue !== undefined) {
                  state.lazyTree = newValue;
                }
              } else {
                state.lazyTree = next;
              }
            }),
          recursiveLoadData: async (options, cb) => {
            const { key = '', newNodeKey, queryType, nodeDetail } = options || {};
            const {
              onRefresh,
              searchValue,
              searchType,
              setTreeData,
              setLoading,
              setNodePaginationState,
              scrollTreeNode,
              loadMoreText,
              setExpandedKeys,
              loadDataDefaultParams,
            } = get();
            const _newNodeKey = newNodeKey as string;
            setLoading(true);
            onRefresh({ key, id: key } as any, false);
            let childrenData: UnsTreeNode[] = [];
            const keyStr = key.toString();
            const parentId = searchValue && keyStr === ROOT_NODE_ID ? undefined : keyStr;
            const parentInfo = findNodeInfoById(get().treeData, parentId as string);
            // 修改为返回Promise的函数
            async function fn(page: number): Promise<boolean> {
              const params = {
                ...loadDataDefaultParams,
                parentId,
                pageNo: page,
                pageSize: PAGE_SIZE,
                keyword: searchValue,
                searchType,
              };

              try {
                const { data, ...restResponse } = await getUnsLazyTree(params, {
                  [CustomAxiosConfigEnum.BusinessResponse]: true,
                });

                // 处理返回的数据
                const childNodes = formatNodeData(
                  data,
                  parentInfo?.path ?? '',
                  childrenData?.[childrenData.length - 1]?.id
                );

                childrenData = uniqueArr([...childrenData, ...childNodes]);

                // 检查是否找到目标节点
                if (!data?.some((item: any) => item.id === _newNodeKey)) {
                  // 如果没有找到目标节点且有更多页，继续请求下一页
                  if (hasMoreData(restResponse)) {
                    // 使用await等待递归调用完成
                    return await fn(restResponse?.pageNo + 1);
                  }
                  // 如果没有更多页，返回false表示未找到
                  return false;
                } else {
                  // 找到目标节点，添加hasMore节点（如果需要）
                  const hasMore = hasMoreData(restResponse);
                  if (hasMore) {
                    childrenData = [
                      ...childrenData,
                      createLoadMoreNode(parentId as string, page, parentInfo, loadMoreText),
                    ];
                    setNodePaginationState((pre) => {
                      pre[parentId as string] = { currentPage: restResponse?.pageNo, hasMore, isLoading: false };
                    });
                  }
                  // 返回true表示找到了目标节点
                  return true;
                }
              } catch (error) {
                console.error('递归加载数据出错:', error);
                return false;
              }
            }

            // 等待异步操作完成后再处理结果
            try {
              const found = await fn(1);
              console.log('递归加载完成，找到节点:', found);
              console.log('加载的数据:', childrenData, parentId);

              if (!parentId || keyStr === ROOT_NODE_ID) {
                setTreeData(childrenData);
              } else {
                setTreeData((pre) => {
                  appendTreeData(pre, parentId as string, childrenData, queryType, nodeDetail, () => {
                    if (key) {
                      setTimeout(() => {
                        setExpandedKeys((draft) => {
                          const index = draft.findIndex((id) => id === key);
                          if (index !== -1) draft.splice(index, 1);
                        });
                      }, 0);
                    }
                  });
                });
              }
              setTimeout(() => {
                scrollTreeNode(_newNodeKey);
              }, 0);
              // 调用回调函数
              cb?.(
                childrenData,
                childrenData?.find((f) => f.id === _newNodeKey)
              );
            } catch (error) {
              console.error('递归加载过程出错:', error);
            } finally {
              setLoading(false);
            }
          },
          loadData: async (options, cb) => {
            const {
              key = ROOT_NODE_ID,
              page = 1,
              reset = false,
              parentInfo = { key: ROOT_NODE_ID, path: '' },
              queryType,
              clearSelect,
              clearExpanded,
              clearLoadedKeys,
              startLoading,
            } = options || {};
            const {
              lazyTree,
              loadingKeys,
              addLoadingKey,
              setNodePaginationState,
              setAbortController,
              setTreeData,
              setLoadedKeys,
              removeLoadingKey,
              handleRootNodeData,
              treeType,
              setExpandedKeys,
              setLoading,
              searchType,
              searchValue,
              abortAllRequests,
              scrollTreeNode,
              recursiveLoadData,
              loadMoreText,
              setSelectedNode,
              resetTreeData,
              loadDataDefaultParams,
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
            if (clearSelect) {
              // 重置选中
              setSelectedNode();
            }
            if (clearExpanded) {
              // 重置展开key
              setExpandedKeys([]);
            }
            if (startLoading) {
              // 设置为true
              setLoading(true);
            }
            if (clearLoadedKeys) {
              // 重置展开key
              // 重置异步加载key
              setLoadedKeys([]);
              // 取消所有请求
              abortAllRequests();
              // 重置请求页
              setNodePaginationState({});
            }
            switch (treeType) {
              case 'uns':
                {
                  if (lazyTree) {
                    if (queryType === 'editFolderName' || queryType === 'editFileName') {
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
                    if (queryType === 'addFile' || queryType === 'addFolder') {
                      recursiveLoadData(options, cb);
                      // 新增操作特殊处理，要递归请求新增成功后的父级分页
                      return;
                    }
                    if (queryType === 'deleteFile' || queryType === 'deleteFolder') {
                      recursiveLoadData(options, cb);
                      // 删除操作特殊处理，要递归请求删除成功后的父级分页
                      return;
                    }

                    // reset必须是false，不然enter搜索会取消请求
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
                      if (queryType === 'search' && searchValue && keyStr === ROOT_NODE_ID) {
                        const tree = await getTreeData({
                          ...loadDataDefaultParams,
                          type: searchType,
                          key: searchValue,
                          keyword: searchValue,
                        });
                        if (tree?.length) {
                          const nextTree = handlerTreeData(tree);
                          setTreeData(nextTree);
                          setExpandedKeys(
                            searchValue.includes('/')
                              ? collectChildrenIds(nextTree as any, '')
                              : findParentIds(searchValue, nextTree as any)
                          );
                          scrollTreeNode?.(nextTree?.[0]?.id);
                          cb?.(nextTree);
                        } else {
                          resetTreeData();
                        }
                        return;
                      }
                      const pageSize = PAGE_SIZE;
                      const parentId = searchValue && keyStr === ROOT_NODE_ID ? undefined : keyStr;
                      const params = {
                        ...loadDataDefaultParams,
                        parentId,
                        pageNo: page,
                        pageSize: pageSize,
                        keyword: searchValue,
                        searchType,
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
                          if (!searchValue) set({ rootInitialized: true });
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
                              ? [
                                  ...filteredData,
                                  ...newNodes,
                                  createLoadMoreNode(ROOT_NODE_ID, page, parentInfo, loadMoreText),
                                ]
                              : [...filteredData, ...newNodes];
                          });
                          setNodePaginationState((pre) => {
                            pre[key as string] = { currentPage: page, hasMore, isLoading: false };
                          });
                          if (reset) {
                            scrollTreeNode?.(get().treeData?.[0]?.id);
                          }
                        }
                        cb?.(get().treeData);
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
                      cb?.();
                    } catch (error: any) {
                      // Axios取消请求时会设置error.name为'CanceledError'或message为'canceled'
                      if (
                        error?.name === 'AbortError' ||
                        error?.name === 'CanceledError' ||
                        error?.message === 'canceled'
                      ) {
                        console.log(`取消请求 ${key}`);
                      } else {
                        console.error(`获取节点数据失败 ${key}:`, error);
                      }
                      setNodePaginationState((pre) => {
                        pre[key as string] = { ...pre[key as string], isLoading: false };
                      });
                    } finally {
                      removeLoadingKey(key);
                      setLoading(false);
                    }
                  } else {
                    setLoading(true);
                    getTreeData({
                      ...loadDataDefaultParams,
                      type: searchType,
                      key: searchValue,
                    })
                      .then((res: any) => {
                        let tree: any[] = [];
                        if (res?.length) {
                          tree = handlerTreeData(res);
                          setTreeData(handlerTreeData(res));
                          if (reset) {
                            scrollTreeNode?.(res?.[0]?.id);
                          }
                        } else {
                          resetTreeData();
                        }
                        if (!searchValue) set({ rootInitialized: true });
                        cb?.(tree);
                      })
                      .catch((err) => {
                        console.log(err);
                        resetTreeData();
                      })
                      .finally(() => {
                        if (queryType === 'search' && searchValue && treeType === 'uns') {
                          // 搜索要打开
                          if (searchValue.includes('/')) {
                            setExpandedKeys(collectChildrenIds(get().treeData as any, ''));
                          } else {
                            setExpandedKeys(findParentIds(searchValue, get().treeData as any));
                          }
                        }
                        setLoading(false);
                      });
                  }
                }
                break;
              default:
                break;
            }
          },
          resetTreeData: () => {
            const { setTreeData, setSelectedNode } = get();
            setTreeData([]);
            setSelectedNode();
          },
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
          setSelectedNode: (selectedNode, quick = false) => {
            get().setBreadcrumbList(selectedNode, quick);
            return set({ selectedNode });
          },
          setBreadcrumbList: (selectedNode, quick = false) => {
            const { treeData, treeType, setExpandedKeys } = get();
            const breadcrumbList = quick
              ? ([selectedNode] as UnsTreeNode[])
              : selectedNode && treeType === 'uns'
                ? getParentNodes(treeData, selectedNode.key)
                : [];
            // 设置自动展开所有父级
            if (selectedNode && treeType === 'uns') {
              setExpandedKeys((pre) => {
                breadcrumbList.slice(0, -1)?.forEach((f) => {
                  if (!pre.includes(f.id as string)) {
                    pre.push(f.id as string);
                  }
                });
              });
            }
            // 设置面包屑
            set({
              breadcrumbList,
            });
          },
          setOperationFns: (fns) => set({ operationFns: { ...get().operationFns, ...fns } }),
          setPasteNode: (pasteNode) => set({ pasteNode }),
          setTreeMap: (treeMap) => set({ treeMap }),
          onRefresh: (node, expandedBySelf = true) => {
            const { abortRequest, setLoadedKeys, setTreeData, setNodePaginationState, setExpandedKeys } = get();
            const nodeKey = node.key as string;
            // 获取所有后代 key
            const descendantKeys = getDescendantKeys(get().treeData, nodeKey);
            // 取消所有后代节点的请求
            descendantKeys.forEach((descendantKey: any) => {
              const descendantKeyStr = descendantKey.toString();
              abortRequest(descendantKeyStr);
            });
            // 从 loadedKeys 中移除自身及所有后代，强制重新加载
            if (expandedBySelf) {
              setLoadedKeys((prev) => prev.filter((k) => k !== nodeKey && !descendantKeys.includes(k)));
            } else {
              setLoadedKeys((prev) => {
                if (!prev.includes(nodeKey)) {
                  prev.push(nodeKey);
                }
                return prev.filter((k) => !descendantKeys.includes(k));
              });
            }
            // 清空所有children
            setTreeData((pre) => {
              setTreeDescendantChildren(pre, nodeKey);
            });
            // 重置该节点及所有后代的分页状态
            setNodePaginationState((prev) => {
              delete prev[nodeKey];
              descendantKeys.forEach((k: any) => delete prev[k]);
            });
            // 如果节点当前是展开的，先折叠（移除自身及其所有后代），再立即展开以触发重新加载
            if (get().expandedKeys.includes(nodeKey)) {
              if (expandedBySelf) {
                setExpandedKeys((expandedKeys) => {
                  return expandedKeys.filter((k) => k !== nodeKey && !descendantKeys.includes(k));
                });
                setTimeout(() => {
                  setExpandedKeys((expandedKeys) => {
                    expandedKeys.push(nodeKey);
                  });
                }, 0);
              } else {
                setExpandedKeys((expandedKeys) => {
                  return expandedKeys.filter((k) => !descendantKeys.includes(k));
                });
              }
            } else {
              setExpandedKeys((expandedKeys) => {
                return expandedKeys.filter((k) => !descendantKeys.includes(k));
              });
            }
            // 如果节点未展开，则无需操作 expandedKeys，下次用户展开时会自动加载
          },
          setCurrentTreeMapType: (currentTreeMapType) => set({ currentTreeMapType }),
          setScrollTreeNode: (scrollTreeNode) => set({ scrollTreeNode }),
          handleExpandNode: (expand, node) => {
            const { treeData, setExpandedKeys } = get();
            if (node.id === undefined) return;
            const ids: Key[] = [node.id, ...(collectChildrenIds(treeData as any, node.id as string) ?? [])];
            setExpandedKeys((pre) => {
              return expand ? [...new Set([...pre, ...ids])] : pre.filter((id: any) => !ids.includes(id));
            });
          },
          setWalkHighlightKey: (walkHighlightKey) => set({ walkHighlightKey }),
        })),
        {
          name: APP_UNS_TREE,
          partialize: (state) => ({
            lazyTree: state.lazyTree,
          }),
        }
      )
    )
  );
};

const TreeStoreContext = createContext<ReturnType<typeof createTreeStore> | null>(null);

export function TreeStoreProvider({
  children,
  initProps,
}: {
  children: ReactNode;
  initProps?: Partial<TreeStoreProps>;
}) {
  const formatMessage = useTranslate();
  const [TreeStoreProps] = useState(() =>
    createTreeStore({
      loadMoreText: formatMessage('common.loadMore'),
      ...initProps,
    })
  );

  return <TreeStoreContext.Provider value={TreeStoreProps}>{children}</TreeStoreContext.Provider>;
}

export function useTreeStore<U>(selector: (state: TreeStoreProps) => U) {
  const store = useContext(TreeStoreContext);

  if (store === null) {
    throw new Error('useTreeStore must be used within TreeStoreProvider');
  }

  return useStoreWithEqualityFn(store, selector, shallow);
}

/**
 * 获取TreeStore实例的引用，可以通过getState()获取最新状态而不触发组件重新渲染
 * 类似于useRef的功能，但是针对store状态
 * @returns TreeStore实例
 */
export function useTreeStoreRef() {
  const store = useContext(TreeStoreContext);

  if (store === null) {
    throw new Error('useTreeStoreState must be used within TreeStoreProvider');
  }

  // 使用useStoreWithEqualityFn来订阅状态更新
  return store;
}

/**
 * 获取TreeStore的最新状态，不会订阅状态更新，不会触发组件重新渲染
 * 用于在事件处理函数等场景中获取最新状态
 * @param selector 可选的选择器函数，用于选择特定的状态值
 * @returns 如果提供了选择器，则返回选择器选择的状态值；否则返回整个状态对象
 * getTreeStoreSnapshot(treeStoreRef, (state) => state.treeType)
 * getTreeStoreSnapshot(treeStoreRef).treeType
 */
export function getTreeStoreSnapshot<T = TreeStoreProps>(
  store: ReturnType<typeof useTreeStoreRef>,
  selector?: (state: TreeStoreProps) => T
): T | TreeStoreProps {
  const state = store.getState() as TreeStoreProps;
  return selector ? selector(state) : state;
}
