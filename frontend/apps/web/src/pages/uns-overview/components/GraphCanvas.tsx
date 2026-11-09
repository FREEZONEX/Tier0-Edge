import { useCallback, useEffect, useMemo, useRef, useState, type FC } from 'react';
import ReactFlow, {
  Background,
  Handle,
  Position,
  ReactFlowProvider,
  useReactFlow,
  useNodesInitialized,
  useNodesState,
  type Edge,
  type Node,
  type NodeProps,
  type Viewport,
} from 'reactflow';
import { Tooltip } from 'antd';
import {
  DataBase,
  Document,
  Folder,
  Information,
  Maximize,
  Network_1,
  Reset,
  ZoomIn,
  ZoomOut,
} from '@carbon/icons-react';
import classNames from 'classnames';
import ComButton from '@/components/com-button';
import { useTranslate } from '@/hooks';
import type { GraphMeta, GraphEdge } from '@/apis/core-api/uns-graph';
import type { UnsRecord } from '@/apis/core-api/uns-metadata';
import { initialPositions, layoutStep, mergeLayoutPositions, recordNodeKey, type LayoutPoint } from '../graph-layout';
import styles from '../index.module.scss';
import 'reactflow/dist/style.css';

export interface SphereData {
  meta?: GraphMeta;
  record?: UnsRecord;
  name: string;
  kind: string;
  hasSample?: boolean;
  historyFailed?: boolean;
  focused?: boolean;
  faded?: boolean;
  onInformation?: () => void;
  onOpen?: () => void;
}
interface GraphCanvasProps {
  cacheKey: string;
  metas: GraphMeta[];
  relations: GraphEdge[];
  records: UnsRecord[];
  samples: Record<string, boolean>;
  historyFailed?: boolean;
  focus?: string;
  onOpen: (data: SphereData) => void;
  onInformation: (data: SphereData) => void;
}
const sessionLayouts = new Map<string, { positions: Record<string, LayoutPoint>; viewport?: Viewport }>();
const remember = (key: string, positions: Record<string, LayoutPoint>, viewport?: Viewport) => {
  const previous = sessionLayouts.get(key);
  sessionLayouts.delete(key);
  sessionLayouts.set(key, {
    positions: mergeLayoutPositions(previous?.positions, positions),
    viewport: viewport ?? previous?.viewport,
  });
  if (sessionLayouts.size > 20) sessionLayouts.delete(sessionLayouts.keys().next().value!);
};

const Sphere: FC<NodeProps<SphereData>> = ({ data }) => {
  const t = useTranslate('UnsOverview');
  const Icon =
    data.kind === 'directory'
      ? Folder
      : data.kind === 'record' || data.kind === 'collection'
        ? DataBase
        : data.kind === 'topic'
          ? Document
          : Network_1;
  return (
    <div
      className={classNames(
        styles.sphere,
        data.record && styles['record-sphere'],
        data.focused && styles.focused,
        data.faded && styles.faded
      )}
      style={
        {
          '--sphere-color': `var(--ui-graph-${data.kind.replace(/_/g, '-')}, var(--ui-theme-color))`,
        } as React.CSSProperties
      }
    >
      <Handle type="target" position={Position.Left} className={styles['hidden-handle']} isConnectable={false} />
      <Tooltip
        styles={{ root: { pointerEvents: 'none' } }}
        title={
          <>
            {data.name}
            <br />
            {t(`kind.${data.kind}`)} · {data.meta?.id || data.record?.id}
            {data.record && (
              <>
                <br />
                {JSON.stringify(data.record.data).slice(0, 180)}
              </>
            )}
          </>
        }
      >
        <div
          className={styles.ball}
          tabIndex={0}
          role="button"
          aria-label={`${t(`kind.${data.kind}`)}: ${data.name}`}
          onKeyDown={(event) => {
            if (event.key === 'Enter' || event.key === ' ') {
              event.preventDefault();
              data.onOpen?.();
            }
          }}
        >
          <Icon size={data.record ? 20 : 26} />
          {data.meta?.archived && <span className={styles['archive-mark']} aria-label={t('archivedState')} />}
          {data.record && (
            <span
              className={classNames(styles['sample-mark'], data.hasSample && styles['has-sample'])}
              title={t(
                data.historyFailed
                  ? 'historyFailed'
                  : data.hasSample === undefined
                    ? 'checking'
                    : data.hasSample
                      ? 'hasSamples'
                      : 'noSamples'
              )}
            />
          )}
        </div>
      </Tooltip>
      <span className={styles['sphere-name']}>{data.name}</span>
      {data.record && (
        <span className={styles['record-summary']}>{JSON.stringify(data.record.data).slice(0, 120)}</span>
      )}
      <button
        className={classNames(styles['node-info'], 'nodrag', 'nopan')}
        onClick={(event) => {
          event.stopPropagation();
          data.onInformation?.();
        }}
        aria-label={t('information')}
      >
        <Information size={16} />
      </button>
      <Handle type="source" position={Position.Right} className={styles['hidden-handle']} isConnectable={false} />
    </div>
  );
};
const nodeTypes = { sphere: Sphere };

