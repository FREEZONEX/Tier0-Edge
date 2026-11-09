import { forwardRef, type Key, type ReactNode, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react';
import ComEmpty from '@/components/com-empty';
import { Flex, Spin, Tree, type TreeProps } from 'antd';
import { useSize } from 'ahooks';
import cx from 'classnames';
import type { DataNodeProps, ProTreeProps, ProTreeRef } from './types';
import HighlightText from '@/components/pro-tree/HighlightText.tsx';
import ControlledDropdown, { type ControlledDropdownRef } from '../controlled-dropdown';
import {
  DndContext,
  MouseSensor,
  TouchSensor,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  DragOverlay,
  pointerWithin,
} from '@dnd-kit/core';
import { SortableContext, verticalListSortingStrategy } from '@dnd-kit/sortable';
import './index.scss';
import { flattenTree } from './utils.ts';
import SortableTreeNode from './SortableTreeNode.tsx';
import { OverlayItem } from './OverlayItem.tsx';
import { createPortal } from 'react-dom';

const ProTree = forwardRef<ProTreeRef, ProTreeProps>((props, ref) => {
  const {
    footer,
    header,
    empty,
    height,
    treeData = [],
    wrapperStyle,
    loadingStyle,
    wrapperClassName,
    specialStyle = true,
    rightClickMenuItems,
    onRightClick,
    titleRender,
    treeNodeIcon,
    treeNodeExtra,
    treeNodePersistentExtra,
    treeNodeCount,
    treeNodeClassName,
    loading,
    loadMoreData,
    lazy,
    loadData,
    showSwitcherIcon = true,
    matchHighlightValue,
    isShow,
    onDndDragStart,
    onDndDragEnd,
    overlayChildren,
    drapOverChanges,
    /** 自己实现，不使用antd Tree方案（默认false） */
    dndDraggable,
    renderTitleStyle,
    ...restProps
  } = props;
  const treeContentRef = useRef<HTMLDivElement | null>(null);
  const dropdownRef = useRef<ControlledDropdownRef>(null);
  const treeRef = useRef<any>(null);
  const [treeHeight, setTreeHeight] = useState(height);
  const [rightClickNode, setRightClickNode] = useState<Key>();
  const [activeItem, setActiveItem] = useState<DataNodeProps | null>(null);
  const [isInset, setIsInset] = useState(false);

  useImperativeHandle(ref, () => ({
    scrollTo: treeRef.current?.scrollTo,
  }));

  const treeContentSize = useSize(treeContentRef);

  useEffect(() => {
    if (treeContentSize?.width === 0) return;
    if (
      height !== undefined &&
      treeContentSize?.height !== undefined &&
      [true, undefined, null].includes(isShow?.current)
    ) {
      // 虚拟滚动设置自适应高度
      setTreeHeight(treeContentSize?.height);
    }
  }, [treeContentSize?.height, height]);

  const onRightClickHandle: TreeProps['onRightClick'] = (info) => {
    const { event, node } = info;
    const _node = { ...node };
    // 如果是加载更多节点，右键不生效
    if ((_node as DataNodeProps).isLoadMoreNode && lazy) return;
    if (rightClickMenuItems) {
      const items = typeof rightClickMenuItems === 'function' ? rightClickMenuItems(info) : rightClickMenuItems;
      if (_node && items?.length) {
        setRightClickNode(_node.key);
      }
      dropdownRef?.current?.showDropdown(event, items);
    }
    onRightClick?.(info);
  };

  const _Empty = empty ? empty : <ComEmpty />;

  const _titleRender = (node: DataNodeProps) => {
    const title = node.title as ReactNode;
    const _title = titleRender ? titleRender?.(node) : title;
    const Icon = typeof treeNodeIcon === 'function' ? treeNodeIcon(node) : treeNodeIcon;
    const Extra = typeof treeNodeExtra === 'function' ? treeNodeExtra(node) : treeNodeExtra;
    const PersistentExtra =
      typeof treeNodePersistentExtra === 'function' ? treeNodePersistentExtra(node) : treeNodePersistentExtra;
    const Count = typeof treeNodeCount === 'function' ? treeNodeCount(node) : treeNodeCount;
    const nodeClassName = typeof treeNodeClassName === 'function' ? treeNodeClassName(node) : treeNodeClassName;
    const titleStyle = typeof renderTitleStyle === 'function' ? renderTitleStyle(node) : renderTitleStyle;
    if (node.isLoadMoreNode && loadMoreData && lazy) {
      loadMoreData?.(node);
      return title;
      // return <LoadMoreNode node={node} loadMoreData={loadMoreData} />;
    }
    const Dom = rightClickNode ? (
      <span className={cx({ 'has-right-click': rightClickNode === node.key })}>
        <HighlightText needle={matchHighlightValue} haystack={_title} />
      </span>
    ) : (
      <HighlightText needle={matchHighlightValue} haystack={_title} />
    );
    if (specialStyle) {
      return (
        <SortableTreeNode
          node={node}
          isActive={activeItem?.id === node.id}
          isInset={isInset}
          drapOverChanges={drapOverChanges}
          draggable={dndDraggable}
        >
          <Flex className={cx('treeNodeClassName', 'custom-tree-node', nodeClassName)} align="center" gap={8}>
            <Flex style={{ height: '32px', flex: 1, overflow: 'hidden' }} align="center" gap={8}>
              <Flex
                style={{
                  maxWidth: '100%',
                  ...titleStyle,
                }}
                align="center"
                gap={4}
              >
                {Icon && <div className="custom-tree-node-icon">{Icon}</div>}
                <div className="custom-tree-node-title">
                  {Dom}
                  {Count}
                </div>
              </Flex>
            </Flex>
            {PersistentExtra && <div className="custom-tree-node-persistent-extra">{PersistentExtra}</div>}
            {Extra && <div className="custom-tree-node-extra">{Extra}</div>}
          </Flex>
        </SortableTreeNode>
      );
    }
    return (
      <SortableTreeNode
        node={node}
        isActive={activeItem?.id === node.id}
        isInset={isInset}
        drapOverChanges={drapOverChanges}
        draggable={dndDraggable}
      >
        <div className={nodeClassName}>
          {Icon}
          {Dom}
          {Count}
          {PersistentExtra}
          {Extra}
        </div>
      </SortableTreeNode>
    );
  };

  const flattenedItems = useMemo(() => {
    return flattenTree(treeData as DataNodeProps[]);
  }, [treeData]);

  const sortedIds = useMemo(() => flattenedItems.map(({ id }) => id), [flattenedItems]);

  const sensors = useSensors(
    useSensor(MouseSensor),
    useSensor(TouchSensor),
    useSensor(KeyboardSensor),
    useSensor(PointerSensor, {
      activationConstraint: {
        distance: 5,
      },
    })
  );

  return (
    <Spin wrapperClassName="pro-tree-loading" style={loadingStyle} spinning={!!loading}>
      <div
        className={cx('pro-tree-wrap', wrapperClassName, {
          'pro-tree-special': specialStyle,
          'pro-tree-expend': !showSwitcherIcon,
        })}
        style={wrapperStyle}
        onContextMenu={(event) => {
          if (event.target instanceof Element) {
            const inModal = event.target.closest?.('.ant-modal-root') !== null;
            if (inModal) {
              return;
            }
          }
          onRightClickHandle({ event } as any);
        }}
      >
        {header && <div className="pro-tree-header">{header}</div>}
        <div className="pro-tree-content" ref={treeContentRef}>
          {!treeData?.length ? (
            <Flex justify="center" align="center" style={{ height: '100%' }}>
              {_Empty}
            </Flex>
          ) : dndDraggable ? (
            <DndContext
              sensors={sensors}
              collisionDetection={pointerWithin}
              onDragEnd={(event) => {
                const { active, activatorEvent, over } = event;
                onDndDragEnd?.({
                  event: activatorEvent,
                  active: active?.data?.current?.node as DataNodeProps,
                  over: over?.data?.current?.node as DataNodeProps,
                  isInset: isInset,
                });
              }}
              onDragStart={(event) => {
                const { active, activatorEvent } = event;
                // console.log(event);
                setActiveItem(active?.data?.current?.node as unknown as DataNodeProps);
                onDndDragStart?.({ event: activatorEvent, active: active?.data?.current?.node as DataNodeProps });
              }}
              onDragMove={(event) => {
                const { over, activatorEvent, delta } = event;
                if (!over?.rect) return;
                const { left } = over.rect;
                const { x } = delta;
                const mouseX = (activatorEvent as MouseEvent).clientX;
                setIsInset(left + 30 - (mouseX + x) < 0);
              }}
            >
              <SortableContext items={sortedIds} strategy={verticalListSortingStrategy}>
                <Tree
                  ref={treeRef}
                  blockNode
                  {...restProps}
                  onRightClick={(info) => {
                    info.event.stopPropagation();
                    onRightClickHandle(info);
                  }}
                  titleRender={_titleRender}
                  treeData={treeData}
                  height={treeHeight}
                  loadData={lazy ? loadData : undefined}
                  draggable={false}
                />
              </SortableContext>
              {/*取消掉落动画*/}
              {createPortal(
                <DragOverlay dropAnimation={null}>
                  {activeItem?.id ? (
                    <OverlayItem
                      overlayChildren={
                        typeof overlayChildren === 'function' ? overlayChildren?.(activeItem) : overlayChildren
                      }
                    />
                  ) : null}
                </DragOverlay>,
                document.body
              )}
            </DndContext>
          ) : (
            <Tree
              ref={treeRef}
              blockNode
              {...restProps}
              onRightClick={(info) => {
                info.event.stopPropagation();
                onRightClickHandle(info);
              }}
              titleRender={_titleRender}
              treeData={treeData}
              height={treeHeight}
              loadData={lazy ? loadData : undefined}
            />
          )}
        </div>
        <ControlledDropdown
          ref={dropdownRef}
          onOpenChange={(open) => {
            if (!open) {
              setRightClickNode(undefined);
            }
          }}
        />
        {footer && <div className="pro-tree-footer">{footer}</div>}
      </div>
    </Spin>
  );
});

export default ProTree;
