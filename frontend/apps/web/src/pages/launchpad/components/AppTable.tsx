import { ProTable } from '@/components';
import type { ProTableColumns } from '@/components/pro-table/types';
import { Pin } from '@/components/lucide-icon/carbon';
import { useTranslate } from '@/hooks';
import type { App } from '@/pages/launchpad/type';
import { formatTimestamp } from '@/utils/format';
import classNames from 'classnames';
import { type FC } from 'react';
import AppIcon from './AppIcon';
import HighlightMatch from './HighlightMatch';
import styles from './LaunchpadTable.module.scss';

interface AppTableProps {
  apps: App[];
  onAppClick: (app: App) => void | Promise<void>;
  isAppPinned: (id: string | number) => boolean;
  onTogglePin: (app: App) => void;
  /** 搜索关键词，用于应用名命中高亮 */
  keyword?: string;
}

const AppTable: FC<AppTableProps> = ({ apps, onAppClick, isAppPinned, onTogglePin, keyword }) => {
  const formatMessage = useTranslate();

  const columns: ProTableColumns<App> = [
    {
      title: '',
      dataIndex: 'isPinned',
      width: 28,
      maxWidth: 28,
      align: 'center',
      render: (_: unknown, record: App) => {
        const pinned = isAppPinned(record.appId);
        return (
          <button
            type="button"
            className={classNames(
              styles.pinButton,
              'ui-pin-button',
              pinned && styles.pinActive,
              pinned && 'ui-pin-button-active'
            )}
            aria-label={pinned ? 'Unpin' : 'Pin'}
            onClick={(event) => {
              event.stopPropagation();
              onTogglePin(record);
            }}
          >
            <Pin size={16} />
          </button>
        );
      },
    },
    {
      title: formatMessage('common.name'),
      dataIndex: 'appName',
      render: (_: unknown, record: App) => {
        const name = record.displayName || record.appName;
        return (
          <div className={styles.nameCell} title={name}>
            <span className={styles.nameIcon}>
              <AppIcon iconUrl={record.iconUrl} alt={name} size={16} imgClassName={styles.nameIconImg} />
            </span>
            <span className={styles.nameText}>
              <HighlightMatch text={name} keyword={keyword} />
            </span>
          </div>
        );
      },
    },
    {
      title: formatMessage('common.latestUpdate'),
      dataIndex: 'updatedAt',
      width: 220,
      render: (updatedAt?: string) => (updatedAt ? formatTimestamp(updatedAt, 'YYYY/MM/DD HH:mm', true) : '-'),
    },
  ];

  return (
    <ProTable
      columns={columns}
      dataSource={apps}
      rowKey="appId"
      pagination={false}
      onRow={(record: App) => ({
        onClick: () => onAppClick(record),
        style: { cursor: 'pointer' },
      })}
      rowNavigationColumnKey="appName"
    />
  );
};

export default AppTable;
