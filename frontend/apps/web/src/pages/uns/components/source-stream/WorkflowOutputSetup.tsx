import { useState } from 'react';
import { Alert, Collapse } from 'antd';
import { Add, Renew } from '@carbon/icons-react';
import { useTranslate } from '@/hooks';
import ComButton from '@/components/com-button';
import { ButtonPermission } from '@/common-types/button-permission';
import type { UnsMetadata } from '@/apis/core-api/uns-metadata';
import CollectionSelect from '../metadata/collection-select';
import CollectionPreview from '../metadata/collection-preview';
import MetadataEditor from '../metadata/metadata-editor';
import { outputCollection } from './use-workflow-metadata';
import type useWorkflowMetadata from './use-workflow-metadata';
import styles from './workflow.module.scss';
import { validWorkflowTarget } from './program-workflow';
import { inferRecordSchema, recordFields } from './workflow-records';

export default function WorkflowOutputSetup({
  target,
  metadata,
  onChange,
  records,
}: {
  target: string;
  metadata: ReturnType<typeof useWorkflowMetadata>;
  onChange: (target: string) => void;
  records?: Record<string, unknown>[];
}) {
  const t = useTranslate();
  const [creating, setCreating] = useState(false);
  const collection = outputCollection(target, metadata.items);
  const inferred = records?.length ? inferRecordSchema(records) : undefined;
  const untyped = records ? recordFields(records).filter((key) => !inferred?.properties?.[key]) : [];
  const saved = (item: UnsMetadata) => {
    metadata.saved(item);
    onChange(`collection:${item.id}`);
    setCreating(false);
  };
  return (
    <div className={styles['form-stack']}>
      <label className={styles.field}>
        <span>{t('Output.collection')}</span>
        <CollectionSelect
          items={metadata.items}
          value={collection?.id}
          loading={metadata.loading}
          canSelect={(item) => validWorkflowTarget(`collection:${item.id}`)}
          onChange={(id) => onChange(id ? `collection:${id}` : '')}
        />
      </label>
      <div className={styles['inline-row']}>
        <ComButton icon={<Add />} auth={ButtonPermission['uns.metadataWrite']} onClick={() => setCreating(true)}>
          {t('Output.newCollection')}
        </ComButton>
        <ComButton type="text" icon={<Renew />} aria-label={t('UnsMeta.refresh')} onClick={metadata.refresh} />
      </div>
      {!!records?.length && !collection && <Alert type="info" message={t('Ingest.inferredSchema')} />}
      {!!untyped.length && !collection && (
        <Alert type="warning" message={t('Ingest.untypedFields', { fields: untyped.join(', ') })} />
      )}
      {metadata.failed && <Alert type="error" showIcon message={t('UnsMeta.loadFailed')} />}
      {target && !collection && !metadata.loading && !metadata.failed && (
        <Alert type="warning" showIcon message={t('Output.targetUnavailable')} />
      )}
      {collection && (
        <>
          <div className={styles['schema-summary']}>
            <strong>{t('Output.availableFields')}</strong>
            {Object.entries(collection.data.recordSchema?.properties || {}).map(([name, field]) => (
              <span key={name}>
                <code>{name}</code>
                <small>
                  {field.type}
                  {collection.data.recordSchema?.required?.includes(name) ? ' *' : ''}
                </small>
              </span>
            ))}
            {!Object.keys(collection.data.recordSchema?.properties || {}).length && (
              <span>{t('Output.noSchemaFields')}</span>
            )}
            {!!collection.data.sampleSchema && <span>{t('Output.historyAvailable')}</span>}
          </div>
          <Collapse
            ghost
            items={[
              {
                key: 'preview',
                label: t('Output.existingData'),
                children: <CollectionPreview collection={collection} />,
              },
            ]}
          />
        </>
      )}
      {creating && (
        <MetadataEditor
          kind="collection"
          initialData={inferred ? { recordSchema: inferred } : undefined}
          standalone
          items={metadata.items}
          onClose={() => setCreating(false)}
          onSaved={saved}
        />
      )}
    </div>
  );
}
