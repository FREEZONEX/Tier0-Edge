import { Fragment, useState, type ReactNode, type CSSProperties, type PointerEvent } from 'react';
import { Empty, Pagination, Spin } from 'antd';
import { ChevronDown, ChevronRight, ArrowDown, ArrowUp } from '@carbon/icons-react';
import { useTranslate } from '@/hooks';
import styles from './connect-list.module.scss';

export type ConnectColumn<T> = {
  key?: string;
  dataIndex?: keyof T;
  title: ReactNode;
  width?: number;
  render?: (value: never, row: T) => ReactNode;
};
// Port of Cloud ConnectGroupTable: one header, collapsible page-local groups, resizable columns.
export default function ConnectList<T extends { id: number; name: string; updatedAt?: string | number }>({
  rows,
  columns,
  loading,
  groupBy,
  groupLabel,
  cards,
  activeId,
  onOpen,
  pagination,
  empty,
}: {
  rows: T[];
  columns: ConnectColumn<T>[];
  loading: boolean;
  groupBy: (row: T) => string;
  groupLabel: (key: string) => string;
  cards?: boolean;
  activeId?: number;
  onOpen: (row: T) => void;
  pagination: { page: number; pageSize: number; total: number; onChange: (page: number, size: number) => void };
  empty: ReactNode;
}) {
  const t = useTranslate();
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({ disabled: true });
  const [sort, setSort] = useState({ key: 'status', desc: false });
  const [widths, setWidths] = useState<Record<number, number>>({});
  const priority = ['unhealthy', 'healthy', 'unverified', 'enabled', 'draft', 'disabled'];
  const ordered = [...rows].sort((a, b) => {
    const value =
      sort.key === 'name'
        ? a.name.localeCompare(b.name)
        : sort.key === 'updatedAt'
          ? new Date(b.updatedAt || 0).getTime() - new Date(a.updatedAt || 0).getTime()
          : priority.indexOf(groupBy(a)) - priority.indexOf(groupBy(b));
    return sort.desc ? -value : value;
  });
  const groups = new Map<string, T[]>();
  ordered.forEach((row) => {
    const key = groupBy(row);
    groups.set(key, [...(groups.get(key) || []), row]);
  });
  const cell = (column: ConnectColumn<T>, row: T) =>
    column.render
      ? column.render((column.dataIndex ? row[column.dataIndex] : undefined) as never, row)
      : String(column.dataIndex ? (row[column.dataIndex] ?? '—') : '');
  const grid: CSSProperties = {
    gridTemplateColumns: columns
      .map((column, index) =>
        widths[index]
          ? `${widths[index]}px`
          : index === 0
            ? 'minmax(210px, 1.6fr)'
            : index === columns.length - 1
              ? '48px'
              : `minmax(110px, ${(column.width || 150) / 180}fr)`
      )
      .join(' '),
  };
  const resize = (index: number) => (event: PointerEvent<HTMLSpanElement>) => {
    event.preventDefault();
    event.stopPropagation();
    const handle = event.currentTarget,
      start = event.clientX,
      width = handle.parentElement!.getBoundingClientRect().width;
    handle.setPointerCapture(event.pointerId);
    const move = (next: globalThis.PointerEvent) =>
      setWidths((previous) => ({ ...previous, [index]: Math.max(index ? 90 : 180, width + next.clientX - start) }));
    const stop = () => {
      handle.removeEventListener('pointermove', move);
      handle.removeEventListener('pointerup', stop);
      handle.removeEventListener('pointercancel', stop);
    };
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', stop);
    handle.addEventListener('pointercancel', stop);
  };
  return (
    <Spin spinning={loading}>
      <div className={styles.viewport}>
        {!rows.length ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={empty} />
        ) : (
          <div role={cards ? undefined : 'table'} className={cards ? styles.cards : styles.table}>
            {!cards && (
              <div role="row" className={styles.heading} style={grid}>
                {columns.map((column, index) => {
                  const key =
                    column.dataIndex === 'name' || column.dataIndex === 'updatedAt'
                      ? String(column.dataIndex)
                      : undefined;
                  return (
                    <div role="columnheader" key={index}>
                      {key ? (
                        <button
                          onClick={() => setSort((previous) => ({ key, desc: previous.key === key && !previous.desc }))}
                        >
                          {column.title}
                          {sort.key === key && (sort.desc ? <ArrowDown size={12} /> : <ArrowUp size={12} />)}
                        </button>
                      ) : (
                        column.title
                      )}
                      {index < columns.length - 1 && (
                        <span
                          role="separator"
                          aria-label={String(column.title)}
                          aria-orientation="vertical"
                          className={styles.resize}
                          onPointerDown={resize(index)}
                          onDoubleClick={() =>
                            setWidths((previous) => {
                              const next = { ...previous };
                              delete next[index];
                              return next;
                            })
                          }
                        />
                      )}
                    </div>
                  );
                })}
              </div>
            )}
            {[...groups].map(([key, items]) => (
              <Fragment key={key}>
                {key && (
                  <button
                    className={styles.group}
                    aria-expanded={!collapsed[key]}
                    onClick={() => setCollapsed((previous) => ({ ...previous, [key]: !previous[key] }))}
                  >
                    {collapsed[key] ? <ChevronRight size={14} /> : <ChevronDown size={14} />}
                    <span>{groupLabel(key)}</span>
                    <small>{items.length}</small>
                  </button>
                )}
                {(!key || !collapsed[key]) && (
                  <div role={cards ? undefined : 'rowgroup'} className={cards ? styles['card-grid'] : undefined}>
                    {items.map((row) => (
                      <div
                        role={cards ? 'article' : 'row'}
                        key={row.id}
                        className={`${cards ? styles.card : styles.row} ${row.id === activeId ? styles.active : ''}`}
                        style={cards ? undefined : grid}
                        onClick={() => onOpen(row)}
                      >
                        {columns.map((column, index) => (
                          <div
                            role={cards ? undefined : 'cell'}
                            className={index === columns.length - 1 ? styles.actions : undefined}
                            key={index}
                            onClick={index === columns.length - 1 ? (event) => event.stopPropagation() : undefined}
                          >
                            {cell(column, row)}
                          </div>
                        ))}
                      </div>
                    ))}
                  </div>
                )}
              </Fragment>
            ))}
          </div>
        )}
      </div>
      <div className={styles.pagination}>
        <Pagination
          size="small"
          current={pagination.page}
          pageSize={pagination.pageSize}
          total={pagination.total}
          onChange={pagination.onChange}
          showSizeChanger={false}
          hideOnSinglePage
          aria-label={t('ConnectList.pagination')}
        />
      </div>
    </Spin>
  );
}
