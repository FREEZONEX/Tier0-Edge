import type { App, Project } from '@/pages/launchpad/type';
import { useTranslate } from '@/hooks';
import { Folder, Box, Pin } from '@/components/lucide-icon/carbon';
import { Card } from 'antd';
import classNames from 'classnames';
import { type FC } from 'react';
import AppIcon from './AppIcon';
import HighlightMatch from './HighlightMatch';
import styles from './ProjectGrid.module.scss';

interface ProjectGridProps {
  projects: Project[];
  onProjectClick: (project: Project) => void;
  onAppClick: (app: App) => void;
  isProjectPinned: (id: string | number) => boolean;
  onTogglePin: (project: Project) => void;
  /** 搜索关键词，用于项目名命中高亮 */
  keyword?: string;
}

const ProjectGrid: FC<ProjectGridProps> = ({
  projects,
  onProjectClick,
  onAppClick,
  isProjectPinned,
  onTogglePin,
  keyword,
}) => {
  const formatMessage = useTranslate('Launchpad');

  return (
    <div className={styles.projectGrid}>
      {projects.map((project) => {
        const displayName = project.displayName || project.projectName;
        const pinned = isProjectPinned(project.projectId);

        return (
          <Card key={project.projectId} className={styles.projectCard} onClick={() => onProjectClick(project)}>
            <div className={styles.cardHeader}>
              <div className={styles.projectInfo}>
                <div className={styles.iconWrapper}>
                  <Folder size={26} className={styles.folderIcon} />
                </div>
                <div className={styles.textInfo}>
                  <div className={styles.projectName} title={displayName}>
                    <HighlightMatch text={displayName} keyword={keyword} />
                  </div>
                  <div className={styles.appsCount}>
                    {formatMessage('appsCount', { count: project.apps?.length || 0 })}
                  </div>
                </div>
              </div>
              <button
                type="button"
                className={classNames(
                  styles.pinButton,
                  'ui-pin-button',
                  pinned && styles.pinActive,
                  pinned && 'ui-pin-button-active'
                )}
                aria-label={formatMessage(pinned ? 'unpin' : 'pin', undefined, pinned ? 'Unpin' : 'Pin')}
                onClick={(event) => {
                  event.stopPropagation();
                  onTogglePin(project);
                }}
              >
                <Pin size={12} />
              </button>
            </div>
            <div className={styles.appIcons}>
              {Array.from({ length: 4 }, (_, index) => {
                const app = project.apps?.[index];
                const isEmpty = !app?.appId;
                if (isEmpty) {
                  return (
                    <div
                      key={`placeholder-${project.projectId}-${index}`}
                      className={classNames(styles.appIcon, styles.appIconEmpty)}
                    >
                      <Box size={17} strokeWidth={1.5} className={styles.emptyIcon} />
                    </div>
                  );
                }

                return (
                  <button
                    key={app.appId}
                    type="button"
                    className={styles.appIcon}
                    title={app.displayName || app.appName}
                    onClick={(event) => {
                      event.stopPropagation();
                      onAppClick({
                        ...app,
                        projectName: app.projectName || project.projectName,
                        projectDisplayName: app.projectDisplayName || project.displayName,
                      });
                    }}
                  >
                    <div className={styles.iconContainer}>
                      {/* 走 AppIcon 而不是裸 img：图标是需要鉴权的 /assets/:id/download，
                          403/404 时必须回退到 Box，否则页面上直接露出浏览器破图 */}
                      <AppIcon
                        iconUrl={app.iconUrl}
                        alt={app.displayName || app.appName}
                        size={17}
                        strokeWidth={1.75}
                        imgClassName={styles.appIconImage}
                      />
                    </div>
                    {app.coverUrl ? (
                      <img
                        src={app.coverUrl}
                        alt={app.displayName || app.appName}
                        className={styles.coverImage}
                        onError={(event) => {
                          event.currentTarget.hidden = true;
                        }}
                      />
                    ) : null}
                    <span className={styles.appNameOverlay}>
                      <span className={styles.appNameText}>{app.displayName || app.appName}</span>
                    </span>
                  </button>
                );
              })}
            </div>
          </Card>
        );
      })}
    </div>
  );
};

export default ProjectGrid;
