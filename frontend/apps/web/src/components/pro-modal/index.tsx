import { type FC, useRef, useState, useCallback, useEffect, useContext, type ReactNode } from 'react';
import Draggable from 'react-draggable';
import { Modal, Tooltip, type ModalProps, Button } from 'antd';
import { ExpandOutlined, CompressOutlined, CloseOutlined } from '@ant-design/icons';
import classNames from 'classnames';
import { useTranslate } from '@/hooks';
import { ForegroundDialogContext } from '@/contexts/foreground-dialog-context';
import './index.scss';

const { confirm } = Modal;

export type ProModalSizeType = 'xxs' | 'xs' | 'sm' | 'md' | 'lg';
export interface ProModalProps extends Omit<ModalProps, 'children'> {
  /*内置大小*/
  size?: ProModalSizeType;
  closeButtonTitle?: string;
  /*是否开启拖拽*/
  draggable?: boolean;
  /*是否开启全屏*/
  fullScreenable?: boolean;
  defaultFull?: boolean;
  onFullScreenCallBack?: (fullScreen: boolean) => void;
  children?: ((isFullscreen?: boolean) => ReactNode) | ReactNode;
}

const sizeMap: Record<ProModalSizeType, number> = {
  xxs: 400,
  xs: 600,
  sm: 800,
  md: 1000,
  lg: 1200,
};

const ProModal: FC<ProModalProps> & { confirm: typeof confirm } = ({
  className,
  children,
  fullScreenable = true,
  defaultFull = false,
  closeButtonTitle,
  draggable = true,
  afterClose,
  closable = true,
  onFullScreenCallBack,
  width,
  centered = true,
  ...restProps
}) => {
  const suspended = useContext(ForegroundDialogContext);
  const formatMessage = useTranslate();
  const [bounds, setBounds] = useState({ left: 0, right: 0, top: 0, bottom: 0 });
  const [isFullscreen, setIsFullscreen] = useState(defaultFull);
  const [tooltipOpen, setTooltipOpen] = useState(false);
  const [dragPosition, setDragPosition] = useState({
    x: 0,
    y: 0,
    windowWidth: window.innerWidth,
    windowHeight: window.innerHeight,
  });
  const draggableRef = useRef<HTMLDivElement>(null);

  const toggleFullscreen = () => {
    setTooltipOpen(false);
    setIsFullscreen(!isFullscreen);
    onFullScreenCallBack?.(!isFullscreen);
    setDragPosition((pre) => {
      return {
        ...pre,
        x: 0,
        y: 0,
      };
    });
  };

  const handleDragStart = useCallback(
    (_event: any, uiData: any) => {
      if (isFullscreen) return;

      const { clientWidth, clientHeight } = document.documentElement;
      const targetRect = draggableRef.current?.getBoundingClientRect();

      if (!targetRect) return;

      setBounds({
        left: -targetRect.left + uiData.x,
        right: clientWidth - (targetRect.right - uiData.x),
        top: -targetRect.top + uiData.y,
        bottom: clientHeight - (targetRect.bottom - uiData.y),
      });
    },
    [isFullscreen]
  );

  useEffect(() => {
    const handleResize = () => {
      if (isFullscreen || !draggable) return;

      const { x, y, windowWidth: oldWidth, windowHeight: oldHeight } = dragPosition;
      const newWidth = window.innerWidth;
      const newHeight = window.innerHeight;

      if (oldWidth === 0 || oldHeight === 0) return;

      const scaleX = newWidth / oldWidth;
      const scaleY = newHeight / oldHeight;
      const newX = x * scaleX;
      const newY = y * scaleY;

      const targetRect = draggableRef.current?.getBoundingClientRect();
      if (!targetRect) return;

      const clientWidth = newWidth;
      const clientHeight = newHeight;

      const left = -targetRect.left + newX;
      const right = clientWidth - (targetRect.right - newX);
      const top = -targetRect.top + newY;
      const bottom = clientHeight - (targetRect.bottom - newY);

      const adjustedX = Math.max(left, Math.min(newX, right));
      const adjustedY = Math.max(top, Math.min(newY, bottom));

      setDragPosition((prev) => ({
        ...prev,
        x: adjustedX,
        y: adjustedY,
        windowWidth: newWidth,
        windowHeight: newHeight,
      }));
      setBounds({ left, right, top, bottom });
    };

    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, [isFullscreen, draggable, dragPosition]);

  return (
    <Modal
      footer={null}
      closable={false}
      {...restProps}
      styles={
        suspended
          ? {
              ...restProps.styles,
              // rc-dialog owns wrapper.display; visibility also preserves the open lifecycle.
              wrapper: { ...restProps.styles?.wrapper, visibility: 'hidden' },
              mask: { ...restProps.styles?.mask, display: 'none' },
            }
          : restProps.styles
      }
      wrapProps={suspended ? { ...restProps.wrapProps, inert: '', 'aria-hidden': true } : restProps.wrapProps}
      centered={centered}
      className={classNames('pro-modal', className, { 'fullscreen-mode': isFullscreen })}
      width={isFullscreen ? '100%' : width ? width : sizeMap[restProps?.size || 'sm']}
      title={
        <div className="modal-header">
          <div className="drag-handle" style={{ cursor: draggable ? (isFullscreen ? '' : 'move') : '' }}>
            {restProps?.title}
          </div>
          <div className="header-controls">
            {fullScreenable ? (
              <Tooltip
                open={tooltipOpen}
                onOpenChange={(open) => setTooltipOpen(open)}
                title={isFullscreen ? formatMessage('common.exitFullScreen') : formatMessage('common.fullScreen')}
              >
                <Button
                  type="text"
                  icon={isFullscreen ? <CompressOutlined /> : <ExpandOutlined />}
                  onClick={toggleFullscreen}
                  className="control-button"
                />
              </Tooltip>
            ) : (
              ''
            )}
            {closable && (
              <Button
                type="text"
                icon={<CloseOutlined className="close-icon" />}
                onClick={restProps?.onCancel}
                className="control-button close-button"
                aria-label={closeButtonTitle ?? formatMessage('common.close')}
              />
            )}
          </div>
        </div>
      }
      modalRender={(modal) => (
        <Draggable
          disabled={!draggable || isFullscreen}
          bounds={bounds}
          nodeRef={draggableRef}
          onStart={handleDragStart}
          handle=".drag-handle"
          onDrag={(_, data) => {
            setDragPosition((prev) => ({
              ...prev,
              x: data.x,
              y: data.y,
            }));
          }}
          onStop={(_, data) => {
            setDragPosition({
              x: data.x,
              y: data.y,
              windowWidth: window.innerWidth,
              windowHeight: window.innerHeight,
            });
          }}
          position={{ x: dragPosition.x, y: dragPosition.y }}
        >
          <div ref={draggableRef}>{modal}</div>
        </Draggable>
      )}
      afterClose={() => {
        setDragPosition((pre) => {
          return {
            ...pre,
            x: 0,
            y: 0,
          };
        });
        setIsFullscreen(defaultFull);
        afterClose?.();
      }}
    >
      {typeof children === 'function' ? children?.(isFullscreen) : children}
    </Modal>
  );
};

ProModal.confirm = confirm;

export default ProModal;
