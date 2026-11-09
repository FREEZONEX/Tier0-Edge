import {
  useState,
  useRef,
  useEffect,
  memo,
  type FC,
  useMemo,
  isValidElement,
  type MouseEvent,
  type ReactNode,
} from 'react';
import { Button, Dropdown, Table } from 'antd';
import type { TableColumnsType } from 'antd';
import classNames from 'classnames';
import useTranslate from '@/hooks/useTranslate.ts';
import { useThemeStore } from '@/stores/theme-store.ts';
import { useI18nStore } from '@/stores/i18n-store.ts';
import { hasPermission } from '@/utils/auth.ts';
import { commonLabelRender } from '../operation-buttons/utils.tsx';
import expandIcon from '@/assets/icons/expand.svg';
import collapseIcon from '@/assets/icons/collapse.svg';
import { ResizableTitle } from './ResizableTitle.tsx';
import type { ATableProps } from './types.ts';
import { fitColumnsToContainer, getColumnMax, getColumnMin, SELECTION_COL_WIDTH } from './column-fit.ts';
import './index.scss';
import { Pin } from '@/components/lucide-icon/carbon';
import { OverflowMenuHorizontal } from '@/components/lucide-icon/carbon';
import { ChevronRight } from 'lucide-react';
import ComButton from '../com-button';

const resolveOperationIcon = (icon?: ReactNode, extra?: ReactNode): ReactNode => {
  if (icon) return icon;
  if (!extra || !isValidElement(extra)) return extra;
  const { children } = extra.props as { children?: ReactNode };
  if (children != null && children !== false) {
    return children;
  }
  return extra;
};

const colorObj: any = {
  blue: {
    light: '#E8F1FF',
    dark: '#061833',
  },
  chartreuse: {
    light: '#F0FBD2',
    dark: '#242F06',
  },
};

