import { isRemovedFeatureEntry } from '@/utils/removed-feature-pages';
import { capabilityEnabled } from '@/utils/product-capabilities';
import { productName } from '@/utils/product-edition';
import { type ReactNode, createElement, useCallback, useEffect, useRef, useState } from 'react';
import ComEmpty from '@/components/com-empty';
import { AuthButton } from '@/components/auth';
import { ButtonPermission } from '@/common-types/button-permission';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import styles from './index.module.scss';
import { useActivate } from '@/contexts/tabs-lifecycle-context.ts';
import ComLayout from '@/components/com-layout';
import ComContent from '@/components/com-layout/ComContent';
import { fetchBaseStore, useBaseStore } from '@/stores/base';
import { useTranslate } from '@/hooks';
import { getHomeRecents, type HomeRecentItem } from '@/apis/core-api';
import { getRuntimeMetrics, type RuntimeMetrics } from '@/apis/core-api/system-config';
import useMenuNavigate from '@/hooks/useMenuNavigate';
import type { ResourceProps } from '@/stores/types';
import { resolveMenuLucideIcon } from '@/components/lucide-icon';
import { useNavigate } from 'react-router';

dayjs.extend(relativeTime);

type RecentWorkItem = {
  key: string;
  type: string;
  title: string;
  time: string;
  icon: ReactNode;
  item?: ResourceProps;
  path?: string;
  disabled?: boolean;
};

type MetricItem = {
  key: string;
  label: string;
  value: string;
  meta?: string;
  metaUsed?: string;
  metaTotal?: string;
  percent: number;
};

const RECENT_TYPE_KEY_BY_RESOURCE_TYPE: Record<string, string> = {
  uns: 'home.recentTypeUns',
  namespace: 'home.recentTypeUns',
  sourceflow: 'home.recentTypeFlow',
  eventflow: 'home.recentTypeFlow',
  flow: 'home.recentTypeFlow',
  notebook: 'Notebook.title',
  project: 'Project',
  projectapp: 'project.apps',
  app: 'project.apps',
  launchpad: 'Launchpad.title',
  oauthclient: 'menu.oauthClient',
  apikey: 'menu.apiKey',
  edgeconnection: 'menu.mqttAuth',
  mqttauth: 'menu.mqttAuth',
  routingmanagement: 'route.routingManagement',
  gateway: 'route.routingManagement',
  cluster: 'home.actionCluster',
};

const normalizeRecentType = (value?: string) =>
  String(value || '')
    .replace(/[\s_-]+/g, '')
    .toLowerCase();

const getRecentTypeKey = (item: HomeRecentItem) => {
  const resourceKey = String(item.resourceKey || '').trim();
  const resourceType = normalizeRecentType(item.resourceType);
  if (resourceType === 'app' || resourceType === 'projectapp') return 'project.apps';
  if (resourceKey.startsWith('uns.')) return 'home.recentTypeUns';
  if (resourceKey.startsWith('flow.')) return 'home.recentTypeFlow';
  if (resourceKey.startsWith('notebook.')) return 'Notebook.title';
  if (resourceKey.startsWith('project.')) return 'Project';
  if (resourceKey.startsWith('launchpad.')) return 'Launchpad.title';
  if (resourceKey.startsWith('oauth.client.')) return 'menu.oauthClient';
  if (resourceKey.startsWith('apikey.')) return 'menu.apiKey';
  if (resourceKey.startsWith('mqtt.auth.')) return 'menu.mqttAuth';
  if (resourceKey.startsWith('gateway.route.')) return 'route.routingManagement';
  if (resourceKey.startsWith('cluster.')) return 'home.actionCluster';
  return RECENT_TYPE_KEY_BY_RESOURCE_TYPE[resourceType];
};

const formatNumber = (value?: number) => new Intl.NumberFormat().format(Number(value || 0));

const formatTime = (value?: number) => (value ? dayjs(value).fromNow() : '-');

const formatBytes = (value?: number) => {
  const bytes = Number(value || 0);
  if (!bytes) return '-';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let size = bytes;
  let index = 0;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index += 1;
  }
  return `${size >= 10 || index === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[index]}`;
};

const SectionHeader = ({ title, description }: { title: string; description?: string }) => (
  <div className={styles['section-header']}>
    <h2>{title}</h2>
    {description ? <p>{description}</p> : null}
  </div>
);

