import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Alert, Button, Drawer, Empty, Space, Spin } from 'antd';
import { ArrowLeft, Flow } from '@carbon/icons-react';
import { useSearchParams } from 'react-router';
import ReactFlow, {
  Background,
  Controls,
  ReactFlowProvider,
  useNodesState,
  useReactFlow,
  type Node,
  type Viewport,
} from 'reactflow';
import 'reactflow/dist/style.css';
import { useTranslate } from '@/hooks';
import { loadTopology, type TopologyKind, type TopologyMeta } from '@/apis/core-api/uns-topology';
import { useIntegrationRoute } from '../source-stream/IntegrationTabs';
import CanvasToolbar from './toolbar';
import GroupNode, { type GroupData } from './group-node';
import OuterEdge from './outer-edge';
import CollectionPreview from './collection-preview';
import RelationDetails from './relation-details';
import {
  buildGraph,
  displayRelations,
  groups,
  relatedRows,
  treeRows,
  filterRows,
  NODE_CHROME_HEIGHT,
  NODE_ROW_HEIGHT,
  type GraphRow,
  type Group,
} from './model';
import { layoutPanels, type PanelSize, type PanelLink } from './layout';
import { canvasEdges } from './edges';
import { loadCanvasPreferences, saveCanvasPreferences } from './preferences';
import styles from './index.module.scss';

const nodeTypes = { objects: GroupNode };
const edgeTypes = { outer: OuterEdge };
const groupFor = (kind: TopologyKind): Group => (kind === 'directory' || kind === 'topic' ? 'namespace' : kind);

export default function RelationshipCanvas({
  embedded = false,
  showHeading = true,
}: {
  embedded?: boolean;
  showHeading?: boolean;
}) {
  const t = useTranslate();
  const [params, setParams] = useSearchParams();
  const open = params.get('topology') === '1';
  const close = () =>
    setParams(
      (old) => {
        const next = new URLSearchParams(old);
        next.delete('topology');
        return next;
      },
      { replace: true }
    );
  if (embedded)
    return (
      <div className={styles['embedded-page']}>
        {showHeading && (
          <header className={styles['standalone-heading']}>
            <Flow size={20} />
            <h2>{t('UnsGraph.title')}</h2>
          </header>
        )}
        <ReactFlowProvider>
          <Canvas />
        </ReactFlowProvider>
      </div>
    );
  return (
    <Drawer
      open={open}
      width="100%"
      placement="right"
      closable={false}
      onClose={close}
      destroyOnClose
      rootClassName={styles.drawer}
      styles={{ body: { padding: 0, overflow: 'hidden' } }}
      title={
        <div className={styles['page-heading']}>
          <Button type="text" icon={<ArrowLeft />} onClick={close}>
            {t('UnsGraph.back')}
          </Button>
          <h2>{t('UnsGraph.title')}</h2>
          <span>{t('UnsGraph.global')}</span>
        </div>
      }
    >
      {open && (
        <ReactFlowProvider>
          <Canvas />
        </ReactFlowProvider>
      )}
    </Drawer>
  );
}

