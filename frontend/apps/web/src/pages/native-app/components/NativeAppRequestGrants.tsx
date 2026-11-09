import { useEffect, useState } from 'react';
import { Alert, Button, Select, Space, Spin, Typography } from 'antd';
import { useBaseStore } from '@/stores/base';
import { useTranslate } from '@/hooks';
import {
  appRequestGrantsApi,
  requestGrantActions,
  type AppRequestEntry,
  type RequestCandidate,
} from '@/apis/core-api/app-request-grants';
import NativeAppRequestGrantEditor from './NativeAppRequestGrantEditor';
import styles from './NativeAppPanel.module.scss';

interface Props {
  appId: string;
  disabled: boolean;
  onLockedChange: (locked: boolean) => void;
}

export default function NativeAppRequestGrants(props: Props) {
  const user = useBaseStore((state) => state.currentUserInfo);
  const has = (key: string) => user?.superAdmin === true || user?.resourceKeys?.includes(key) === true;
  if (!has('uns.write') || !has('stream.requests.manage')) return null;
  return <RequestList key={`${props.appId}:${user?.sub}`} {...props} grantable={requestGrantActions.filter(has)} />;
}

function RequestList({
  appId,
  disabled,
  onLockedChange,
  grantable,
}: Props & { grantable: readonly (typeof requestGrantActions)[number][] }) {
  const t = useTranslate();
  const [entries, setEntries] = useState<AppRequestEntry[]>([]);
  const [candidates, setCandidates] = useState<RequestCandidate[]>([]);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [candidateFailed, setCandidateFailed] = useState(false);
  const [selected, setSelected] = useState<string>();
  const [generation, setGeneration] = useState(0);
  const [locked, setLocked] = useState(false);
  useEffect(() => {
    onLockedChange(locked);
    return () => onLockedChange(false);
  }, [locked, onLockedChange]);
  useEffect(() => {
    const controller = new AbortController();
    Promise.allSettled([
      appRequestGrantsApi.list(appId, controller.signal),
      appRequestGrantsApi.candidates(controller.signal),
    ]).then(([history, available]) => {
      if (controller.signal.aborted) return;
      setFailed(history.status === 'rejected');
      setCandidateFailed(available.status === 'rejected');
      setEntries(history.status === 'fulfilled' ? history.value : []);
      setCandidates(available.status === 'fulfilled' ? available.value : []);
      setLoading(false);
    });
    return () => controller.abort();
  }, [appId, generation]);
  const refresh = () => {
    setLoading(true);
    setSelected(undefined);
    setGeneration((value) => value + 1);
  };
  const options = [...new Set([...entries.map((entry) => entry.code), ...candidates.map((entry) => entry.code)])].map(
    (code) => {
      const entry = entries.find((entry) => entry.code === code);
      const name = entry?.entryName || candidates.find((entry) => entry.code === code)?.name || code;
      const state = entry
        ? t(
            entry.configuredActive === 2
              ? 'nativeApp.requests.revoked'
              : entry.state === 'available'
                ? 'nativeApp.requests.granted'
                : 'nativeApp.requests.blocked'
          )
        : '';
      return { value: code, label: `${name}${name !== code ? ` · ${code}` : ''}${state ? ` · ${state}` : ''}` };
    }
  );
  return (
    <section className={`${styles.stack} ${styles['stream-policy']}`}>
      <Space>
        <Typography.Text strong>{t('nativeApp.requests.title')}</Typography.Text>
        <Button size="small" disabled={disabled || locked || loading} onClick={refresh}>
          {t('common.refresh')}
        </Button>
      </Space>
      {loading ? (
        <Spin size="small" />
      ) : failed ? (
        <Alert type="error" message={t('nativeApp.requests.loadFailed')} />
      ) : (
        <>
          {candidateFailed && <Alert type="warning" message={t('nativeApp.requests.candidatesFailed')} />}
          <Select
            showSearch
            allowClear
            optionFilterProp="label"
            aria-label={t('nativeApp.requests.select')}
            placeholder={t('nativeApp.requests.select')}
            disabled={disabled || locked}
            value={selected}
            options={options}
            onChange={setSelected}
            notFoundContent={t('nativeApp.requests.empty')}
          />
          {selected && (
            <NativeAppRequestGrantEditor
              key={selected}
              appId={appId}
              code={selected}
              disabled={disabled}
              grantable={grantable}
              onLockedChange={setLocked}
              onSaved={refresh}
            />
          )}
        </>
      )}
    </section>
  );
}
