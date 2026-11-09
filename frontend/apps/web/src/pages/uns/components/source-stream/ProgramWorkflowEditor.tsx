import { useState } from 'react';
import { Alert, Collapse, ConfigProvider, Input, Select, Space, Tabs } from 'antd';
import { Add, ArrowDown, ArrowUp, Close, Plug, TrashCan } from '@carbon/icons-react';
import { v4 as uuid } from 'uuid';
import { type ProgramDocument } from '@/apis/core-api/source-stream';
import { useTranslate } from '@/hooks';
import ComButton from '@/components/com-button';
import ProgramConfigEditor from './ProgramConfigEditor';
import WorkflowSourceSetup from './WorkflowSourceSetup';
import WorkflowOutputSetup from './WorkflowOutputSetup';
import WorkflowOutputEditor from './WorkflowOutputEditor';
import useWorkflowMetadata, { outputCollection } from './use-workflow-metadata';
import WorkflowValueEditor from './WorkflowValueEditor';
import WorkflowTestPanel from './WorkflowTestPanel';
import WorkflowRunSettings from './WorkflowRunSettings';
import WorkflowScriptEditor from './WorkflowScriptEditor';
import WorkflowConnections from './WorkflowConnections';
import { workflowUsedSources } from './workflow-connections';
import { workflowStages } from './workflow-data';
import { readProgramDocument } from './program-document';
import {
  newWorkflowAction,
  previousValue,
  readWorkflow,
  withWorkflow,
  workflowThrough,
  workflowJSON,
  requestAllowsBody,
  executableWorkflowDocument,
  type ProgramWorkflow,
  type WorkflowAction,
  type WorkflowTrace,
  type WorkflowValue,
} from './program-workflow';
import WorkflowCanvas from './WorkflowCanvas';
import WorkflowEditorToolbar, { type WorkflowEditorMode } from './WorkflowEditorToolbar';
import { bindingValue, isRecord, recordLists } from './workflow-records';
import WorkflowPagination from './WorkflowPagination';
import WorkflowRequestOptions from './WorkflowRequestOptions';
import styles from './workflow.module.scss';

