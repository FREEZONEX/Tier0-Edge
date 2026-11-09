import ProTree from '@/components/pro-tree';
import { Checkbox, Divider, Flex } from 'antd';
import cx from 'classnames';
import { Renew } from '@/components/lucide-icon/carbon';
import { toolbarIconProps } from '@/components/lucide-icon/icon-props';
import ComButton from '@/components/com-button';
import { useTranslate } from '@/hooks';
import { useTreeStore } from './treeStore.tsx';
import { type FC, type Key, memo, useCallback, useEffect, useRef } from 'react';
import { useBaseStore } from '@/stores/base';
import type { UnsTreeNode } from '../../types.tsx';
import { exportExcel } from '@/apis/core-api/uns';
import { processedCheckedKeys } from '@/pages/uns/store/utils.ts';
import { getParamsForArray } from '@/utils';
import {
  getFolderTopicType,
  getNodeTopicType,
  getUnsTopicFolderTitleStyle,
  isTopicTypeFolder,
  UnsTreeNodeIcon,
} from '../uns-tree/tree-icons';
import styles from './index.module.scss';

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

export const UnsTree: FC<{ open: boolean }> = ({ open }) => {
  const formatMessage = useTranslate();
  const treeRef = useRef<any>(null);

  const {
    loadData,
    treeData,
    setCheckedKeys,
    checkedKeys,
    loading,
    loadedKeys,
    loadingKeys,
    nodePaginationState,
    expandedKeys,
    setExpandedKeys,
    setLoadedKeys,
    setLazyTree,
    setScrollTreeNode,
    allChecked,
    setAllChecked,
    setLoading,
    setJsonData,
    setParams,
    setSmallFile,
  } = useTreeStore((state) => ({
    loadData: state.loadData,
    treeData: state.treeData,
    setCheckedKeys: state.setCheckedKeys,
    checkedKeys: state.checkedKeys,
    loading: state.loading,
    loadedKeys: state.loadedKeys,
    setLoadedKeys: state.setLoadedKeys,
    loadingKeys: state.loadingKeys,
    nodePaginationState: state.nodePaginationState,
    expandedKeys: state.expandedKeys,
    setExpandedKeys: state.setExpandedKeys,
    setLazyTree: state.setLazyTree,
    setScrollTreeNode: state.setScrollTreeNode,
    allChecked: state.allChecked,
    setAllChecked: state.setAllChecked,
    setLoading: state.setLoading,
    setJsonData: state.setJsonData,
    setParams: state.setParams,
    setSmallFile: state.setSmallFile,
  }));

  const {
    lazyTree,
    systemInfo: { enableAutoCategorization },
  } = useBaseStore((state) => ({
    lazyTree: state.systemInfo?.lazyTree || false,
    systemInfo: state.systemInfo,
  }));

  useEffect(() => {
    setLazyTree(lazyTree || false);
    if (open) {
      loadData({ reset: true });
    }
  }, [open, lazyTree, loadData]);

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
      if (currentPage === state.currentPage) {
        loadData({
          key: nodeKey,
          page: state.currentPage + 1,
          parentInfo: moreNodeData?.parentInfo,
        });
      }
    }
  };

  const scrollTreeNode = useCallback((id: Key) => {
    setTimeout(() => {
      if (treeRef.current) treeRef.current.scrollTo?.({ key: id, align: 'top' });
    }, 500);
  }, []);

  useEffect(() => {
    setScrollTreeNode(scrollTreeNode);
  }, [scrollTreeNode]);

  return (
    <>
      <div style={{ flex: 1, overflow: 'hidden' }}>
        <ProTree
          disabled={allChecked}
          ref={treeRef}
          selectable={false}
          checkedKeys={checkedKeys}
          onCheck={(checkedKeysValue) => {
            setCheckedKeys(checkedKeysValue as Key[]);
          }}
          checkable
          wrapperStyle={{
            border: '1px solid var(--ui-control-border)',
            borderRadius: 4,
            padding: 4,
            flex: 1,
          }}
          height={0}
          treeData={treeData}
          treeNodeIcon={(dataNode) => <TreeNodeIcon dataNode={dataNode} />}
          treeNodeCount={(dataNode) => {
            return (
              dataNode.pathType === 0 && (
                <span className={cx('uns-tree-node-count', !enableAutoCategorization && 'uns-tree-node-count--plain')}>
                  ({dataNode.countChildren})
                </span>
              )
            );
          }}
          renderTitleStyle={(dataNode) => {
            const folderTopicType = getFolderTopicType(dataNode);
            if (!folderTopicType || !enableAutoCategorization) {
              return {};
            }

            return getUnsTopicFolderTitleStyle(folderTopicType, { paddingLeft: 4 });
          }}
          header={
            <>
              <Flex justify="space-between" align="center" style={{ marginTop: 4, padding: '0 4px' }}>
                <Checkbox
                  checked={allChecked}
                  onChange={(e) => {
                    if (e.target.checked) {
                      setCheckedKeys([]);
                    }
                    setAllChecked(e.target.checked);
                  }}
                >
                  {formatMessage('common.selectAll')}
                </Checkbox>
                <Renew
                  {...toolbarIconProps}
                  style={{ cursor: 'pointer' }}
                  onClick={() => {
                    loadData({ reset: true });
                  }}
                />
              </Flex>
              <Divider style={{ margin: '8px 0' }} />
            </>
          }
          loadData={onLoadData}
          loading={loading}
          loadMoreData={handleRenderLoadMoreNode}
          loadedKeys={loadedKeys}
          expandedKeys={expandedKeys}
          onLoad={(newLoadedKeys) => setLoadedKeys(newLoadedKeys)}
          onExpand={(expandedKeys) => {
            setExpandedKeys(expandedKeys);
          }}
          lazy={lazyTree}
        />
      </div>

      <Flex className={styles.exportPanelActions} justify="end" gap={8} style={{ marginTop: 16 }}>
        <ComButton
          onClick={() => {
            setCheckedKeys([]);
            setAllChecked(false);
          }}
        >
          {formatMessage('common.reset')}
        </ComButton>
        <ComButton
          loading={loading}
          disabled={checkedKeys?.length === 0 && !allChecked}
          type="primary"
          onClick={() => {
            setLoading(true);
            let params: any = {
              fileType: 'json',
              checkSmallFile: true,
            };
            if (allChecked) {
              params['exportType'] = 'ALL';
            } else {
              const matchedNodes = processedCheckedKeys({
                checkedKeys,
                strategy: 'SHOW_PARENT',
                treeData,
              });
              params = {
                ...params,
                ...getParamsForArray(matchedNodes as any[], 'pathType', {
                  groups: {
                    0: 'folders',
                    2: 'files',
                  },
                  extract: 'id',
                }),
                checkSmallFile: true,
              };
            }
            return exportExcel(params)
              .then((info) => {
                if (info?.code) {
                  if (info.code === 200) {
                    setSmallFile(false);
                  } else {
                    setJsonData(undefined);
                    setSmallFile(true);
                  }
                } else {
                  try {
                    setJsonData(JSON.stringify(info, null, 2));
                  } catch (e) {
                    console.log(e);
                    setJsonData(undefined);
                  }
                  setSmallFile(true);
                }
              })
              .finally(() => {
                setParams({
                  ...params,
                  checkSmallFile: undefined,
                });
                setLoading(false);
              });
          }}
        >
          {formatMessage('appSpace.newgenerate')}
        </ComButton>
      </Flex>
    </>
  );
};
