import { Select } from 'antd';
import { useTranslate } from '@/hooks';
import type { UnsMetadata } from '@/apis/core-api/uns-metadata';

export default function CollectionSelect({
  value,
  onChange,
  items,
  loading,
  allowClear,
  canSelect,
}: {
  value?: string;
  onChange?: (id: string | undefined) => void;
  items: UnsMetadata[];
  loading?: boolean;
  allowClear?: boolean;
  canSelect?: (item: UnsMetadata) => boolean;
}) {
  const t = useTranslate();
  return (
    <Select
      style={{ width: '100%' }}
      aria-label={t('Output.collection')}
      placeholder={t('Output.chooseCollection')}
      value={value}
      onChange={onChange}
      loading={loading}
      allowClear={allowClear}
      showSearch
      optionFilterProp="search"
      notFoundContent={t(loading ? 'common.loading' : 'common.noData')}
      options={items
        .filter((item) => item.kind === 'collection' && !item.deletedAt)
        .map((item) => {
          const names = Object.keys(item.data.recordSchema?.properties || {});
          const samples = Object.keys(item.data.sampleSchema?.properties || {});
          return {
            value: item.id,
            label: item.data.name,
            disabled: canSelect ? !canSelect(item) : false,
            search: `${item.data.name} ${item.id} ${names.join(' ')} ${samples.join(' ')}`,
            description:
              canSelect && !canSelect(item)
                ? t('Output.unsupportedTarget')
                : [...names, ...samples].slice(0, 5).join(' · ') || t('Output.noSchemaFields'),
          };
        })}
      optionRender={(option) => (
        <div>
          <strong>{option.label}</strong>
          <div style={{ fontSize: 12, opacity: 0.65 }}>{option.data.description}</div>
        </div>
      )}
    />
  );
}