export default function ProgramWorkflowEditor({
  value,
  onChange,
  readOnly = false,
  initialMode = 'steps',
}: {
  value: string;
  onChange: (value: string) => void;
  readOnly?: boolean;
  initialMode?: 'steps' | 'code';
}) {
  const t = useTranslate();
  const document = readProgramDocument(value);
  const workflow = readWorkflow(document);
  const metadata = useWorkflowMetadata(!!workflow?.actions.some((step) => step.kind === 'write'));
  const [mode, setMode] = useState<WorkflowEditorMode>(initialMode);
  const [connectionsOpen, setConnectionsOpen] = useState(false);
  const [selected, setSelected] = useState<string>();
  const [tab, setTab] = useState('setup');
  const [proofs, setProofs] = useState<{ trace: WorkflowTrace; fingerprint: string }[]>([]);
  const [testInput, setTestInput] = useState('{}');
  const [testResponses, setTestResponses] = useState<Record<string, string>>({});
  const fingerprint = (step: string) =>
    workflowJSON([workflow && workflowThrough(workflow, step), testInput, testResponses]);
  const results = proofs
    .filter((proof) => proof.fingerprint === fingerprint(proof.trace.id))
    .map((proof) => proof.trace);
  const setResults = (traces: WorkflowTrace[]) =>
    setProofs(traces.map((trace) => ({ trace, fingerprint: fingerprint(trace.id) })));
  const update = (next: ProgramWorkflow) => {
    if (document && !readOnly) onChange(JSON.stringify(withWorkflow(document, next), null, 2));
  };
  const toolbar = (
    <WorkflowEditorToolbar document={document} readOnly={readOnly} mode={mode} onMode={setMode} onChange={onChange} />
  );
  const codeEditor = (editorMode: 'code' | 'json') => (
    <ProgramConfigEditor
      value={document ? JSON.stringify(executableWorkflowDocument(document), null, 2) : value}
      onChange={onChange}
      readOnly={readOnly}
      hideHeader
      editorMode={editorMode}
    />
  );
  if (!document || !workflow)
    return (
      <div className={styles.editor}>
        {toolbar}
        {mode !== 'steps' || !document ? (
          <div className={styles['code-panel']}>{codeEditor(!document ? 'json' : 'code')}</div>
        ) : (
          <WorkflowCanvas selected={selected} onSelect={setSelected}>
            {selected === 'script' && (
              <section className={styles['configuration-panel']}>
                <div className={styles['panel-heading']}>
                  <h3>{t('Workflow.scriptStep')}</h3>
                  <ComButton
                    type="text"
                    icon={<Close />}
                    aria-label={t('common.close')}
                    onClick={() => setSelected(undefined)}
                  />
                </div>
                {codeEditor('code')}
              </section>
            )}
          </WorkflowCanvas>
        )}
      </div>
    );
  const index = workflow.actions.findIndex((action) => action.id === selected);
  const action = workflow.actions[index];
  const triggerSelected = !action;
  const selectedId = triggerSelected ? 'trigger' : action.id;
  const stages = workflowStages(workflow, selectedId);
  const collection = action?.kind === 'write' ? outputCollection(action.target, metadata.items) : undefined;
  const outputValue =
    action?.kind === 'write' ? bindingValue(action.each?.records || action.record, results) : undefined;
  const outputRecords =
    Array.isArray(outputValue) && outputValue.every(isRecord)
      ? outputValue
      : isRecord(outputValue)
        ? [outputValue]
        : undefined;
  const outputUnavailable = action?.kind === 'write' && (metadata.loading || metadata.failed || !collection);
  const activeTab =
    stages.setupIssues.length || outputUnavailable || (tab === 'configure' && !stages.configure)
      ? 'setup'
      : tab === 'test' && stages.issues.length
        ? triggerSelected
          ? 'setup'
          : 'configure'
        : tab;
  const testStep = (id: string) => {
    setSelected(id);
    setTab('test');
  };
  const subscribed = workflow.trigger.kind === 'source-event';
  const sourceCount = new Set(
    [...workflowUsedSources(workflow), ...(workflow.connections ?? [])].map((source) => source.id)
  ).size;
  const stepOptions = [
    { value: 'trigger', label: t(subscribed ? 'Workflow.receivedData' : 'Workflow.inputData') },
    ...workflow.actions.slice(0, Math.max(0, index)).map((step, index) => ({
      value: step.id,
      label: `${index + (subscribed ? 2 : 1)}. ${t(`Workflow.kind.${step.kind}`)}`,
    })),
  ];
  const patchAction = (patch: Partial<WorkflowAction>) =>
    update({
      ...workflow,
      actions: workflow.actions.map((item) =>
        item.id === selectedId ? ({ ...item, ...patch } as WorkflowAction) : item
      ),
    });
  const add = (kind: WorkflowAction['kind'], position: number) => {
    const id = `step-${uuid()}`;
    const previous = workflow.actions[position - 1]?.id ?? 'trigger';
    const next = newWorkflowAction(kind, id, previous);
    const list = recordLists(results.find((trace) => trace.id === previous)?.output)[0];
    if (next.kind === 'write' && list)
      next.each = { records: { kind: 'field', step: previous, path: list.path }, identityPath: '' };
    update({
      ...workflow,
      actions: [...workflow.actions.slice(0, position), next, ...workflow.actions.slice(position)],
    });
    setSelected(id);
    setTab(kind === 'script' ? 'configure' : 'setup');
  };
  const field = (label: string, binding: WorkflowValue, change: (next: WorkflowValue) => void) => (
    <div className={styles.field}>
      <span>{label}</span>
      <WorkflowValueEditor
        label={label}
        value={binding}
        onChange={change}
        steps={stepOptions}
        results={results}
        onTestStep={testStep}
      />
    </div>
  );
  const move = (direction: number) => {
    const actions = [...workflow.actions];
    [actions[index], actions[index + direction]] = [actions[index + direction], actions[index]];
    update({ ...workflow, actions });
  };
  const setup = (
    <div className={styles['form-stack']}>
      {(triggerSelected && workflow.trigger.kind === 'source-event') || action?.kind === 'source' ? (
        <WorkflowSourceSetup
          key={selectedId}
          event={triggerSelected}
          value={triggerSelected ? workflow.trigger.source : action.kind === 'source' ? action.source : undefined}
          operation={action?.kind === 'source' ? action.operation : undefined}
          connections={workflow.connections}
          onChange={(source, operation) => {
            if (triggerSelected) update({ ...workflow, trigger: { ...workflow.trigger, source } });
            else if (action.kind === 'source') {
              patchAction({ source, operation: operation ?? action.operation });
            }
          }}
        />
      ) : null}
      {action?.kind === 'write' && (
        <WorkflowOutputSetup
          target={action.target}
          metadata={metadata}
          records={outputRecords}
          onChange={(target) => patchAction({ target })}
        />
      )}
      {stages.setupIssues.map((issue) => (
        <Alert key={issue.code} type="warning" showIcon message={t(`Workflow.issue.${issue.code}`)} />
      ))}
    </div>
  );
  const configure = (
    <div className={styles['form-stack']}>
      {triggerSelected ? null : action.kind === 'source' ? (
        <>
          {action.operation === 'request' && (
            <>
              <label className={styles.field}>
                <span>{t('Workflow.method')}</span>
                <Select
                  aria-label={t('Workflow.method')}
                  value={action.method}
                  onChange={(method) => patchAction({ method })}
                  options={['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'].map((method) => ({
                    value: method,
                  }))}
                />
              </label>
              <label className={styles.field}>
                <span>{t('Workflow.requestPath')}</span>
                <Input
                  aria-label={t('Workflow.requestPath')}
                  value={action.path}
                  placeholder="/api/v1/items"
                  onChange={(event) => patchAction({ path: event.target.value })}
                />
              </label>
              <WorkflowRequestOptions action={action} onChange={patchAction} />
              <WorkflowPagination
                value={action.pagination}
                method={action.method}
                onChange={(pagination) => patchAction({ pagination })}
              />
            </>
          )}
          {action.input ? (
            <>
              {field(
                t(action.operation === 'request' ? 'Workflow.requestBody' : 'Workflow.readOptions'),
                action.input,
                (input) => patchAction({ input })
              )}
              <ComButton onClick={() => patchAction({ input: undefined })}>{t('Workflow.removeInput')}</ComButton>
            </>
          ) : action.operation !== 'request' || requestAllowsBody(action.method) ? (
            <ComButton icon={<Add />} onClick={() => patchAction({ input: { kind: 'json', value: '{}' } })}>
              {t(action.operation === 'request' ? 'Workflow.addBody' : 'Workflow.addOptions')}
            </ComButton>
          ) : null}
        </>
      ) : action.kind === 'map' ? (
        <>
          {action.fields.map((item, fieldIndex) => (
            <div className={styles['mapping-field']} key={fieldIndex}>
              <div className={styles['inline-row']}>
                <Input
                  aria-label={`${t('Workflow.outputField')} ${fieldIndex + 1}`}
                  placeholder={t('Workflow.outputField')}
                  value={item.key}
                  onChange={(event) =>
                    patchAction({
                      fields: action.fields.map((row, rowIndex) =>
                        rowIndex === fieldIndex ? { ...row, key: event.target.value } : row
                      ),
                    })
                  }
                />
                <ComButton
                  type="text"
                  aria-label={t('Workflow.removeField')}
                  icon={<Close />}
                  onClick={() =>
                    patchAction({ fields: action.fields.filter((_, rowIndex) => rowIndex !== fieldIndex) })
                  }
                />
              </div>
              <WorkflowValueEditor
                label={item.key || `${t('Workflow.outputField')} ${fieldIndex + 1}`}
                value={item.value}
                steps={stepOptions}
                results={results}
                onTestStep={testStep}
                onChange={(value) =>
                  patchAction({
                    fields: action.fields.map((row, rowIndex) => (rowIndex === fieldIndex ? { ...row, value } : row)),
                  })
                }
              />
            </div>
          ))}
          <ComButton
            icon={<Add />}
            onClick={() =>
              patchAction({
                fields: [
                  ...action.fields,
                  { key: '', value: previousValue(stepOptions[stepOptions.length - 1].value) },
                ],
              })
            }
          >
            {t('Workflow.addField')}
          </ComButton>
        </>
      ) : action.kind === 'script' ? (
        <>
          <Collapse
            ghost
            items={[
              {
                key: 'input',
                label: t('Workflow.scriptInput'),
                children: field(t('Workflow.scriptInput'), action.input, (input) => patchAction({ input })),
              },
            ]}
          />
          <WorkflowScriptEditor
            code={action.code}
            readOnly={readOnly}
            onChange={(code) => patchAction({ code })}
            connections={workflow.connections}
            onConnections={() => setConnectionsOpen(true)}
          />
        </>
      ) : action.kind === 'filter' ? (
        <>
          {field(t('Workflow.leftValue'), action.left, (left) => patchAction({ left }))}
          <Select
            aria-label={t('Workflow.condition')}
            value={action.operator}
            onChange={(operator) => patchAction({ operator })}
            options={['exists', 'eq', 'ne', 'gt', 'lt'].map((operator) => ({
              value: operator,
              label: t(`Workflow.operator.${operator}`),
            }))}
          />
          {action.operator !== 'exists' &&
            field(t('Workflow.rightValue'), action.right, (right) => patchAction({ right }))}
        </>
      ) : (
        <WorkflowOutputEditor
          key={action.id}
          action={action}
          collection={collection}
          items={metadata.items}
          onSaved={metadata.saved}
          onChange={patchAction}
          steps={stepOptions}
          results={results}
          onTestStep={testStep}
        />
      )}
      {[...new Set(stages.issues.map((issue) => issue.code))].map((code) => (
        <Alert key={code} type="warning" showIcon message={t(`Workflow.issue.${code}`)} />
      ))}
      {stages.issues.some((issue) => issue.step !== selectedId) && (
        <Alert type="warning" showIcon message={t('Workflow.upstreamInvalid')} />
      )}
      <ComButton className={styles['primary-button']} disabled={!!stages.issues.length} onClick={() => setTab('test')}>
        {t('Workflow.continueTest')}
      </ComButton>
    </div>
  );
  const testPanel = (
    <WorkflowTestPanel
      document={document as ProgramDocument}
      workflow={workflow}
      selected={selectedId}
      results={results}
      onResults={setResults}
      collectionNames={Object.fromEntries(metadata.items.map((item) => [item.id, item.data.name]))}
      input={testInput}
      responses={testResponses}
      onInput={setTestInput}
      onResponse={(key, response) => setTestResponses({ ...testResponses, [key]: response })}
    />
  );
  return (
    <div className={styles.editor}>
      {toolbar}
      {mode === 'steps' && (
        <WorkflowRunSettings
          value={workflow.trigger}
          readOnly={readOnly}
          onChange={(trigger) => update({ ...workflow, trigger })}
        >
          <ComButton icon={<Plug />} onClick={() => setConnectionsOpen(true)}>
            {t('Workflow.connections')} · {sourceCount}
          </ComButton>
        </WorkflowRunSettings>
      )}
      {mode !== 'steps' ? (
        <div className={styles['code-panel']}>{codeEditor(mode)}</div>
      ) : (
        <WorkflowCanvas
          workflow={workflow}
          selected={selected}
          results={results}
          metadata={metadata.items}
          onAdd={readOnly ? undefined : add}
          onSelect={(id) => {
            setSelected(id);
            setTab('setup');
          }}
        >
          {selected && (
            <section className={styles['configuration-panel']}>
              <div className={styles['panel-heading']}>
                <h3>
                  {triggerSelected
                    ? t(subscribed ? 'Workflow.receivedData' : 'Workflow.inputData')
                    : index + (subscribed ? 2 : 1) + '. ' + t('Workflow.kind.' + action.kind)}
                </h3>
                <Space size={0}>
                  {!triggerSelected && !readOnly && (
                    <>
                      <ComButton
                        type="text"
                        aria-label={t('Workflow.moveUp')}
                        icon={<ArrowUp />}
                        disabled={index === 0}
                        onClick={() => move(-1)}
                      />
                      <ComButton
                        type="text"
                        aria-label={t('Workflow.moveDown')}
                        icon={<ArrowDown />}
                        disabled={index === workflow.actions.length - 1}
                        onClick={() => move(1)}
                      />
                      <ComButton
                        type="text"
                        aria-label={t('Workflow.deleteStep')}
                        icon={<TrashCan />}
                        onClick={() => {
                          update({ ...workflow, actions: workflow.actions.filter((item) => item.id !== selectedId) });
                          setSelected(undefined);
                          setTab('setup');
                        }}
                      >
                        {t('Workflow.deleteStep')}
                      </ComButton>
                    </>
                  )}
                  <ComButton
                    type="text"
                    icon={<Close />}
                    aria-label={t('common.close')}
                    onClick={() => setSelected(undefined)}
                  />
                </Space>
              </div>
              <ConfigProvider componentDisabled={readOnly}>
                <fieldset className={styles['panel-fields']} disabled={readOnly}>
                  <Tabs
                    activeKey={activeTab === 'setup' ? 'configure' : activeTab}
                    onChange={setTab}
                    items={[
                      {
                        key: 'configure',
                        label: t('Workflow.configure'),
                        children: (
                          <>
                            {setup}
                            {stages.configure && !stages.setupIssues.length && !outputUnavailable && configure}
                          </>
                        ),
                      },
                      { key: 'test', label: t('Workflow.test'), disabled: !!stages.issues.length, children: testPanel },
                    ]}
                  />
                </fieldset>
              </ConfigProvider>
            </section>
          )}
        </WorkflowCanvas>
      )}
      {connectionsOpen && (
        <WorkflowConnections
          workflow={workflow}
          readOnly={readOnly}
          onChange={(connections) => update({ ...workflow, connections })}
          onClose={() => setConnectionsOpen(false)}
        />
      )}
    </div>
  );
}
