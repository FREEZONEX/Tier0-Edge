import { useEffect, useState } from 'react';
import { Alert, Checkbox, Input, Select } from 'antd';
import HelpTooltip from '@/components/help-tooltip';
import { useTranslate } from '@/hooks';
import { loadMetadata, type UnsMetadata } from '@/apis/core-api/uns-metadata';
import type { MetadataTarget } from '@/apis/core-api/source-stream';
import styles from './index.module.scss';

export default function TargetEditor({
  value,
  onChange,
  readOnly,
}: {
  value?: MetadataTarget;
  onChange: (target: MetadataTarget) => void;
  readOnly: boolean;
}) {
  const t = useTranslate();
  const [items, setItems] = useState<UnsMetadata[]>([]);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let alive = true;
    loadMetadata()
      .then((items) => {
        if (alive) setItems(items);
      })
      .catch(() => {
        if (alive) setFailed(true);
      });
    return () => {
      alive = false;
    };
  }, []);
  const path = (item: UnsMetadata) => {
    const names = [item.data.name];
    let parent = item.data.parentId;
    const seen = new Set<string>();
    while (parent && !seen.has(parent)) {
      seen.add(parent);
      const node = items.find((entry) => entry.id === parent);
      if (!node) break;
      names.unshift(node.data.name);
      parent = node.data.parentId;
    }
    return names.join(' / ');
  };
  const field = (key: keyof MetadataTarget, label: string) => (
    <label className={styles['mapping-field']}>
      <span>{t(`UnsMeta.${label}`)}</span>
      <Input
        value={String(value?.[key] || '')}
        disabled={readOnly || !value}
        onChange={(event) => value && onChange({ ...value, [key]: event.target.value })}
      />
    </label>
  );
  const collection = items.find((item) => item.id === value?.collectionId);
  return (
    <div className={styles['mapping-fields']}>
      {failed && <Alert type="error" message={t('UnsMeta.loadFailed')} />}
      <label className={styles['mapping-field']}>
        <span>{t('UnsMeta.targetTopic')}</span>
        <Select
          showSearch
          optionFilterProp="label"
          disabled={readOnly}
          value={value?.topicId}
          options={items
            .filter((item) => item.kind === 'topic' && item.data.collectionId)
            .map((item) => ({ value: item.id, label: path(item) }))}
          onChange={(id) => {
            const topic = items.find((item) => item.id === id)!;
            onChange({ topicId: id, collectionId: topic.data.collectionId!, recordIdPath: 'id' });
          }}
        />
      </label>
      {value && (
        <>
          <span>
            {t('UnsMeta.collection')}: {collection?.data.name || value.collectionId}{' '}
            <HelpTooltip title={t('UnsMeta.mappingHelp')} />
          </span>
          <div className={styles['form-grid']}>
            <label className={styles['mapping-field']}>
              <span>{t('UnsMeta.recordIdentity')}</span>
              <Select
                value={value.recordId !== undefined ? 'fixed' : 'path'}
                disabled={readOnly}
                options={[
                  { value: 'fixed', label: t('UnsMeta.fixedId') },
                  { value: 'path', label: t('UnsMeta.idFromField') },
                ]}
                onChange={(mode) =>
                  onChange({
                    ...value,
                    recordId: mode === 'fixed' ? 'record' : undefined,
                    recordIdPath: mode === 'path' ? 'id' : undefined,
                  })
                }
              />
            </label>
            {value.recordId !== undefined ? field('recordId', 'recordId') : field('recordIdPath', 'recordIdPath')}
            {field('recordPath', 'recordPath')}
          </div>
          <Checkbox
            checked={value.sampleEnabled}
            disabled={readOnly || !collection?.data.sampleSchema}
            onChange={(event) => onChange({ ...value, sampleEnabled: event.target.checked })}
          >
            {t('UnsMeta.writeSamples')}
          </Checkbox>
          {value.sampleEnabled && (
            <div className={styles['form-grid']}>
              {field('samplePath', 'samplePath')}
              {field('timePath', 'timePath')}
              {field('qualityPath', 'qualityPath')}
            </div>
          )}
        </>
      )}
    </div>
  );
}
