import { useEffect, useState } from 'react';
import { Alert, Empty, Space, Spin, Tabs, Tag } from 'antd';
import { Edit, Renew } from '@carbon/icons-react';
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router';
import ComButton from '@/components/com-button';
import ComLayout from '@/components/com-layout';
import ComLeft from '@/components/com-layout/ComLeft';
import ComContent from '@/components/com-layout/ComContent';
import { ButtonPermission } from '@/common-types/button-permission';
import { useTranslate } from '@/hooks';
import { loadMetadata, type UnsMetadata } from '@/apis/core-api/uns-metadata';
import { unsV2Api } from '@/apis/core-api/uns-v2';
import { SchemaView } from '@/pages/uns/components/metadata/data-panel';
import MetadataEditor from '@/pages/uns/components/metadata/metadata-editor';
import useConnections from '@/pages/uns/components/metadata/use-connections';
import IntegrationTabs from '@/pages/uns/components/source-stream/IntegrationTabs';
import CollectionNavigator from './components/collection-navigator';
import Records from './components/records';
import Samples from './components/samples';
import Relations from './components/relations';
import styles from './index.module.scss';

export default function Collections() {
  const route = useParams();
  const [params, setParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const t = useTranslate();
  const [revision, setRevision] = useState(0);
  const collectionId = route.collectionId || params.get('collectionId') || '';
  const select = (id: string, replace = false) => {
    // 缓存页签失活后，不允许自动选择首项把用户导航拉回集合页。
    if (
      location.pathname !== '/uns' &&
      location.pathname !== '/collections' &&
      !location.pathname.startsWith('/collections/')
    )
      return;
    if (location.pathname === '/uns') {
      setParams(
        (previous) => {
          const next = new URLSearchParams(previous);
          next.set('view', 'collections');
          next.set('collectionId', id);
          next.delete('tab');
          return next;
        },
        { replace }
      );
      return;
    }
    navigate({ pathname: '/collections', search: new URLSearchParams({ collectionId: id }).toString() }, { replace });
  };
  return (
    <ComLayout className="unsContainer">
      <ComLeft defaultWidth={300} resize style={{ height: '100%', overflow: 'hidden' }}>
        <div className={styles['collection-sidebar']}>
          <IntegrationTabs />
          <CollectionNavigator selectedId={collectionId} revision={revision} onSelect={select} />
        </div>
      </ComLeft>
      <ComContent wrapperStyle={{ minWidth: 0 }} style={{ padding: 0, display: 'flex', minHeight: 0 }}>
        <main className={styles['collection-main']}>
          {collectionId ? (
            <CollectionWorkspace
              key={collectionId}
              collectionId={collectionId}
              onSaved={() => setRevision((v) => v + 1)}
            />
          ) : (
            <Empty description={t('Collections.selectCollection')} />
          )}
        </main>
      </ComContent>
    </ComLayout>
  );
}

function CollectionWorkspace({ collectionId, onSaved }: { collectionId: string; onSaved: () => void }) {
  const t = useTranslate();
  const [params, setParams] = useSearchParams();
  const [collection, setCollection] = useState<UnsMetadata>();
  const [failed, setFailed] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const [editing, setEditing] = useState(false);
  const [items, setItems] = useState<UnsMetadata[]>([]);
  const connections = useConnections(refresh, editing);
  useEffect(() => {
    const controller = new AbortController();
    unsV2Api
      .collectionGet(collectionId, controller.signal)
      .then((result) => {
        if (!controller.signal.aborted) {
          setCollection(result);
          setFailed(false);
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) setFailed(true);
      });
    return () => controller.abort();
  }, [collectionId, refresh]);
  if (failed)
    return (
      <Alert
        type="error"
        message={t('UnsMeta.loadFailed')}
        action={<ComButton onClick={() => setRefresh((v) => v + 1)}>{t('UnsMeta.refresh')}</ComButton>}
      />
    );
  if (!collection) return <Spin />;
  const requestedTab = params.get('tab') || 'records';
  const tab = ['records', 'schema', 'relations', ...(collection.data.sampleSchema ? ['samples'] : [])].includes(
    requestedTab
  )
    ? requestedTab
    : 'records';
  const refreshAll = () => setRefresh((v) => v + 1);
  const edit = async () => {
    setItems(await loadMetadata());
    setEditing(true);
  };
  return (
    <div className={styles.workspace}>
      <header className={styles.header}>
        <div>
          <h2>{collection.data.name}</h2>
          <Space wrap>
            <span className={styles.muted}>{collection.id}</span>
            <Tag>{t(collection.data.sampleSchema ? 'Collections.withSamples' : 'Collections.recordsOnly')}</Tag>
          </Space>
          {collection.data.description && <p className={styles.description}>{collection.data.description}</p>}
        </div>
        <Space>
          <ComButton icon={<Renew />} onClick={refreshAll}>
            {t('UnsMeta.refresh')}
          </ComButton>
          {!collection.mountId && (
            <ComButton icon={<Edit />} auth={ButtonPermission['uns.metadataWrite']} onClick={edit}>
              {t('Collections.edit')}
            </ComButton>
          )}
        </Space>
      </header>
      <Tabs
        activeKey={tab}
        onChange={(key) =>
          setParams((previous) => {
            const next = new URLSearchParams(previous);
            next.set('tab', key);
            return next;
          })
        }
        items={[
          {
            key: 'records',
            label: t('UnsMeta.records'),
            children: <Records key={refresh} collection={collection} readOnly={!!collection.mountId} />,
          },
          ...(collection.data.sampleSchema
            ? [
                {
                  key: 'samples',
                  label: t('Collections.samples'),
                  children: <Samples key={refresh} collection={collection} />,
                },
              ]
            : []),
          { key: 'schema', label: t('Collections.schema'), children: <SchemaView collection={collection} /> },
          {
            key: 'relations',
            label: t('Collections.relations'),
            children: <Relations key={refresh} collection={collection} />,
          },
        ]}
      />
      {editing && (
        <MetadataEditor
          kind="collection"
          standalone
          current={collection}
          items={items}
          connections={connections}
          onClose={() => setEditing(false)}
          onSaved={() => {
            setEditing(false);
            refreshAll();
            onSaved();
          }}
        />
      )}
    </div>
  );
}
