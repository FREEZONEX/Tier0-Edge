import { useState } from 'react';
import { Alert, AutoComplete, Collapse, Segmented, Select } from 'antd';
import { Add, Close } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import HelpTooltip from '@/components/help-tooltip';
import { ButtonPermission } from '@/common-types/button-permission';
import type { UnsMetadata } from '@/apis/core-api/uns-metadata';
import { useTranslate } from '@/hooks';
import MetadataEditor from '../metadata/metadata-editor';
import { fieldPaths } from '../metadata/schema-editor';
import WorkflowValueEditor from './WorkflowValueEditor';
import WorkflowDataView from './WorkflowDataView';
import { outputAt } from './workflow-data';
import type { WorkflowAction, WorkflowTrace, WorkflowValue } from './program-workflow';
import styles from './workflow.module.scss';
import WorkflowBatchOutput from './WorkflowBatchOutput';
import { recordLists } from './workflow-records';

type WriteAction = Extract<WorkflowAction, { kind: 'write' }>;
function previewValue(value: WorkflowValue, results: WorkflowTrace[]): unknown {
  if (value.kind === 'text') return value.value;
  if (value.kind === 'field') return outputAt(results.find((item) => item.id === value.step)?.output, value.path);
  try {
    return JSON.parse(value.value);
  } catch {
    return undefined;
  }
}

