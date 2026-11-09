import { useEffect, useState } from 'react';
import { Alert, Checkbox, Select, Space } from 'antd';
import { useTranslate } from '@/hooks';
import HelpTooltip from '@/components/help-tooltip';
import { unsV2Api } from '@/apis/core-api/uns-v2';
import type { UnsMetadata } from '@/apis/core-api/uns-metadata';
import type { FleetCollectionScope } from '@/apis/core-api/fleet-connection';

type Selection = Pick<FleetCollectionScope, 'mode' | 'collectionIds'>;
export default function FleetCollectionSelection({
  value,
  onChange,
}: {
  value?: Selection;
  onChange?: (value: Selection) => void;
}) {
  const t = useTranslate();
  const [options, setOptions] = useState<{ label: string; value: string }[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(false);
  const all = !value || value.mode === 'all';
  useEffect(() => {
    let active = true;
    const load = async () => {
      setBusy(true);
      try {
        const items: UnsMetadata[] = [];
        for (let page = 1; ; page++) {
          const result = await unsV2Api.metadataAction<{ items: UnsMetadata[]; pageInfo: { hasMore: boolean } }>(
            'find',
            {
              kinds: ['collection'],
              state: 'active',
              page: { mode: 'offset', page, size: 200 },
            }
          );
          if (!active) return;
          items.push(...result.items);
          if (!result.pageInfo.hasMore) break;
        }
        setOptions(
          items
            .filter((item) => !item.mountId && !item.originNodeId)
            .map((item) => ({ label: item.data.name, value: item.id }))
        );
        setError(false);
      } catch {
        if (active) setError(true);
      } finally {
        if (active) setBusy(false);
      }
    };
    void load();
    return () => {
      active = false;
    };
  }, []);
  return (
    <Space direction="vertical" style={{ width: '100%' }}>
      <Space>
        <Checkbox
          checked={all}
          onChange={(e) => onChange?.({ mode: e.target.checked ? 'all' : 'selected', collectionIds: [] })}
        >
          {t('fleet.collections.selectAll')}
        </Checkbox>
        <HelpTooltip title={t('fleet.collections.scopeHelp')} />
      </Space>
      {!all && (
        <Select
          mode="multiple"
          maxCount={2000}
          showSearch
          optionFilterProp="label"
          value={value?.collectionIds || []}
          loading={busy}
          disabled={busy || error}
          options={options}
          style={{ width: '100%' }}
          placeholder={t('fleet.collections.selectPlaceholder')}
          onChange={(collectionIds) => onChange?.({ mode: 'selected', collectionIds })}
        />
      )}
      {error && <Alert type="error" message={t('fleet.resources.unavailable')} />}
    </Space>
  );
}
