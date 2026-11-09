import { type FC, useEffect, useRef, useState } from 'react';
import { Alert, App, Button, Checkbox, Space, Spin, Typography } from 'antd';
import { v4 as uuid } from 'uuid';
import {
  unsV2Api,
  type AppStreamPermission,
  type AppStreamPolicy,
  type AppStreamPolicyUpdate,
} from '@/apis/core-api/uns-v2';
import { AuthButton } from '@/components/auth';
import HelpTooltip from '@/components/help-tooltip';
import { ButtonPermission } from '@/common-types/button-permission';
import { useTranslate } from '@/hooks';
import { useBaseStore } from '@/stores/base';
import styles from './NativeAppPanel.module.scss';

interface Props {
  appId: string;
  disabled: boolean;
  onLockedChange: (locked: boolean) => void;
}

const requestActions = ['stream.requests.read', 'stream.requests.invoke', 'stream.requests.write'] as const;
const rejectedCodes = new Set([
  'FORBIDDEN',
  'INVALID_REQUEST',
  'INVALID_JSON',
  'VERSION_CONFLICT',
  'NOT_FOUND',
  'APP_NOT_READY',
]);

const NativeAppStreamPolicy: FC<Props> = (props) => {
  const user = useBaseStore((state) => state.currentUserInfo);
  const has = (key: string) => user?.superAdmin === true || user?.resourceKeys?.includes(key) === true;
  if (!has('uns.write') || !has('stream.requests.manage')) return null;
  return <PolicyEditor {...props} grantable={requestActions.filter(has)} />;
};

const PolicyEditor: FC<Props & { grantable: readonly AppStreamPermission[] }> = ({
  appId,
  disabled,
  onLockedChange,
  grantable,
}) => {
  const t = useTranslate();
  const { message } = App.useApp();
  const [profile, setProfile] = useState<AppStreamPolicy>();
  const [scopes, setScopes] = useState<AppStreamPermission[]>([]);
  const [generation, setGeneration] = useState(0);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState('');
  const [unknown, setUnknown] = useState(false);
  const pending = useRef<AppStreamPolicyUpdate>();
  const running = useRef(false);
  const alive = useRef(false);
  const locked = busy || unknown;
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    onLockedChange(locked);
    return () => onLockedChange(false);
  }, [locked, onLockedChange]);
  useEffect(() => {
    const controller = new AbortController();
    unsV2Api
      .appStreamPolicyGet(appId, controller.signal)
      .then((value) => {
        if (controller.signal.aborted) return;
        setProfile(value);
        setScopes(value.platformPermissions);
      })
      .catch(() => {
        if (!controller.signal.aborted) setFailure('nativeApp.streamPolicy.loadFailed');
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [appId, generation]);

  const reload = () => {
    setLoading(true);
    setProfile(undefined);
    setFailure('');
    setGeneration((value) => value + 1);
  };

  const canAdd = (permission: AppStreamPermission) =>
    profile?.authorityStatus === 'available' && grantable.includes(permission);
  const changed =
    !!profile &&
    (scopes.length !== profile.platformPermissions.length ||
      scopes.some((scope) => !profile.platformPermissions.includes(scope)));
  const canSave = !!profile && scopes.every((scope) => profile.platformPermissions.includes(scope) || canAdd(scope));
  const toggle = (permission: (typeof requestActions)[number], checked: boolean) => {
    setFailure('');
    setScopes((current) => {
      const next = new Set(current);
      if (checked) {
        next.add(permission);
        if (permission === 'stream.requests.write') next.add('stream.requests.invoke');
      } else {
        next.delete(permission);
        if (permission === 'stream.requests.invoke') next.delete('stream.requests.write');
      }
      return [...next];
    });
  };
  const save = async (retry = false) => {
    if (!profile || running.current || disabled || (!retry && (!changed || !canSave || unknown))) return;
    const body = retry
      ? pending.current
      : {
          appId,
          expectedRevision: profile.revision,
          status: scopes.length ? ('active' as const) : ('disabled' as const),
          platformPermissions: scopes,
          // 此处仅编辑 Request 权限，保留消息权限与服务端主题策略。
          idempotencyKey: uuid(),
          dryRun: false,
        };
    if (!body) return;
    pending.current = body;
    running.current = true;
    setBusy(true);
    setFailure('');
    try {
      const outcome = await unsV2Api.appStreamPolicyUpdate(body);
      if (!alive.current) return;
      if (outcome.status !== 'committed') throw new Error('Unconfirmed policy outcome');
      pending.current = undefined;
      setUnknown(false);
      reload();
      message.success(t('nativeApp.streamPolicy.saved'));
    } catch (error) {
      if (!alive.current) return;
      const code = (error as { error?: { code?: string } })?.error?.code;
      if (code && rejectedCodes.has(code)) {
        pending.current = undefined;
        setUnknown(false);
        setFailure(code === 'VERSION_CONFLICT' ? 'nativeApp.streamPolicy.conflict' : 'nativeApp.streamPolicy.rejected');
      } else {
        // 响应丢失时禁止生成新意图；同一请求重放取得原提交回执。
        setUnknown(true);
      }
    } finally {
      running.current = false;
      if (alive.current) setBusy(false);
    }
  };
  return (
    <section className={`${styles.stack} ${styles['stream-policy']}`} aria-label={t('nativeApp.streamPolicy.title')}>
      <Space>
        <Typography.Text strong>{t('nativeApp.streamPolicy.title')}</Typography.Text>
        <HelpTooltip title={t('nativeApp.streamPolicy.hint')} />
      </Space>
      {loading ? (
        <Spin size="small" />
      ) : (
        profile && (
          <>
            {profile.authorityStatus !== 'available' && (
              <Alert type="warning" showIcon message={t('nativeApp.streamPolicy.notReady')} />
            )}
            <div className={styles.stack}>
              {requestActions.map((permission) => (
                <Checkbox
                  key={permission}
                  checked={scopes.includes(permission)}
                  disabled={
                    disabled ||
                    locked ||
                    (!scopes.includes(permission) &&
                      (!canAdd(permission) ||
                        (permission === 'stream.requests.write' &&
                          !scopes.includes('stream.requests.invoke') &&
                          !canAdd('stream.requests.invoke'))))
                  }
                  onChange={(event) => toggle(permission, event.target.checked)}
                >
                  {t(`nativeApp.streamPolicy.${permission.split('.').at(-1)}`)}
                </Checkbox>
              ))}
            </div>
          </>
        )
      )}
      {failure && <Alert type="error" showIcon message={t(failure)} />}
      {unknown && <Alert type="warning" showIcon message={t('nativeApp.streamPolicy.unknown')} />}
      <Space>
        {unknown ? (
          <AuthButton
            auth={ButtonPermission['StreamRequests.manage']}
            loading={busy}
            disabled={disabled}
            onClick={() => void save(true)}
          >
            {t('nativeApp.streamPolicy.retry')}
          </AuthButton>
        ) : (
          <>
            <AuthButton
              auth={ButtonPermission['StreamRequests.manage']}
              type="primary"
              loading={busy}
              disabled={disabled || loading || !changed || !canSave || !!failure}
              onClick={() => void save()}
            >
              {t('common.save')}
            </AuthButton>
            <Button disabled={disabled || loading || locked} onClick={reload}>
              {t('common.refresh')}
            </Button>
          </>
        )}
      </Space>
    </section>
  );
};

export default NativeAppStreamPolicy;
