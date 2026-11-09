import { useSourceStreamApi } from '@/apis/core-api/source-stream-scope';
import { useEffect, useState, type FC } from 'react';
import { Tabs } from 'antd';
import { Flow, Plug, Network_3 } from '@carbon/icons-react';
import { ButtonPermission } from '@/common-types/button-permission';
import { useTranslate } from '@/hooks';
import { useBaseStore } from '@/stores/base';
import { hasPermission } from '@/utils/auth';
import { useFleetConnectionAccess } from '@/pages/fleet-management/fleet-permission';

import { useRegistryRevision, type useIntegrationRoute } from './IntegrationTabs';
import { ConnectHeaderSlot } from './ConnectHeaderActions';
import styles from './integration-navigation.module.scss';

const IntegrationNavigation: FC<ReturnType<typeof useIntegrationRoute>> = ({
  view,
  go,
  sourceId,
  streamId,
  create,
}) => {
  const sourceStreamApi = useSourceStreamApi();
  const { isHost, allowed: edgeAllowed } = useFleetConnectionAccess();
  const t = useTranslate();
  const { revision } = useRegistryRevision();
  const user = useBaseStore((state) => state.currentUserInfo);
  useBaseStore((state) => state.buttonList);
  useBaseStore((state) => state.systemInfo?.authEnable);
  const sourceAllowed = hasPermission(ButtonPermission['uns.sources']);
  const streamAllowed = hasPermission(ButtonPermission['uns.streams']);
  const [counts, setCounts] = useState<{ source?: number; stream?: number }>({});
  useEffect(() => {
    const controller = new AbortController();
    setCounts({});
    if (view !== 'namespace' && view !== 'edge') {
      if (sourceAllowed)
        sourceStreamApi
          .sources({ page: 1, size: 1 }, controller.signal)
          .then((result) => {
            if (!controller.signal.aborted) setCounts((previous) => ({ ...previous, source: result.total }));
          })
          .catch(() => {});
      if (streamAllowed)
        sourceStreamApi
          .streams({ page: 1, size: 1 }, controller.signal)
          .then((result) => {
            if (!controller.signal.aborted) setCounts((previous) => ({ ...previous, stream: result.total }));
          })
          .catch(() => {});
    }
    return () => controller.abort();
  }, [sourceAllowed, streamAllowed, view, sourceId, streamId, create, user?.sub, revision]);
  if (view === 'namespace') return null;
  const items = [
    ...(sourceAllowed
      ? [
          {
            key: 'source',
            label: (
              <>
                <Plug size={18} />
                {t('SourceStream.source')}
                {counts.source !== undefined && <span className={styles.count}>{counts.source}</span>}
              </>
            ),
          },
        ]
      : []),
    ...(streamAllowed
      ? [
          {
            key: 'stream',
            label: (
              <>
                <Flow size={18} />
                {t('SourceStream.stream')}
                {counts.stream !== undefined && <span className={styles.count}>{counts.stream}</span>}
              </>
            ),
          },
        ]
      : []),
    ...(edgeAllowed
      ? [
          {
            key: 'edge',
            label: (
              <>
                <Network_3 size={18} />
                {t(isHost ? 'fleet.connect.fleet' : 'fleet.connect.edge')}
              </>
            ),
          },
        ]
      : []),
  ];
  return (
    <header className={styles.navigation}>
      <div className={styles.heading}>
        <h1>{t('UnsConnect.connect')}</h1>
        <ConnectHeaderSlot className={styles.actions} />
      </div>
      <Tabs activeKey={view} items={items} onChange={(next) => go({ view: next as 'source' | 'stream' | 'edge' })} />
    </header>
  );
};
export default IntegrationNavigation;
