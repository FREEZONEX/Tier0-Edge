import { pasteUns } from '@/apis/core-api/uns';
import { getDataQualityActiveSummary } from '@/apis/core-api/data-quality';
import { ButtonPermission } from '@/common-types/button-permission';
import ComButton from '@/components/com-button';
import cx from 'classnames';
import { DocumentAdd, FolderAdd, Renew, RepoArtifact, SquarePlus, TrashCan } from '@/components/lucide-icon/carbon';
import { toolbarIconProps } from '@/components/lucide-icon/icon-props';
import ProSearch from '@/components/pro-search';
import ProTree, { type ProTreeProps } from '@/components/pro-tree';
import TagAdd from '@/components/svg-components/TagAdd';
import { usePageIsShow } from '@/contexts/tabs-lifecycle-context.ts';
import useTranslate from '@/hooks/useTranslate';
import ReverseModal from '@/pages/uns/components/reverse-modal';
import type { UnsTreeNode } from '@/pages/uns/types';
import { useBaseStore } from '@/stores/base';
import { useI18nStore } from '@/stores/i18n-store.ts';
import { getTargetNode } from '@/utils';
import { filterPermissionToList, hasPermission } from '@/utils/auth';
import Icon from '@ant-design/icons';
import { App, Button, Divider, Dropdown, Flex } from 'antd';
import type { ItemType } from 'antd/es/menu/interface';
import { debounce } from 'lodash-es';
import { type CSSProperties, type Key, memo, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ROOT_NODE_ID, useTreeStore } from '../../store/treeStore';
import type { TTreeType } from '../../store/types';
import {
  getFolderTopicType,
  getNodeTopicType,
  getUnsTopicFolderTextColor,
  getUnsTopicFolderTitleStyle,
  isTopicTypeFolder,
  UnsTreeNodeIcon,
} from './tree-icons';
import { buildQualityTreeStatus, qualityStatusForNode } from './data-quality-tree';
import './index.scss';

const renderOperationDom = (type: string) => {
  switch (type) {
    case 'DocumentAdd':
      return <DocumentAdd {...toolbarIconProps} />;
    case 'FolderAdd':
      return <FolderAdd {...toolbarIconProps} />;
    case 'RepoArtifact':
      return <RepoArtifact {...toolbarIconProps} />;
    case 'TagAdd':
      return <Icon component={TagAdd} />;
    case 'Renew':
      return <Renew {...toolbarIconProps} />;
    default:
      return null;
  }
};

// 操作
interface UnsTreeBehaviorProps {
  // Fallback to current data root when no node is selected.
  enableRootAsParent?: boolean;
  // Explicit fallback parent for create actions.
  defaultCreateParentNode?: UnsTreeNode;
  // Custom resolver for current data root node.
  resolveCurrentDataRootNode?: (treeData: UnsTreeNode[]) => UnsTreeNode | undefined;
  // Disable delete action for current data root node.
  disableDeleteRoot?: boolean;
  // Extra non-deletable node ids.
  nonDeletableNodeIds?: Key[];
}

export const getDefaultDataRootNode = (treeData: UnsTreeNode[]) => {
  return treeData.find((node) => node?.pathType === 0) || treeData[0];
};

export const normalizeNodeId = (id?: Key | null) => {
  return id === null || id === undefined ? '' : String(id);
};

export const hasMountedTopicInSubtree = (node?: UnsTreeNode): boolean => {
  if (!node) {
    return false;
  }
  if (node.mount) {
    return true;
  }
  return (node.children || []).some((child) => hasMountedTopicInSubtree(child));
};

const decorateMountedNamespaceTree = (nodes: UnsTreeNode[]) => {
  const decorate = (items: UnsTreeNode[], depth = 0): UnsTreeNode[] =>
    items.map((node) => {
      const isMountedTopic = Boolean(node.mount && node.pathType === 2);
      const decoratedNode: UnsTreeNode = {
        ...node,
        className: isMountedTopic
          ? cx(node.className, 'uns-mount-node', 'uns-mount-group-start', 'uns-mount-group-end')
          : node.className,
        style: isMountedTopic
          ? ({
              ...node.style,
              '--uns-mount-offset': `${depth * 24}px`,
            } as CSSProperties)
          : node.style,
      };
      decoratedNode.children = decorate(node.children || [], depth + 1);
      return decoratedNode;
    });

  return decorate(nodes);
};

export const resolveCreateFallbackNode = ({
  treeData,
  enableRootAsParent,
  defaultCreateParentNode,
  resolveCurrentDataRootNode,
}: {
  treeData: UnsTreeNode[];
  enableRootAsParent?: boolean;
  defaultCreateParentNode?: UnsTreeNode;
  resolveCurrentDataRootNode?: (treeData: UnsTreeNode[]) => UnsTreeNode | undefined;
}) => {
  if (defaultCreateParentNode) {
    return defaultCreateParentNode;
  }
  if (!enableRootAsParent) {
    return undefined;
  }
  return resolveCurrentDataRootNode?.(treeData) || getDefaultDataRootNode(treeData);
};

