import type { TableColumnsType, TableProps } from 'antd';
import type { CSSProperties, Key } from 'react';
import type { OperationProps } from '@/components/operation-buttons/utils.tsx';

export type ProTableColumn<RecordType = any> = TableColumnsType<RecordType>[number] & {
  titleIntlId?: string;
  maxWidth?: number;
};

export type ProTableColumns<RecordType = any> = ProTableColumn<RecordType>[];

export interface ATableProps<RecordType = any> extends Omit<TableProps<RecordType>, 'columns'> {
  // titleIntlId 国际化key
  columns: ProTableColumns<RecordType>;
  resizeable?: boolean;
  /** 列宽铺满容器、互斥弹性收缩、最大宽度限制；默认与 resizeable 一并开启 */
  columnFit?: boolean;
  // 是否隐藏空白
  hiddenEmpty?: boolean;
  fixedPosition?: boolean; // 是否固定页码在底部
  showExpand?: boolean; // 是否显示展开按钮
  /** 行可点击时是否自动在首个数据列显示 ChevronRight，已有自定义指示器的表格可关闭 */
  rowNavigationIndicator?: boolean;
  /** 指定 ChevronRight 所在列；未指定时自动选择首个非操作列 */
  rowNavigationColumnKey?: Key;
  wrapperStyle?: CSSProperties;
  // 操作项配置
  operationOptions?: {
    title?: string | (() => string);
    width?: number | string;
    render: (record: any, index: number) => (OperationProps | null)[];
    disabled?: boolean;
  };
  // 置顶配置
  pinOptions?: {
    title?: string | (() => string);
    width?: number | string;
    disabled?: boolean;
    onClick?: (record: any) => Promise<any>;
    renderPinIcon?: (record: any) => boolean;
    auth?: string | string[];
  };
}

export interface TitlePropsType {
  width?: number;
  minWidth?: number;
  maxWidth?: number;
  changeWidth: (width: number) => void;
}