const Canvas: FC<GraphCanvasProps> = ({
  cacheKey,
  metas,
  relations,
  records,
  samples,
  historyFailed,
  focus,
  onOpen,
  onInformation,
}) => {
  const t = useTranslate('UnsOverview');
  const flow = useReactFlow<SphereData>();
  const flowRef = useRef(flow);
  flowRef.current = flow;
  const [nodes, setNodes, onNodesChange] = useNodesState<SphereData>([]);
  const [hover, setHover] = useState<string>();
  const [resetKey, setResetKey] = useState(0);
  const [layoutSettled, setLayoutSettled] = useState(false);
  const nodesInitialized = useNodesInitialized();
  const pendingFit = useRef(false);
  const viewportReady = useRef(false);
  const restoredViewport = useRef<Viewport>();
  const resetRequested = useRef<string>();
  const dragged = useRef(false);
  const stopLayout = useRef(false);
  const positions = useRef<Record<string, LayoutPoint>>({});
  const onInfo = useRef(onInformation);
  onInfo.current = onInformation;
  const onEnter = useRef(onOpen);
  onEnter.current = onOpen;
  const graph = useMemo(() => {
    const graphNodes: Node<SphereData>[] = metas.map((meta) => ({
      id: meta.key,
      type: 'sphere',
      position: { x: 0, y: 0 },
      data: { meta, name: meta.name || meta.id, kind: meta.kind, focused: meta.key === focus },
    }));
    const graphEdges: Edge[] = relations.map((relation) => ({
      id: relation.key,
      source: relation.source,
      target: relation.target,
      data: { relation: relation.relation },
      style: { stroke: 'var(--ui-border-color)', strokeWidth: 1 },
      type: 'smoothstep',
    }));
    records.forEach((record) => {
      const id = recordNodeKey(record.collectionId, record.id);
      graphNodes.push({
        id,
        type: 'sphere',
        position: { x: 0, y: 0 },
        data: { record, name: record.id, kind: 'record', hasSample: samples[id] },
      });
      const collection = metas.find((meta) => meta.kind === 'collection' && meta.id === record.collectionId);
      if (collection)
        graphEdges.push({
          id: `contains:${id}`,
          source: collection.key,
          target: id,
          data: { relation: 'contains' },
          style: { stroke: 'var(--ui-border-color)', strokeWidth: 1 },
        });
    });
    const loaded = new Set(graphNodes.map((node) => node.id));
    return {
      nodes: graphNodes,
      edges: graphEdges.filter((edge) => loaded.has(edge.source) && loaded.has(edge.target)),
    };
  }, [metas, relations, records, samples, focus]);
  const graphRef = useRef(graph);
  graphRef.current = graph;
  const topology = graph.nodes.map((node) => node.id).join('|') + graph.edges.map((edge) => edge.id).join('|');
  useEffect(() => {
    const current = graphRef.current;
    if (resetRequested.current === cacheKey) sessionLayouts.delete(cacheKey);
    const cached = sessionLayouts.get(cacheKey);
    resetRequested.current = undefined;
    const fullyCached = !!cached && current.nodes.every((node) => cached.positions[node.id]);
    const layoutNodes = current.nodes.map((node) => ({
      id: node.id,
      radius: node.data.record ? 24 : 40,
      position: cached?.positions[node.id],
    }));
    let points = initialPositions(layoutNodes, focus);
    positions.current = points;
    stopLayout.current = false;
    viewportReady.current = false;
    setLayoutSettled(false);
    pendingFit.current = !fullyCached || !cached?.viewport;
    restoredViewport.current = fullyCached ? cached?.viewport : undefined;
    const render = () =>
      setNodes(
        current.nodes.map((node) => ({
          ...node,
          position: points[node.id],
          data: {
            ...node.data,
            onInformation: () => onInfo.current(node.data),
            onOpen: () => onEnter.current(node.data),
          },
        }))
      );
    render();
    let frame = 0;
    let ticks = 0;
    const started = performance.now();
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    const advance = () => {
      if (stopLayout.current) return;
      for (let i = 0; i < 3; i += 1) points = layoutStep(layoutNodes, current.edges, points, focus);
      positions.current = points;
      render();
      ticks += 1;
      if (ticks < 100 && performance.now() - started < 1800) frame = requestAnimationFrame(advance);
      else {
        remember(cacheKey, points, fullyCached ? cached?.viewport : undefined);
        setLayoutSettled(true);
      }
    };
    if (fullyCached) {
      setLayoutSettled(true);
    } else if (reduced) {
      for (let i = 0; i < 180; i += 1) points = layoutStep(layoutNodes, current.edges, points, focus);
      positions.current = points;
      render();
      remember(cacheKey, points);
      setLayoutSettled(true);
    } else {
      frame = requestAnimationFrame(advance);
    }
    return () => {
      cancelAnimationFrame(frame);
      const api = flowRef.current;
      if (Object.keys(positions.current).length) {
        // Move-end already captured the live viewport. Never replace it with an
        // uninitialized helper's default during provider teardown.
        const viewport =
          sessionLayouts.get(cacheKey)?.viewport ||
          (viewportReady.current && api.viewportInitialized ? api.getViewport() : undefined);
        remember(cacheKey, positions.current, viewport);
      }
    };
  }, [cacheKey, topology, resetKey, focus, setNodes]);
  // Wait for React Flow to measure the committed nodes, not the setNodes call above.
  // Two frames allow its bounds/store to settle; fitting happens once, never after user movement.
  useEffect(() => {
    if (!layoutSettled || !nodesInitialized || !flow.viewportInitialized || stopLayout.current) return;
    let nextFrame = 0;
    let cancelled = false;
    const frame = requestAnimationFrame(() => {
      nextFrame = requestAnimationFrame(async () => {
        if (stopLayout.current) return;
        const api = flowRef.current;
        const targetViewport = restoredViewport.current;
        if (targetViewport) {
          await api.setViewport(targetViewport);
          restoredViewport.current = undefined;
        } else if (pendingFit.current) {
          pendingFit.current = false;
          await api.fitView({ padding: 0.22, duration: 0 });
        }
        if (cancelled || stopLayout.current) return;
        viewportReady.current = true;
        remember(cacheKey, positions.current, targetViewport ?? api.getViewport());
      });
    });
    return () => {
      cancelled = true;
      cancelAnimationFrame(frame);
      cancelAnimationFrame(nextFrame);
    };
  }, [cacheKey, layoutSettled, nodesInitialized, flow.viewportInitialized]);
  const interruptLayout = () => {
    stopLayout.current = true;
    pendingFit.current = false;
    restoredViewport.current = undefined;
    viewportReady.current = true;
  };
  const highlighted = useMemo(() => {
    if (!hover) return undefined;
    const ids = new Set([hover]);
    graph.edges.forEach((edge) => {
      if (edge.source === hover) ids.add(edge.target);
      if (edge.target === hover) ids.add(edge.source);
    });
    return ids;
  }, [hover, graph.edges]);
  const currentData = new Map(graph.nodes.map((node) => [node.id, node.data]));
  const shownNodes = nodes.map((node) => {
    const data = currentData.get(node.id) || node.data;
    return {
      ...node,
      data: {
        ...data,
        onInformation: () => onInfo.current(data),
        onOpen: () => onEnter.current(data),
        hasSample: data.record ? samples[node.id] : undefined,
        historyFailed,
        faded: !!highlighted && !highlighted.has(node.id),
      },
    };
  });
  const edges = graph.edges.map((edge) => ({
    ...edge,
    label: hover && (edge.source === hover || edge.target === hover) ? t(`relation.${edge.data.relation}`) : undefined,
    style: { ...edge.style, opacity: hover ? (edge.source === hover || edge.target === hover ? 0.8 : 0.12) : 0.25 },
    labelStyle: { fill: 'var(--ui-text-color)', fontSize: 14 },
    labelBgStyle: { fill: 'var(--ui-bg-color)' },
  }));
  const savePositions = useCallback(
    (viewport?: Viewport) => {
      // React Flow emits initialization movement before its helper is ready.
      // Cache only a restored/measured viewport, and use the event's real value.
      if (!viewportReady.current) return;
      const api = flowRef.current;
      const loaded = api.getNodes();
      if (!loaded.length || !api.viewportInitialized) return;
      positions.current = Object.fromEntries(loaded.map((node) => [node.id, node.position]));
      remember(cacheKey, positions.current, viewport ?? api.getViewport());
    },
    [cacheKey]
  );
  return (
    <ReactFlow
      nodes={shownNodes}
      edges={edges}
      nodeTypes={nodeTypes}
      onNodesChange={onNodesChange}
      nodesConnectable={false}
      edgesFocusable={false}
      elementsSelectable={false}
      deleteKeyCode={null}
      minZoom={0.1}
      maxZoom={2}
      zoomOnDoubleClick={false}
      nodeDragThreshold={5}
      onNodeMouseEnter={(_, node) => setHover(node.id)}
      onNodeMouseLeave={() => setHover(undefined)}
      onNodeDragStart={() => {
        interruptLayout();
        dragged.current = true;
      }}
      onNodeDragStop={(_, node) => {
        savePositions();
        // The callback contains the final pointer position before the store's
        // last render. Preserve it instead of caching the penultimate frame.
        positions.current = { ...positions.current, [node.id]: node.position };
        remember(cacheKey, positions.current);
        setTimeout(() => {
          dragged.current = false;
        }, 0);
      }}
      onNodeClick={(_, node) => {
        if (!dragged.current) onOpen(node.data);
      }}
      onMoveEnd={(_, viewport) => savePositions(viewport)}
      onMoveStart={(event) => {
        if (event) interruptLayout();
      }}
      onNodeDoubleClick={() => undefined}
      proOptions={{ hideAttribution: true }}
    >
      <Background color="var(--ui-table-tr-color)" gap={28} size={1} />
      <div className={styles['canvas-controls']}>
        <ComButton
          icon={<ZoomOut />}
          onClick={() => {
            interruptLayout();
            void flow.zoomOut();
          }}
          aria-label={t('zoomOut')}
        />
        <ComButton
          icon={<ZoomIn />}
          onClick={() => {
            interruptLayout();
            void flow.zoomIn();
          }}
          aria-label={t('zoomIn')}
        />
        <ComButton
          icon={<Maximize />}
          onClick={() => {
            interruptLayout();
            void flow.fitView({ padding: 0.22, duration: 200 });
          }}
          aria-label={t('fit')}
        />
        <ComButton
          icon={<Reset />}
          onClick={() => {
            resetRequested.current = cacheKey;
            sessionLayouts.delete(cacheKey);
            setResetKey((value) => value + 1);
          }}
          aria-label={t('reset')}
        />
      </div>
    </ReactFlow>
  );
};
const GraphCanvas: FC<GraphCanvasProps> = (props) => (
  <ReactFlowProvider>
    <Canvas {...props} />
  </ReactFlowProvider>
);
export default GraphCanvas;
