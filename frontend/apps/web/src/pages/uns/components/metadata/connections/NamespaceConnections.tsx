import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { Alert, Empty, Input, Spin } from 'antd';
import { ChevronDown, ChevronRight, GitBranch, Plug, RefreshCw, Search } from 'lucide-react';
import { useTranslate } from '@/hooks';
import ComButton from '@/components/com-button';
import { useIntegrationRoute } from '../../source-stream/IntegrationTabs';
import ConnectNamespacePanel from '../../source-stream/ConnectNamespacePanel';
import { buildConnectModel, resourceKey, sourceKey, streamKey } from '../../source-stream/connect-model';
import { buildConnectNamespace } from '../../source-stream/connect-namespace';
import { useConnectWorkspace } from '../../source-stream/use-connect-workspace';
import { useTreeTargets } from './use-tree-targets';
import styles from './graph.module.scss';

type Point = { x: number; y: number };
// Same cubic path and output chevron as SaaS namespace-connections/ConnectionsView.
function curve(start: Point, end: Point) {
  const bend = (start.x + end.x) / 2;
  return `M ${start.x} ${start.y} C ${bend} ${start.y}, ${bend} ${end.y}, ${end.x} ${end.y}`;
}
const arrowColors = {
  muted: 'var(--ui-description-card-color)',
  connected: 'var(--ui-theme-color)',
  issue: 'var(--ui-error-color)',
};