export default function WorkflowOutputEditor({
  action,
  collection,
  items,
  onSaved,
  onChange,
  steps,
  results,
  onTestStep,
}: {
  action: WriteAction;
  collection?: UnsMetadata;
  items: UnsMetadata[];
  onSaved: (meta: UnsMetadata) => void;
  onChange: (patch: Partial<WriteAction>) => void;
  steps: { value: string; label: string }[];
  results: WorkflowTrace[];
  onTestStep: (id: string) => void;
}) {
  const t = useTranslate();
  const [namespace, setNamespace] = useState(false);
  const properties = collection?.data.recordSchema?.properties || {};
  const fields = action.fields;
  const mode = action.mode || 'replace';
  const partial = mode === 'upsert' || mode === 'update';
  const missing =
    fields && !partial
      ? collection?.data.recordSchema?.required?.filter((key) => !fields.some((field) => field.key === key)) || []
      : [];
  const recordId = previewValue(action.recordId, results);
  const values = fields?.map((field) => [field.key, previewValue(field.value, results)] as const);
  const preview = values
    ? values.every(([, value]) => value !== undefined)
      ? Object.fromEntries(values)
      : undefined
    : previewValue(action.record, results);
  const bind = (label: string, value: WorkflowValue, onChange: (value: WorkflowValue) => void) => (
    <div className={styles.field}>
      <span>{label}</span>
      <WorkflowValueEditor
        label={label}
        value={value}
        onChange={onChange}
        steps={steps}
        results={results}
        onTestStep={onTestStep}
      />
    </div>
  );
  const defaultValue = (key: string): WorkflowValue =>
    action.record.kind === 'field'
      ? { ...action.record, path: [action.record.path, key].filter(Boolean).join('.') }
      : { kind: 'text', value: '' };
  return (
    <div className={styles['form-stack']}>
      <label className={styles.field}>
        <span>
          {t('Output.writeMode')} <HelpTooltip title={t(`Output.modeHelp.${mode}`)} />
        </span>
        <Select
          aria-label={t('Output.writeMode')}
          value={mode}
          options={(['upsert', 'create', 'update', 'replace'] as const).map((value) => ({
            value,
            label: t(`Output.mode.${value}`),
          }))}
          onChange={(mode) => onChange({ mode, unset: ['upsert', 'update'].includes(mode) ? action.unset : undefined })}
        />
      </label>
      {mode === 'replace' && <Alert type="warning" showIcon message={t('Output.replaceWarning')} />}
      <Segmented
        block
        value={action.each ? 'list' : 'single'}
        options={[
          { value: 'list', label: t('Ingest.listMode') },
          { value: 'single', label: t('Ingest.singleMode') },
        ]}
        onChange={(mode) => {
          const previous = steps[steps.length - 1]?.value || 'trigger';
          const list = recordLists(results.find((trace) => trace.id === previous)?.output)[0];
          onChange({
            each:
              mode === 'list'
                ? { records: { kind: 'field', step: previous, path: list?.path || '' }, identityPath: '' }
                : undefined,
          });
        }}
      />
      {action.each ? (
        <WorkflowBatchOutput
          action={action}
          onChange={onChange}
          steps={steps}
          results={results}
          onTestStep={onTestStep}
        />
      ) : (
        <>
          <div className={styles.field}>
            <span>
              {t('Output.recordIdentity')} <HelpTooltip title={t('Output.recordIdentityHelp')} />
            </span>
            <WorkflowValueEditor
              label={t('Output.recordIdentity')}
              value={action.recordId}
              onChange={(recordId) => onChange({ recordId })}
              steps={steps}
              results={results}
              onTestStep={onTestStep}
            />
          </div>
          <Segmented
            block
            value={fields ? 'fields' : 'object'}
            options={[
              { value: 'object', label: t('Output.wholeRecord') },
              { value: 'fields', label: t('Output.mapFields') },
            ]}
            onChange={(mode) =>
              onChange({
                fields:
                  mode === 'fields'
                    ? Object.keys(properties).map((key) => ({ key, value: defaultValue(key) }))
                    : undefined,
              })
            }
          />
          {fields ? (
            <>
              {fields.map((field, index) => (
                <div className={styles['mapping-field']} key={index}>
                  <div className={styles['inline-row']}>
                    <AutoComplete
                      aria-label={`${t('Workflow.outputField')} ${index + 1}`}
                      style={{ flex: 1 }}
                      value={field.key}
                      placeholder={t('Workflow.outputField')}
                      options={Object.keys(properties).map((key) => ({ value: key }))}
                      onChange={(key) =>
                        onChange({ fields: fields.map((item, i) => (i === index ? { ...item, key } : item)) })
                      }
                    />
                    <small>
                      {properties[field.key]?.type}
                      {collection?.data.recordSchema?.required?.includes(field.key) ? ' *' : ''}
                    </small>
                    <ComButton
                      type="text"
                      aria-label={`${t('Workflow.removeField')} ${field.key}`}
                      icon={<Close />}
                      onClick={() => onChange({ fields: fields.filter((_, i) => i !== index) })}
                    />
                  </div>
                  <WorkflowValueEditor
                    label={field.key || t('Workflow.outputField')}
                    value={field.value}
                    steps={steps}
                    results={results}
                    onTestStep={onTestStep}
                    onChange={(value) =>
                      onChange({ fields: fields.map((item, i) => (i === index ? { ...item, value } : item)) })
                    }
                  />
                </div>
              ))}
              <ComButton
                icon={<Add />}
                onClick={() => onChange({ fields: [...fields, { key: '', value: { kind: 'text', value: '' } }] })}
              >
                {t('Workflow.addField')}
              </ComButton>
              {!!missing.length && (
                <Alert type="warning" message={t('Output.missingRequired', { fields: missing.join(', ') })} />
              )}
            </>
          ) : (
            bind(t('Workflow.record'), action.record, (record) => onChange({ record }))
          )}
          <Collapse
            ghost
            items={[
              {
                key: 'sample',
                label: t('Output.appendSamples'),
                children: (
                  <div className={styles['form-stack']}>
                    {(['sample', 'time', 'quality'] as const).map((key) => (
                      <div key={key}>
                        {action[key] ? (
                          <>
                            {bind(t(`Workflow.${key}`), action[key]!, (value) => onChange({ [key]: value }))}
                            <ComButton type="text" onClick={() => onChange({ [key]: undefined })}>
                              {t('Workflow.removeField')}
                            </ComButton>
                          </>
                        ) : (
                          <ComButton
                            icon={<Add />}
                            onClick={() =>
                              onChange({
                                [key]: {
                                  kind: 'json',
                                  value: key === 'sample' ? '{}' : key === 'time' ? '0' : '"good"',
                                },
                              })
                            }
                          >
                            {t(`Workflow.${key}`)}
                          </ComButton>
                        )}
                      </div>
                    ))}
                  </div>
                ),
              },
              {
                key: 'preview',
                label: t('Output.preview'),
                children:
                  preview === undefined ? (
                    <span className={styles['muted-text']}>{t('Output.testForPreview')}</span>
                  ) : (
                    <WorkflowDataView value={{ recordId: recordId ?? null, record: preview }} />
                  ),
              },
            ]}
          />
        </>
      )}
      {partial && (
        <Collapse
          ghost
          items={[
            {
              key: 'unset',
              label: t('Output.removeFields'),
              children: (
                <label className={styles.field}>
                  <span>
                    {t('Output.removeFields')} <HelpTooltip title={t('Output.removeFieldsHelp')} />
                  </span>
                  <Select
                    mode="multiple"
                    aria-label={t('Output.removeFields')}
                    value={action.unset || []}
                    options={Object.keys(properties)
                      .filter((key) => !collection?.data.recordSchema?.required?.includes(key))
                      .map((value) => ({ value }))}
                    onChange={(unset) => onChange({ unset })}
                  />
                </label>
              ),
            },
          ]}
        />
      )}
      {collection && (
        <ComButton auth={ButtonPermission['uns.metadataWrite']} onClick={() => setNamespace(true)}>
          {t('Output.addNamespace')}
        </ComButton>
      )}
      {namespace && collection && (
        <MetadataEditor
          kind="topic"
          items={items}
          initialData={{
            name: collection.data.name,
            ...(typeof recordId === 'string' && recordId.trim() && fieldPaths(collection.data.recordSchema).length
              ? {
                  fields: fieldPaths(collection.data.recordSchema)
                    .filter((path) => !path.includes('.'))
                    .map((path) => ({
                      name: path,
                      kind: 'record' as const,
                      collectionId: collection.id,
                      recordId,
                      fieldPath: path,
                    })),
                }
              : { collectionId: collection.id }),
          }}
          onClose={() => setNamespace(false)}
          onSaved={(meta) => {
            onSaved(meta);
            setNamespace(false);
          }}
        />
      )}
    </div>
  );
}
