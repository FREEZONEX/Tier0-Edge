import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { Alert, App, Button, Checkbox, Drawer, Spin, Tag, Typography } from 'antd';
import { collectionStorageAction, type CollectionStorage, type StorageMode } from '@/apis/core-api/collection-storage';
import { useTranslate } from '@/hooks';
import { hasPermission } from '@/utils/auth';
import { ButtonPermission } from '@/common-types/button-permission';
import HelpTooltip from '@/components/help-tooltip';
import styles from './storage-policy.module.scss';

export function StoragePolicyButton() {
  const t = useTranslate();
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button onClick={() => setOpen(true)}>{t('collectionStorage.title')}</Button>
      <Drawer
        open={open}
        title={t('collectionStorage.title')}
        width={680}
        destroyOnClose
        onClose={() => setOpen(false)}
      >
        <StoragePolicy />
      </Drawer>
    </>
  );
}

export default function StoragePolicy({
  nodeId,
  children,
  onSaved,
}: {
  nodeId?: string;
  children?: ReactNode;
  onSaved?: () => void;
}) {
  const t = useTranslate();
  const { message } = App.useApp();
  const [current, setCurrent] = useState<CollectionStorage>();
  const [locations, setLocations] = useState<string[]>(['local']);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [stale, setStale] = useState(false);
  const adopt = (value: CollectionStorage) => {
    setCurrent(value);
    setLocations(value.policy.mode === 'both' ? ['local', 'center'] : [value.policy.mode]);
    setStale(false);
  };
  const load = useCallback(async () => {
    setBusy(true);
    setError('');
    try {
      adopt(await collectionStorageAction<CollectionStorage>(nodeId, 'get'));
    } catch {
      setStale(true);
      setError(t('collectionStorage.loadFailed'));
    } finally {
      setBusy(false);
    }
  }, [nodeId, t]);
  useEffect(() => {
    void load();
  }, [load]);
  const save = async (next: string[]) => {
    if (!current || busy) return;
    if (!next.length) {
      setError(t('collectionStorage.atLeastOne'));
      return;
    }
    setBusy(true);
    setError('');
    try {
      const mode: StorageMode = next.length === 2 ? 'both' : (next[0] as StorageMode);
      adopt(
        await collectionStorageAction<CollectionStorage>(nodeId, 'save', {
          expectedRevision: current.policy.revision,
          mode,
        })
      );
      message.success(t('collectionStorage.saved'));
      onSaved?.();
    } catch (e) {
      setStale(true);
      setError(
        t((e as { code?: number }).code === 409 ? 'collectionStorage.conflict' : 'collectionStorage.saveFailed')
      );
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className={styles.policy}>
      <div className={styles.toolbar}>
        <span className={styles.label}>
          <Typography.Text strong>{t('collectionStorage.title')}</Typography.Text>
          <HelpTooltip title={t('collectionStorage.hint')} />
        </span>
        <Checkbox.Group
          aria-label={t('collectionStorage.title')}
          value={current ? locations : []}
          disabled={!current || busy || stale || !hasPermission(ButtonPermission['uns.metadataWrite'])}
          onChange={(values) => void save(values.map(String))}
          options={[
            { value: 'local', label: t('collectionStorage.localLabel') },
            { value: 'center', label: t('collectionStorage.centerLabel'), disabled: !current?.connected },
          ]}
        />
        {busy && <Spin size="small" />}
        {stale && <Tag color="warning">{t('collectionStorage.lastKnown')}</Tag>}
        {children && <div className={styles.search}>{children}</div>}
      </div>
      {error && (
        <Alert
          type="error"
          message={error}
          action={
            stale && (
              <Button size="small" loading={busy} onClick={() => void load()}>
                {t('common.refresh')}
              </Button>
            )
          }
        />
      )}
      {current && !current.connected && (
        <Typography.Text type="secondary">{t('collectionStorage.notConnected')}</Typography.Text>
      )}
      {current?.defaultPending && <Alert type="warning" message={t('collectionStorage.defaultPending')} />}
    </div>
  );
}
