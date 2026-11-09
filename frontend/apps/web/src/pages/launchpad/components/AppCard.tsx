import { Folder, Pin, Time } from '@/components/lucide-icon/carbon';
import { useTranslate } from '@/hooks';
import type { App } from '@/pages/launchpad/type';
import { formatTimestamp } from '@/utils/format';
import classNames from 'classnames';
import { type FC } from 'react';
import AppIcon from './AppIcon';
import HighlightMatch from './HighlightMatch';
import styles from './AppCard.module.scss';

interface AppCardProps {
  app: App;
  isPinned: boolean;
  onTogglePin: (app: App) => void;
  onAppClick: (app: App) => void | Promise<void>;
  /** 顶部"收藏与最近"区展示所属项目与时间；项目钻取页内无需重复 */
  showProject?: boolean;
  /** 项目钻取页等场景仅展示更新时间 */
  showUpdatedAt?: boolean;
  /** 搜索关键词，命中高亮 */
  keyword?: string;
  className?: string;
}

const AppCard: FC<AppCardProps> = ({
  app,
  isPinned,
  onTogglePin,
  onAppClick,
  showProject = false,
  showUpdatedAt = false,
  keyword,
  className,
}) => {
  const formatMessage = useTranslate('Launchpad');
  const name = app.displayName || app.appName;
  const projectName = app.projectDisplayName || app.projectName || '';
  const showFooter = showProject || (showUpdatedAt && Boolean(app.updatedAt));

  return (
    <div className={classNames(styles.appCard, className)} onClick={() => onAppClick(app)}>
      <div className={styles.thumbnail}>
        <span className={styles.appIcon}>
          <AppIcon iconUrl={app.iconUrl} alt={name} size={28} imgClassName={styles.appIconImage} />
        </span>
        {app.coverUrl ? (
          <img
            src={app.coverUrl}
            alt={name}
            className={styles.coverImage}
            onError={(event) => {
              event.currentTarget.hidden = true;
            }}
          />
        ) : null}
        <button
          type="button"
          className={classNames(
            styles.pinButton,
            'ui-pin-button',
            isPinned && styles.pinActive,
            isPinned && 'ui-pin-button-active'
          )}
          aria-label={formatMessage(isPinned ? 'unpin' : 'pin', undefined, isPinned ? 'Unpin' : 'Pin')}
          onClick={(event) => {
            event.stopPropagation();
            onTogglePin(app);
          }}
        >
          <Pin size={12} />
        </button>
      </div>

      <div className={styles.info}>
        <div className={styles.appName} title={name}>
          <HighlightMatch text={name} keyword={keyword} />
        </div>
        {showFooter && (
          <div className={styles.footer}>
            {showProject ? (
              <span className={styles.project} title={projectName}>
                <Folder size={12} className={styles.footerIcon} />
                <span className={styles.projectText}>{projectName}</span>
              </span>
            ) : (
              <span />
            )}
            {app.updatedAt && (showProject || showUpdatedAt) && (
              <span className={styles.time}>
                <Time size={12} className={styles.footerIcon} />
                {formatTimestamp(app.updatedAt, 'YYYY/MM/DD HH:mm', true)}
              </span>
            )}
          </div>
        )}
      </div>
    </div>
  );
};

export default AppCard;
