import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Alert, Empty, Input, Segmented, Space, Spin } from 'antd';
import { useSize } from 'ahooks';
import { Add, List, Renew, Search } from '@carbon/icons-react';
import ReactFlow, { Background, Controls, MiniMap, useNodesState, type Edge, type Node } from 'reactflow';
import 'reactflow/dist/style.css';
import ComButton from '@/components/com-button';
import HelpTooltip from '@/components/help-tooltip';
import { useTranslate } from '@/hooks';
import { ButtonPermission } from '@/common-types/button-permission';
import { hasPermission } from '@/utils/auth';
import { useConnectWorkspace } from './use-connect-workspace';
import { useIntegrationRoute } from './IntegrationTabs';
import { ConnectMissingSource, ConnectSourceGroup, ConnectStreamNode, type SourceGroupData } from './ConnectNodes';
import {
  buildConnectModel,
  connectCategories,
  sourceKey,
  streamKey,
  type ConnectBasis,
  type ConnectTarget,
} from './connect-model';
import ConnectNamespacePanel from './ConnectNamespacePanel';
import ConnectCables from './ConnectCables';
import { buildConnectNamespace } from './connect-namespace';
import { indexSourceStreams, sourceCardLayout, sourceGroupHeight, sourceGroupLayoutKey } from './connect-source';
import styles from './connect-workspace.module.scss';

const nodeTypes = {
  sourceGroup: ConnectSourceGroup,
  stream: ConnectStreamNode,
  missingSource: ConnectMissingSource,
};
const MIN_HORIZONTAL_CANVAS_WIDTH = 720;

