import { useEffect, useMemo, useRef, useState } from 'react';
import { Input, Popover, Spin, type InputRef } from 'antd';
import { Handle, Position, useUpdateNodeInternals, type NodeProps } from 'reactflow';
import {
  Application,
  ChevronDown,
  ChevronRight,
  DataBase,
  Document,
  Folder,
  Flow,
  Plug,
  Search,
  Maximize,
  Minimize,
} from '@carbon/icons-react';
import { useTranslate } from '@/hooks';
import {
  treeRows,
  relationKinds,
  summaryOffset,
  NODE_HEADER_HEIGHT,
  NODE_ROW_HEIGHT,
  type GraphRow,
  type Group,
} from './model';
import styles from './index.module.scss';
import AppEntry from './app-entry';

export interface GroupData {
  group: Group;
  rows: GraphRow[];
  core: boolean;
  compact: boolean;
  bodyHeight: number;
  total: number;
  search: string;
  recordCounts: Record<string, number>;
  loading?: boolean;
  selected?: string;
  related: Set<string>;
  collapsed: Set<string>;
  unavailable: boolean;
  onToggle: (key: string) => void;
  onSelect: (key: string) => void;
  onSearch: (group: Group, search: string) => void;
  onToggleAll: (group: Group, expand: boolean) => void;
  onMagnify: (group: Group) => void;
}
const icons = {
  directory: Folder,
  topic: Document,
  collection: DataBase,
  source: Plug,
  stream: Flow,
  app: Application,
  binding: Flow,
};
export function RowIcon({ row }: { row: GraphRow }) {
  const Icon = icons[row.kind];
  return <Icon size={16} />;
}