function Canvas() {
  const t = useTranslate();
  const route = useIntegrationRoute();
  const flow = useReactFlow();
  const [items, setItems] = useState<TopologyMeta[]>([]);
  const [unavailable, setUnavailable] = useState<TopologyKind[]>([]);
  const [loading, setLoading] = useState(true);
  const [refresh, setRefresh] = useState(0);
  const [preferences] = useState(loadCanvasPreferences);
  const [core, setCore] = useState<Group>(preferences.core);
  const [enabled, setEnabled] = useState<Group[]>(preferences.enabled);
  useEffect(() => saveCanvasPreferences({ core, enabled }), [core, enabled]);
  const [recordCounts, setRecordCounts] = useState<Record<string, number>>({});
  const onRecordCount = useCallback(
    (id: string, count: number) =>
      setRecordCounts((previous) => (previous[id] === count ? previous : { ...previous, [id]: count })),
    []
  );
  const [searches, setSearches] = useState<Partial<Record<Group, string>>>({});
  const expandedSearch = useMemo(() => new Set<string>(), []);
  const [selected, setSelected] = useState<string>();
  const [previewKey, setPreviewKey] = useState<string>();
  const overviewViewport = useRef<Viewport>();
  const magnifiedGroup = useRef<Group>();
  const [collapsed, setCollapsed] = useState(new Set<string>());
  const [focusedCollapsed, setFocusedCollapsed] = useState(new Set<string>());
  useEffect(() => setFocusedCollapsed(new Set()), [selected]);
  const initializedDirectories = useRef(new Set<string>());
  const [relationSelection, setRelationSelection] = useState<string[]>([]);
  const inspectorOpen = !!previewKey || !!relationSelection.length;
  const inspectorOpenRef = useRef(inspectorOpen);
  useEffect(() => {
    inspectorOpenRef.current = inspectorOpen;
  }, [inspectorOpen]);
  const closeDetails = useCallback(() => {
    setPreviewKey(undefined);
    setRelationSelection([]);
  }, []);
  useEffect(() => {
    if (!inspectorOpen && !selected) return;
    const onEscape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented || event.isComposing) return;
      // 先让选择器、日期框和弹窗处理 Esc，避免同时关闭其后的详情面板。
      const overlays = document.querySelectorAll(
        '.ant-select-dropdown:not(.ant-select-dropdown-hidden), .ant-picker-dropdown:not(.ant-picker-dropdown-hidden), .ant-popover:not(.ant-popover-hidden), [role="dialog"]'
      );
      if ([...overlays].some((element) => element.getClientRects().length)) return;
      event.preventDefault();
      event.stopPropagation();
      if (inspectorOpen) closeDetails();
      else setSelected(undefined);
    };
    window.addEventListener('keydown', onEscape, true);
    return () => window.removeEventListener('keydown', onEscape, true);
  }, [inspectorOpen, selected, closeDetails]);
  const canvasRef = useRef<HTMLDivElement>(null);
  const [aspect, setAspect] = useState(1.6);
  const [canvasHeight, setCanvasHeight] = useState(600);
  useEffect(() => {
    const element = canvasRef.current;
    if (!element) return;
    const observer = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect;
      if (height > 0 && !inspectorOpenRef.current) {
        setCanvasHeight(Math.round(height));
        setAspect(Math.round(Math.max(0.75, Math.min(2, width / height)) * 100) / 100);
      }
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  const [nodes, setNodes, onNodesChange] = useNodesState<GroupData>([]);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    loadTopology(controller.signal)
      .then((result) => {
        if (!controller.signal.aborted) {
          setItems(result.items);
          setUnavailable(result.unavailable);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [refresh]);
  const graph = useMemo(() => buildGraph(items), [items]);
  useEffect(() => {
    const added = graph.rows.filter((row) => row.kind === 'directory' && !initializedDirectories.current.has(row.key));
    if (!added.length) return;
    added.forEach((row) => initializedDirectories.current.add(row.key));
    setCollapsed((previous) => new Set([...previous, ...added.map((row) => row.key)]));
  }, [graph.rows]);
  const selectedRow = graph.rows.find((row) => row.key === selected);
  const previewRow = graph.rows.find((row) => row.key === previewKey);
  useEffect(() => {
    if (!loading && selected && !selectedRow) {
      setSelected(undefined);
    }
    if (!loading && previewKey && !previewRow) setPreviewKey(undefined);
  }, [loading, selected, selectedRow, previewKey, previewRow]);
  const related = useMemo(() => relatedRows(graph.rows, graph.relations, selected), [graph, selected]);
  const shownGroups = useMemo(
    () => groups.filter((group) => group === core || enabled.includes(group)),
    [core, enabled]
  );
  const displayedRelations = useMemo(
    () => displayRelations(graph.rows, graph.relations, shownGroups),
    [graph, shownGroups]
  );
  const onToggle = useCallback(
    (key: string) =>
      (selected ? setFocusedCollapsed : setCollapsed)((previous) => {
        const next = new Set(previous);
        if (next.has(key)) next.delete(key);
        else next.add(key);
        return next;
      }),
    [selected]
  );
  const rowMap = useMemo(() => new Map(graph.rows.map((row) => [row.key, row])), [graph]);
  const onSelect = useCallback(
    (key: string) => {
      const row = rowMap.get(key);
      // 查看关联集合的数据时保留原聚焦对象，避免改变其他卡片的过滤范围。
      if (row?.kind !== 'collection' || !selected || rowMap.get(selected)?.group === 'collection') {
        setSelected(key);
      }
      setRelationSelection([]);
      setPreviewKey(row?.kind === 'collection' ? key : undefined);
    },
    [rowMap, selected]
  );
  const onMagnify = useCallback(
    (group: Group) => {
      if (magnifiedGroup.current === group && overviewViewport.current) {
        flow.setViewport(overviewViewport.current, { duration: 300 });
        overviewViewport.current = undefined;
        magnifiedGroup.current = undefined;
        return;
      }
      const node = flow.getNode(group);
      const canvas = canvasRef.current;
      if (!node?.width || !node.height || !canvas) return;
      if (!overviewViewport.current) overviewViewport.current = flow.getViewport();
      magnifiedGroup.current = group;
      const zoom = Math.max(
        0.25,
        Math.min(1.6, (canvas.clientWidth - 64) / node.width, (canvas.clientHeight - 64) / node.height)
      );
      flow.setViewport(
        {
          x: canvas.clientWidth / 2 - (node.position.x + node.width / 2) * zoom,
          y: canvas.clientHeight / 2 - (node.position.y + node.height / 2) * zoom,
          zoom,
        },
        { duration: 300 }
      );
    },
    [flow]
  );
  const scopeRows = useMemo(
    () =>
      Object.fromEntries(groups.map((group) => [group, graph.rows.filter((row) => row.group === group)])) as Record<
        Group,
        GraphRow[]
      >,
    [graph.rows]
  );
  const panelRows = useMemo(
    () =>
      Object.fromEntries(
        groups.map((group) => [
          group,
          filterRows(
            selected && group !== selectedRow?.group
              ? scopeRows[group].filter((row) => related.has(row.key))
              : scopeRows[group],
            searches[group] || ''
          ),
        ])
      ) as Record<Group, GraphRow[]>,
    [scopeRows, searches, selected, selectedRow?.group, related]
  );
  const onSearch = useCallback(
    (group: Group, value: string) => setSearches((previous) => ({ ...previous, [group]: value })),
    []
  );
  const onToggleAll = useCallback(
    (group: Group, expand: boolean) => {
      const parents = scopeRows[group].filter((row) => row.kind === 'directory').map((row) => row.key);
      (selected ? setFocusedCollapsed : setCollapsed)((previous) => {
        const next = new Set(previous);
        parents.forEach((key) => (expand ? next.delete(key) : next.add(key)));
        return next;
      });
      if (!expand) setSearches((previous) => ({ ...previous, [group]: '' }));
    },
    [scopeRows, selected]
  );
  const sizes = useMemo(
    () =>
      Object.fromEntries(
        groups.map((group) => [
          group,
          {
            width: 300,
            height:
              NODE_CHROME_HEIGHT +
              Math.max(
                96,
                Math.min(
                  shownGroups.length > 3 ? Math.max(180, (canvasHeight - 130) / 2) : Math.max(180, canvasHeight - 100),
                  treeRows(
                    filterRows(scopeRows[group], searches[group] || ''),
                    searches[group] ? expandedSearch : collapsed
                  ).length * NODE_ROW_HEIGHT
                )
              ),
          },
        ])
      ) as Record<Group, PanelSize>,
    [scopeRows, collapsed, shownGroups.length, canvasHeight, searches, expandedSearch]
  );
  const panelLinks = useMemo(() => {
    const links = new Map<string, PanelLink>();
    for (const relation of displayedRelations) {
      const from = rowMap.get(relation.from),
        to = rowMap.get(relation.to);
      if (!from || !to || !shownGroups.includes(from.group) || !shownGroups.includes(to.group)) continue;
      const pair = [from.group, to.group].sort();
      const key = pair.join('/'),
        link = links.get(key);
      if (link) link.weight += 1;
      else links.set(key, { from: pair[0] as Group, to: pair[1] as Group, weight: 1 });
    }
    return [...links.values()];
  }, [displayedRelations, rowMap, shownGroups]);
  const positions = useMemo(() => {
    return layoutPanels(shownGroups, core, sizes, panelLinks, aspect);
  }, [core, shownGroups, sizes, panelLinks, aspect]);
  useEffect(() => {
    setNodes((previous) =>
      shownGroups.map((group): Node<GroupData> => ({
        ...previous.find((node) => node.id === group),
        id: group,
        type: 'objects',
        zIndex: 2,
        position: previous.find((node) => node.id === group)?.position || positions[group]!,
        dragHandle: '.canvas-drag-handle',
        data: {
          group,
          core: group === core,
          compact: shownGroups.length > 3,
          total: scopeRows[group].filter((row) => row.kind !== 'binding').length,
          search: searches[group] || '',
          recordCounts,
          bodyHeight: sizes[group].height - NODE_CHROME_HEIGHT,
          rows: panelRows[group],
          selected: group === 'collection' && previewKey ? previewKey : selected,
          related,
          collapsed: searches[group] ? expandedSearch : selected ? focusedCollapsed : collapsed,
          unavailable: unavailable.some((kind) => groupFor(kind) === group),
          onSelect,
          onToggle,
          onSearch,
          onToggleAll,
          onMagnify,
        },
      }))
    );
  }, [
    graph,
    shownGroups,
    core,
    selected,
    previewKey,
    related,
    collapsed,
    focusedCollapsed,
    unavailable,
    onSelect,
    onToggle,
    positions,
    panelRows,
    scopeRows,
    searches,
    sizes,
    expandedSearch,
    recordCounts,
    onSearch,
    onToggleAll,
    onMagnify,
    setNodes,
  ]);
  useEffect(() => {
    setNodes((previous) => previous.map((node) => ({ ...node, position: positions[node.id as Group]! })));
  }, [positions, loading, setNodes]);
  useEffect(() => {
    if (loading) return;
    overviewViewport.current = undefined;
    magnifiedGroup.current = undefined;
    const timer = window.setTimeout(() => {
      flow.fitView({ padding: 0.04, maxZoom: 1, duration: 350 });
    }, 160);
    return () => window.clearTimeout(timer);
  }, [positions, loading, flow]);
  const validRelations = graph.relations.filter((relation) => rowMap.has(relation.from) && rowMap.has(relation.to));
  const missingCount = graph.relations.length - validRelations.length;
  const detailed = !!selected;
  const edges = useMemo(
    () => canvasEdges(nodes, displayedRelations, rowMap, selected, related),
    [nodes, displayedRelations, rowMap, selected, related]
  );
  const focus = (key?: string) => {
    setSelected(key);
    setRelationSelection([]);
    setPreviewKey(undefined);
    if (!key) {
      return;
    }
    const group = rowMap.get(key)?.group;
    if (group) setEnabled((previous) => (previous.includes(group) ? previous : [...previous, group]));
    // 先展开树，然后将每个面板中的第一条关联对象滚动到可见区域。
    const active = relatedRows(graph.rows, graph.relations, key);
    window.setTimeout(() => {
      for (const group of shownGroups) {
        const panel = document.querySelector(`[data-group="${group}"]`);
        const node = flow.getNode(group) as Node<GroupData> | undefined;
        if (!node) continue;
        const ordered = treeRows(node.data.rows, node.data.collapsed);
        let index = ordered.findIndex(({ row }) => row.key === key);
        if (index < 0)
          index = ordered.findIndex(({ row }) => active.has(row.key) && !['directory', 'source'].includes(row.kind));
        const body = panel?.querySelector<HTMLElement>('[data-group-scroll]');
        if (index >= 0 && body) body.scrollTop = index * NODE_ROW_HEIGHT;
      }
    }, 80);
  };
  const changeCore = (group: Group, key?: string) => {
    if (group !== core) {
      overviewViewport.current = undefined;
      closeDetails();
    }
    setCore(group);
    setEnabled((previous) => [...new Set([...previous, group])]);
    focus(key);
  };
  return (
    <div className={styles.workspace}>
      <CanvasToolbar
        core={core}
        selected={selectedRow?.group === core ? selected : undefined}
        options={graph.rows
          .filter((row) => row.group === core && row.kind !== 'binding')
          .map((row) => ({
            value: row.key,
            label: row.name,
            searchText: row.name + ' ' + row.id,
          }))}
        shown={shownGroups}
        onCore={changeCore}
        onLocate={focus}
        onToggle={(group, checked) => {
          setRelationSelection([]);
          setEnabled((previous) => (checked ? [...previous, group] : previous.filter((value) => value !== group)));
        }}
        onRefresh={() => setRefresh((value) => value + 1)}
      />
      {!!unavailable.length && (
        <Alert banner type="warning" message={`${t('UnsGraph.partial')}: ${unavailable.join(', ')}`} />
      )}
      <div className={styles['canvas-workspace']}>
        <div className={styles['canvas-area']} ref={canvasRef}>
          {loading ? (
            <div className={styles.loading}>
              <Spin />
            </div>
          ) : !graph.rows.length ? (
            <Empty description={t(unavailable.length ? 'UnsGraph.unavailable' : 'UnsGraph.empty')} />
          ) : (
            <ReactFlow
              nodes={nodes}
              edges={edges}
              nodeTypes={nodeTypes}
              edgeTypes={edgeTypes}
              onNodesChange={onNodesChange}
              nodesConnectable={false}
              edgesUpdatable={false}
              zoomOnDoubleClick={false}
              deleteKeyCode={null}
              onPaneClick={() => {
                focus(undefined);
              }}
              minZoom={0.25}
              maxZoom={1.6}
              onlyRenderVisibleElements
              onEdgeClick={(_, edge) => {
                setRelationSelection(edge.data.relationIds || []);
                setPreviewKey(undefined);
              }}
              proOptions={{ hideAttribution: true }}
            >
              <Background color="var(--ui-line-color)" gap={24} size={1} />
              <Controls showInteractive={false} />
            </ReactFlow>
          )}
        </div>
        {!!relationSelection.length && (
          <RelationDetails
            key={relationSelection.join('|')}
            relations={displayedRelations.filter((relation) => relationSelection.includes(relation.id))}
            rowMap={rowMap}
            onClose={closeDetails}
            onSelect={onSelect}
          />
        )}
        {previewRow?.kind === 'collection' && previewRow.meta && (
          <CollectionPreview
            key={previewRow.id}
            collection={previewRow.meta}
            refresh={refresh}
            onClose={closeDetails}
            onCount={onRecordCount}
            onOpen={() => route.go({ view: 'collections', collectionId: previewRow.id })}
          />
        )}
      </div>
      <footer className={styles.status}>
        <Space wrap>
          <span>
            {t('UnsGraph.global')} · {graph.rows.filter((row) => row.kind !== 'binding').length} {t('UnsGraph.objects')}
          </span>
          <span>
            {t('UnsGraph.connections')} {edges.length} / {validRelations.length}
          </span>
          {missingCount > 0 && (
            <span>
              {missingCount} {t('UnsGraph.unresolved')}
            </span>
          )}
        </Space>
        <span>{t(detailed ? 'UnsGraph.detailHint' : 'UnsGraph.summaryHint')}</span>
      </footer>
    </div>
  );
}
