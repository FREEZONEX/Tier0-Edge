import { useTabName, useTranslate } from '@/hooks';
import { useActivate, useUnActivate, usePageIsShow } from '@/contexts/tabs-lifecycle-context';
import { type FC, useCallback, useEffect, useState } from 'react';
import { useDeepCompareEffect } from 'ahooks';
import {
  getTreeStoreSnapshot,
  ROOT_NODE_ID,
  TreeStoreProvider,
  useTreeStore,
  useTreeStoreRef,
} from './store/treeStore';
import { findNodeInfoById, getParentNodes, handlerTreeData } from './store/utils';
import ModalContext from './ModalContext';
import TopDom from './TopDom';
import DetailDom from './DetailDom';
import LeftDom from './LeftDom';
import IntegrationTabs, {
  IntegrationGuardProvider,
  useIntegrationRoute,
} from './components/source-stream/IntegrationTabs';
import IntegrationWorkspace from './components/source-stream/IntegrationWorkspace';
import WorkspaceTransition from './components/source-stream/WorkspaceTransition';
import SourceOAuthCallback from './components/source-stream/SourceOAuth';
import MetadataWorkspace from './components/metadata';
import { ConnectEdgeWorkspace } from '@/pages/mqtt-auth';
import RelationshipCanvas from './components/relationship-canvas';
import type { UnsTreeNode } from './types';
// import { useLocation } from 'react-router';
// import { guideSteps } from './guide-steps';

import './index.scss';
import ComLayout from '@/components/com-layout';
import ComContent from '@/components/com-layout/ComContent';
import { setAiResult, useAiStore } from '@/stores/ai-store.ts';
import { UnsContextProvider } from './UnsContext';
import type { PageProps } from '@/common-types.ts';
import { Navigate, useLocation } from 'react-router';
import { getModelInfo, getTreeData } from '@/apis/core-api/uns';

const initNode = {
  key: '',
  id: '',
  pathType: null,
};

const normalizeNodeKey = (value: unknown) => {
  const key = String(value ?? '').trim();
  return key && key !== 'undefined' && key !== 'null' ? key : '';
};

const findFirstMatchingNode = (treeData: UnsTreeNode[], rawValue: string) => {
  const target = normalizeNodeKey(rawValue).toLowerCase();
  if (!target) return undefined;
  let fallback: UnsTreeNode | undefined;

  const walk = (nodes: UnsTreeNode[]): UnsTreeNode | undefined => {
    for (const node of nodes) {
      const candidates = [node.id, node.key, node.path, node.alias, node.name, node.title, node.pathName]
        .map((value) => normalizeNodeKey(value).toLowerCase())
        .filter(Boolean);
      if (candidates.some((value) => value === target)) {
        return node;
      }
      if (!fallback && candidates.some((value) => value.includes(target))) {
        fallback = node;
      }
      const child = walk(node.children || []);
      if (child) {
        return child;
      }
    }
    return undefined;
  };

  return walk(treeData) || fallback;
};


