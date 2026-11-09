import { useCallback, useEffect, useState } from 'react';
import { Space, Tag, Tooltip } from 'antd';
import { fleetAction } from '@/apis/core-api/fleet-acquisition';
import { useTranslate } from '@/hooks';
export default function FleetBusinessStatus({
  nodeId,
  registrationStatus,
  networkStatus: observedNetworkStatus,
}: {
  nodeId: string;
  registrationStatus?: string;
  networkStatus?: string;
}) {
  const t = useTranslate();
  const [state, setState] = useState<{
    businessStatus: string;
    networkStatus: string;
    networkAddresses?: string[];
    reason?: string;
  }>();
  const check = useCallback(async () => {
    try {
      setState(await fleetAction('nodes/health/check', { nodeId }, { silent: true }));
    } catch {
      setState({ businessStatus: 'unchecked', networkStatus: 'unchecked' });
    }
  }, [nodeId]);
  useEffect(() => {
    if (registrationStatus !== 'registered') return;
    void check();
    const timer = window.setInterval(() => void check(), 30000);
    return () => window.clearInterval(timer);
  }, [check, registrationStatus]);
  const networkStatus =
    (registrationStatus === 'registered' ? state?.networkStatus : undefined) || observedNetworkStatus || 'unchecked';
  const businessStatus =
    registrationStatus && registrationStatus !== 'registered'
      ? registrationStatus
      : state?.businessStatus || 'unchecked';
  return (
    <Space wrap>
      {networkStatus !== 'direct' && (
        <Tooltip title={state?.networkAddresses?.join(', ') || t('fleet.collaboration.networkHelp')}>
          <Tag color={networkStatus === 'online' ? 'success' : 'default'}>
            Tailscale: {t('fleet.collaboration.' + networkStatus)}
          </Tag>
        </Tooltip>
      )}
      <Tooltip title={state?.reason}>
        <Tag color={businessStatus === 'available' ? 'success' : 'default'}>
          {t('fleet.ui.connection')}:{' '}
          {t(businessStatus === 'available' ? 'fleet.ui.connected' : 'fleet.collaboration.' + businessStatus)}
        </Tag>
      </Tooltip>
    </Space>
  );
}
