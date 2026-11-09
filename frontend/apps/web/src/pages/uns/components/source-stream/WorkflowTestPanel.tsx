import { useLayoutEffect, useRef, useState } from 'react';
import { Alert, Collapse, Input, Tabs, Segmented } from 'antd';
import { ButtonPermission } from '@/common-types/button-permission';
import { sourceStreamApi, type ProgramDocument, type StreamTestResult } from '@/apis/core-api/source-stream';
import { useTranslate } from '@/hooks';
import ComButton from '@/components/com-button';
import WorkflowDataView from './WorkflowDataView';
import WorkflowWritePreview from './WorkflowWritePreview';
import { workflowTestCalls } from './workflow-connections';
import {
  withWorkflow,
  workflowIssues,
  workflowThrough,
  type ProgramWorkflow,
  type WorkflowTrace,
} from './program-workflow';
import styles from './workflow.module.scss';

export default function WorkflowTestPanel({
  document,
  workflow,
  selected,
  onResults,
  input,
  responses,
  onInput,
  onResponse,
  results,
  collectionNames,
}: {
  document: ProgramDocument;
  workflow: ProgramWorkflow;
  selected: string;
  onResults: (results: WorkflowTrace[]) => void;
  input: string;
  responses: Record<string, string>;
  onInput: (input: string) => void;
  onResponse: (key: string, value: string) => void;
  results: WorkflowTrace[];
  collectionNames: Record<string, string>;
}) {
  const t = useTranslate();
  const [response, setResponse] = useState<{ snapshot: string; value: StreamTestResult }>();
  const [failedSnapshot, setFailed] = useState('');
  const [runningSnapshot, setRunning] = useState('');
  const [live, setLive] = useState(true);
  const epoch = useRef(0);
  const prefix = workflowThrough(workflow, selected);
  const calls = [...new Map(workflowTestCalls(prefix).map((call) => [call.key, call])).entries()];
  const scriptConnections = prefix.actions.some((step) => step.kind === 'script') && !!prefix.connections?.length;
  const canFetch = calls.length > 0 && !scriptConnections && calls.every(([, step]) => step.safeFetch);
  const fetch = live && canFetch;
  const snapshot = JSON.stringify([document, selected, input, responses, fetch]);
  const current = useRef(snapshot);
  useLayoutEffect(() => {
    current.current = snapshot;
    return () => {
      current.current = '';
    };
  }, [snapshot]);
  const result = response?.snapshot === snapshot ? response.value : undefined;
  const failed = failedSnapshot === snapshot;
  const running = runningSnapshot === snapshot;
  let payload: Record<string, unknown> | undefined;
  try {
    const sourceSamples: Record<string, Record<string, unknown>> = {};
    for (const [key, step] of fetch ? [] : calls) {
      if (step.optional && !responses[key]?.trim()) continue;
      const source = step.source!;
      const response = JSON.parse(responses[key] ?? '');
      const resource = step.operation === 'request' ? 'request' : source.resource;
      if (
        step.operation === 'request' &&
        (!response || typeof response !== 'object' || !Number.isInteger(response.status) || !('data' in response))
      )
        throw new Error('HTTP response required');
      sourceSamples[source.id] ??= {};
      sourceSamples[source.id][resource] = response;
    }
    payload = {
      data: JSON.parse(input),
      sourceSamples,
      previewWrites: prefix.actions.some((step) => step.kind === 'write'),
    };
  } catch {
    /* Keep invalid or absent samples editable. */
  }
  const issues = workflowIssues(prefix, false);
  const trace = (result?.result.data as { steps?: WorkflowTrace[] } | undefined)?.steps ?? [];
  const selectedResult = trace.find((step) => step.id === selected) ?? results.find((step) => step.id === selected);
  const writeErrors = result?.targets?.some((target) => target.preview?.status === 'error') || selectedResult?.failed;
  const hasWritePreview = result?.targets?.some((target) => target.preview);
  const run = async () => {
    if (!payload || issues.length) return;
    const requestEpoch = ++epoch.current;
    const requestSnapshot = snapshot;
    setRunning(snapshot);
    setFailed('');
    setResponse(undefined);
    onResults([]);
    try {
      const program = withWorkflow(document, prefix);
      await sourceStreamApi.validate(program, 0);
      const value = await sourceStreamApi.test(program, payload, 0, fetch ? 'fetch' : 'input', 'trigger');
      if (epoch.current !== requestEpoch || current.current !== requestSnapshot) return;
      setResponse({ snapshot, value });
      const blocked = value.targets?.some((target) => target.preview?.status === 'error');
      const writes = new Set(prefix.actions.filter((step) => step.kind === 'write').map((step) => step.id));
      onResults(
        ((value.result.data as { steps?: WorkflowTrace[] })?.steps ?? []).map((step) => ({
          ...step,
          failed: !!blocked && writes.has(step.id),
        }))
      );
    } catch {
      if (epoch.current === requestEpoch && current.current === requestSnapshot) setFailed(snapshot);
    } finally {
      if (epoch.current === requestEpoch) setRunning('');
    }
  };
  return (
    <div className={styles['form-stack']}>
      {canFetch && (
        <Segmented
          block
          value={fetch ? 'live' : 'sample'}
          onChange={(mode) => {
            setLive(mode === 'live');
            onResults([]);
          }}
          options={[
            { value: 'live', label: t('Ingest.liveRead') },
            { value: 'sample', label: t('Workflow.testData') },
          ]}
        />
      )}
      <Alert type="info" showIcon message={t(fetch ? 'Ingest.liveReadHint' : 'Workflow.dryRunHint')} />
      {scriptConnections && <span className={styles['muted-text']}>{t('Workflow.scriptTestSources')}</span>}
      <Collapse
        ghost
        defaultActiveKey={fetch || selectedResult ? [] : ['input']}
        items={[
          {
            key: 'input',
            label: t('Workflow.testData'),
            children: (
              <div className={styles['form-stack']}>
                <label className={styles.field}>
                  <span>{t('Workflow.triggerData')}</span>
                  <Input.TextArea
                    aria-label={t('Workflow.triggerData')}
                    value={input}
                    autoSize={{ minRows: 5, maxRows: 12 }}
                    onChange={(event) => onInput(event.target.value)}
                  />
                </label>
                {(fetch ? [] : calls).map(([key, step]) => (
                  <label className={styles.field} key={key}>
                    <span>
                      {t('Workflow.sourceResponse')} · {step.source!.name} ·{' '}
                      {step.operation === 'request' ? 'HTTP' : step.source!.resource}
                    </span>
                    <Input.TextArea
                      aria-label={`${t('Workflow.sourceResponse')} ${step.source!.name}`}
                      value={responses[key] ?? ''}
                      autoSize={{ minRows: 3, maxRows: 10 }}
                      placeholder={t(
                        step.operation === 'request' ? 'Workflow.httpResponseHint' : 'Workflow.responseHint'
                      )}
                      onChange={(event) => onResponse(key, event.target.value)}
                    />
                  </label>
                ))}
              </div>
            ),
          },
        ]}
      />
      {!payload && <span className={styles['muted-text']}>{t('Workflow.sampleRequired')}</span>}
      <ComButton
        auth={fetch ? ButtonPermission['uns.streamExecute'] : undefined}
        className={styles['primary-button']}
        loading={running}
        disabled={!payload || !!issues.length}
        onClick={run}
      >
        {t(fetch ? 'Ingest.fetchPreview' : selectedResult ? 'Workflow.retestStep' : 'Workflow.testThrough')}
      </ComButton>
      {failed && <Alert type="error" showIcon message={t('SourceStream.testFailed')} />}
      {(result || selectedResult) && (
        <>
          <Alert
            type={writeErrors ? 'error' : selectedResult && !selectedResult.stopped ? 'success' : 'warning'}
            showIcon
            message={t(
              writeErrors
                ? 'Output.batchBlocked'
                : !selectedResult
                  ? 'Workflow.notReached'
                  : selectedResult.stopped
                    ? 'Workflow.filtered'
                    : 'Workflow.testPassed'
            )}
          />
          {selectedResult && (
            <Tabs
              defaultActiveKey="out"
              items={[
                {
                  key: 'in',
                  label: t('Workflow.dataIn'),
                  children: <WorkflowDataView value={selectedResult.input} />,
                },
                {
                  key: 'out',
                  label: t('Workflow.dataOut'),
                  children: hasWritePreview ? (
                    <WorkflowWritePreview targets={result!.targets!} collectionNames={collectionNames} />
                  ) : (
                    <WorkflowDataView value={selectedResult.output} />
                  ),
                },
              ]}
            />
          )}
          {!selectedResult && hasWritePreview && (
            <WorkflowWritePreview targets={result!.targets!} collectionNames={collectionNames} />
          )}
          {!!result?.targets?.length && !result.targets.some((target) => target.preview) && (
            <Collapse
              items={[
                {
                  key: 'writes',
                  label: t('Workflow.pendingWrites'),
                  children: <pre className={styles['json-result']}>{JSON.stringify(result.targets, null, 2)}</pre>,
                },
              ]}
            />
          )}
        </>
      )}
    </div>
  );
}