const Operation = ({
  enableRootAsParent,
  defaultCreateParentNode,
  resolveCurrentDataRootNode,
}: Pick<UnsTreeBehaviorProps, 'enableRootAsParent' | 'defaultCreateParentNode' | 'resolveCurrentDataRootNode'>) => {
  const formatMessage = useTranslate();
  const { treeType, operationFns, selectedNode, loadData, treeData } = useTreeStore((state) => ({
    treeType: state.treeType,
    operationFns: state.operationFns,
    selectedNode: state.selectedNode,
    loadData: state.loadData,
    treeData: state.treeData,
  }));
  const {
    systemInfo: { enableAutoCategorization },
  } = useBaseStore((state) => ({
    systemInfo: state.systemInfo,
  }));
  const { lang } = useI18nStore((state) => ({
    lang: state.lang,
  }));
  const { message } = App.useApp();
  const [reverserOpen, setReverserOpen] = useState<boolean>(false); //复制的topic

  const createTargetNode =
    selectedNode ||
    resolveCreateFallbackNode({
      treeData,
      enableRootAsParent,
      defaultCreateParentNode,
      resolveCurrentDataRootNode,
    });

  const hasTopicType = enableAutoCategorization && createTargetNode && getNodeTopicType(createTargetNode) > 0;
  const isMountedTopic = Boolean(createTargetNode?.mount && createTargetNode.pathType === 2);

  const options = useMemo(() => {
    return filterPermissionToList<{
      onClick: () => void;
      buttonType: string;
      showTreeType?: TTreeType;
      key: string;
      title?: string;
      id?: string;
      disabled?: boolean;
      // Dropdown配置
      items?: any[];
      auth?: string[] | string;
    }>([
      {
        title: formatMessage('UserManagement.add'),
        onClick: () => {
          operationFns?.setOptionOpen?.('addFile', createTargetNode);
        },
        buttonType: 'Dropdown',
        showTreeType: 'uns',
        key: 'unsAdd',
        items: [
          {
            label: formatMessage('uns.newFolder'),
            auth: ButtonPermission['uns.folderAdd'],
            onClick: () => {
              operationFns?.setOptionOpen?.('addFolder', createTargetNode);
            },
            key: 'addFolder',
            disabled: isMountedTopic || hasTopicType,
          },
          {
            label: formatMessage('uns.newFile'),
            auth: ButtonPermission['uns.fileAdd'],
            onClick: () => {
              operationFns?.setOptionOpen?.('addFile', createTargetNode);
            },
            key: 'addFile',
            disabled: isMountedTopic,
          },
          // {
          //   label: formatMessage('uns.batchGeneration'),
          //   auth: ButtonPermission['uns.batchGeneration'],
          //   onClick: () => {
          //     setReverserOpen(true);
          //   },
          //   key: 'batchGeneration',
          //   disabled: !!selectedNode?.mount || hasTopicType,
          // },
        ],
      },
      {
        title: formatMessage('common.refresh'),
        onClick: () => {
          loadData({ reset: true, clearSelect: true }, () => {
            message.success(formatMessage('common.refreshSuccessful'));
          });
        },
        buttonType: 'Renew',
        key: 'reNew',
      },
    ])?.filter((item) => !item.showTreeType || item.showTreeType === treeType);
  }, [createTargetNode, formatMessage, hasTopicType, isMountedTopic, lang, loadData, message, operationFns, treeType]);
  return (
    <>
      {options?.map((item) => {
        if (item.buttonType === 'Dropdown') {
          const items: any = item.items?.filter((f) => hasPermission(f.auth));
          if (items?.length === 0) return null;
          return (
            <Dropdown menu={{ items }} placement="bottom" key={item.key}>
              <Button
                className="uns-tree-toolbar-button"
                style={{
                  background: 'var(--ui-switchwrap-bg-color)',
                  padding: '7px',
                }}
                color="default"
                variant="filled"
                title={item.title}
              >
                <SquarePlus {...toolbarIconProps} />
              </Button>
            </Dropdown>
          );
        } else {
          return (
            <ComButton
              className="uns-tree-toolbar-button"
              style={{
                background: 'var(--ui-switchwrap-bg-color)',
                padding: '7px',
              }}
              auth={item.auth}
              color="default"
              variant="filled"
              id={item.id}
              onClick={item.disabled ? undefined : item.onClick}
              key={item.key}
              title={item.title}
            >
              {renderOperationDom(item.buttonType)}
            </ComButton>
          );
        }
      })}
      {reverserOpen && (
        <ReverseModal
          reverserOpen={reverserOpen}
          setReverserOpen={setReverserOpen}
          currentNode={selectedNode as any}
          initTreeData={loadData}
        />
      )}
    </>
  );
};