const ProTable: FC<ATableProps> = ({
  resizeable = true,
  columnFit = true,
  columns,
  components,
  scroll,
  className,
  hiddenEmpty,
  pagination,
  fixedPosition,
  showExpand,
  rowNavigationIndicator = true,
  rowNavigationColumnKey,
  wrapperStyle,
  ...restProps
}) => {
  const { theme, primaryColor } = useThemeStore((state) => ({
    theme: state.theme,
    primaryColor: state.primaryColor,
  }));
  const selectBgColor = colorObj?.[primaryColor]?.[theme];
  const [resizeColumns, setResizeColumns] = useState<TableColumnsType>(columns);
  const [layoutWidth, setLayoutWidth] = useState(0);
  const tableWrapRef = useRef<HTMLDivElement>(null);
  const containerWidthRef = useRef(0);
  const selectionWidth = restProps?.rowSelection ? SELECTION_COL_WIDTH : 0;
  const useColumnFit = Boolean(resizeable && columnFit);

  const [isExpanded, setIsExpanded] = useState<boolean>(false);
  const [showAllColumns, setShowAllColumns] = useState<boolean>(true);

  const newPagination = pagination
    ? {
        total: pagination?.total,
        pageSize: 20,
        showQuickJumper: true,
        ...pagination,
        showTotal: undefined,
      }
    : pagination;

  const calculateEffectiveWidth = (cols: TableColumnsType) => {
    return cols.reduce((sum, col) => {
      let width = typeof col.width === 'number' ? col.width : 0;
      if (col.fixed) width += 2;
      return sum + width;
    }, 0);
  };

  const findFlexColumn = (cols: TableColumnsType) => {
    for (let i = cols.length - 1; i >= 0; i--) {
      if (!cols[i].fixed) return i;
    }
    return Math.max(0, cols.length - 2);
  };

  const balanceColumns = (cols: TableColumnsType, changedIndex?: number) => {
    const containerWidth = containerWidthRef.current;
    if (!containerWidth) return cols;
    const SCROLLBAR_WIDTH = 17;
    const totalWidth = calculateEffectiveWidth(cols);
    const delta = containerWidth - totalWidth - SCROLLBAR_WIDTH - (restProps?.rowSelection ? 35 : 0);

    if (delta > 0) {
      const targetIndex = findFlexColumn(cols);
      const newColumns = [...cols];
      newColumns[targetIndex] = {
        ...newColumns[targetIndex],
        width: (newColumns[targetIndex].width as number) + delta,
      };
      return newColumns;
    }

    if (changedIndex !== undefined && cols[changedIndex]?.fixed) {
      const newColumns = [...cols];
      const nextIndex = changedIndex + 1;
      if (nextIndex < cols.length && !cols[nextIndex].fixed) {
        newColumns[nextIndex] = {
          ...newColumns[nextIndex],
          width: (newColumns[nextIndex].width as number) - delta,
        };
      }
      return newColumns;
    }
    return cols;
  };

  const applyFit = (cols: TableColumnsType, resize?: { index: number; width: number }) => {
    const width = tableWrapRef.current?.clientWidth ?? layoutWidth;
    if (!width) return cols;
    return fitColumnsToContainer(cols, width, selectionWidth, resize);
  };

  const handleResize = (index: number) => (width?: number) => {
    if (!width || !tableWrapRef.current) return;

    if (useColumnFit) {
      setResizeColumns((prev) => applyFit(prev, { index, width }));
      return;
    }

    const newColumns = [...resizeColumns];
    newColumns[index] = { ...newColumns[index], width };
    const balancedColumns = balanceColumns(newColumns, index);

    balancedColumns.forEach((col) => {
      if (col.fixed) {
        const selector = col.fixed === 'right' ? '.ant-table-cell-fix-right' : '.ant-table-cell-fix-left';
        document.querySelectorAll(selector).forEach((el) => {
          const cell = el as HTMLElement;
          if (cell.textContent === col.title?.toString()) {
            cell.style.width = `${col.width}px`;
          }
        });
      }
    });

    setResizeColumns(balancedColumns);
  };

  useEffect(() => {
    const observer = new ResizeObserver((entries) => {
      const width = entries[0]?.contentRect.width ?? 0;
      if (!width) return;

      if (useColumnFit) {
        setLayoutWidth(width);
        setResizeColumns((prev) => fitColumnsToContainer(prev, width, selectionWidth));
        return;
      }

      containerWidthRef.current = width;
      setResizeColumns((prev) => balanceColumns(prev));
    });

    if (tableWrapRef.current) {
      observer.observe(tableWrapRef.current);
      const width = tableWrapRef.current.clientWidth;
      if (useColumnFit) {
        setLayoutWidth(width);
        setResizeColumns((prev) => fitColumnsToContainer(prev, width, selectionWidth));
      } else {
        containerWidthRef.current = width;
        setResizeColumns((prev) => balanceColumns(prev));
      }
    }
    return () => observer.disconnect();
  }, [selectionWidth, useColumnFit]);

  useEffect(() => {
    const containerWidth = useColumnFit ? layoutWidth : containerWidthRef.current;
    if (!containerWidth) return;

    const newColumns = columns.map((col) => {
      return typeof col.width === 'string'
        ? {
            ...col,
            width: col.width.includes('%') ? containerWidth * (parseFloat(col.width) / 100) : col.width,
          }
        : col;
    });

    if (useColumnFit) {
      setResizeColumns(() => fitColumnsToContainer(newColumns, containerWidth, selectionWidth));
      return;
    }

    setResizeColumns(() => balanceColumns(newColumns));
  }, [columns, layoutWidth, selectionWidth, useColumnFit]);

  const mergedColumns = resizeColumns.map<TableColumnsType[number]>((col, index) => ({
    ...col,
    onHeaderCell: (column: TableColumnsType[number]) =>
      useColumnFit
        ? {
            width: column.width,
            minWidth: getColumnMin(column),
            maxWidth: getColumnMax(column),
            changeWidth: handleResize(index),
          }
        : {
            width: column.width,
            minWidth: column.minWidth,
            changeWidth: handleResize(index),
          },
  }));

  const _classNames = classNames(className, 'pro-table', {
    'resizable-table': resizeable,
    'hidden-empty': hiddenEmpty && restProps?.dataSource?.length === 0,
    'fixed-pagination-bottom': fixedPosition,
  });

  const toggleExpanded = () => {
    setIsExpanded(!isExpanded);
  };

  const handleExpandClick = () => {
    setShowAllColumns(!showAllColumns);
  };

  const changeShowColumns = (oldColumns: TableColumnsType) => {
    return !showExpand
      ? oldColumns
      : oldColumns.map((col) => {
          if ([true, false].includes(col?.hidden as boolean)) {
            col.hidden = showAllColumns;
          }
          return col;
        });
  };

  const mergedScroll = useColumnFit
    ? {
        ...scroll,
        x: layoutWidth > 0 ? layoutWidth : scroll?.x,
      }
    : resizeable
      ? {
          x: 'max-content',
          ...scroll,
        }
      : scroll;

  const sourceOnRow = restProps.onRow;
  const interactiveOnRow = sourceOnRow
    ? (record: any, index?: number) => {
        const rowProps = sourceOnRow(record, index);
        if (!rowProps?.onClick) return rowProps;
        const sourceOnClick = rowProps.onClick;
        return {
          ...rowProps,
          className: classNames(rowProps.className, 'pro-table-clickable-row'),
          onClick: (event: MouseEvent<HTMLElement>) => {
            const target = event.target as HTMLElement;
            // 弹出层的 React 事件仍会冒泡到行，但不属于行内点击。
            if (!event.currentTarget.contains(target)) return;
            if (
              target.closest(
                'button, a, input, select, textarea, [role="button"], [data-row-action="true"], .custom-operation, .custom-pin, .ant-checkbox-wrapper, .ant-dropdown-trigger'
              )
            ) {
              return;
            }
            sourceOnClick(event);
          },
        };
      }
    : undefined;

  const withRowNavigationIndicator = (tableColumns: TableColumnsType) => {
    if (!sourceOnRow || !rowNavigationIndicator) return tableColumns;
    const navigationColumnIndex = tableColumns.findIndex((column) => {
      const columnDataIndex = 'dataIndex' in column ? column.dataIndex : undefined;
      const dataIndex = Array.isArray(columnDataIndex) ? columnDataIndex.join('.') : columnDataIndex;
      if (rowNavigationColumnKey !== undefined) {
        return column.key === rowNavigationColumnKey || dataIndex === rowNavigationColumnKey;
      }
      return dataIndex !== 'pin' && dataIndex !== 'isPinned' && dataIndex !== 'operation' && column.key !== 'operation';
    });
    if (navigationColumnIndex < 0) return tableColumns;

    return tableColumns.map((column, columnIndex) => {
      if (columnIndex !== navigationColumnIndex) return column;
      const originalRender = column.render;
      return {
        ...column,
        render: (value: unknown, record: any, rowIndex: number) => {
          const content = originalRender ? originalRender(value, record, rowIndex) : (value as ReactNode);
          const rowProps = sourceOnRow(record, rowIndex);
          if (!rowProps?.onClick) return content;
          if (content && typeof content === 'object' && !isValidElement(content) && 'props' in content) return content;
          const contentNode = content as ReactNode;
          return (
            <div className="pro-table-row-link-cell">
              <div className="pro-table-row-link-content">{contentNode}</div>
              <ChevronRight className="pro-table-row-chevron" size={14} strokeWidth={1.75} aria-hidden="true" />
            </div>
          );
        },
      };
    });
  };

  const interactiveResizeColumns = withRowNavigationIndicator(mergedColumns);
  const interactiveColumns = withRowNavigationIndicator(columns);

  return (
    <div
      ref={tableWrapRef}
      className="pro-table-container"
      style={{
        '--ui-table-select-bg-color': selectBgColor,
        width: '100%',
        ...wrapperStyle,
      }}
      onMouseEnter={toggleExpanded}
      onMouseLeave={toggleExpanded}
    >
      {resizeable ? (
        <Table
          rowKey="id"
          size={'small'}
          {...restProps}
          onRow={interactiveOnRow}
          className={_classNames}
          columns={changeShowColumns(interactiveResizeColumns)}
          pagination={newPagination}
          scroll={mergedScroll}
          components={{ ...components, header: { cell: ResizableTitle } }}
          tableLayout="fixed"
        />
      ) : (
        <Table
          rowKey="id"
          size={'small'}
          {...restProps}
          onRow={interactiveOnRow}
          className={_classNames}
          components={components}
          scroll={scroll}
          columns={changeShowColumns(interactiveColumns)}
          pagination={newPagination}
        />
      )}
      {showExpand && (
        <div
          className={`pro-table-expand-button ${isExpanded ? 'pro-table-expanded' : ''}`}
          onClick={handleExpandClick}
        >
          <img src={showAllColumns ? expandIcon : collapseIcon} alt="" />
        </div>
      )}
    </div>
  );
};