export default function GroupNode({ id, data }: NodeProps<GroupData>) {
  const t = useTranslate();
  const update = useUpdateNodeInternals();
  const [scrollTop, setScrollTop] = useState(0);
  const [searchOpen, setSearchOpen] = useState(false);
  const [draft, setDraft] = useState(data.search);
  const composing = useRef(false);
  const searchInput = useRef<InputRef>(null);
  const body = useRef<HTMLDivElement>(null);
  const contentSignature = data.rows.map((row) => row.key).join('|');
  useEffect(() => {
    if (body.current) body.current.scrollTop = 0;
    setScrollTop(0);
  }, [contentSignature]);
  const searchLabel = t('UnsGraph.searchCategory', { category: t(`UnsGraph.${data.group}`) });
  useEffect(() => {
    if (!composing.current) setDraft(data.search);
  }, [data.search]);
  const rows = useMemo(() => treeRows(data.rows, data.collapsed), [data.rows, data.collapsed]);
  const parents = new Set(data.rows.map((row) => row.parentKey).filter(Boolean));
  const allExpanded = [...parents].every((key) => !data.collapsed.has(key!));
  const first = Math.max(0, Math.floor(scrollTop / NODE_ROW_HEIGHT) - 2);
  const windowRows = rows.slice(first, Math.ceil((scrollTop + data.bodyHeight) / NODE_ROW_HEIGHT) + 2);
  const visible = windowRows
    .map((item, index) => ({ ...item, y: (first + index) * NODE_ROW_HEIGHT + NODE_ROW_HEIGHT / 2 - scrollTop }))
    .filter(({ y }) => y >= 0 && y <= data.bodyHeight);
  const signature = visible.map(({ row }) => row.key).join('|');
  useEffect(() => {
    update(id);
    // scrollTop changes handle positions even if the set of visible rows is unchanged.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [signature, scrollTop, id, update, data.bodyHeight]);
  return (
    <section
      className={`${styles['group-node']} ${data.core ? styles['core-node'] : ''} ${data.compact ? styles['compact-node'] : ''}`}
      data-group={data.group}
    >
      <header
        className={`canvas-drag-handle ${styles['group-header']}`}
        onDoubleClick={(event) => {
          if ((event.target as Element).closest('button, input')) return;
          event.stopPropagation();
          data.onMagnify(data.group);
        }}
      >
        <strong>{t(`UnsGraph.${data.group}`)}</strong>
        <span>
          {data.rows.filter((row) => row.kind !== 'binding').length}
          {data.rows.filter((row) => row.kind !== 'binding').length !== data.total && ` / ${data.total}`}
        </span>
        {data.loading && <Spin size="small" />}
        <div className={`${styles['header-actions']} nodrag nopan nowheel`}>
          {data.core && <em>{t('UnsGraph.core')}</em>}
          {data.group === 'namespace' && parents.size > 0 && (
            <button
              type="button"
              className={styles['search-toggle']}
              title={t(allExpanded ? 'UnsGraph.collapseAll' : 'UnsGraph.expandAll')}
              aria-label={t(allExpanded ? 'UnsGraph.collapseAll' : 'UnsGraph.expandAll')}
              onClick={() => data.onToggleAll(data.group, !allExpanded)}
            >
              {allExpanded ? <Minimize size={16} /> : <Maximize size={16} />}
            </button>
          )}
          <Popover
            trigger="click"
            placement="bottomRight"
            open={searchOpen}
            onOpenChange={setSearchOpen}
            afterOpenChange={(open) => {
              if (open) searchInput.current?.focus({ cursor: 'end', preventScroll: true });
            }}
            content={
              <div className={`${styles['node-search']} nodrag nopan nowheel`}>
                <Input
                  ref={searchInput}
                  prefix={<Search size={14} />}
                  allowClear
                  value={draft}
                  placeholder={t('UnsGraph.cardSearch')}
                  aria-label={searchLabel}
                  onCompositionStart={() => {
                    composing.current = true;
                  }}
                  onCompositionEnd={(event) => {
                    composing.current = false;
                    const value = event.currentTarget.value;
                    setDraft(value);
                    data.onSearch(data.group, value);
                  }}
                  onChange={(event) => {
                    const value = event.target.value;
                    setDraft(value);
                    // Keep IME's provisional text local; graph reconciliation must wait for commit.
                    if (!composing.current && !(event.nativeEvent as InputEvent).isComposing) {
                      data.onSearch(data.group, value);
                    }
                  }}
                  onKeyDown={(event) => {
                    event.stopPropagation();
                    if (composing.current || event.nativeEvent.isComposing || event.keyCode === 229) return;
                    if (event.key === 'Escape') {
                      setSearchOpen(false);
                      event.preventDefault();
                    }
                  }}
                />
              </div>
            }
          >
            <button
              type="button"
              className={`${styles['search-toggle']} ${data.search ? styles['search-active'] : ''}`}
              aria-label={searchLabel}
              aria-expanded={searchOpen}
              title={searchLabel}
            >
              <Search size={18} />
            </button>
          </Popover>
        </div>
      </header>
      <div
        className={`${styles['group-rows']} nodrag nopan nowheel`}
        ref={body}
        data-group-scroll
        style={{ height: data.bodyHeight }}
        onScroll={(event) => setScrollTop(event.currentTarget.scrollTop)}
      >
        <div style={{ height: rows.length * NODE_ROW_HEIGHT }}>
          <div style={{ paddingTop: first * NODE_ROW_HEIGHT }}>
            {windowRows.map(({ row, depth, hasChildren }) => (
              <div
                key={row.key}
                data-row-key={row.key}
                className={`${styles['object-row']} ${data.selected === row.key ? styles['selected-row'] : ''} ${data.related.has(row.key) ? styles['related-row'] : ''}`}
                style={{ paddingLeft: 12 + Math.min(depth, 8) * 18 }}
                onClick={() => data.onSelect(row.key)}
              >
                {hasChildren ? (
                  <button
                    type="button"
                    className={styles['tree-toggle']}
                    aria-label={`${t(data.collapsed.has(row.key) ? 'UnsGraph.expand' : 'UnsGraph.collapse')} ${row.name}`}
                    onClick={(event) => {
                      event.stopPropagation();
                      data.onToggle(row.key);
                    }}
                  >
                    {data.collapsed.has(row.key) ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
                  </button>
                ) : (
                  <span className={styles['tree-spacer']} />
                )}
                <button type="button" className={styles['row-button']} title={row.name}>
                  <RowIcon row={row} />
                  <span>
                    <b>{row.name}</b>
                  </span>
                </button>
                {row.kind === 'app' && row.meta?.data.entry?.path && <AppEntry app={row.meta} compact />}
              </div>
            ))}
          </div>
        </div>
        {!rows.length && (
          <p className={styles['empty-group']}>
            {t(data.unavailable ? 'UnsGraph.unavailable' : data.search ? 'UnsGraph.noMatches' : 'UnsGraph.empty')}
          </p>
        )}
      </div>
      {visible.flatMap(({ row, y }) =>
        [Position.Left, Position.Right].flatMap((position) =>
          ['source', 'target'].map((type) => (
            <Handle
              key={`${row.key}:${position}:${type}`}
              id={`${row.key}:${position}:${type}`}
              position={position}
              type={type as 'source' | 'target'}
              isConnectable={false}
              style={{ top: NODE_HEADER_HEIGHT + y }}
              className={styles['row-handle']}
            />
          ))
        )
      )}
      {relationKinds.flatMap((kind) =>
        Object.values(Position).flatMap((position) =>
          ['source', 'target'].map((type) => (
            <Handle
              key={`summary:${kind}:${position}:${type}`}
              id={`summary:${kind}:${position}:${type}`}
              position={position}
              type={type as 'source' | 'target'}
              isConnectable={false}
              style={
                position === Position.Left || position === Position.Right
                  ? { top: `calc(50% + ${summaryOffset(kind)}px)` }
                  : { left: `calc(50% + ${summaryOffset(kind)}px)` }
              }
              className={styles['row-handle']}
            />
          ))
        )
      )}
    </section>
  );
}