const RecentWorkRow = ({ item, onNavigate }: { item: RecentWorkItem; onNavigate: (item?: ResourceProps) => void }) => {
  const navigate = useNavigate();
  const canNavigate = !item.disabled && Boolean(item.item || item.path);
  const openItem = () => {
    if (!canNavigate) {
      return;
    }
    if (item.path) {
      navigate(item.path, {
        state: {
          tabName: item.title,
        },
      });
      return;
    }
    onNavigate(item.item);
  };

  return (
    <button type="button" className={styles['recent-row']} onClick={openItem} disabled={!canNavigate}>
      <span className={styles['recent-icon']}>{item.icon}</span>
      <span className={styles['recent-name']}>
        <strong>{item.title}</strong>
      </span>
      <span className={styles['recent-type']}>{item.type}</span>
      <span className={styles['recent-time']}>{item.time}</span>
    </button>
  );
};

const NodeResourcesPanel = ({ metrics, onOpen }: { metrics?: RuntimeMetrics; onOpen: () => void }) => {
  const showRuntime = useBaseStore((state) => capabilityEnabled(state.systemInfo, 'containerConsole'));
  const formatMessage = useTranslate();
  const cpuMetric = metrics?.hostCpu || metrics?.cpu;
  const memoryMetric = metrics?.hostMemory || metrics?.memory;
  const hasMetrics = Boolean(metrics?.collectedAt || cpuMetric || memoryMetric || metrics?.disk);
  const metricItems: MetricItem[] = [
    {
      key: 'cpu',
      label: formatMessage('home.nodeCpu'),
      value: cpuMetric && cpuMetric.available !== false ? `${Number(cpuMetric.usagePercent || 0).toFixed(1)}%` : '-',
      meta: cpuMetric?.logicalCores
        ? formatMessage('home.nodeCpuMeta', { count: formatNumber(cpuMetric.logicalCores) })
        : undefined,
      percent: cpuMetric?.usagePercent || 0,
    },
    {
      key: 'memory',
      label: formatMessage('home.nodeMemory'),
      value:
        memoryMetric && memoryMetric.available !== false
          ? `${Number(memoryMetric.usagePercent || 0).toFixed(1)}%`
          : '-',
      metaUsed: formatBytes(memoryMetric?.usedBytes),
      metaTotal: formatBytes(memoryMetric?.totalBytes),
      percent: memoryMetric?.usagePercent || 0,
    },
    {
      key: 'disk',
      label: formatMessage('home.nodeDisk'),
      value: `${Number(metrics?.disk?.usagePercent || 0).toFixed(1)}%`,
      metaUsed: formatBytes(metrics?.disk?.usedBytes),
      metaTotal: formatBytes(metrics?.disk?.totalBytes),
      percent: metrics?.disk?.usagePercent || 0,
    },
  ];

  return (
    <section className={styles['node-resource-panel']}>
      <div className={styles['resource-summary']}>
        <SectionHeader
          title={formatMessage('home.nodeResources')}
          description={formatMessage('home.nodeResourcesDesc')}
        />
        {showRuntime && (
          <AuthButton
            auth={ButtonPermission['FleetRuntime.view']}
            type="primary"
            className={styles['runtime-action']}
            onClick={onOpen}
          >
            {formatMessage('fleet.runtime.title')}
          </AuthButton>
        )}
      </div>
      {hasMetrics ? (
        <div className={styles['resource-metrics']}>
          {metricItems.map((item) => (
            <div className={styles['resource-card']} key={item.key}>
              <span className={styles['resource-label']}>{item.label}</span>
              <strong className={styles['resource-value']}>{item.value}</strong>
              <div className={styles['resource-meter']}>
                <i style={{ width: `${Math.min(Math.max(item.percent, 0), 100)}%` }} />
              </div>
              {item.metaUsed || item.metaTotal ? (
                <em className={styles['resource-meta']}>
                  <span className={styles['resource-meta-used']}>{item.metaUsed || '-'}</span>
                  <span className={styles['resource-meta-total']}>{` / ${item.metaTotal || '-'}`}</span>
                </em>
              ) : (
                <em className={styles['resource-meta']}>{item.meta}</em>
              )}
            </div>
          ))}
        </div>
      ) : (
        <div className={styles['empty-state']}>
          <ComEmpty description={formatMessage('home.nodeResourcesEmpty')} />
        </div>
      )}
    </section>
  );
};

