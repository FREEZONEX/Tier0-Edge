import { useEffect, useState } from 'react';
import { Alert, Button, Input, Select, Space } from 'antd';
import { Add, TrashCan } from '@carbon/icons-react';
import type { NativeAppGrant } from '@/apis/core-api/native-app';
import { unsV2Api } from '@/apis/core-api/uns-v2';
import { useTranslate } from '@/hooks';
import HelpTooltip from '@/components/help-tooltip';
import styles from './NativeAppPanel.module.scss';

const permissions = ['record:read', 'record:write', 'record:delete', 'sample:read', 'sample:write'];

export default function NativeAppGrants({
  value,
  onChange,
  disabled,
}: {
  value: NativeAppGrant[];
  onChange: (value: NativeAppGrant[]) => void;
  disabled: boolean;
}) {
  const t = useTranslate();
  const [search, setSearch] = useState('');
  const [options, setOptions] = useState<{ value: string; label: string }[]>([]);
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(async () => {
      setLoading(true);
      try {
        const result = await unsV2Api.collectionList({ size: 50, search }, controller.signal);
        if (controller.signal.aborted) return;
        setOptions(result.list.map((item) => ({ label: item.data.name, value: item.id })));
        setFailed(false);
      } catch {
        if (!controller.signal.aborted) setFailed(true);
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    }, 250);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [search]);
  const update = (index: number, patch: Partial<NativeAppGrant>) =>
    onChange(value.map((grant, i) => (i === index ? { ...grant, ...patch } : grant)));
  return (
    <div className={styles.grants}>
      <Space>
        {t('nativeApp.grants')}
        <HelpTooltip title={t('nativeApp.grantsHelp')} />
      </Space>
      {failed && <Alert type="error" message={t('nativeApp.collectionsFailed')} />}
      {value.map((grant, index) => (
        <div className={styles['grant-row']} key={index}>
          <Input
            aria-label={t('nativeApp.alias')}
            placeholder={t('nativeApp.alias')}
            value={grant.alias}
            disabled={disabled}
            onChange={(event) => update(index, { alias: event.target.value })}
          />
          <Select
            aria-label={t('nativeApp.collection')}
            placeholder={t('nativeApp.collection')}
            showSearch
            filterOption={false}
            onSearch={setSearch}
            options={options}
            value={grant.collectionId || undefined}
            loading={loading}
            disabled={disabled}
            onChange={(collectionId) => update(index, { collectionId })}
          />
          <Select
            aria-label={t('nativeApp.permissions')}
            mode="multiple"
            value={grant.permissions}
            disabled={disabled}
            options={permissions.map((permission) => ({ value: permission, label: t(`nativeApp.${permission}`) }))}
            onChange={(selected) => update(index, { permissions: selected })}
          />
          <Button
            aria-label={t('common.delete')}
            icon={<TrashCan size={16} />}
            disabled={disabled}
            onClick={() => onChange(value.filter((_, i) => i !== index))}
          />
        </div>
      ))}
      <Button
        type="dashed"
        icon={<Add size={16} />}
        disabled={disabled || value.length >= 50}
        onClick={() => onChange([...value, { alias: '', collectionId: '', permissions: ['record:read'] }])}
      >
        {t('nativeApp.addGrant')}
      </Button>
    </div>
  );
}
