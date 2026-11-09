import empty from '@/assets/common/empty.svg';
import { ChevronDown, ChevronUp } from '@/components/lucide-icon/carbon';
import { useTranslate } from '@/hooks';
import type { App } from '@/pages/launchpad/type';
import { Empty, Typography } from 'antd';
import { type FC, useState } from 'react';
import AppCard from './AppCard';
import styles from './PinnedRecentSection.module.scss';

interface PinnedRecentSectionProps {
  /** 最近打开的应用列表（已按后端规则排序，收藏的 App 会优先展示） */
  apps: App[];
  isAppPinned: (id: string | number) => boolean;
  onTogglePin: (app: App) => void;
  onAppClick: (app: App) => void | Promise<void>;
}

/**
 * 收藏与最近应用区块。当前展示的是后端返回的最近打开应用列表，
 * 已包含收藏优先排序；点击收藏图标会同步更新后端收藏状态。
 */
const PinnedRecentSection: FC<PinnedRecentSectionProps> = ({ apps, isAppPinned, onTogglePin, onAppClick }) => {
  const formatMessage = useTranslate('Launchpad');
  const [collapsed, setCollapsed] = useState(false);

  return (
    <section className={styles.pinnedRecent}>
      <button
        type="button"
        className={styles.sectionTitle}
        onClick={() => setCollapsed((v) => !v)}
        aria-expanded={!collapsed}
      >
        <span>{formatMessage('pinnedRecent', undefined, 'Pinned & Recent Apps')}</span>
        {collapsed ? <ChevronDown size={16} /> : <ChevronUp size={16} />}
      </button>

      {!collapsed &&
        (apps.length > 0 ? (
          <div className={styles.cardGrid}>
            {apps.map((app) => (
              <AppCard
                key={app.appId}
                app={app}
                showProject
                isPinned={isAppPinned(app.appId)}
                onTogglePin={onTogglePin}
                onAppClick={onAppClick}
              />
            ))}
          </div>
        ) : (
          <div className={styles.emptyState}>
            <Empty
              image={<img src={empty} alt="" />}
              description={
                <Typography.Text type="secondary">
                  {formatMessage('noPinnedRecent', undefined, 'No pinned or recent apps yet')}
                </Typography.Text>
              }
            />
          </div>
        ))}
    </section>
  );
};

export default PinnedRecentSection;