const Index = () => {
  const formatMessage = useTranslate();
  const navigateMenu = useMenuNavigate();
  const navigate = useNavigate();
  const appTitle = useBaseStore((state) => state.systemInfo?.appTitle || productName(state.systemInfo));
  const isAdmin = useBaseStore((state) => state.currentUserInfo?.superAdmin === true);

  const [initialLoading, setInitialLoading] = useState(false);
  const [runtimeMetrics, setRuntimeMetrics] = useState<RuntimeMetrics>();
  const [homeRecents, setHomeRecents] = useState<HomeRecentItem[]>([]);
  const hasLoadedHomeRef = useRef(false);

  const loadHome = useCallback(
    async (options: { silent?: boolean } = {}) => {
      const showLoading = !options.silent && !hasLoadedHomeRef.current;
      if (showLoading) {
        setInitialLoading(true);
      }
      try {
        if (isAdmin) {
          try {
            setRuntimeMetrics(await getRuntimeMetrics());
          } catch {
            setRuntimeMetrics(undefined);
          }
        } else {
          setRuntimeMetrics(undefined);
        }
        setHomeRecents(await getHomeRecents());
        hasLoadedHomeRef.current = true;
      } finally {
        if (showLoading) {
          setInitialLoading(false);
        }
      }
    },
    [isAdmin]
  );

  useEffect(() => {
    fetchBaseStore?.();
  }, []);

  useEffect(() => {
    void loadHome();
  }, [loadHome]);

  useEffect(() => {
    if (!isAdmin) return;
    let active = true;
    const timer = window.setInterval(() => {
      getRuntimeMetrics()
        .then((metrics) => {
          if (active) setRuntimeMetrics(metrics);
        })
        .catch(() => {
          // 临时刷新失败时保留上一次成功采样，避免卡片闪空。
        });
    }, 5000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [isAdmin]);

  useActivate(() => {
    fetchBaseStore?.();
    void loadHome({ silent: true });
  });

  const recentItems: RecentWorkItem[] = homeRecents
    .filter((item) => !isRemovedFeatureEntry({ resourceKey: item.resourceKey, url: item.routePath }))
    .map((item) => {
      const typeKey = getRecentTypeKey(item);
      const actionType = normalizeRecentType(item.businessType);
      const disabled = actionType === 'delete';
      return {
        key: item.key,
        type: typeKey ? formatMessage(typeKey, undefined, item.resourceType) : item.resourceType,
        title: item.resourceName,
        time: formatTime(item.operatedAt),
        icon: createElement(
          resolveMenuLucideIcon({
            resourceKey: item.resourceKey,
            url: item.routePath,
            icon: item.icon,
          } as ResourceProps),
          { size: 12, strokeWidth: 1.75 }
        ),
        path: disabled ? undefined : item.routePath,
        disabled,
      };
    });

  return (
    <ComLayout className={styles['home-page']} loading={initialLoading}>
      <ComContent title={<div />} hasBack={false} mustShowTitle={false}>
        <div className={styles['home-shell']}>
          <header className={styles['welcome-block']}>
            <h1 className={styles['welcome-title']}>
              {formatMessage('common.welcome', { appTitle }, `Welcome to ${appTitle}!`)}
            </h1>
          </header>

          <div className={styles['home-content']}>
            {isAdmin ? <NodeResourcesPanel metrics={runtimeMetrics} onOpen={() => navigate('/runtime')} /> : null}

            <section className={styles['recent-panel']}>
              <SectionHeader
                title={formatMessage('home.recentWork')}
                description={formatMessage('home.recentWorkDesc')}
              />
              <div className={styles['recent-list']}>
                {recentItems.length ? (
                  recentItems.map((item) => <RecentWorkRow key={item.key} item={item} onNavigate={navigateMenu} />)
                ) : (
                  <div className={styles['empty-state']}>
                    <ComEmpty description={formatMessage('home.noRecentWork')} />
                  </div>
                )}
              </div>
            </section>
          </div>
        </div>
      </ComContent>
    </ComLayout>
  );
};

export default Index;