// 搜索
const Search = () => {
  const formatMessage = useTranslate();
  const { searchValue, setSearchValue, loadData, searchType, treeType } = useTreeStore((state) => ({
    searchValue: state.searchValue,
    setSearchValue: state.setSearchValue,
    loadData: state.loadData,
    searchType: state.searchType,
    treeType: state.treeType,
  }));

  const debouncedInitTreeData = useCallback(
    // eslint-disable-next-line react-hooks/use-memo
    debounce(() => {
      loadData({ reset: true, clearSelect: true, queryType: 'search' });
    }, 500),
    [loadData, searchType] // 仅当 initTreeData 或 searchType 发生变化时更新 debounce 函数
  );

  const onSearchChange = () => debouncedInitTreeData();
  const selectRef = useRef<any>(null);
  const isComposingRef = useRef(false); // 拼音输入中..
  useEffect(() => {
    //处理拼音输入法
    const inputElement = selectRef.current;

    if (inputElement) {
      const handleCompositionStart = () => {
        isComposingRef.current = true;
      };

      const handleCompositionEnd = (e: any) => {
        isComposingRef.current = false;
        const value = e.target.value;
        if (value) onSearchChange();
      };

      inputElement.addEventListener('compositionstart', handleCompositionStart);
      inputElement.addEventListener('compositionend', handleCompositionEnd);

      return () => {
        inputElement.removeEventListener('compositionstart', handleCompositionStart);
        inputElement.removeEventListener('compositionend', handleCompositionEnd);
      };
    }
  }, []);

  const placeholderMap = {
    uns: formatMessage('common.searchPlaceholderUns'),
    label: formatMessage('common.searchPlaceholderLabel'),
  };

  return (
    <ProSearch
      ref={selectRef}
      closeButtonLabelText={formatMessage('common.clearSearchInput')}
      placeholder={placeholderMap[treeType]}
      size="sm"
      value={searchValue ?? ''}
      onChange={(e) => {
        const val = e.target.value || '';
        setSearchValue(val);
        if (isComposingRef.current) return;
        onSearchChange();
      }}
      onKeyDown={(e) => {
        if (e.key === 'Enter') {
          loadData({ reset: true });
        }
      }}
      title={searchValue || placeholderMap[treeType]}
    />
  );
};

const TreeHeader = ({
  customTreeTab,
  enableRootAsParent,
  defaultCreateParentNode,
  resolveCurrentDataRootNode,
}: {
  customTreeTab?: React.ReactNode;
  enableRootAsParent?: boolean;
  defaultCreateParentNode?: UnsTreeNode;
  resolveCurrentDataRootNode?: (treeData: UnsTreeNode[]) => UnsTreeNode | undefined;
}) => {
  return (
    <div style={{ paddingTop: customTreeTab ? 8 : 14 }}>
      {customTreeTab}
      <Flex gap={8} align="center" className="tree-toolbar" style={{ marginTop: customTreeTab ? 8 : 0 }}>
        <div className="tree-toolbar-search">
          <Search />
        </div>
        <div className="tree-toolbar-actions">
          <Operation
            enableRootAsParent={enableRootAsParent}
            defaultCreateParentNode={defaultCreateParentNode}
            resolveCurrentDataRootNode={resolveCurrentDataRootNode}
          />
        </div>
      </Flex>

      <Divider
        style={{
          background: 'var(--ui-table-tr-color)',
          margin: '14px 0 12px',
        }}
      />
    </div>
  );
};

// uns树的icon展示
const TreeNodeIcon = memo(({ dataNode }: { dataNode: UnsTreeNode }) => {
  const {
    systemInfo: { enableAutoCategorization },
  } = useBaseStore((state) => ({
    systemInfo: state.systemInfo,
  }));

  return (
    <UnsTreeNodeIcon
      dataNode={dataNode}
      topicType={enableAutoCategorization ? getNodeTopicType(dataNode) : 0}
      enableAutoCategorization={enableAutoCategorization}
      isTopicTypeFolder={isTopicTypeFolder}
    />
  );
});

const findNodeWithParent = (
  tree: any[],
  id: string,
  parent: any | null = null
): { node: any; parent: any | null; index: number } | null => {
  for (let i = 0; i < tree.length; i++) {
    const node = tree[i];
    if (node.id === id) return { node, parent, index: i };
    if (node.children) {
      const result = findNodeWithParent(node.children, id, node);
      if (result) return result;
    }
  }
  return null;
};

/** 树上行走：按 UNS namespace（node.path，见 agent/tree-adapter.ts 的 `namespace: node.path`）
 * 定位树节点；找不到（懒加载未展开到、路径不存在）返回 undefined，调用方静默跳过。 */

/** 树上行走高亮维持时长：过后自动淡出（inline style 的 transition 接管，见 renderTitleStyle）。 */
const WALK_HIGHLIGHT_VISIBLE_MS = 2000;

