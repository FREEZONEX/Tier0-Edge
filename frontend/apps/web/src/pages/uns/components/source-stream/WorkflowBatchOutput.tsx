import { Alert, Checkbox, Collapse, Select } from 'antd';
import { useTranslate } from '@/hooks';
import HelpTooltip from '@/components/help-tooltip';
import type { WorkflowAction, WorkflowTrace } from './program-workflow';
import { bindingValue, identityFields, isRecord, recordFields } from './workflow-records';
import WorkflowValueEditor from './WorkflowValueEditor';
import WorkflowRecordList from './WorkflowRecordList';
import styles from './workflow.module.scss';

type Write = Extract<WorkflowAction, { kind: 'write' }>;
export default function WorkflowBatchOutput({
  action,
  onChange,
  steps,
  results,
  onTestStep,
}: {
  action: Write;
  onChange: (patch: Partial<Write>) => void;
  steps: { value: string; label: string }[];
  results: WorkflowTrace[];
  onTestStep: (id: string) => void;
}) {
  const t = useTranslate();
  const each = action.each!;
  const value = bindingValue(each.records, results);
  const records = Array.isArray(value) && value.every(isRecord) ? value : undefined;
  const keys = records ? recordFields(records) : [];
  const identities = records ? identityFields(records) : [];
  const change = (patch: Partial<typeof each>) => onChange({ each: { ...each, ...patch } });
  const preview = records?.map((row) =>
    each.fields
      ? Object.fromEntries(each.fields.filter((key) => Object.hasOwn(row, key)).map((key) => [key, row[key]]))
      : row
  );
  return (
    <div className={styles['form-stack']}>
      <label className={styles.field}>
        <span>{t('Ingest.recordList')}</span>
        <WorkflowValueEditor
          label={t('Ingest.recordList')}
          value={each.records}
          onChange={(records) => change({ records, identityPath: '', fields: undefined })}
          steps={steps}
          results={results}
          onTestStep={onTestStep}
        />
      </label>
      <label className={styles.field}>
        <span>
          {t('Ingest.identity')} <HelpTooltip title={t('Ingest.identityHelp')} />
        </span>
        <Select
          showSearch
          aria-label={t('Ingest.identity')}
          value={each.identityPath || undefined}
          placeholder={t('Ingest.chooseIdentity')}
          options={[...new Set([...identities, ...(each.identityPath ? [each.identityPath] : [])])].map((value) => ({
            value,
          }))}
          onChange={(identityPath) => change({ identityPath })}
        />
      </label>
      {records && each.identityPath && !identities.includes(each.identityPath) && (
        <Alert type="error" message={t('Ingest.invalidIdentity')} />
      )}
      {!records && <Alert type="info" message={t('Ingest.fetchFirst')} />}
      <Collapse
        ghost
        items={[
          {
            key: 'fields',
            label: t('Ingest.selectFields'),
            children: (
              <Checkbox.Group
                value={each.fields ?? keys}
                options={keys}
                onChange={(fields) => change({ fields: fields as string[] })}
              />
            ),
          },
        ]}
      />
      {preview && <WorkflowRecordList records={preview} />}
    </div>
  );
}
