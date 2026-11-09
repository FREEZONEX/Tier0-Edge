import { getLaunchpadProjectByNameApi, recordLaunchpadAppViewApi } from '@/apis/core-api/launchpad';
import ComDetailHeader from '@/components/com-detail-header';
import { ComEmptyState } from '@/components';
import { useActivate } from '@/contexts/tabs-lifecycle-context';
import { useDebouncedRequest, useTranslate } from '@/hooks';
import type { App, ProjectDetail } from '@/pages/launchpad/type';
import { useLocationNavigate } from '@/routers';
import { Search } from '@/components/lucide-icon/carbon';
import ViewModeSegmented from '@/components/lucide-icon/ViewModeSegmented';
import { Input, Spin } from 'antd';
import { type FC, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useParams, type Location } from 'react-router';
import type { ProjectItem } from '../project/types';
import AppGrid from './components/AppGrid';
import AppTable from './components/AppTable';
import { useLaunchpadPinned } from './useLaunchpadPinned';
import styles from './project-detail.module.scss';

const SEARCH_DEBOUNCE_MS = 300;

interface ProjectDetailPageProps {
  projectName?: string;
  location?: Location;
  onBack?: () => void;
  onAppClick?: (app: App) => void | Promise<void>;
}

const decodePathSegment = (value?: string) => {
  if (!value) return '';
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
};

const getLaunchpadProjectRouteParam = (pathname: string) => {
  const parts = pathname.split('/').filter(Boolean);
  if (parts[0] !== 'launchpad') {
    return '';
  }
  return decodePathSegment(parts[1]);
};

