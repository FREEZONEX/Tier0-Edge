import { useEffect, useState } from 'react';
import { Alert, Button, Spin } from 'antd';
import { useParams } from 'react-router';
import { useTranslate } from '@/hooks';
import { loadTopologyApp, type TopologyMeta } from '@/apis/core-api/uns-topology';
import styles from './app-page.module.scss';

// Resolve the authorized entry on open/refresh; route state never supplies an iframe URL.
export default function UnsAppPage() {
  const { appId = '' } = useParams();
  const t = useTranslate();
  const [reload, setReload] = useState(0);
  const requestKey = `${appId}:${reload}`;
  const [result, setResult] = useState<{ key: string; app?: TopologyMeta; failed?: boolean }>();
  useEffect(() => {
    const controller = new AbortController();
    loadTopologyApp(appId, controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) setResult({ key: requestKey, app: value });
      })
      .catch(() => {
        if (!controller.signal.aborted) setResult({ key: requestKey, failed: true });
      });
    return () => controller.abort();
  }, [appId, requestKey]);
  if (result?.key !== requestKey)
    return (
      <div className={styles['status']}>
        <Spin />
      </div>
    );
  const { app, failed } = result;
  const href = app?.launch?.status === 'ready' ? app.launch.url : undefined;
  if (failed || !href || !app)
    return (
      <div className={styles['status']}>
        <Alert
          type="warning"
          showIcon
          message={t(failed ? 'AppEntry.loadFailed' : `AppEntry.${app?.launch?.status || 'unconfigured'}`)}
          action={<Button onClick={() => setReload((value) => value + 1)}>{t('AppEntry.retry')}</Button>}
        />
      </div>
    );
  return <iframe className={styles['frame']} title={app.data.name} src={href} referrerPolicy="no-referrer" />;
}
