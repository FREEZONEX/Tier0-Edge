import searchEmpty from '@/assets/common/search.svg';
import empty from '@/assets/common/empty.svg';
import { Search } from '@carbon/icons-react';
import ViewModeSegmented from '@/components/lucide-icon/ViewModeSegmented';
import { useTranslate } from '@/hooks';
import type { App, Project } from '@/pages/launchpad/type';
import { Empty, Input, Typography } from 'antd';
import { type FC } from 'react';
import ProjectGrid from './ProjectGrid';
import ProjectTable from './ProjectTable';
import styles from './AllProjectsSection.module.scss';

interface AllProjectsSectionProps {
  projects: Project[];
  searchValue: string;
  onSearchChange: (value: string) => void;
  appliedSearchKeyword: string;
  hasSearchKeyword: boolean;
  viewMode: string;
  onViewModeChange: (mode: string) => void;
  isProjectPinned: (id: string | number) => boolean;
  onTogglePin: (project: Project) => void;
  onProjectClick: (project: Project) => void;
  onAppClick: (app: App) => void;
}

const AllProjectsSection: FC<AllProjectsSectionProps> = ({
  projects,
  searchValue,
  onSearchChange,
  appliedSearchKeyword,
  hasSearchKeyword,
  viewMode,
  onViewModeChange,
  isProjectPinned,
  onTogglePin,
  onProjectClick,
  onAppClick,
}) => {
  const formatMessage = useTranslate('Launchpad');
  const formatCommon = useTranslate();

  return (
    <section className={styles.allProjects}>
      <div className={styles.header}>
        <h2 className={styles.title}>{formatMessage('allProjects', undefined, 'All Projects')}</h2>
        <div className={styles.actions}>
          <Input
            value={searchValue}
            allowClear
            placeholder={formatMessage('inputText')}
            prefix={<Search size={16} />}
            className={styles.searchInput}
            onChange={(event) => onSearchChange(event.target.value)}
          />
          <ViewModeSegmented
            value={viewMode}
            onChange={onViewModeChange}
            cardTitle={formatCommon('common.cardMode')}
            listTitle={formatCommon('common.listMode')}
          />
        </div>
      </div>

      {projects.length === 0 ? (
        <div className={styles.emptyState}>
          <Empty
            image={<img src={hasSearchKeyword ? searchEmpty : empty} alt="" />}
            description={
              <Typography.Text type="secondary">
                {formatMessage(hasSearchKeyword ? 'searchEmptyTitle' : 'noProjectsTitle')}
              </Typography.Text>
            }
          />
        </div>
      ) : viewMode === 'list' ? (
        <ProjectTable
          projects={projects}
          onProjectClick={onProjectClick}
          isProjectPinned={isProjectPinned}
          onTogglePin={onTogglePin}
          keyword={appliedSearchKeyword}
        />
      ) : (
        <ProjectGrid
          projects={projects}
          onProjectClick={onProjectClick}
          onAppClick={onAppClick}
          isProjectPinned={isProjectPinned}
          onTogglePin={onTogglePin}
          keyword={appliedSearchKeyword}
        />
      )}
    </section>
  );
};

export default AllProjectsSection;