export default function ConnectWorkspace({
  sourceAllowed,
  streamAllowed,
  metadataAllowed,
  refreshKey,
  onList,
}: {
  sourceAllowed: boolean;
  streamAllowed: boolean;
  metadataAllowed: boolean;
  refreshKey: number;
  onList: () => void;
}) {
  const t = useTranslate();
  const route = useIntegrationRoute();
  const { go } = route;
  const [refresh, setRefresh] = useState(0);
  const [basis, setBasis] = useState<ConnectBasis>('active');
  const [search, setSearch] = useState('');
  const [collapsed, setCollapsed] = useState(new Set<string>());
  const [expanded, setExpanded] = useState(new Set<number>());
  const [selected, setSelected] = useState<string>();
  const [focusedResource, setFocusedResource] = useState<string>();
  const stage = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLDivElement>(null);
  const canvasSize = useSize(canvas);
  const compact = (canvasSize?.width ?? MIN_HORIZONTAL_CANVAS_WIDTH) < MIN_HORIZONTAL_CANVAS_WIDTH;
  const { data, loading } = useConnectWorkspace(sourceAllowed, streamAllowed, metadataAllowed, refresh + refreshKey);
  const openSource = useCallback((id: number) => go({ view: 'source', sourceId: id }), [go]);
  const openStream = useCallback((id: number) => go({ view: 'stream', streamId: id }), [go]);
  const canManageStreams = streamAllowed && hasPermission(ButtonPermission['uns.streamManage']);
  const canManageSource =
    sourceAllowed &&
    (hasPermission(ButtonPermission['uns.sourceManage']) || hasPermission(ButtonPermission['uns.sourceCredentials']));
  const manageSource = useCallback(
    (id: number) => {
      if (
        sourceAllowed &&
        (hasPermission(ButtonPermission['uns.sourceManage']) ||
          hasPermission(ButtonPermission['uns.sourceCredentials']))
      )
        go({ view: 'source', sourceId: id, tab: 'connection' });
    },
    [sourceAllowed, go]
  );
  const createStream = useCallback(
    (id: number) => {
      if (streamAllowed && hasPermission(ButtonPermission['uns.streamManage']))
        go({ view: 'stream', sourceId: id, create: 'stream' });
    },
    [streamAllowed, go]
  );
  const openProgram = useCallback(
    (id: number) => {
      if (streamAllowed && hasPermission(ButtonPermission['uns.streamManage']))
        go({ view: 'stream', streamId: id, edit: 'stream' });
    },
    [streamAllowed, go]
  );
  const expandSource = useCallback((id: number) => {
    setExpanded((previous) => {
      const next = new Set(previous);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);
  const openResource = useCallback(
    (target: ConnectTarget) => {
      if (target.missing) return;
      go(
        target.kind === 'collection'
          ? { view: 'collections', collectionId: target.id }
          : { view: 'namespace', metaId: target.id }
      );
    },
    [go]
  );
  const model = useMemo(
    () =>
      buildConnectModel(
        {
          ...data,
          sources: sourceAllowed ? data.sources : [],
          streams: streamAllowed ? data.streams : [],
          resources: metadataAllowed ? data.resources : [],
        },
        basis,
        search
      ),
    [data, basis, search, sourceAllowed, streamAllowed, metadataAllowed]
  );
  const namespace = useMemo(
    () => buildConnectNamespace(model.targets, data.resources),
    [model.targets, data.resources]
  );
  const sourceStreams = useMemo(() => indexSourceStreams(model.streams, basis), [model.streams, basis]);
  const streamsComplete = !data.unavailable.includes('streams') && !data.truncated.includes('streams');
  const geometry = useMemo(() => {
    const nodes: Node[] = [];
    const endpoints = new Map<string, { node: string; source?: string; target?: string }>();
    const layouts: string[] = [];
    let y = 24;
    for (const category of connectCategories) {
      const sources = model.sources.filter((source) => source.category === category);
      if (!sources.length) continue;
      const id = `group:${category}`;
      const closed = collapsed.has(category);
      const heights = sources.map(
        (source) =>
          sourceCardLayout(
            sourceStreams.get(String(source.id))?.length || 0,
            expanded.has(source.id),
            streamAllowed,
            canManageStreams
          ).height
      );
      const layoutKey = sourceGroupLayoutKey(
        sources.map((source) => source.id),
        heights,
        closed
      );
      layouts.push(`${id}:${layoutKey}`);
      const group: SourceGroupData = {
        category,
        sources,
        collapsed: closed,
        onOpen: openSource,
        streams: sourceStreams,
        expanded,
        readable: streamAllowed,
        complete: streamsComplete,
        layoutKey,
        onExpand: expandSource,
        onManage: canManageSource ? manageSource : undefined,
        onCreate: canManageStreams ? createStream : undefined,
        onOpenStream: openStream,
        onEditStream: canManageStreams ? openProgram : undefined,
        onToggle: () =>
          setCollapsed((previous) => {
            const next = new Set(previous);
            if (next.has(category)) next.delete(category);
            else next.add(category);
            return next;
          }),
      };
      nodes.push({ id, type: 'sourceGroup', position: { x: 24, y }, data: group });
      y += sourceGroupHeight(heights, closed) + 24;
      for (const source of sources) endpoints.set(sourceKey(source.id), { node: id, source: sourceKey(source.id) });
    }
    for (const id of model.missingSources) {
      const key = sourceKey(id);
      nodes.push({ id: key, type: 'missingSource', position: { x: 24, y }, data: { id } });
      endpoints.set(key, { node: key, source: key });
      y += 104;
    }
    model.streams.forEach((stream, index) => {
      const key = streamKey(stream.id);
      nodes.push({
        id: key,
        type: 'stream',
        position: compact ? { x: 24, y: y + index * 156 } : { x: 410, y: 24 + index * 156 },
        data: { stream, basis, stacked: compact, onOpen: openStream },
      });
      endpoints.set(key, { node: key, source: 'output', target: 'input' });
    });
    const edges: Edge[] = model.relations.flatMap((relation) => {
      if (relation.kind !== 'dependency') return [];
      const from = endpoints.get(relation.from),
        to = endpoints.get(relation.to);
      if (!from || !to) return [];
      return [
        {
          id: relation.id,
          source: from.node,
          target: to.node,
          sourceHandle: from.source,
          targetHandle: to.target,
          // 窄区用折线和统一图例，不让重复依赖文字压在 Source 卡片上。
          type: compact ? 'smoothstep' : 'default',
          pathOptions: compact ? { offset: 16, borderRadius: 8 } : undefined,
          label: compact ? undefined : t(`UnsConnect.relation.${relation.kind}`),
          ariaLabel: `${t('UnsConnect.relation.dependency')}: ${relation.from} → ${relation.to}`,
          style: {
            stroke: 'var(--ui-theme-color)',
            strokeWidth: selected === relation.id ? 3 : 1.5,
            strokeDasharray: relation.kind === 'dependency' ? '5 4' : undefined,
          },
          labelStyle: { fill: 'var(--ui-text-color)', fontSize: 12 },
          labelBgStyle: { fill: 'var(--ui-bg-color)' },
          data: { streamId: relation.to },
        },
      ];
    });
    return { nodes, edges, sourceBottom: y, layoutKey: layouts.join('|') };
  }, [
    model,
    basis,
    collapsed,
    expanded,
    sourceStreams,
    streamsComplete,
    streamAllowed,
    canManageStreams,
    canManageSource,
    expandSource,
    manageSource,
    createStream,
    openProgram,
    openSource,
    openStream,
    selected,
    t,
    compact,
  ]);
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const layoutKey = JSON.stringify([basis, search, compact, geometry.layoutKey]);
  const previousLayout = useRef(layoutKey);
  useEffect(() => {
    const retainPositions = previousLayout.current === layoutKey;
    previousLayout.current = layoutKey;
    setNodes((previous) =>
      geometry.nodes.map((node) => ({
        ...node,
        position: (retainPositions && previous.find((item) => item.id === node.id)?.position) || node.position,
      }))
    );
  }, [geometry.nodes, setNodes, layoutKey]);
  const selectedRelation = model.relations.find((relation) => relation.id === selected);
  const selectedStream =
    selectedRelation &&
    model.streams.find(
      (stream) =>
        streamKey(stream.id) === (selectedRelation.kind === 'write' ? selectedRelation.from : selectedRelation.to)
    );
  const selectedTarget =
    selectedRelation &&
    model.targets.find(
      (target) => target.key === (selectedRelation.kind === 'write' ? selectedRelation.to : selectedRelation.from)
    );
  const highlightedResources = useMemo(() => {
    const keys = new Set<string>();
    let key =
      focusedResource ||
      (selectedRelation?.kind === 'write'
        ? selectedRelation.to
        : selectedRelation?.kind === 'read'
          ? selectedRelation.from
          : undefined);
    while (key && !keys.has(key)) {
      keys.add(key);
      key = namespace.parentByKey.get(key);
    }
    return keys;
  }, [focusedResource, selectedRelation, namespace]);
  const editStream = (id: number, editable: boolean) => {
    if (editable && canManageStreams) openProgram(id);
    else openStream(id);
  };
  return (
    <section className={styles.workspace}>
      <div className={styles.toolbar}>
        <Input
          allowClear
          prefix={<Search size={16} />}
          value={search}
          aria-label={t('SourceStream.search')}
          placeholder={t('SourceStream.search')}
          onChange={(event) => setSearch(event.target.value)}
        />
        <Segmented
          value={basis}
          aria-label={t('UnsConnect.basis')}
          options={[
            { value: 'active', label: t('UnsConnect.active') },
            { value: 'draft', label: t('SourceStream.status.draft') },
          ]}
          onChange={(value) => setBasis(value as ConnectBasis)}
        />
        <HelpTooltip title={t('UnsConnect.graphHint')} />
        <Space wrap className={styles.actions}>
          <ComButton type="text" icon={<List size={18} />} onClick={onList}>
            {t('SourceRegistry.allSources')}
          </ComButton>
          <ComButton
            icon={<Renew size={18} />}
            aria-label={t('SourceStream.refresh')}
            onClick={() => setRefresh((value) => value + 1)}
          />
          <ComButton
            auth={ButtonPermission['uns.sourceManage']}
            type="primary"
            icon={<Add size={18} />}
            onClick={() => route.go({ view: 'source', create: 'source' })}
          >
            {t('SourceStream.addSource')}
          </ComButton>
        </Space>
      </div>
      {!!data.unavailable.length && <Alert showIcon type="warning" message={t('UnsGraph.partial')} />}
      {!!data.truncated.length && (
        <Alert showIcon type="warning" message={t('UnsConnect.truncated', { limit: 2000 })} />
      )}
      <div className={styles.legend}>
        <span>
          {t('UnsConnect.counts', {
            sources: model.sources.length,
            streams: model.streams.length,
            targets: model.targets.length,
          })}
        </span>
        <span>{t('UnsConnect.legend')}</span>
      </div>
      <div ref={stage} className={styles.stage} aria-label={t('UnsConnect.workspace')}>
        <div ref={canvas} className={styles.canvas} data-connect-canvas>
          <Spin spinning={loading}>
            {!loading && !nodes.length ? (
              <Empty description={t('SourceStream.noObjects')} />
            ) : (
              <ReactFlow
                nodes={nodes}
                edges={geometry.edges}
                nodeTypes={nodeTypes}
                onNodesChange={onNodesChange}
                nodesConnectable={false}
                elementsSelectable
                nodesDraggable
                defaultViewport={{ x: 0, y: 0, zoom: 1 }}
                minZoom={0.2}
                maxZoom={2}
                onEdgeClick={(_, edge) => setSelected(edge.id)}
                onEdgeDoubleClick={(_, edge) => {
                  const key = edge.data?.streamId as string | undefined;
                  const stream = model.streams.find((item) => streamKey(item.id) === key);
                  if (stream) editStream(stream.id, stream.editable);
                }}
                onPaneClick={() => {
                  setSelected(undefined);
                  setFocusedResource(undefined);
                }}
                proOptions={{ hideAttribution: true }}
              >
                <Background gap={20} color="var(--ui-line-color)" />
                <Controls position={compact ? 'top-right' : 'bottom-left'} showInteractive={false} />
                <MiniMap pannable zoomable nodeColor="var(--ui-fill-secondary)" maskColor="var(--ui-fill-tertiary)" />
              </ReactFlow>
            )}
          </Spin>
          {selectedStream && (
            <aside className={styles.inspector}>
              <strong>{selectedStream.name}</strong>
              <span>{t(`UnsConnect.relation.${selectedRelation!.kind}`)}</span>
              {selectedTarget && <span title={selectedTarget.id}>{selectedTarget.name}</span>}
              <ComButton onClick={() => openStream(selectedStream.id)}>{t('common.detail')}</ComButton>
              {selectedStream.editable && (
                <ComButton
                  auth={ButtonPermission['uns.streamManage']}
                  onClick={() => editStream(selectedStream.id, true)}
                >
                  {t('SourceStream.editProgram')}
                </ComButton>
              )}
              <ComButton type="text" onClick={() => setSelected(undefined)}>
                {t('common.close')}
              </ComButton>
            </aside>
          )}
        </div>
        {metadataAllowed && (
          <ConnectNamespacePanel
            targets={model.targets}
            resources={data.resources}
            highlighted={highlightedResources}
            onSelect={(key) => {
              setFocusedResource(key);
              setSelected(undefined);
            }}
            onOpen={openResource}
          />
        )}
        <ConnectCables
          stage={stage}
          relations={model.relations}
          parentByKey={namespace.parentByKey}
          streams={model.streams}
          targets={model.targets}
          selected={selected}
          focusedResource={focusedResource}
          onSelect={(id) => {
            setSelected(id);
            setFocusedResource(undefined);
          }}
          onEdit={editStream}
        />
      </div>
    </section>
  );
}
