import { useSourceStreamScope } from '@/apis/core-api/source-stream-scope';
import { useEffect, useState, type FC } from 'react';
import { Alert, Select, Spin } from 'antd';
import { type UnsMetadata } from '@/apis/core-api/uns-metadata';
import { useTranslate } from '@/hooks';

export function selectableInternalResources(items: UnsMetadata[], write: boolean): UnsMetadata[] {
  return items.filter(
    (item) =>
      !item.deletedAt &&
      (item.kind === 'collection' || (item.kind === 'topic' && (!write || item.data.viewMode !== 'fields')))
  );
}

const InternalResourceSelect: FC<{
  value?: string[];
  onChange: (value: string[]) => void;
  write?: boolean;
  topicsOnly?: boolean;
  single?: boolean;
}> = ({ value, onChange, write = false, topicsOnly = false, single = false }) => {
  const { loadMetadata } = useSourceStreamScope();
  const t = useTranslate();
  const [items, setItems] = useState<UnsMetadata[]>([]);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let alive = true;
    loadMetadata()
      .then((result) => alive && setItems(result))
      .catch(() => alive && setFailed(true))
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [loadMetadata]);
  if (failed) return <Alert type="error" showIcon message={t('SourceStream.loadFailed')} />;
  return (
    <Select
      mode={single ? undefined : 'multiple'}
      aria-label={t('SourceStream.output')}
      value={single ? value?.[0] : value}
      onChange={(next) => onChange(single ? (next ? [String(next)] : []) : (next as string[]))}
      loading={loading}
      showSearch
      optionFilterProp="label"
      notFoundContent={loading ? <Spin size="small" /> : t('SourceStream.noTopics')}
      options={selectableInternalResources(items, write)
        .filter((item) => !topicsOnly || item.kind === 'topic')
        .map((item) => ({
          value: `${item.kind}:${item.id}`,
          label: `${item.data.name} · ${item.kind} · ${item.id}`,
        }))}
    />
  );
};
export default InternalResourceSelect;
