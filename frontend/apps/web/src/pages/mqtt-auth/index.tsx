import { Alert } from 'antd';
import { Navigate, useLocation } from 'react-router';
import ComLayout from '@/components/com-layout';
import { useTranslate } from '@/hooks';
import FleetCenterConnection from '@/pages/fleet-management/FleetCenterConnection';
import { useFleetConnectionAccess } from '@/pages/fleet-management/fleet-permission';
import IntegrationTabs from '@/pages/uns/components/source-stream/IntegrationTabs';
import styles from './index.module.scss';

export function ConnectEdgeWorkspace() {
  const t = useTranslate();
  const { allowed } = useFleetConnectionAccess();
  return (
    <ComLayout className={styles.page}>
      <IntegrationTabs />
      <div className={`${styles.content} ${styles.edgeContent}`}>
        {!allowed ? (
          <Alert type="error" showIcon message={t('SourceStream.forbidden')} />
        ) : (
          <FleetCenterConnection />
        )}
      </div>
    </ComLayout>
  );
}

export default function EdgeConnectionPage() {
  const location = useLocation();
  const params = new URLSearchParams(location.search);
  params.set('view', 'edge');
  return <Navigate to={{ pathname: '/connect', search: `?${params}`, hash: location.hash }} replace />;
}