const withIntlTable = (WrappedTable: FC<ATableProps>) => {
  const IntlTableWrapper = memo(({ columns, operationOptions, pinOptions, ...restProps }: ATableProps) => {
    const lang = useI18nStore((state) => state.lang);
    const formatMessage = useTranslate();
    const _columns = useMemo(() => {
      const nextColumns = [...columns];
      if (pinOptions) {
        const { disabled, onClick, renderPinIcon, auth, ...restProps } = pinOptions;
        nextColumns.unshift({
          title: ' ',
          dataIndex: 'pin',
          align: 'center',
          fixed: 'left',
          width: 40,
          render: (_: any, record: any) => {
            const isPin = renderPinIcon?.(record) ?? false;
            return (
              <ComButton
                title={isPin ? formatMessage('common.pin') : formatMessage('common.unPin')}
                auth={auth}
                disabled={disabled}
                onClick={() => onClick?.(record)}
                className={classNames('custom-pin', 'ui-pin-button', !isPin && 'custom-pin-fixed ui-pin-button-active')}
                icon={<Pin size={12} />}
                size="small"
                type={'text'}
              />
            );
          },
          ...restProps,
        });
      }
      if (operationOptions) {
        nextColumns.push({
          title: () => formatMessage('common.operation'),
          width: 120,
          dataIndex: 'operation',
          align: 'left',
          fixed: 'right',
          ...operationOptions,
          render: (_: any, record: any, index: number) => {
            const visibleItems =
              operationOptions.render?.(record, index).filter((item: any) => {
                return item && (!item.auth || hasPermission(item.auth));
              }) || [];
            const contentRaw = visibleItems.filter((item: any, itemIndex: number) => {
              if (item?.type !== 'divider') return true;
              const prev = visibleItems[itemIndex - 1];
              const next = visibleItems[itemIndex + 1];
              return prev && prev.type !== 'divider' && next && next.type !== 'divider';
            });
            if (contentRaw?.length === 0) return null;
            const menuItems = contentRaw.map((item: any) => {
              if (item?.type === 'divider') {
                return { type: 'divider' as const };
              }
              const { key, label, icon, extra, title, onClick, disabled, danger, children, type } = item;
              return {
                key,
                label: commonLabelRender(item),
                icon: resolveOperationIcon(icon, extra),
                title: title ? title : typeof label === 'string' ? label : '',
                onClick: type !== 'Popconfirm' && onClick,
                disabled,
                // 全局统一：删除操作为危险样式（红色）
                danger: danger ?? key === 'delete',
                children,
              };
            });
            return (
              <div className="custom-operation" onClick={(event) => event.stopPropagation()}>
                <Dropdown
                  disabled={operationOptions.disabled}
                  overlayClassName="pro-table-operation-menu"
                  menu={{ items: menuItems }}
                  trigger={['click']}
                  placement="bottomRight"
                >
                  <Button type="text" icon={<OverflowMenuHorizontal size={16} />} />
                </Dropdown>
              </div>
            );
          },
        });
      }
      return nextColumns.map((i: any) => {
        const type = typeof i.title;
        if (i.titleIntlId) {
          return {
            ...i,
            title: () => formatMessage(i.titleIntlId),
          };
        } else if (type === 'function') {
          const originalTitleFn: any = i.title;
          return {
            ...i,
            title: (params: any) => originalTitleFn({ ...params, formatMessage }),
          };
        } else {
          return i;
        }
      });
    }, [lang, columns, operationOptions]);
    return <WrappedTable {...restProps} columns={_columns} />;
  });
  return IntlTableWrapper;
};

export default withIntlTable(ProTable);
