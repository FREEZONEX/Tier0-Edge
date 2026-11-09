import { getLaunchpadProjectsApi, getLaunchpadRecentsApi, recordLaunchpadAppViewApi } from '@/apis/core-api/launchpad';
import NativeAppPanel from '@/pages/native-app/components/NativeAppPanel';
import { useBaseStore } from '@/stores/base';
import empty from '@/assets/common/empty.svg';
import { useActivate } from '@/contexts/tabs-lifecycle-context';
import { PageTitleIcon } from '@/components/lucide-icon';
import { useDebouncedRequest, useTranslate } from '@/hooks';
import type { App, Project } from '@/pages/launchpad/type';
import { Empty, Spin, Typography } from 'antd';
import { type FC, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import AllProjectsSection from './components/AllProjectsSection';
import PinnedRecentSection from './components/PinnedRecentSection';
import { useLaunchpadPinned } from './useLaunchpadPinned';
import { withLaunchpadStandaloneQuery } from '@/utils/launchpad-site';
import styles from './index.module.scss';

const SEARCH_DEBOUNCE_MS = 300;

const LaunchpadPage: FC = () => {
  const formatMessage = useTranslate('Launchpad');
  const navigate = useNavigate();
  const [searchValue, setSearchValue] = useState('');
  const [appliedSearchKeyword, setAppliedSearchKeyword] = useState('');
  const [viewMode, setViewMode] = useState('card');
  const [recentApps, setRecentApps] = useState<App[]>([]);
  const latestSearchValueRef = useRef('');
  const hasSearchKeyword = Boolean(appliedSearchKeyword);
  const { isAppPinned, isProjectPinned, toggleAppPin, toggleProjectPin, refreshPinned } = useLaunchpadPinned();

  const {
    loading,
    data: projects,
    runImmediate,
    runRefresh,
    runDebounced,
  } = useDebouncedRequest(
    async (keyword?: string) => {
      try {
        return await getLaunchpadProjectsApi(keyword);
      } catch (error: any) {
        if (error?.code === 403) {
          return [] as Project[];
        }
        throw error;
      }
    },
    {
      debounceMs: SEARCH_DEBOUNCE_MS,
      initialData: [] as Project[],
    }
  );

  const loadRecents = useCallback(async () => {
    try {
      const res = await getLaunchpadRecentsApi();
      setRecentApps(res?.list || []);
    } catch {
      setRecentApps([]);
    }
  }, []);

  useEffect(() => {
    void runImmediate(undefined);
    void loadRecents();
  }, [runImmediate, loadRecents]);

  useActivate(() => {
    runDebounced.cancel();
    void runRefresh(appliedSearchKeyword || undefined);
    void loadRecents();
    void refreshPinned();
  });

  const handleSearchChange = useCallback(
    (value: string) => {
      setSearchValue(value);
      latestSearchValueRef.current = value;
      const keyword = value.trim();
      if (!keyword) {
        runDebounced.cancel();
        void runImmediate(undefined).finally(() => {
          if (!latestSearchValueRef.current.trim()) {
            setAppliedSearchKeyword('');
          }
        });
        return;
      }
      setAppliedSearchKeyword(keyword);
      runDebounced(keyword);
    },
    [runDebounced, runImmediate]
  );

  const handleProjectClick = (project: Project) => {
    navigate(withLaunchpadStandaloneQuery(`/launchpad/${encodeURIComponent(project.projectName)}`), {
      state: {
        tabName: project.displayName || project.projectName,
        tabParentPathname: '/launchpad',
      },
    });
  };

  const handleAppClick = (app: App) => {
    const openInNewTab = app.manual && app.openInPlatform === false && app.url;
    if (openInNewTab) {
      void recordLaunchpadAppViewApi(app.appId);
      window.open(app.url, '_blank', 'noopener,noreferrer');
      return;
    }
    navigate(
      withLaunchpadStandaloneQuery(
        `/launchpad/${encodeURIComponent(app.projectName || '')}/${encodeURIComponent(String(app.appId || app.appName))}`
      ),
      {
        state: {
          tabName: app.displayName || app.appName,
          tabContextName: app.projectDisplayName || app.projectName,
          tabParentPathname: `/launchpad/${encodeURIComponent(app.projectName || '')}`,
        },
      }
    );
  };

  const isInitialEmpty = !loading && projects.length === 0 && !hasSearchKeyword;

  // 置顶项目排最前；其余对齐云端：App 数量降序，数量相同按名称升序，保证顺序稳定
  const sortedProjects = useMemo(
    () =>
      [...projects].sort((a, b) => {
        const pinnedDiff = Number(isProjectPinned(b.projectId)) - Number(isProjectPinned(a.projectId));
        if (pinnedDiff !== 0) return pinnedDiff;
        const countDiff = (b.apps?.length || 0) - (a.apps?.length || 0);
        if (countDiff !== 0) return countDiff;
        const nameA = a.displayName || a.projectName;
        const nameB = b.displayName || b.projectName;
        return nameA.localeCompare(nameB);
      }),
    [projects, isProjectPinned]
  );

  return (
    <div className={styles.launchpadPage}>
      <div className={styles.pageHeader}>
        <div className={styles.pageTitle}>
          <PageTitleIcon resourceKey="launchpad.view" />
          <h1>{formatMessage('title')}</h1>
        </div>
      </div>
      <div className={styles.mainContent}>
        {loading ? (
          <div className={styles.loading}>
            <Spin size="large" />
          </div>
        ) : isInitialEmpty ? (
          <div className={styles.emptyState}>
            <Empty
              image={<img src={empty} alt="" />}
              description={
                <div className={styles['empty-desc']}>
                  <Typography.Title level={3} className={styles['empty-title']}>
                    {formatMessage('noProjectsTitle')}
                  </Typography.Title>
                  <Typography.Text type="secondary" className={styles['empty-text']}>
                    {formatMessage('noProjectsDescription')}
                  </Typography.Text>
                </div>
              }
            />
          </div>
        ) : (
          <>
            {!hasSearchKeyword && (
              <PinnedRecentSection
                apps={recentApps}
                isAppPinned={isAppPinned}
                onTogglePin={(app) => toggleAppPin(app.appId)}
                onAppClick={handleAppClick}
              />
            )}
            <AllProjectsSection
              projects={sortedProjects}
              searchValue={searchValue}
              onSearchChange={handleSearchChange}
              appliedSearchKeyword={appliedSearchKeyword}
              hasSearchKeyword={hasSearchKeyword}
              viewMode={viewMode}
              onViewModeChange={setViewMode}
              isProjectPinned={isProjectPinned}
              onTogglePin={(project) => toggleProjectPin(project.projectId)}
              onProjectClick={handleProjectClick}
              onAppClick={handleAppClick}
            />
          </>
        )}
      </div>
    </div>
  );
};

export default function Launchpad() {
  const t = useTranslate('Launchpad');
  const nativeApps = useBaseStore(
    (s) => s.systemInfo?.runtimeMode === 'native' && s.systemInfo?.capabilities?.app?.enabled
  );
  return nativeApps ? (
    <div className={styles.launchpadPage}>
      <div className={styles.pageHeader}>
        <div className={styles.pageTitle}>
          <PageTitleIcon resourceKey="launchpad.view" />
          <h1>{t('title')}</h1>
        </div>
      </div>
      <div className={styles.mainContent}>
        <NativeAppPanel launchpad />
      </div>
    </div>
  ) : (
    <LaunchpadPage />
  );
}
