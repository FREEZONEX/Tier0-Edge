import { Tabs } from 'antd';
import { Navigate, useLocation, useSearchParams } from 'react-router';
import { useTranslate } from '@/hooks';
import RelationshipGraph from '@/pages/uns-overview';
import { relationshipGraphLocation } from '@/pages/uns-overview/navigation';
import RelationshipCanvas from './index';
import { MetadataBrowser, CollectionDataBrowser } from './data-browser';
import styles from './data-browser.module.scss';
import { IntegrationGuardProvider } from '../source-stream/IntegrationTabs';

export function OverviewRedirect() {
  const location = useLocation();
  return <Navigate to={relationshipGraphLocation(location.search)} state={location.state} replace />;
}

export default function DataTopologyPage() {
  const t = useTranslate();
  const [params, setParams] = useSearchParams();
  const view = params.get('view') || 'topology';
  const active = ['graph', 'meta', 'record', 'sample'].includes(view) ? view : 'topology';
  return (
    <IntegrationGuardProvider>
      <div className={styles.page}>
        <header className={styles.heading}>
          <h2>{t('UnsGraph.title')}</h2>
        </header>
        <Tabs
          activeKey={active}
          destroyInactiveTabPane
          onChange={(value) =>
            setParams((old) => {
              const next = new URLSearchParams(old);
              next.set('view', value);
              if (!old.get('collectionId') && old.get('kind') === 'collection' && old.get('id')) {
                next.set('collectionId', old.get('id')!);
              }
              // 图谱的对象层级与筛选只属于图谱，不能带入其他视图。
              [
                'kind',
                'id',
                'recordId',
                'page',
                'recordPage',
                'samplePage',
                'time',
                'from',
                'to',
                'kinds',
                'search',
                'archived',
              ].forEach((key) => next.delete(key));
              return next;
            })
          }
          items={[
            {
              key: 'topology',
              label: t('DataBrowser.topology'),
              children: <RelationshipCanvas embedded showHeading={false} />,
            },
            { key: 'graph', label: t('UnsOverview.title'), children: <RelationshipGraph /> },
            { key: 'meta', label: t('DataBrowser.meta'), children: <MetadataBrowser /> },
            { key: 'record', label: t('DataBrowser.record'), children: <CollectionDataBrowser mode="record" /> },
            { key: 'sample', label: t('DataBrowser.sample'), children: <CollectionDataBrowser mode="sample" /> },
          ]}
        />
      </div>
    </IntegrationGuardProvider>
  );
}