const TopTreeCom = ({
  header,
  treeNodeExtra,
  changeCurrentPath,
  enableRightClick = true,
  enableRootAsParent,
  defaultCreateParentNode,
  resolveCurrentDataRootNode,
  disableDeleteRoot,
  nonDeletableNodeIds,
}: {
  header: ProTreeProps['header'];
  treeNodeExtra?: ProTreeProps['treeNodeExtra'];
  changeCurrentPath?: any;
  enableRightClick?: boolean;
  enableRootAsParent?: boolean;
  defaultCreateParentNode?: UnsTreeNode;
  resolveCurrentDataRootNode?: (treeData: UnsTreeNode[]) => UnsTreeNode | undefined;
  disableDeleteRoot?: boolean;
  nonDeletableNodeIds?: Key[];
}) => {
  const formatMessage = useTranslate();
  const { message } = App.useApp();
  // 创建一个 ref 来引用 tree 元素
  const treeRef = useRef<any>(null);
  const { systemInfo, currentUserInfo } = useBaseStore((state) => ({
    systemInfo: state.systemInfo,
    currentUserInfo: state.currentUserInfo,
  }));
  const { enableAutoCategorization } = systemInfo;
  const resourceKeys = (currentUserInfo as (typeof currentUserInfo & { resourceKeys?: string[] }) | undefined)
    ?.resourceKeys;
  const canViewDataQuality = Boolean(
    systemInfo?.dataQualityEnabled === true &&
    (systemInfo?.authEnable === false ||
      currentUserInfo?.superAdmin === true ||
      resourceKeys?.includes('uns.dataQuality.read') ||
      resourceKeys?.includes('uns.dataQuality.manage'))
  );

  const {
    lazyTree,
    loadData,
    treeData,
    expandedKeys,
    setExpandedKeys,
    loadedKeys,
    setLoadedKeys,
    nodePaginationState,
    loadingKeys,
    loading,
    treeType,
    selectedNode,
    setSelectedNode,
    setTreeMap,
    breadcrumbList,
    searchValue,
    operationFns,
    setPasteNode,
    pasteNode,
    onRefresh,
    setCurrentTreeMapType,
    setScrollTreeNode,
    handleExpandNode,
    setTreeData,
    setLoading,
    walkHighlightKey,
  } = useTreeStore((state) => ({
    lazyTree: state.lazyTree,
    loadData: state.loadData,
    treeData: state.treeData,
    expandedKeys: state.expandedKeys,
    setExpandedKeys: state.setExpandedKeys,
    loadedKeys: state.loadedKeys,
    setLoadedKeys: state.setLoadedKeys,
    nodePaginationState: state.nodePaginationState,
    setNodePaginationState: state.setNodePaginationState,
    loadingKeys: state.loadingKeys,
    loading: state.loading,
    treeType: state.treeType,
    selectedNode: state.selectedNode,
    setSelectedNode: state.setSelectedNode,
    breadcrumbList: state.breadcrumbList,
    searchValue: state.searchValue,
    operationFns: state.operationFns,
    setPasteNode: state.setPasteNode,
    pasteNode: state.pasteNode,
    onRefresh: state.onRefresh,
    setTreeMap: state.setTreeMap,
    setCurrentTreeMapType: state.setCurrentTreeMapType,
    setScrollTreeNode: state.setScrollTreeNode,
    handleExpandNode: state.handleExpandNode,
    setTreeData: state.setTreeData,
    setLoading: state.setLoading,
    walkHighlightKey: state.walkHighlightKey,
  }));

  //滚动到目标树节点
  const scrollTreeNode = useCallback((id: Key) => {
    setTimeout(() => {
      if (treeRef.current) treeRef.current.scrollTo?.({ key: id, align: 'top' });
    }, 500);
  }, []);

  useEffect(() => {
    setScrollTreeNode(scrollTreeNode);
  }, [scrollTreeNode]);

  // 树上行走：描边高亮维持 WALK_HIGHLIGHT_VISIBLE_MS 后自动淡出（renderTitleStyle 依据
  // highlightVisibleKey 决定是否套用高亮样式；淡出靠 inline style 的 transition 接管，见下方）。
  const [highlightVisibleKey, setHighlightVisibleKey] = useState<Key | undefined>(undefined);
  useEffect(() => {
    if (walkHighlightKey == null) {
      setHighlightVisibleKey(undefined);
      return;
    }
    setHighlightVisibleKey(walkHighlightKey);
    const timer = setTimeout(() => setHighlightVisibleKey(undefined), WALK_HIGHLIGHT_VISIBLE_MS);
    return () => clearTimeout(timer);
  }, [walkHighlightKey]);

  const onLoadData = async (node: any) => {
    const _node = { ...node };
    return loadData({
      key: _node.key,
      parentInfo: _node,
    });
  };
  const handleRenderLoadMoreNode = (moreNodeData: any) => {
    const { parentKey: nodeKey, currentPage } = moreNodeData;
    if (loadingKeys.has(nodeKey)) {
      console.log(`正在loading ${nodeKey}`);
      return;
    }
    const state = nodePaginationState[nodeKey];
    if (state && state.hasMore && !state.isLoading) {
      // 处理根节点和子节点的加载更多
      if (currentPage === state.currentPage) {
        // 以防重复请求2次
        loadData({
          key: nodeKey,
          page: state.currentPage + 1,
          parentInfo: moreNodeData?.parentInfo,
        });
      }
    }
  };
  const isUns = treeType === 'uns';
  const qualityTreeEnabled = isUns && canViewDataQuality;
  const isShow = usePageIsShow();
  const [qualityTreeStatus, setQualityTreeStatus] = useState(() => buildQualityTreeStatus([]));
  const qualityPollingActive = useRef(false);
  const qualityRequestRunning = useRef(false);

  useEffect(() => {
    if (!qualityTreeEnabled) return;
    qualityPollingActive.current = true;

    const refreshQualityTreeStatus = async () => {
      if (qualityRequestRunning.current || isShow?.current === false) return;
      qualityRequestRunning.current = true;
      try {
        const summary = await getDataQualityActiveSummary();
        if (qualityPollingActive.current) {
          setQualityTreeStatus(buildQualityTreeStatus(summary.list || []));
        }
      } catch {
        // Tree markers are supplementary. Keep the previous snapshot on transient API failures.
      } finally {
        qualityRequestRunning.current = false;
      }
    };

    const initialTimer = window.setTimeout(() => void refreshQualityTreeStatus(), 0);
    const timer = window.setInterval(() => void refreshQualityTreeStatus(), 15_000);
    return () => {
      qualityPollingActive.current = false;
      window.clearTimeout(initialTimer);
      window.clearInterval(timer);
    };
  }, [isShow, qualityTreeEnabled]);

  const renderedTreeData = useMemo(
    () => (isUns ? decorateMountedNamespaceTree(treeData) : treeData),
    [isUns, treeData]
  );

  const renderQualityTreeStatus = useCallback(
    (dataNode: UnsTreeNode) => {
      if (!qualityTreeEnabled) return null;
      const status = qualityStatusForNode(dataNode, qualityTreeStatus);
      if (!status || dataNode.pathType === 2) return null;

      const title = formatMessage('dataQuality.tree.abnormalNodes', { count: status.count });

      return (
        <span aria-label={title} className="uns-tree-quality-summary" title={title}>
          {formatMessage('dataQuality.tree.abnormalSummary', { count: status.count })}
        </span>
      );
    },
    [formatMessage, qualityTreeEnabled, qualityTreeStatus]
  );
  const renderQualityTreeClassName = useCallback(
    (dataNode: UnsTreeNode) => {
      if (!qualityTreeEnabled) return undefined;
      const status = qualityStatusForNode(dataNode, qualityTreeStatus);
      if (!status) return undefined;
      return cx(
        'uns-tree-quality-row',
        `uns-tree-quality-row--${status.severity}`,
        dataNode.pathType === 2 ? 'uns-tree-quality-row--leaf' : 'uns-tree-quality-row--branch'
      );
    },
    [qualityTreeEnabled, qualityTreeStatus]
  );
  const createFallbackNode = resolveCreateFallbackNode({
    treeData,
    enableRootAsParent,
    defaultCreateParentNode,
    resolveCurrentDataRootNode,
  });
  const currentDataRootNode = resolveCurrentDataRootNode?.(treeData) || getDefaultDataRootNode(treeData);
  const currentDataRootNodeId = normalizeNodeId(currentDataRootNode?.id);
  const nonDeletableNodeIdSet = useMemo(
    () => new Set((nonDeletableNodeIds || []).map((id) => normalizeNodeId(id))),
    [nonDeletableNodeIds]
  );

  const isNodeDeletable = useCallback(
    (node?: UnsTreeNode) => {
      if (!node) {
        return false;
      }
      if (hasMountedTopicInSubtree(node)) {
        return false;
      }
      const nodeId = normalizeNodeId(node.id);
      if (!nodeId) {
        return true;
      }
      if (nonDeletableNodeIdSet.has(nodeId)) {
        return false;
      }
      if (disableDeleteRoot && currentDataRootNodeId && nodeId === currentDataRootNodeId) {
        return false;
      }
      return true;
    },
    [disableDeleteRoot, currentDataRootNodeId, nonDeletableNodeIdSet]
  );

  const pasteHandle = (source: any, target: any) => {
    if (source) {
      setLoading(true);
      const targetId = (target?.pathType === 0 ? target?.id : target?.parentId) || '';
      pasteUns({
        sourceId: source?.id || undefined,
        targetId: targetId || undefined,
      })
        .then(({ data, msg, code }) => {
          const { parentId, id } = data;
          const hasParentNode = getTargetNode(treeData || [], parentId);

          const _parentId = hasParentNode ? parentId : targetId ? targetId : ROOT_NODE_ID;
          const _childId = hasParentNode || parentId === targetId || !lazyTree ? id : parentId;
          if (code === 206) {
            message.warning(msg);
          } else {
            message.success(formatMessage('uns.pasteSuccess'));
          }
          loadData(
            {
              queryType: source?.pathType === 0 ? 'addFolder' : 'addFile',
              // key: targetId ? targetId : ROOT_NODE_ID,
              // newNodeKey: data,
              key: _parentId,
              newNodeKey: _childId,
              // reset: !targetId,
              reset: !(targetId || parentId),
              nodeDetail: source,
            },
            (_, selectInfo, opt) => {
              if (!selectInfo && !_childId) return;
              const currentNode = getTargetNode(_ || [], _childId);
              changeCurrentPath(
                selectInfo ||
                  currentNode || {
                    key: _childId,
                    id: _childId,
                    pathType: source?.pathType === 0 ? 0 : 2,
                  }
              );
              setTreeMap(false);
              if (selectInfo) {
                // 非lasy树
                opt?.scrollTreeNode?.(id);
              }
            }
          );
        })
        .catch(() => {
          setLoading(false);
        });
    } else {
      message.warning(formatMessage('uns.copyTip'));
    }
  };
  return (
    <>
      <ProTree
        {...(isUns ? { wrapperClassName: 'uns-namespace-tree', itemHeight: 36 } : {})}
        onDndDragStart={(info) => {
          const { active } = info;
          if (active.pathType == 0 && expandedKeys.includes(active?.id)) {
            // 关闭文件夹
            setExpandedKeys((expandedKeys) => {
              return expandedKeys.filter((k) => active?.id !== k);
            });
          }
        }}
        onDndDragEnd={(info) => {
          const { active, over, isInset } = info;
          if (active?.id && over?.id && active.id !== over.id) {
            let _isInset = isInset;
            if (over?.pathType === 2) {
              _isInset = false;
            }
            // active是我移动的节点，over是我放置的节点位置，_isInset是我放置在over这个节点里面还是over节点的下面
            console.log(active, over, _isInset);
            setTreeData((draft) => {
              const activeInfo = findNodeWithParent(draft, active.id);
              const overInfo = findNodeWithParent(draft, over.id);
              if (activeInfo && overInfo) {
                // 移除原节点
                if (activeInfo.parent) {
                  activeInfo.parent.children.splice(activeInfo.index, 1);
                } else {
                  draft.splice(activeInfo.index, 1);
                }

                // 处理插入位置
                const isRootOperation = !overInfo.parent;
                const targetArray = isRootOperation ? draft : overInfo.parent?.children || [];

                let insertIndex = _isInset ? 0 : overInfo.index + 1;

                // 边界检查
                insertIndex = Math.min(insertIndex, targetArray.length);

                if (_isInset) {
                  overInfo.node.children = [activeInfo.node, ...(overInfo.node.children || [])];
                } else {
                  targetArray.splice(insertIndex, 0, activeInfo.node);
                }
              }
            });
          }
        }}
        // dndDraggable={isUns}
        isShow={isShow}
        ref={treeRef}
        rightClickMenuItems={
          enableRightClick
            ? isUns
              ? ({ node }) => {
                  if (!node) {
                    // 空白点击
                    return [
                      {
                        auth: [ButtonPermission['uns.folderPaste'], ButtonPermission['uns.filePaste']],
                        key: 'paste',
                        label: formatMessage('common.paste'),
                        onClick: () => {
                          pasteHandle(pasteNode, null);
                        },
                      },
                      {
                        auth: [ButtonPermission['uns.folderPaste'], ButtonPermission['uns.filePaste']],
                        key: 'pasteAndEdit',
                        label: formatMessage('common.pasteAndEdit'),
                        onClick: () => {
                          if (pasteNode) {
                            //纯前端方案
                            operationFns?.setOptionOpen?.('paste', null, pasteNode);
                          } else {
                            message.warning(formatMessage('uns.copyTip'));
                          }
                        },
                        disabled: !!(
                          enableAutoCategorization &&
                          pasteNode?.pathType === 0 &&
                          getFolderTopicType(pasteNode)
                        ),
                      },
                      {
                        auth: ButtonPermission['uns.folderAdd'],
                        key: 'addFolder',
                        label: formatMessage('common.createNewFolder'),
                        onClick: () => {
                          operationFns?.setOptionOpen?.('addFolder', createFallbackNode);
                        },
                      },
                      {
                        auth: ButtonPermission['uns.fileAdd'],
                        key: 'addFile',
                        label: formatMessage('common.createNewFile'),
                        onClick: () => {
                          operationFns?.setOptionOpen?.('addFile', createFallbackNode);
                        },
                      },
                    ];
                  }
                  const _node = { ...node };
                  const nodeTopicType = getNodeTopicType(_node);
                  const baseItems =
                    (_node.pathType === 0 && !getFolderTopicType(_node)) || !enableAutoCategorization
                      ? ['copy', 'paste', 'pasteAndEdit', 'addFolder', 'addFile', 'delete']
                      : ['copy', 'paste', 'pasteAndEdit', 'addFile', 'delete'];
                  let disabledPaste = false;
                  let disabledPasteAndEdit = false;
                  if (enableAutoCategorization && pasteNode) {
                    if (pasteNode.pathType === 0) {
                      const pasteTopicType = getFolderTopicType(pasteNode);
                      if (pasteTopicType) {
                        if (
                          (_node.pathType === 0 && nodeTopicType && pasteTopicType !== nodeTopicType) ||
                          (_node.pathType === 2 && pasteTopicType !== nodeTopicType)
                        ) {
                          disabledPaste = true;
                          disabledPasteAndEdit = true;
                        }
                        if (
                          (_node.pathType === 0 && pasteTopicType === nodeTopicType) ||
                          (_node.pathType === 2 && pasteTopicType === nodeTopicType) ||
                          !nodeTopicType
                        ) {
                          disabledPasteAndEdit = true;
                        }
                      } else {
                        if (nodeTopicType) {
                          disabledPaste = true;
                          disabledPasteAndEdit = true;
                        }
                      }
                    } else if (pasteNode.pathType === 2) {
                      if (
                        (_node.pathType === 0 && nodeTopicType && pasteNode.parentDataType !== nodeTopicType) ||
                        (_node.pathType === 2 && pasteNode.parentDataType !== nodeTopicType)
                      ) {
                        disabledPaste = true;
                        disabledPasteAndEdit = true;
                      }
                    }
                  }
                  const folderItems = lazyTree
                    ? ['refresh', ...baseItems, 'expandFolder', 'collapseFolder']
                    : [...baseItems, 'expandFolder', 'collapseFolder'];

                  const isMountedTopic = Boolean(_node.mount && _node.pathType === 2);
                  const mapItem = (_node.pathType === 0 ? folderItems : baseItems).filter(
                    (item) => !isMountedTopic || !['copy', 'delete'].includes(item)
                  );
                  const allowDelete = isNodeDeletable(_node);

                  return filterPermissionToList<ItemType>(
                    [
                      {
                        key: 'refresh',
                        label: formatMessage('common.refresh'),
                        onClick: () => {
                          onRefresh(_node);
                        },
                      },
                      {
                        auth:
                          _node.pathType === 0 ? ButtonPermission['uns.folderCopy'] : ButtonPermission['uns.fileCopy'],
                        key: 'copy',
                        label: formatMessage('common.copy'),
                        onClick: () => {
                          setPasteNode(_node);
                          message.success(formatMessage('common.copySuccess'));
                        },
                      },
                      {
                        auth:
                          _node.pathType === 0
                            ? ButtonPermission['uns.folderPaste']
                            : ButtonPermission['uns.filePaste'],
                        key: 'paste',
                        label: formatMessage('common.paste'),
                        onClick: () => {
                          pasteHandle(pasteNode, _node);
                        },
                        disabled: isMountedTopic || disabledPaste,
                      },
                      {
                        auth:
                          _node.pathType === 0
                            ? ButtonPermission['uns.folderPaste']
                            : ButtonPermission['uns.filePaste'],
                        key: 'pasteAndEdit',
                        label: formatMessage('common.pasteAndEdit'),
                        onClick: () => {
                          if (pasteNode) {
                            //纯前端方案
                            operationFns?.setOptionOpen?.('paste', _node, pasteNode);
                          } else {
                            message.warning(formatMessage('uns.copyTip'));
                          }
                        },
                        disabled: isMountedTopic || disabledPasteAndEdit,
                      },
                      {
                        auth: ButtonPermission['uns.folderAdd'],
                        key: 'addFolder',
                        label: formatMessage('common.createNewFolder'),
                        onClick: () => {
                          operationFns?.setOptionOpen?.('addFolder', _node);
                        },
                        disabled: isMountedTopic,
                      },
                      {
                        auth: ButtonPermission['uns.fileAdd'],
                        key: 'addFile',
                        label: formatMessage('common.createNewFile'),
                        onClick: () => {
                          operationFns?.setOptionOpen?.('addFile', _node);
                        },
                        disabled: isMountedTopic,
                      },
                      {
                        key: 'expandFolder',
                        label: formatMessage('common.expandFolder'),
                        onClick: () => {
                          handleExpandNode(true, _node);
                        },
                      },
                      {
                        key: 'collapseFolder',
                        label: formatMessage('common.collapseFolder'),
                        onClick: () => {
                          handleExpandNode(false, _node);
                        },
                      },
                      {
                        type: 'divider',
                      },
                      {
                        auth:
                          _node.pathType === 0
                            ? ButtonPermission['uns.folderDelete']
                            : ButtonPermission['uns.fileDelete'],
                        key: 'delete',
                        label: formatMessage('common.delete'),
                        danger: true,
                        onClick: () => {
                          if (!allowDelete) {
                            return;
                          }
                          operationFns?.setDeleteOpen?.(_node);
                        },
                        disabled: !allowDelete,
                        extra: (
                          <div style={{ display: 'flex' }}>
                            <TrashCan {...toolbarIconProps} />
                          </div>
                        ),
                      },
                    ]?.filter((f) => !f.key || mapItem.includes(f.key)) as any
                  );
                }
              : undefined
            : undefined
        }
        matchHighlightValue={searchValue}
        showSwitcherIcon={isUns}
        selectedKeys={selectedNode ? [selectedNode.key] : []}
        onSelect={(_, { node, selected }) => {
          const selectedNode = selected ? { ...node } : undefined;
          setSelectedNode(selectedNode);
          setTreeMap(false);
          setCurrentTreeMapType('all');
        }}
        loading={loading}
        treeData={renderedTreeData}
        loadData={onLoadData}
        loadMoreData={handleRenderLoadMoreNode}
        loadedKeys={loadedKeys}
        onLoad={(newLoadedKeys) => setLoadedKeys(newLoadedKeys)}
        expandedKeys={expandedKeys}
        onExpand={(expandedKeys) => {
          setExpandedKeys(expandedKeys);
        }}
        lazy={lazyTree}
        header={header}
        wrapperStyle={{ padding: '0 14px' }}
        height={0}
        treeNodeIcon={isUns ? (dataNode) => <TreeNodeIcon dataNode={dataNode} /> : undefined}
        filterTreeNode={(node) => {
          // 高亮字段
          return (
            breadcrumbList
              ?.slice(0, -1)
              .map((e) => e.key)
              .includes(node.key) ?? false
          );
        }}
        treeNodeCount={(dataNode) => {
          return (
            dataNode.pathType === 0 && (
              <span className={cx('uns-tree-node-count', !enableAutoCategorization && 'uns-tree-node-count--plain')}>
                ({dataNode.countChildren})
              </span>
            )
          );
        }}
        treeNodePersistentExtra={qualityTreeEnabled ? renderQualityTreeStatus : undefined}
        treeNodeClassName={qualityTreeEnabled ? renderQualityTreeClassName : undefined}
        treeNodeExtra={treeNodeExtra}
        overlayChildren={(dataNode: any) => {
          return (
            <Flex
              justify="flex-start"
              align="center"
              className="overlay-dom"
              style={{
                color: 'var(--ui-text-color)',
              }}
            >
              {isUns ? <TreeNodeIcon dataNode={dataNode} /> : undefined}
              <span style={{ fontWeight: 'bold', fontSize: 14 }}>{dataNode.title}</span>
              {dataNode.pathType === 0 && (
                <span className={cx('uns-tree-node-count', !enableAutoCategorization && 'uns-tree-node-count--plain')}>
                  ({dataNode.countChildren})
                </span>
              )}
            </Flex>
          );
        }}
        drapOverChanges={(info) => {
          const { node, classNames, isInset } = info;
          if (node?.pathType === 2) {
            return classNames.out;
          } else if (node?.pathType === 0) {
            return isInset ? classNames.in : classNames.out;
          } else {
            return '';
          }
        }}
        renderTitleStyle={(dataNode) => {
          const folderTopicType = getFolderTopicType(dataNode);
          const highNode =
            breadcrumbList
              ?.slice(0, -1)
              .map((e) => e.key)
              .includes(dataNode.key) ?? false;

          const baseStyle =
            !folderTopicType || !enableAutoCategorization
              ? {}
              : {
                  ...getUnsTopicFolderTitleStyle(folderTopicType),
                  color: highNode ? 'var(--ui-theme-color)' : getUnsTopicFolderTextColor(folderTopicType),
                };

          // 树上行走高亮：与选中态无关，纯视觉描边闪烁，WALK_HIGHLIGHT_VISIBLE_MS 后经 transition
          // 自动淡出（品牌绿，描边色对齐 .ant-tree-treenode-selected 已在用的
          // --ui-t-chartreuse-color-60）。非高亮态挂一条较慢的 transition，一旦 isWalking 变 false
          // 就靠它把 highlight 态的背景/描边平滑淡回去。
          const isWalking = highlightVisibleKey != null && String(dataNode.key) === String(highlightVisibleKey);
          const walkStyle = isWalking
            ? {
                backgroundColor: 'var(--ui-t-chartreuse-color-20, #e5f9b4)',
                boxShadow: 'inset 0 0 0 1.5px var(--ui-t-chartreuse-color-60, #b2ed1d)',
                borderRadius: 3,
                transition: 'background-color .12s ease-in, box-shadow .12s ease-in',
              }
            : { transition: 'background-color 1.6s ease-out, box-shadow 1.6s ease-out' };

          return { ...baseStyle, ...walkStyle };
        }}
      />
    </>
  );
};