const ProjectDetailPage: FC<ProjectDetailPageProps> = ({
  projectName: propProjectName,
  location: keepAliveLocation,
  onBack,
  onAppClick,
}) => {
  const navigate = useLocationNavigate();
  const routerNavigate = useNavigate();
  const routerLocation = useLocation();
  const location = keepAliveLocation || routerLocation;
  const formatMessage = useTranslate('Launchpad');
  const formatCommon = useTranslate();
  const { isAppPinned, pinnedAppIds, toggleAppPin, refreshPinned } = useLaunchpadPinned();
  const params = useParams<{ projectName: string }>();
  const routeProjectName = useMemo(() => getLaunchpadProjectRouteParam(location.pathname), [location.pathname]);
  const projectName = propProjectName || routeProjectName || params.projectName;
  const [searchValue, setSearchValue] = useState('');
  const [appliedSearchKeyword, setAppliedSearchKeyword] = useState('');
  const [viewMode, setViewMode] = useState('card');
  const latestSearchValueRef = useRef('');
  const hasSearchKeyword = Boolean(appliedSearchKeyword);

  const {
    loading,
    data: projectDetail,
    runImmediate,
    runRefresh,
    runDebounced,
  } = useDebouncedRequest(
    async (name: string, key?: string) => {
      try {
        return await getLaunchpadProjectByNameApi(name, key);
      } catch (error: any) {
        if (error?.code === 403) {
          navigate({
            pathname: '/launchpad',
            state: { tabName: formatMessage('title') },
          });
          return null;
        }
        throw error;
      }
    },
    {
      debounceMs: SEARCH_DEBOUNCE_MS,
      initialData: null as ProjectDetail | null,
    }
  );

  const project: ProjectItem | undefined = projectDetail?.project;

  // 置顶应用排前，其余保持后端返回顺序（sort 为稳定排序）
  const apps: App[] = useMemo(() => {
    const list = projectDetail?.apps ?? [];
    const pinned = new Set(pinnedAppIds);
    return [...list].sort((a, b) => Number(pinned.has(String(b.appId))) - Number(pinned.has(String(a.appId))));
  }, [projectDetail?.apps, pinnedAppIds]);

  const fallbackProjectName = useMemo(() => {
    if (!projectName) {
      return '';
    }

    try {
      return decodeURIComponent(projectName);
    } catch {
      return projectName;
    }
  }, [projectName]);

  useEffect(() => {
    if (!projectName) {
      return;
    }

    setSearchValue('');
    latestSearchValueRef.current = '';
    setAppliedSearchKeyword('');
    runDebounced.cancel();
    void runImmediate(projectName, undefined);
  }, [projectName, runDebounced, runImmediate]);

  useActivate(() => {
    void refreshPinned();
    if (!projectName) {
      return;
    }
    runDebounced.cancel();
    void runRefresh(projectName, appliedSearchKeyword || undefined);
  });

  useEffect(() => {
    const tabName = project?.displayName || project?.name || fallbackProjectName;
    if (routerLocation.pathname !== location.pathname || routerLocation.search !== location.search) {
      return;
    }
    if (!tabName || location.state?.tabName === tabName) {
      return;
    }
    routerNavigate(location.pathname + location.search, {
      replace: true,
      state: {
        ...(location.state || {}),
        tabName,
      },
    });
  }, [
    fallbackProjectName,
    project?.displayName,
    project?.name,
    location.pathname,
    location.search,
    location.state,
    routerLocation.pathname,
    routerLocation.search,
    routerNavigate,
  ]);

  const handleBack = () => {
    if (onBack) {
      onBack();
    } else {
      navigate({
        pathname: '/launchpad',
        state: { tabName: formatMessage('title') },
      });
    }
  };

  const handleSearchChange = useCallback(
    (value: string) => {
      setSearchValue(value);
      latestSearchValueRef.current = value;

      if (!projectName) {
        return;
      }

      const keyword = value.trim();
      if (!keyword) {
        runDebounced.cancel();
        void runImmediate(projectName, undefined).finally(() => {
          if (!latestSearchValueRef.current.trim()) {
            setAppliedSearchKeyword('');
          }
        });
        return;
      }
      setAppliedSearchKeyword(keyword);
      runDebounced(projectName, keyword);
    },
    [projectName, runDebounced, runImmediate]
  );

  const handleAppClick = async (app: App) => {
    if (onAppClick) {
      await onAppClick(app);
      return;
    }
    const openInNewTab = app.manual && app.openInPlatform === false && app.url;
    if (openInNewTab) {
      void recordLaunchpadAppViewApi(app.appId);
      window.open(app.url, '_blank', 'noopener,noreferrer');
      return;
    }
    navigate({
      pathname: `/launchpad/${encodeURIComponent(String(project?.id || projectName || ''))}/${encodeURIComponent(
        String(app.appId || app.appName)
      )}`,
      state: {
        tabName: app?.displayName || app?.appName,
        tabContextName: project?.displayName || project?.name || fallbackProjectName,
        tabParentPathname: location.pathname,
      },
    });
  };

  return (
    <div className={styles.projectDetailPage}>
      <ComDetailHeader
        title={project?.displayName || project?.name || fallbackProjectName}
        desc={project?.description || ''}
        showDesc
        showBack
        onBack={handleBack}
        rightExtra={
          <div className={styles.headerActions}>
            <Input
              value={searchValue}
              allowClear
              placeholder={formatMessage('inputText')}
              prefix={<Search size={16} />}
              className={styles.searchInput}
              onChange={(event) => {
                handleSearchChange(event.target.value);
              }}
            />
            <ViewModeSegmented
              value={viewMode}
              onChange={setViewMode}
              cardTitle={formatCommon('common.cardMode')}
              listTitle={formatCommon('common.listMode')}
            />
          </div>
        }
      />

      <div className={styles.mainContent}>
        {loading ? (
          <div className={styles.loading}>
            <Spin size="large" />
          </div>
        ) : !project ? (
          <div className={styles.emptyState}>
            <p>{formatMessage('projectNotFound', undefined, 'Project not found')}</p>
          </div>
        ) : apps.length === 0 ? (
          <ComEmptyState
            title={formatMessage(hasSearchKeyword ? 'searchEmptyTitle' : 'emptyApps')}
            description={hasSearchKeyword ? formatMessage('searchEmptyDescription') : undefined}
          />
        ) : viewMode === 'list' ? (
          <AppTable
            apps={apps}
            onAppClick={handleAppClick}
            isAppPinned={isAppPinned}
            onTogglePin={(app) => toggleAppPin(app.appId)}
            keyword={appliedSearchKeyword}
          />
        ) : (
          <AppGrid
            apps={apps}
            onAppClick={handleAppClick}
            isAppPinned={isAppPinned}
            onTogglePin={(app) => toggleAppPin(app.appId)}
            keyword={appliedSearchKeyword}
          />
        )}
      </div>
    </div>
  );
};

export default ProjectDetailPage;