export default function NamespaceConnections({
  sourceAllowed,
  collectionId,
  metadataAllowed,
  refreshKey,
  onOpenResource,
}: {
  sourceAllowed: boolean;
  collectionId?: string;
  metadataAllowed: boolean;
  refreshKey: number;
  onOpenResource: (id: string) => void;
}) {
  const t = useTranslate();
  const { go } = useIntegrationRoute();
  const [refresh, setRefresh] = useState(0);
  const [search, setSearch] = useState('');
  const [expanded, setExpanded] = useState(new Set<string>());
  const [selection, setSelection] = useState<string | undefined>(
    () => collectionId && resourceKey('metadata', 'collection', collectionId)
  );
  const [hover, setHover] = useState<string>();
  const canvas = useRef<HTMLDivElement>(null);
  const arrowId = useId().replaceAll(':', '');
  const { data, loading } = useConnectWorkspace(sourceAllowed, true, metadataAllowed, refresh + refreshKey);
  const model = useMemo(
    () =>
      buildConnectModel(
        {
          ...data,
          sources: sourceAllowed ? data.sources : [],
          streams: data.streams.filter((stream) => stream.activeVersion > 0),
          resources: metadataAllowed ? data.resources : [],
        },
        'active',
        search
      ),
    [data, search, sourceAllowed, metadataAllowed]
  );
  const namespace = useMemo(
    () => buildConnectNamespace(model.targets, data.resources),
    [model.targets, data.resources]
  );
  const targetKeys = useMemo(() => model.targets.map((target) => target.key), [model.targets]);
  const layout = useTreeTargets(canvas, targetKeys, namespace.parentByKey);
  const groups = useMemo(() => {
    const map = new Map<string, { key: string; name: string; id?: number }[]>();
    const add = (protocol: string, key: string, name: string, id?: number) => {
      const list = map.get(protocol) ?? [];
      list.push({ key, name, id });
      map.set(protocol, list);
    };
    model.sources.forEach((s) => add(s.protocol, sourceKey(s.id), s.name, s.id));
    if (sourceAllowed) model.missingSources.forEach((id) => add('', sourceKey(id), id));
    return [...map].sort(([a], [b]) => a.localeCompare(b));
  }, [model, sourceAllowed]);
  const members = new Map(groups.map(([protocol, rows]) => ['group:' + protocol, new Set(rows.map((row) => row.key))]));
  const lines = useMemo(() => {
    const byStream = new Map<string, typeof model.relations>();
    for (const relation of model.relations) {
      const id = relation.kind === 'write' ? relation.from : relation.to;
      const group = byStream.get(id) ?? [];
      group.push(relation);
      byStream.set(id, group);
    }
    return byStream;
  }, [model]);
  const matches = (key: string, focus?: string) => {
    if (!focus) return false;
    if (key === focus || members.get(focus)?.has(key)) return true;
    let parent = namespace.parentByKey.get(key);
    while (parent) {
      if (parent === focus) return true;
      parent = namespace.parentByKey.get(parent);
    }
    return false;
  };
  const related = (id: string, focus?: string) =>
    id === focus || lines.get(id)?.some((r) => matches(r.from, focus) || matches(r.to, focus));
  const focus = hover ?? selection;
  const sourceIdsFor = (stream: string) =>
    new Set(
      lines
        .get(stream)
        ?.filter((relation) => relation.kind === 'dependency')
        .map((relation) => relation.from)
    );
  const select = (key: string) => {
    setHover(undefined);
    setSelection(key);
    if (key.startsWith('stream:')) {
      const ids = sourceIdsFor(key);
      const protocols = groups.filter(([, rows]) => rows.some((row) => ids.has(row.key))).map(([protocol]) => protocol);
      if (protocols.length) setExpanded((previous) => new Set([...previous, ...protocols]));
    }
  };
  const openStream = (id: number) => go({ view: 'stream', streamId: id });
  useEffect(() => {
    if (!selection?.startsWith('stream:')) return;
    const inputs = lines.get(selection)?.filter((relation) => relation.kind === 'dependency');
    if (!inputs?.length) return;
    const frame = requestAnimationFrame(() => {
      const list = canvas.current?.querySelector<HTMLElement>('[data-source-viewport]');
      if (!list) return;
      const first = [...list.querySelectorAll<HTMLElement>('[data-source-anchor]')].find((row) =>
        inputs.some((relation) => relation.from === row.dataset.sourceAnchor)
      );
      if (first) list.scrollTop += first.getBoundingClientRect().top - list.getBoundingClientRect().top - 44;
    });
    return () => cancelAnimationFrame(frame);
  }, [selection, lines]);
  const highlighted = new Set<string>();
  if (selection)
    for (const [id, relations] of lines) {
      if (!related(id, selection)) continue;
      for (const r of relations) {
        let key: string | undefined = r.kind === 'write' ? r.to : r.from;
        while (key) {
          highlighted.add(key);
          key = namespace.parentByKey.get(key);
        }
      }
    }
  return (
    <section className={styles.workspace}>
      <div className={styles.toolbar}>
        <Input
          allowClear
          prefix={<Search size={15} />}
          aria-label={t('SourceStream.search')}
          placeholder={t('SourceStream.search')}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <ComButton
          type="text"
          aria-label={t('SourceStream.refresh')}
          icon={<RefreshCw size={15} />}
          onClick={() => setRefresh((v) => v + 1)}
        />
      </div>
      {!!data.unavailable.length && <Alert showIcon type="warning" message={t('UnsGraph.partial')} />}
      {!!data.truncated.length && (
        <Alert showIcon type="warning" message={t('UnsConnect.truncated', { limit: 2000 })} />
      )}
      <div className={styles.scroller}>
        <div
          className={styles.canvas}
          ref={canvas}
          data-tree={metadataAllowed}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              setSelection(undefined);
              setHover(undefined);
            }
          }}
        >
          <section className={styles.sources}>
            <header>
              {t('SourceStream.source')} <span>{model.sources.length}</span>
            </header>
            <div className={styles.scrollArea} data-source-viewport>
              {groups.map(([protocol, rows]) => {
                const key = 'group:' + protocol;
                const open = !!search.trim() || expanded.has(protocol);
                return (
                  <div key={key} className={styles.protocolGroup}>
                    <button
                      className={styles.protocolHeader}
                      aria-expanded={open}
                      data-source-anchor={open ? undefined : key}
                      data-source-ids={open ? undefined : rows.map((row) => row.key).join(',')}
                      onMouseEnter={() => setHover(key)}
                      onMouseLeave={() => setHover(undefined)}
                      onClick={() =>
                        setExpanded((prev) => {
                          const next = new Set(prev);
                          if (open) next.delete(protocol);
                          else next.add(protocol);
                          return next;
                        })
                      }
                    >
                      {open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                      <span>{protocol.toUpperCase() || t('UnsConnect.unavailableRef')}</span>
                      <small>{rows.length}</small>
                    </button>
                    {open &&
                      rows.map((row) => (
                        <button
                          key={row.key}
                          className={styles.sourceRow}
                          data-source-anchor={row.key}
                          data-source-ids={row.key}
                          aria-pressed={selection === row.key}
                          onMouseEnter={() => setHover(row.key)}
                          onMouseLeave={() => setHover(undefined)}
                          onClick={() => select(row.key)}
                          onDoubleClick={() => row.id && go({ view: 'source', sourceId: row.id })}
                          onKeyDown={(e) => {
                            if (e.key === 'Enter' && row.id) go({ view: 'source', sourceId: row.id });
                          }}
                        >
                          <Plug size={14} />
                          <span>{row.name}</span>
                        </button>
                      ))}
                  </div>
                );
              })}
            </div>
          </section>
          <svg
            className={styles.lines}
            viewBox={`0 0 1000 ${layout.height}`}
            preserveAspectRatio="none"
            aria-hidden="true"
          >
            <defs>
              {Object.entries(arrowColors).map(([tone, color]) => (
                <marker
                  key={tone}
                  id={`${arrowId}-${tone}`}
                  viewBox="0 0 8 8"
                  refX="7"
                  refY="4"
                  markerUnits="userSpaceOnUse"
                  markerWidth="5"
                  markerHeight="7"
                  orient="auto"
                >
                  <path
                    d="M 2 1 L 7 4 L 2 7"
                    fill="none"
                    stroke={color}
                    strokeWidth="1.3"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                </marker>
              ))}
            </defs>
            {model.streams.map((stream) => {
              const key = streamKey(stream.id),
                anchor = layout.streams[key];
              if (!anchor) return null;
              const active = !!focus && related(key, focus);
              const issue = ['source-watch-failed', 'source-unhealthy', 'needs-redeploy'].includes(
                stream.executionState || ''
              );
              const connected =
                stream.status === 'enabled' &&
                stream.executionState !== 'source-disabled' &&
                stream.executionState !== 'starting';
              const seen = new Set<string>();
              return (
                <g
                  key={key}
                  opacity={focus && !active ? 0.5 : 1}
                  onMouseEnter={() => setHover(key)}
                  onMouseLeave={() => setHover(undefined)}
                  onClick={() => select(key)}
                  onDoubleClick={() => openStream(stream.id)}
                >
                  {lines.get(key)?.map((r) => {
                    const input = r.kind === 'dependency';
                    const other = r.kind === 'write' ? r.to : r.from;
                    const endpoint = input ? layout.sources[other] : layout.targets[other];
                    if (!endpoint) return null;
                    const segment = r.kind + ':' + endpoint.id;
                    if (seen.has(segment)) return null;
                    seen.add(segment);
                    const unrelated =
                      input && focus?.startsWith('source:') && endpoint.id !== layout.sources[focus]?.id;
                    const lit = active && !unrelated;
                    const tone = issue ? 'issue' : lit && connected ? 'connected' : 'muted';
                    const path = input
                      ? curve(endpoint, anchor.left)
                      : r.kind === 'write'
                        ? curve(anchor.right, endpoint)
                        : curve(endpoint, anchor.right);
                    return (
                      <g key={segment} color={arrowColors[tone]} opacity={unrelated ? 0.5 : 1}>
                        <path
                          d={path}
                          className={styles.line}
                          strokeDasharray={issue ? '5 4' : connected ? undefined : '3 5'}
                          opacity={lit ? 1 : 0.45}
                          markerEnd={input ? undefined : `url(#${arrowId}-${tone})`}
                        />
                        <path d={path} className={styles.hit} />
                      </g>
                    );
                  })}
                </g>
              );
            })}
          </svg>
          <section className={styles.streams}>
            <header>
              {t('SourceStream.stream')} <span>{model.streams.length}</span>
            </header>
            <div className={styles.scrollArea} data-stream-viewport>
              {model.streams.map((stream) => {
                const key = streamKey(stream.id);
                return (
                  <button
                    key={key}
                    data-flow-stream={key}
                    className={styles.streamCard}
                    aria-pressed={selection === key}
                    data-related={!!selection && !!related(key, selection)}
                    onMouseEnter={() => setHover(key)}
                    onMouseLeave={() => setHover(undefined)}
                    onClick={() => select(key)}
                    onDoubleClick={() => openStream(stream.id)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') openStream(stream.id);
                    }}
                  >
                    <GitBranch size={12} />
                    <span>{stream.name}</span>
                  </button>
                );
              })}
            </div>
          </section>
          {metadataAllowed && (
            <div
              className={styles.treePanel}
              onMouseOver={(e) =>
                setHover((e.target as Element).closest<HTMLElement>('[data-connect-anchor]')?.dataset.connectAnchor)
              }
              onMouseLeave={() => setHover(undefined)}
            >
              <ConnectNamespacePanel
                targets={model.targets}
                resources={data.resources}
                highlighted={highlighted}
                onSelect={select}
                onOpen={(target) =>
                  target.space === 'legacy' ? go({ view: 'namespace', metaId: target.id }) : onOpenResource(target.id)
                }
              />
            </div>
          )}
          {loading ? (
            <div className={styles.empty}>
              <Spin />
            </div>
          ) : (
            !model.sources.length &&
            !model.streams.length && (
              <div className={styles.empty}>
                <Empty description={t('SourceStream.noObjects')} />
              </div>
            )
          )}
        </div>
      </div>
    </section>
  );
}
