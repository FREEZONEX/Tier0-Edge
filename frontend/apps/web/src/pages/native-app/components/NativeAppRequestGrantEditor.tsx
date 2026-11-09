import { useEffect, useRef, useState } from 'react';
import { Alert, App, Button, Checkbox, Space, Spin, Tag, Typography } from 'antd';
import {
  appRequestGrantsApi,
  requestGrantActions,
  requestGrantUpdate,
  type AppRequestGrant,
  type RequestGrantAction,
} from '@/apis/core-api/app-request-grants';
import { openConfirmModal } from '@/components/confirm-modal';
import { AuthButton } from '@/components/auth';
import { ButtonPermission } from '@/common-types/button-permission';
import { useTranslate } from '@/hooks';
import styles from './NativeAppPanel.module.scss';

interface Props {
  appId: string;
  code: string;
  disabled: boolean;
  grantable: readonly RequestGrantAction[];
  onLockedChange: (locked: boolean) => void;
  onSaved: () => void;
}
const sameActions = (a: RequestGrantAction[], b: RequestGrantAction[]) =>
  a.length === b.length && a.every((action) => b.includes(action));

export default function NativeAppRequestGrantEditor({
  appId,
  code,
  disabled,
  grantable,
  onLockedChange,
  onSaved,
}: Props) {
  const t = useTranslate();
  const { modal, message } = App.useApp();
  const [snapshot, setSnapshot] = useState<AppRequestGrant>();
  const [actions, setActions] = useState<RequestGrantAction[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [unknown, setUnknown] = useState(false);
  const [failed, setFailed] = useState(false);
  const controller = useRef(new AbortController());
  const running = useRef(false);
  const locked = busy || unknown;
  useEffect(() => {
    onLockedChange(locked);
    return () => onLockedChange(false);
  }, [locked, onLockedChange]);
  useEffect(() => {
    const lifetime = new AbortController();
    controller.current = lifetime;
    appRequestGrantsApi
      .get(appId, code, lifetime.signal)
      .then((value) => {
        if (lifetime.signal.aborted) return;
        setSnapshot(value);
        setActions(value.active === 1 ? value.actions : []);
      })
      .catch(() => {
        if (!lifetime.signal.aborted) setFailed(true);
      })
      .finally(() => {
        if (!lifetime.signal.aborted) setLoading(false);
      });
    return () => lifetime.abort();
  }, [appId, code]);
  const reconcile = async () => {
    if (running.current) return;
    running.current = true;
    setBusy(true);
    try {
      const value = await appRequestGrantsApi.get(appId, code, controller.current.signal);
      if (controller.current.signal.aborted) return;
      setSnapshot(value);
      setActions(value.active === 1 ? value.actions : []);
      setUnknown(false);
      setFailed(false);
    } catch {
      if (!controller.current.signal.aborted) setFailed(true);
    } finally {
      running.current = false;
      if (!controller.current.signal.aborted) setBusy(false);
    }
  };
  const eligible =
    snapshot?.recipientEligible === 1 && snapshot.activationSupported === 1 && snapshot.status === 'active';
  const allowed = (action: RequestGrantAction) =>
    eligible && grantable.includes(action) && snapshot!.recipientActions.includes(action);
  const canApply = Boolean(
    snapshot &&
    eligible &&
    actions.length &&
    actions.every(allowed) &&
    (!actions.includes('stream.requests.write') || actions.includes('stream.requests.invoke'))
  );
  const changed =
    snapshot &&
    (snapshot.active !== 1 ||
      !sameActions(actions, snapshot.actions) ||
      snapshot.targets.some((target) => target.current === 1 && target.active !== 1));
  const canRevoke =
    snapshot?.registered === 1 && (snapshot.active === 1 || snapshot.targets.some((target) => target.active === 1));
  const submit = async (revoke: boolean) => {
    if (!snapshot || disabled || running.current || unknown || (revoke ? !canRevoke : !canApply)) return;
    running.current = true;
    setBusy(true);
    setFailed(false);
    try {
      const receipt = await appRequestGrantsApi.set(
        requestGrantUpdate(snapshot, revoke ? 2 : 1, actions),
        controller.current.signal
      );
      if (controller.current.signal.aborted) return;
      const observed = await appRequestGrantsApi.get(appId, code, controller.current.signal);
      if (controller.current.signal.aborted) return;
      if (
        receipt.active !== (revoke ? 2 : 1) ||
        observed.snapshotToken !== receipt.snapshotToken ||
        observed.revision !== receipt.revision ||
        observed.active !== receipt.active ||
        !sameActions(observed.actions, receipt.actions)
      ) {
        setSnapshot(observed);
        setActions(observed.active === 1 ? observed.actions : []);
        setFailed(true);
      } else {
        message.success(t('nativeApp.streamPolicy.saved'));
        onSaved();
      }
    } catch {
      // CAS 结果未知时只能复读观察；不得自动获取新版本再次扩权。
      if (!controller.current.signal.aborted) setUnknown(true);
    } finally {
      running.current = false;
      if (!controller.current.signal.aborted) setBusy(false);
    }
  };
  const toggle = (action: RequestGrantAction, checked: boolean) =>
    setActions((previous) => {
      const next = new Set(previous);
      if (checked) {
        next.add(action);
        if (action === 'stream.requests.write') next.add('stream.requests.invoke');
      } else {
        next.delete(action);
        if (action === 'stream.requests.invoke') next.delete('stream.requests.write');
      }
      return [...next];
    });
  return (
    <div className={styles.stack}>
      {loading ? (
        <Spin size="small" />
      ) : (
        snapshot && (
          <>
            {!eligible && <Alert type="warning" message={t('nativeApp.requests.notEligible')} />}
            <div className={styles.stack}>
              {requestGrantActions.map((action) => (
                <Checkbox
                  key={action}
                  checked={actions.includes(action)}
                  disabled={
                    disabled ||
                    locked ||
                    !allowed(action) ||
                    (action === 'stream.requests.write' && !allowed('stream.requests.invoke'))
                  }
                  onChange={(event) => toggle(action, event.target.checked)}
                >
                  {t(`nativeApp.streamPolicy.${action.split('.').at(-1)}`)}
                </Checkbox>
              ))}
            </div>
            {!!snapshot.targets.length && (
              <div className={styles.stack}>
                <Typography.Text strong>{t('nativeApp.requests.resources')}</Typography.Text>
                {snapshot.targets.map((target) => (
                  <div key={target.digest} className={styles['request-target']}>
                    <span>{target.reference || target.digest}</span>
                    <Tag>
                      {t(
                        target.action === 'write' ? 'nativeApp.requests.targetWrite' : 'nativeApp.requests.targetRead'
                      )}
                    </Tag>
                    {target.current === 2 && <Tag>{t('nativeApp.requests.previous')}</Tag>}
                  </div>
                ))}
              </div>
            )}
          </>
        )
      )}
      {failed && <Alert type="error" message={t('nativeApp.requests.changed')} />}
      {unknown && <Alert type="warning" message={t('nativeApp.requests.unknown')} />}
      <Space wrap>
        {unknown || failed ? (
          <Button loading={busy} disabled={disabled} onClick={() => void reconcile()}>
            {t('common.refresh')}
          </Button>
        ) : (
          <>
            <AuthButton
              auth={ButtonPermission['StreamRequests.manage']}
              type="primary"
              loading={busy}
              disabled={disabled || loading || locked || !canApply || !changed}
              onClick={() => void submit(false)}
            >
              {t('nativeApp.requests.grant')}
            </AuthButton>
            <AuthButton
              auth={ButtonPermission['StreamRequests.manage']}
              danger
              disabled={disabled || loading || locked || !canRevoke}
              onClick={() =>
                openConfirmModal(modal, {
                  title: t('nativeApp.requests.revoke'),
                  content: t('nativeApp.requests.revokeHint'),
                  okText: t('nativeApp.requests.revoke'),
                  cancelText: t('common.cancel'),
                  onOk: () => submit(true),
                })
              }
            >
              {t('nativeApp.requests.revoke')}
            </AuthButton>
          </>
        )}
      </Space>
    </div>
  );
}