const Module: FC = () => {
  const location = useLocation();
  const { view: integrationView } = useIntegrationRoute();
  const { treeType, selectedNode, operationFns, rootInitialized } = useTreeStore((state) => ({
    rootInitialized: state.rootInitialized,
    treeType: state.treeType,
    selectedNode: state.selectedNode,
    operationFns: state.operationFns,
  }));
  const stateRef = useTreeStoreRef();

  const { setSelectedNode, setCurrentTreeMapType, setTreeMap } = getTreeStoreSnapshot(stateRef, (state) => ({
    setSelectedNode: state.setSelectedNode,
    setTreeMap: state.setTreeMap,
    setCurrentTreeMapType: state.setCurrentTreeMapType,
    setPasteNode: state.setPasteNode,
  }));

  const [addNamespaceForAi, setAddNamespaceForAi] = useState<any>(null);
  const aiResult = useAiStore((state) => state.aiResult);

  const [currentUnusedTopicNode, setCurrentUnusedTopicNode] = useState<UnsTreeNode>(initNode); // 当前unusedTopic节点
  const [unusedTopicBreadcrumbList, setUnusedTopicBreadcrumbList] = useState<UnsTreeNode[]>([]); //当前文件路径Array

  const resetToNamespaceGuide = useCallback(() => {
    setTreeMap(false);
    setSelectedNode(undefined);
    setCurrentTreeMapType('all');
    setCurrentUnusedTopicNode(initNode);
  }, [setCurrentTreeMapType, setSelectedNode, setTreeMap]);

  useEffect(() => {
    resetToNamespaceGuide();
  }, [location.state?.resetUnsLanding, resetToNamespaceGuide]);

  const focusVisibleUnsNode = useCallback(
    (node: UnsTreeNode, treeData?: UnsTreeNode[]) => {
      const store = stateRef.getState();
      const sourceTree = treeData || store.treeData;
      const nodeKey = normalizeNodeKey(node.id ?? node.key);
      if (!nodeKey) return;

      const visibleNode = findNodeInfoById(sourceTree, nodeKey) || node;
      const parents = getParentNodes(sourceTree, visibleNode.key);
      const selected = parents[parents.length - 1] || visibleNode;
      const parentKeys = parents
        .slice(0, -1)
        .map((item) => normalizeNodeKey(item.id ?? item.key))
        .filter(Boolean);

      store.setTreeMap(false);
      store.setCurrentTreeMapType('all');
      store.setSearchValue('');
      setCurrentUnusedTopicNode(initNode);
      if (parentKeys.length) {
        store.setExpandedKeys((expandedKeys) => Array.from(new Set([...expandedKeys, ...parentKeys])));
      }
      store.setSelectedNode(selected, parents.length === 0);
      window.setTimeout(() => {
        stateRef.getState().scrollTreeNode?.(selected.id ?? selected.key);
      }, 0);
    },
    [stateRef]
  );

  const focusUnsNode = useCallback(
    async (namespaceId: string) => {
      const targetId = normalizeNodeKey(namespaceId);
      if (!targetId) return;

      const store = stateRef.getState();
      const currentNode = findNodeInfoById(store.treeData, targetId);
      if (currentNode && !store.searchValue) {
        focusVisibleUnsNode(currentNode, store.treeData);
        return;
      }

      store.setLoading(true);
      try {
        const detail = await getModelInfo({ id: targetId });
        if (!detail?.id) return;

        if (!store.lazyTree) {
          const tree = handlerTreeData(await getTreeData({ type: 1 }));
          store.setSearchValue('');
          store.setTreeData(tree);
          const targetNode = findNodeInfoById(tree, targetId);
          if (targetNode) focusVisibleUnsNode(targetNode, tree);
          return;
        }

        // 定位只展开目标路径；逐层加载正常子节点，不能用搜索结果替换整棵树。
        const path: UnsTreeNode[] = [detail];
        const visited = new Set([targetId]);
        let parentId = normalizeNodeKey(detail.parentId);
        while (parentId && parentId !== ROOT_NODE_ID && !visited.has(parentId)) {
          visited.add(parentId);
          const visibleParent = !store.searchValue && findNodeInfoById(stateRef.getState().treeData, parentId);
          const parent = visibleParent || (await getModelInfo({ id: parentId }));
          if (!parent?.id) return;
          path.unshift(parent);
          if (visibleParent) break;
          parentId = normalizeNodeKey(parent.parentId);
        }

        if (store.searchValue) {
          store.setSearchValue('');
          store.abortAllRequests();
          store.setTreeData([]);
          store.setLoadedKeys([]);
          store.setExpandedKeys([]);
          store.setNodePaginationState({});
        }

        for (const node of path) {
          const nodeId = normalizeNodeKey(node.id);
          if (findNodeInfoById(stateRef.getState().treeData, nodeId)) continue;
          await store.recursiveLoadData({
            key: normalizeNodeKey(node.parentId) || ROOT_NODE_ID,
            newNodeKey: nodeId,
          });
          if (!findNodeInfoById(stateRef.getState().treeData, nodeId)) return;
        }

        const tree = stateRef.getState().treeData;
        const targetNode = findNodeInfoById(tree, targetId);
        if (targetNode) focusVisibleUnsNode(targetNode, tree);
      } catch (error) {
        console.error('Failed to focus UNS node:', error);
      } finally {
        stateRef.getState().setLoading(false);
      }
    },
    [focusVisibleUnsNode, stateRef]
  );

  const focusUnsSearchKey = useCallback(
    async (namespaceKey: string) => {
      const searchKey = normalizeNodeKey(namespaceKey);
      if (!searchKey) return;

      const store = stateRef.getState();
      store.setLoading(true);
      try {
        const searchedTree = handlerTreeData(await getTreeData({ type: 1, key: searchKey, keyword: searchKey }));
        const targetNode = findFirstMatchingNode(searchedTree, searchKey);
        if (targetNode) {
          await focusUnsNode(normalizeNodeKey(targetNode.id));
        }
      } catch (error) {
        console.error('Failed to focus UNS node:', error);
      } finally {
        stateRef.getState().setLoading(false);
      }
    },
    [focusUnsNode, stateRef]
  );

  useEffect(() => {
    // 根节点首次加载或失败后的刷新成功后，再定位 URL 中的目标。
    if (!rootInitialized) return;
    const params = new URLSearchParams(location.search);
    const namespaceId = params.get('namespaceId');
    const namespaceKey = params.get('namespaceKey');
    if (namespaceId) {
      void focusUnsNode(namespaceId);
    } else if (namespaceKey) {
      void focusUnsSearchKey(namespaceKey);
    }
  }, [focusUnsNode, focusUnsSearchKey, location.search, rootInitialized]);

  useDeepCompareEffect(() => {
    if (aiResult?.uns) {
      setAddNamespaceForAi(aiResult?.uns);
      setAiResult('uns', undefined);
    }
  }, [aiResult?.uns]);

  // uns删除操作
  const handleDelete = (item: UnsTreeNode) => {
    switch (treeType) {
      case 'uns':
        operationFns?.setDeleteOpen(item as any);
        break;
      default:
        break;
    }
  };

  const changeCurrentPath = (node?: UnsTreeNode) => {
    setSelectedNode(node?.id === selectedNode?.id ? undefined : node);
    setCurrentUnusedTopicNode(initNode);
    setCurrentTreeMapType('all');
  };

  // const location = useLocation();
  // 新手导航步骤
  // useGuideSteps(guideSteps(), location?.state?.stepId);

  if (integrationView !== 'namespace') return <IntegrationWorkspace />;

  return (
    <ComLayout className="unsContainer">
      <LeftDom
        customTitle={<IntegrationTabs />}
        changeCurrentPath={changeCurrentPath}
        handleDelete={handleDelete}
        setCurrentUnusedTopicNode={setCurrentUnusedTopicNode}
        setUnusedTopicBreadcrumbList={setUnusedTopicBreadcrumbList}
      />
      <ComContent>
        <div className="chartWrap">
          <TopDom
            changeCurrentPath={changeCurrentPath}
            setCurrentUnusedTopicNode={setCurrentUnusedTopicNode}
            unusedTopicBreadcrumbList={unusedTopicBreadcrumbList}
            currentUnusedTopicNode={currentUnusedTopicNode}
          />
          <DetailDom handleDelete={handleDelete} currentUnusedTopicNode={currentUnusedTopicNode} />
        </div>
      </ComContent>
      <ModalContext
        addNamespaceForAi={addNamespaceForAi}
        setAddNamespaceForAi={setAddNamespaceForAi}
        changeCurrentPath={changeCurrentPath}
      />
    </ComLayout>
  );
};

