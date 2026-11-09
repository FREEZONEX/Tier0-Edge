import { useSourceStreamApi } from '@/apis/core-api/source-stream-scope';
import { useEffect, useRef, useState, type FC } from 'react';
import { Alert, App, Select } from 'antd';
import { json } from '@codemirror/lang-json';
import { v4 as uuid } from 'uuid';
import {
  type ProgramDocument,
  type ProgramManifest,
  type Stream,
  type StreamTestResult,
} from '@/apis/core-api/source-stream';
import { useTranslate } from '@/hooks';
import ProModal from '@/components/pro-modal';
import ProCodemirror from '@/components/pro-codemirror';
import HelpTooltip from '@/components/help-tooltip';
import { openConfirmModal } from '@/components/confirm-modal';
import StreamTestResultView from './StreamTestResultView';
import { sampleInput } from './program-document';
import styles from './index.module.scss';

const ProgramExecutionModal: FC<{
  mode?: 'test' | 'invoke';
  document?: ProgramDocument;
  stream?: Stream;
  manifest?: ProgramManifest;
  onClose: () => void;
}> = ({ mode, document, stream, manifest, onClose }) => {
  const sourceStreamApi = useSourceStreamApi();
  const t = useTranslate();
  const { modal } = App.useApp();
  const [entryId, setEntryId] = useState<string>();
  const [input, setInput] = useState('{}');
  const [result, setResult] = useState<StreamTestResult>();
  const [testedInput, setTestedInput] = useState<unknown>();
  const [invocation, setInvocation] = useState<unknown>();
  const [failed, setFailed] = useState(false);
  const [running, setRunning] = useState(false);
  const [key, setKey] = useState(uuid());
  const generation = useRef(0);
  const current = useRef({ document, mode, stream });
  current.current = { document, mode, stream };
  const entries = (manifest?.entries ?? []).filter((entry) => mode !== 'invoke' || entry.kind === 'request');
  useEffect(() => {
    generation.current++;
    const available = (manifest?.entries ?? []).filter((entry) => mode !== 'invoke' || entry.kind === 'request');
    setEntryId(available.length === 1 ? available[0].id : undefined);
    setResult(undefined);
    setInvocation(undefined);
    setFailed(false);
    setRunning(false);
    setKey(uuid());
    setInput(
      mode === 'test' && manifest ? sampleInput(manifest, available.length === 1 ? available[0].id : undefined) : '{}'
    );
    return () => {
      generation.current++;
    };
  }, [mode, document, stream?.id, stream?.activeVersion, manifest]);
  let validInput = false;
  try {
    JSON.parse(input);
    validInput = true;
  } catch {
    /* Preserve invalid sample text. */
  }
  const changeTestInput = (value: string) => {
    generation.current++;
    setInput(value);
    setResult(undefined);
    setTestedInput(undefined);
    setFailed(false);
  };
  const run = async () => {
    if (!mode || !entryId || !document) return;
    const requestGeneration = generation.current;
    const requestDocument = document;
    setRunning(true);
    setFailed(false);
    setResult(undefined);
    try {
      if (mode === 'test') {
        const value = await sourceStreamApi.test(document, JSON.parse(input), 0, 'input', entryId);
        if (generation.current === requestGeneration && current.current.document === requestDocument) {
          setResult(value);
          setTestedInput(JSON.parse(input));
        }
      } else if (stream) {
        const value = await sourceStreamApi.invoke(stream.id, stream.activeVersion, JSON.parse(input), key, entryId);
        if (generation.current === requestGeneration && current.current.document === requestDocument)
          setInvocation(value);
      }
    } catch {
      if (generation.current === requestGeneration) setFailed(true);
    } finally {
      if (generation.current === requestGeneration) setRunning(false);
    }
  };
  return (
    <ProModal
      className={styles['editor-modal']}
      open={!!mode}
      title={t(mode === 'invoke' ? 'SourceStream.invoke' : 'SourceStream.testRun')}
      width={800}
      footer={undefined}
      onCancel={onClose}
      confirmLoading={running}
      okText={t(mode === 'invoke' ? 'SourceStream.invoke' : 'SourceStream.testRun')}
      cancelText={t('common.close')}
      okButtonProps={{
        className: styles['manage-button'],
        disabled: !entryId || !validInput || running || invocation !== undefined || (mode === 'invoke' && failed),
      }}
      onOk={
        mode === 'invoke'
          ? () =>
              openConfirmModal(modal, {
                title: t('SourceStream.invoke'),
                content: t('SourceStream.invokeWarning'),
                okText: t('common.confirm'),
                cancelText: t('common.cancel'),
                onOk: run,
              })
          : run
      }
    >
      <div className={styles.section}>
        {mode === 'invoke' ? (
          <Alert type="warning" showIcon message={t('SourceStream.invokeWarning')} />
        ) : (
          <div className={styles.row}>
            <span>{t('SourceStream.manualSample')}</span>
            <HelpTooltip title={t('SourceStream.fullProgramSampleHint')} />
          </div>
        )}
        {entries.length > 1 && (
          <Select
            aria-label={t('SourceStream.entryId')}
            placeholder={t('SourceStream.selectEntry')}
            value={entryId}
            disabled={running || invocation !== undefined || (mode === 'invoke' && failed)}
            onChange={(value) => {
              generation.current++;
              if (mode === 'test' && manifest && input === sampleInput(manifest, entryId)) {
                setInput(sampleInput(manifest, value));
              }
              setEntryId(value);
              setResult(undefined);
              setFailed(false);
            }}
            options={entries.map((entry) => ({
              value: entry.id,
              label: `${entry.id} · ${t(`SourceStream.entry.${entry.kind}`)}`,
            }))}
          />
        )}
        {!entries.length && <Alert type="warning" showIcon message={t('SourceStream.noTestEntries')} />}
        <ProCodemirror
          value={input}
          editable={!running && invocation === undefined && !(mode === 'invoke' && failed)}
          onChange={changeTestInput}
          extensions={[json()]}
          height="220px"
          showHint={false}
        />
        {!validInput && <Alert type="error" showIcon message={t('SourceStream.invalidJson')} />}
        {failed && (
          <Alert
            type="error"
            showIcon
            message={t(mode === 'invoke' ? 'SourceStream.invokeFailed' : 'SourceStream.testFailed')}
          />
        )}
        {result && <StreamTestResultView result={result} input={testedInput} />}
        {invocation !== undefined && <pre className={styles.code}>{JSON.stringify(invocation, null, 2)}</pre>}
      </div>
    </ProModal>
  );
};
export default ProgramExecutionModal;
