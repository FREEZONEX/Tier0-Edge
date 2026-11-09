import type { FC } from 'react';
import classNames from 'classnames';
import { ChevronLeft } from '@carbon/icons-react';
import { Button, Tooltip } from 'antd';
import { useTranslate } from '@/hooks';
import './index.scss';

export interface ComBackButtonProps {
  onClick?: () => void;
  className?: string;
  /** 悬浮提示，默认 common.back */
  title?: string;
}

/**
 * 系统统一返回按钮：24×24 描边图标按钮，与 ComDetailHeader 一致。
 */
const ComBackButton: FC<ComBackButtonProps> = ({ onClick, className, title }) => {
  const formatMessage = useTranslate();
  const tip = title ?? formatMessage('common.back');

  return (
    <Tooltip title={tip}>
      <Button type="text" onClick={onClick} className={classNames('com-back-button', className)} aria-label={tip}>
        <ChevronLeft size={20} />
      </Button>
    </Tooltip>
  );
};

export default ComBackButton;
