import { useUnsTreeMapContext } from '@/UnsTreeMapContext.tsx';
import { useMediaSize, useTranslate } from '@/hooks';
import { Tree } from '@/pages/uns/components';
import TreeNodeExtra from '@/pages/uns/components/uns-tree/TreeNodeExtra.tsx';
import { PageTitleIcon } from '@/components/lucide-icon';
import { App, Drawer } from 'antd';
import { type CSSProperties, type FC, type ReactNode, useEffect, useState } from 'react';
// import { ChevronDown, ChevronUp } from '@/components/lucide-icon/carbon';
import ComLeft from '@/components/com-layout/ComLeft';
import RecycleTree from '@/pages/uns/components/uns-tree/RecycleTree';
import { getTreeStoreSnapshot, useTreeStore, useTreeStoreRef } from '@/pages/uns/store/treeStore.tsx';
import type { UnsTreeNode } from '@/pages/uns/types';
// import Loading from '@/components/loading';

// const panelOpenSize = 500;
const initNode = {
  key: '',
  id: '',
  pathType: null,
};

const LeftDom: FC<{
  changeCurrentPath: any;
  handleDelete: any;
  setCurrentUnusedTopicNode?: (node: UnsTreeNode) => void;
  setUnusedTopicBreadcrumbList?: (nodes: UnsTreeNode[]) => void;
  customTitle?: ReactNode;
  customTreeTab?: ReactNode;
  enableRightClick?: boolean;
  className?: string;
  style?: CSSProperties;
  enableRootAsParent?: boolean;
  disableDeleteRoot?: boolean;
}> = ({
  changeCurrentPath,
  handleDelete,
  setCurrentUnusedTopicNode,
  setUnusedTopicBreadcrumbList,
  customTitle,
  customTreeTab,
  enableRightClick,
  className,
  style,
  enableRootAsParent,
  disableDeleteRoot,
}) => {
  const { message } = App.useApp();
  const formatMessage = useTranslate();

  const { treeType, selectedNode } = useTreeStore((state) => ({
    treeType: state.treeType,
    selectedNode: state.selectedNode,
  }));
  const stateRef = useTreeStoreRef();
  const { setTreeMap, setPasteNode, loadData, setCurrentTreeMapType } = getTreeStoreSnapshot(stateRef, (state) => ({
    setTreeMap: state.setTreeMap,
    setPasteNode: state.setPasteNode,
    loadData: state.loadData,
    setCurrentTreeMapType: state.setCurrentTreeMapType,
  }));

  const [recycleOpen, setRecycleOpen] = useState(false);

  useEffect(() => {
    loadData({ reset: true });
  }, []);

  useEffect(() => {
    if (selectedNode?.id) {
      // 如果有选中node，要清空 其他topic
      setCurrentUnusedTopicNode?.(initNode);
    }
  }, [selectedNode?.id]);

  useEffect(() => {
    if (treeType !== 'uns') {
      setRecycleOpen(false);
    }
  }, [treeType]);

  const { isTreeMapVisible, setTreeMapVisible } = useUnsTreeMapContext();
  const { isH5 } = useMediaSize();
  const treeMapHtml = (
    <div className={`treemap-body ${recycleOpen ? 'is-recycle-open' : ''}`}>
      {!recycleOpen && (
        <div className="treemap-main">
          <Tree
            enableRootAsParent={enableRootAsParent}
            disableDeleteRoot={disableDeleteRoot}
            customTreeTab={customTreeTab}
            changeCurrentPath={changeCurrentPath}
            treeNodeExtra={(dataNode) => {
              return (
                <TreeNodeExtra
                  node={dataNode}
                  disableDeleteRoot={disableDeleteRoot}
                  type={dataNode.pathType}
                  handleDelete={() => handleDelete(dataNode)}
                  handleCopy={() => handleCopy(dataNode)}
                />
              );
            }}
            enableRightClick={enableRightClick}
          />
        </div>
      )}
      {/*<Splitter layout="vertical" onResize={setUnusedTopicPanelSize} className="unusedTopicTree-Splitter">*/}
      {/*  <Splitter.Panel min={120} size={unusedTopicPanelSize[0]}>*/}
      {/*    <Tree*/}
      {/*      changeCurrentPath={changeCurrentPath}*/}
      {/*      treeNodeExtra={(dataNode) => {*/}
      {/*        return (*/}
      {/*          <TreeNodeExtra*/}
      {/*            type={dataNode.pathType}*/}
      {/*            handleDelete={() => handleDelete(dataNode)}*/}
      {/*            handleCopy={() => handleCopy(dataNode)}*/}
      {/*          />*/}
      {/*        );*/}
      {/*      }}*/}
      {/*    />*/}
      {/*  </Splitter.Panel>*/}
      {/*  /!*暂时隐藏*!/*/}
      {/*  {treeType === 'uns' && !enableAutoCategorization && (*/}
      {/*    <Splitter.Panel size={unusedTopicPanelSize[1]} min={panelCloseSize} style={{ overflow: 'hidden' }}>*/}
      {/*      <div*/}
      {/*        className="unusedTopicTree-collapsible"*/}
      {/*        onClick={() => {*/}
      {/*          if (unusedTopicPanelSize[1] === panelCloseSize) {*/}
      {/*            setUnusedTopicPanelSize([splitterWrapRef.current?.offsetHeight - panelOpenSize, panelOpenSize]);*/}
      {/*          } else {*/}
      {/*            setUnusedTopicPanelSize([splitterWrapRef.current?.offsetHeight - panelCloseSize, panelCloseSize]);*/}
      {/*          }*/}
      {/*        }}*/}
      {/*      >*/}
      {/*        {formatMessage('uns.otherTopic', 'Raw Topics')}*/}
      {/*        {unusedTopicPanelSize[1] === panelCloseSize ? <ChevronUp /> : <ChevronDown />}*/}
      {/*      </div>*/}
      {/*      <div style={{ height: 'calc(100% - 48px)', padding: '8px 14px 0' }}>*/}
      {/*        <Loading spinning={unusedTopicLoading}>*/}
      {/*          <UnusedTopicTree*/}
      {/*            unsTreeRef={unusedTopicTreeRef}*/}
      {/*            treeData={unusedTopicTreeData}*/}
      {/*            currentNode={currentUnusedTopicNode}*/}
      {/*            initTreeData={initUnusedTopicTreeData}*/}
      {/*            setTreeMap={setTreeMap}*/}
      {/*            changeCurrentPath={changeCurrentUnusedTopicPath}*/}
      {/*            treeType={treeType}*/}
      {/*            unusedTopicBreadcrumbList={unusedTopicBreadcrumbList}*/}
      {/*          />*/}
      {/*        </Loading>*/}
      {/*      </div>*/}
      {/*    </Splitter.Panel>*/}
      {/*  )}*/}
      {/*</Splitter>*/}
      {treeType === 'uns' && (
        <RecycleTree
          open={recycleOpen}
          onOpenChange={setRecycleOpen}
          onRecycleChanged={() => loadData({ reset: true })}
        />
      )}
    </div>
  );

  const handleCopy = (item: UnsTreeNode) => {
    switch (treeType) {
      case 'uns':
        setPasteNode(item);
        message.success(formatMessage('common.copySuccess'));
        break;
      default:
        break;
    }
  };

  return !isH5 ? (
    <ComLeft
      className={className}
      style={{ overflow: 'hidden', ...style }}
      resize
      defaultWidth={customTitle ? 280 : 360}
    >
      {customTitle ? (
        customTitle
      ) : (
        <div
          className="treemapTitle"
          onClick={() => {
            setTreeMap(false);
            setCurrentTreeMapType('all');
            setCurrentUnusedTopicNode?.(initNode);
            setUnusedTopicBreadcrumbList?.([]);
            changeCurrentPath();
          }}
        >
          <PageTitleIcon resourceKey="uns.page" />
          <span>{formatMessage('uns.treeList')}</span>
        </div>
      )}
      {treeMapHtml}
    </ComLeft>
  ) : (
    isTreeMapVisible && (
      <Drawer
        className="treemap-drawer"
        rootClassName="treemap-drawer-root"
        placement="right"
        onClose={() => setTreeMapVisible(false)}
        open={isTreeMapVisible}
        getContainer={false}
      >
        {treeMapHtml}
      </Drawer>
    )
  );
};

export default LeftDom;
