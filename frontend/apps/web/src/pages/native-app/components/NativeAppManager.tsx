import { type FC, useState } from 'react';
import { Alert, Space, Typography } from 'antd';
import type { NativeAppAction, NativeAppInstallation } from '@/apis/core-api/native-app';
import { AuthButton } from '@/components/auth';
import Drawer from '@/components/com-drawer';
import { ButtonPermission } from '@/common-types/button-permission';
import { useTranslate } from '@/hooks';
import styles from './NativeAppPanel.module.scss';
import { NativeAppStatus } from './NativeAppCatalog';
import NativeAppStreamPolicy from './NativeAppStreamPolicy';
import NativeAppRequestGrants from './NativeAppRequestGrants';
import HelpTooltip from '@/components/help-tooltip';
import { useBaseStore } from '@/stores/base';

interface Props {
  app?: NativeAppInstallation;
  busy: boolean;
  actionLoading: boolean;
  onClose: () => void;
  onStart: (app: NativeAppInstallation) => void;
  onConfirm: (app: NativeAppInstallation, action: Exclude<NativeAppAction, 'start'>) => void;
  onReplace: (app: NativeAppInstallation) => void;
  onLogs: (app: NativeAppInstallation) => void;
}

const NativeAppManager: FC<Props> = ({ app, busy, actionLoading, onClose, onStart, onConfirm, onReplace, onLogs }) => {
  const t = useTranslate();
  const auth = ButtonPermission['uns.appManage'];
  const [policyLocked, setPolicyLocked] = useState(false);
  const [requestsLocked, setRequestsLocked] = useState(false);
  const locked = policyLocked || requestsLocked;
  const actionBusy = busy || locked;
  const user = useBaseStore((state) => state.currentUserInfo);
  const canRefreshCredentials =
    user?.superAdmin === true ||
    (user?.resourceKeys?.includes('uns.app.manage') && user.resourceKeys.includes('uns.app.credentials'));
  return (
    <Drawer
      width={560}
      title={t('nativeApp.manage')}
      open={!!app}
      onClose={() => {
        if (!locked) onClose();
      }}
      maskClosable={!locked}
      keyboard={!locked}
    >
      {app && (
        <div className={`${styles.stack} ${styles['manager-content']}`}>
          <Typography.Title level={5}>{app.name}</Typography.Title>
          <Space>
            <Typography.Text type="secondary">{app.version || '-'}</Typography.Text>
            <NativeAppStatus status={app.status} />
          </Space>
          {app.lastError && <Alert type="error" showIcon message={app.lastError} />}
          <Space wrap>
            <AuthButton
              auth={auth}
              loading={actionLoading}
              disabled={actionBusy || app.status === 'starting'}
              onClick={() => (app.status === 'running' ? onConfirm(app, 'stop') : onStart(app))}
            >
              {t(app.status === 'running' ? 'nativeApp.stop' : 'nativeApp.start')}
            </AuthButton>
            <AuthButton
              auth={auth}
              disabled={actionBusy || app.status !== 'running'}
              onClick={() => onConfirm(app, 'restart')}
            >
              {t('nativeApp.restart')}
            </AuthButton>
            <AuthButton auth={auth} disabled={actionBusy} onClick={() => onReplace(app)}>
              {t('nativeApp.update')}
            </AuthButton>
            <AuthButton auth={auth} onClick={() => onLogs(app)}>
              {t('nativeApp.logs')}
            </AuthButton>
            <AuthButton
              auth={auth}
              type="text"
              danger
              disabled={actionBusy}
              onClick={() => onConfirm(app, 'uninstall')}
            >
              {t('nativeApp.uninstall')}
            </AuthButton>
          </Space>
          {canRefreshCredentials && (
            <Space>
              <AuthButton
                auth={ButtonPermission['uns.appCredentials']}
                disabled={actionBusy || app.status !== 'stopped' || app.desiredState !== 'stopped'}
                onClick={() => onConfirm(app, 'refreshCredentials')}
              >
                {t('nativeApp.refreshCredentials')}
              </AuthButton>
              <HelpTooltip title={t('nativeApp.refreshCredsHint')} />
            </Space>
          )}
          <NativeAppStreamPolicy
            key={app.appId}
            appId={app.appId}
            disabled={busy || requestsLocked}
            onLockedChange={setPolicyLocked}
          />
          <NativeAppRequestGrants
            key={`requests:${app.appId}`}
            appId={app.appId}
            disabled={busy || policyLocked}
            onLockedChange={setRequestsLocked}
          />
        </div>
      )}
    </Drawer>
  );
};

export default NativeAppManager;
