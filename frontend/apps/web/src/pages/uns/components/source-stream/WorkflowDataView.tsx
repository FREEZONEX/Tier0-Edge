import { useState } from 'react';
import { Segmented } from 'antd';
import { useTranslate } from '@/hooks';
import { outputPaths } from './program-workflow';
import { displayValue, outputAt } from './workflow-data';
import styles from './workflow.module.scss';
import { recordLists } from './workflow-records';
import WorkflowRecordList from './WorkflowRecordList';

export default function WorkflowDataView({ value }: { value: unknown }) {
  const t = useTranslate();
  const lists = recordLists(value);
  const [mode, setMode] = useState(lists.length ? 'records' : 'fields');
  const paths = outputPaths(value);
  return (
    <div className={styles['data-view']}>
      <Segmented
        size="small"
        value={mode}
        onChange={(next) => setMode(String(next))}
        options={[
          ...(lists.length ? [{ value: 'records', label: t('Ingest.recordsView') }] : []),
          { value: 'fields', label: t('Workflow.fieldsView') },
          { value: 'json', label: t('Workflow.jsonView') },
        ]}
      />
      {mode === 'records' && lists.length ? (
        <WorkflowRecordList records={lists[0].records} />
      ) : mode === 'json' ? (
        <pre className={styles['json-result']}>{JSON.stringify(value, null, 2) ?? '—'}</pre>
      ) : (
        <dl className={styles['data-fields']}>
          {(paths.length ? paths : ['']).map((path) => (
            <div key={path}>
              <dt>{path || t('Workflow.entireOutput')}</dt>
              <dd>{displayValue(outputAt(value, path))}</dd>
            </div>
          ))}
        </dl>
      )}
    </div>
  );
}