const UnsTree = ({
  treeNodeExtra,
  changeCurrentPath,
  customTreeTab,
  enableRightClick,
  enableRootAsParent,
  defaultCreateParentNode,
  resolveCurrentDataRootNode,
  disableDeleteRoot,
  nonDeletableNodeIds,
}: {
  treeNodeExtra?: ProTreeProps['treeNodeExtra'];
  changeCurrentPath?: any;
  customTreeTab?: React.ReactNode;
  enableRightClick?: boolean;
  enableRootAsParent?: boolean;
  defaultCreateParentNode?: UnsTreeNode;
  resolveCurrentDataRootNode?: (treeData: UnsTreeNode[]) => UnsTreeNode | undefined;
  disableDeleteRoot?: boolean;
  nonDeletableNodeIds?: Key[];
}) => {
  return (
    <TopTreeCom
      treeNodeExtra={treeNodeExtra}
      changeCurrentPath={changeCurrentPath}
      header={
        <TreeHeader
          customTreeTab={customTreeTab}
          enableRootAsParent={enableRootAsParent}
          defaultCreateParentNode={defaultCreateParentNode}
          resolveCurrentDataRootNode={resolveCurrentDataRootNode}
        />
      }
      enableRightClick={enableRightClick}
      enableRootAsParent={enableRootAsParent}
      defaultCreateParentNode={defaultCreateParentNode}
      resolveCurrentDataRootNode={resolveCurrentDataRootNode}
      disableDeleteRoot={disableDeleteRoot}
      nonDeletableNodeIds={nonDeletableNodeIds}
    />
  );
};
export default UnsTree;