const Workspace: FC = () => {
  const { view } = useIntegrationRoute();
  const location = useLocation();
  const params = new URLSearchParams(location.search);
  // Metadata UUIDs and historic UNS numeric IDs are distinct contracts.
  // Existing Stream links must still open the legacy Topic/history workspace.
  const legacyTarget = params.has('namespaceId') || params.has('namespaceKey');
  if (view === 'edge') return <ConnectEdgeWorkspace />;
  if (view === 'collections') {
    const params = new URLSearchParams(location.search);
    params.set('view', 'record');
    return <Navigate to={`/uns/topology?${params}`} replace />;
  }
  if (view === 'namespace' && !legacyTarget) return <MetadataWorkspace />;
  return (
    <TreeStoreProvider>
      <UnsContextProvider>
        <Module />
      </UnsContextProvider>
    </TreeStoreProvider>
  );
};

const WrapperModule: FC<PageProps> = () => {
  const isShow = usePageIsShow();
  const [active, setActive] = useState(isShow?.current ?? true);
  useActivate(useCallback(() => setActive(true), []));
  useUnActivate(useCallback(() => setActive(false), []));
  const location = useLocation();
  const t = useTranslate();
  useTabName(t(location.pathname === '/connect' ? 'UnsConnect.connect' : 'UnsMeta.namespace'));
  // 缓存的 Namespace 与 Connect 共用入口；隐藏页不能跟随全局路由挂载抽屉。
  if (!active) return null;
  return (
    <IntegrationGuardProvider>
      <SourceOAuthCallback />
      <WorkspaceTransition>
        <Workspace />
      </WorkspaceTransition>
      <RelationshipCanvas />
    </IntegrationGuardProvider>
  );
};
export default WrapperModule;
