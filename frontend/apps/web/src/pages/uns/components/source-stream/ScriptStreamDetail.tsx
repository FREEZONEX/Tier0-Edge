import { useEffect, useMemo, useState } from 'react';
import {
  Alert,
  App,
  Collapse,
  Dropdown,
  Empty,
  Input,
  InputNumber,
  Segmented,
  Space,
  Tabs,
  Tag,
  type MenuProps,
} from 'antd';
import { Add, ChevronDown, Renew } from '@carbon/icons-react';
import { javascript, javascriptLanguage } from '@codemirror/lang-javascript';
import { json } from '@codemirror/lang-json';
import { v4 as uuid } from 'uuid';
import {
  sourceStreamApi,
  type Source,
  type Stream,
  type ScriptStreamDocument,
  type StreamTestResult,
} from '@/apis/core-api/source-stream';
import { ButtonPermission } from '@/common-types/button-permission';
import ComButton from '@/components/com-button';
import ComDrawer from '@/components/com-drawer';
import HelpTooltip from '@/components/help-tooltip';
import ProCodemirror from '@/components/pro-codemirror';
import { openConfirmModal } from '@/components/confirm-modal';
import { useTranslate } from '@/hooks';
import { hasPermission } from '@/utils/auth';
import { MAX_LENGTHS } from '@/utils/limits';
import { useIntegrationDirty, useIntegrationRoute } from './IntegrationTabs';
import { Versions } from './StreamDetail';
import RunsPanel from './RunsPanel';
import SourcePicker from './SourcePicker';
import StreamEditorStep from './StreamEditorStep';
import StreamTestResultView from './StreamTestResultView';
import { readProgramDocument } from './program-document';
import styles from './index.module.scss';

const identity = 'return { data: input, drop: false, logs: [] };';
const mqttScript = (alias: string) => `const source = sources.get("${alias}");
source.subscribe("devices/+/telemetry", async (input, context) => {
  return { data: input, drop: false, logs: [] };
}, { qos: 1 });`;

export default function ScriptStreamDetail({
  stream,
  initialSource,
  onChanged,
}: {
  stream?: Stream;
  initialSource?: Source;
  onChanged: (stream?: Stream) => void | Promise<void>;
}) {
  const t = useTranslate();
  const { message, modal } = App.useApp();
  const route = useIntegrationRoute();
  const initial = JSON.stringify(
    stream?.document || {
      name: t('SourceStream.untitledStream'),
      description: '',
      model: 'script',
      sourceId: 0,
      sources: initialSource
        ? [{ alias: 'source1', sourceId: initialSource.id, configRevision: initialSource.configRevision }]
        : [],
      topicIds: [],
      script: { code: initialSource?.protocol === 'mqtt' ? mqttScript('source1') : identity, timeoutMs: 5000 },
    },
    null,
    2
  );
  const [raw, setRaw] = useState(initial);
  const [mode, setMode] = useState<'code' | 'json'>('code');
  const [picker, setPicker] = useState(false);
  const [sourceNames, setSourceNames] = useState<Record<number, string>>({});
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const [input, setInput] = useState('{}');
  const [result, setResult] = useState<{ response: StreamTestResult; raw: string; input: string }>();
  const [invocation, setInvocation] = useState<unknown>();
  const [testFailed, setTestFailed] = useState(false);
  const [runRefresh, setRunRefresh] = useState(0);
  const [runKey, setRunKey] = useState(() => uuid());
  let doc: ScriptStreamDocument | undefined;
  try {
    const value = JSON.parse(raw);
    if (
      value.model === 'script' &&
      typeof value.name === 'string' &&
      typeof value.script?.code === 'string' &&
      Array.isArray(value.sources) &&
      value.sources.every(
        (binding: unknown) =>
          binding &&
          typeof binding === 'object' &&
          'alias' in binding &&
          typeof binding.alias === 'string' &&
          'sourceId' in binding &&
          typeof binding.sourceId === 'number'
      )
    )
      doc = value;
  } catch {
    /* Invalid JSON remains editable. */
  }
  const currentResult = result?.raw === raw && result.input === input ? result : undefined;
  let validInput = false;
  try {
    JSON.parse(input);
    validInput = true;
  } catch {
    /* Keep invalid input editable. */
  }
  useEffect(() => {
    setResult(undefined);
    setInvocation(undefined);
    setTestFailed(false);
  }, [raw, input]);
  const sourceIds = doc?.sources.map((binding) => binding.sourceId).join(',') || '';
  useEffect(() => {
    let alive = true;
    if (!sourceIds) {
      setSourceNames({});
      return;
    }
    Promise.all(sourceIds.split(',').map((id) => sourceStreamApi.source(Number(id))))
      .then((items) => {
        if (alive) setSourceNames(Object.fromEntries(items.map((source) => [source.id, source.name])));
      })
      .catch(() => {
        if (alive) setFailed(true);
      });
    return () => {
      alive = false;
    };
  }, [sourceIds]);
  useEffect(() => {
    setRunKey(uuid());
  }, [stream?.activeVersion]);
  const dirty = raw !== initial;
  const subscription = useMemo(() => {
    const code = doc?.script.code || '';
    let found = false;
    // 编辑中的展示提示由语法树判断；最终声明、权限和协议由后端校验。
    javascriptLanguage.parser.parse(code).iterate({
      enter(node) {
        if (node.name === 'PropertyName' && code.slice(node.from, node.to) === 'subscribe') found = true;
      },
    });
    return found;
  }, [doc?.script.code]);
  const writeDocument = readProgramDocument(raw);
  const canEdit = hasPermission(ButtonPermission['uns.streamManage']) && !!writeDocument;
  const canDeploy = hasPermission(ButtonPermission['uns.streamDeploy']);
  const editing = !stream || route.edit === 'stream';
  useIntegrationDirty(`script-${stream?.id || 'new'}`, dirty);
  useEffect(() => {
    setRaw(initial);
  }, [stream?.id, stream?.revision]);
  useEffect(() => {
    const warn = (event: BeforeUnloadEvent) => {
      if (dirty) {
        event.preventDefault();
        event.returnValue = '';
      }
    };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [dirty]);
  const patch = (value: Partial<ScriptStreamDocument>) => {
    if (doc) setRaw(JSON.stringify({ ...doc, ...value }, null, 2));
  };
  const perform = async (action: () => Promise<void>) => {
    setBusy(true);
    setFailed(false);
    try {
      await action();
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  const save = async () => {
    if (!writeDocument) return;
    const saved = stream
      ? await sourceStreamApi.updateStream(stream.id, writeDocument, { expectedRevision: stream.revision })
      : await sourceStreamApi.createStream(writeDocument);
    await onChanged(saved);
    route.go({ view: 'stream', streamId: saved.id }, true);
    message.success(t('SourceStream.saved'));
  };
  const add = (source: Source) => {
    if (!doc) return;
    let n = 1;
    while (doc.sources.some((binding) => binding.alias === `source${n}`)) n++;
    const alias = `source${n}`;
    const options =
      source.protocol === 'http-api' || source.protocol === 'oauth2'
        ? '{ path: "", query: {} }'
        : source.protocol === 'mqtt'
          ? '{ topic: "", qos: 0, timeoutMs: 20000 }'
          : source.protocol === 'opcua'
            ? '{ nodeId: "" }'
            : '{ table: "holding-register", address: 0, quantity: 1 }';
    const call = `const ${alias}Data = await sources.get("${alias}").read(${options});`;
    const code =
      doc.script.code === identity
        ? source.protocol === 'mqtt'
          ? mqttScript(alias)
          : `${call}\nreturn { data: ${alias}Data, drop: false, logs: [] };`
        : doc.script.code;
    patch({
      sources: [...doc.sources, { alias, sourceId: source.id, configRevision: source.configRevision }],
      script: { ...doc.script, code },
    });
    setPicker(false);
  };
  const menu: MenuProps['items'] = [
    ...(canEdit
      ? [
          {
            key: 'edit',
            label: t('SourceStream.configureStream'),
            onClick: () => route.go({ view: 'stream', streamId: stream?.id, edit: 'stream' }),
          },
        ]
      : []),
    ...(stream && canDeploy
      ? [
          {
            key: 'deploy',
            label: t('SourceStream.deploy'),
            disabled: dirty || busy,
            onClick: () =>
              openConfirmModal(modal, {
                title: t('SourceStream.deploy'),
                content: t('SourceStream.deployHint'),
                okText: t('common.confirm'),
                cancelText: t('common.cancel'),
                onOk: async () => {
                  await onChanged(await sourceStreamApi.deploy(stream.id, stream.revision, 0));
                },
              }),
          },
          {
            key: 'disable',
            label: t('SourceStream.disableStream'),
            disabled: stream.status !== 'enabled' || busy,
            onClick: () =>
              void perform(async () => {
                await onChanged(await sourceStreamApi.disable(stream.id, { expectedRevision: stream.revision }));
              }),
          },
        ]
      : []),
    ...(stream && canEdit
      ? [
          {
            key: 'delete',
            danger: true,
            disabled: busy,
            label: t('SourceStream.deleteStream'),
            onClick: () =>
              openConfirmModal(modal, {
                title: t('SourceStream.deleteStream'),
                content: t('SourceStream.deleteStreamHint'),
                danger: true,
                okText: t('common.confirm'),
                cancelText: t('common.cancel'),
                onOk: async () => {
                  await sourceStreamApi.deleteStream(stream.id, { expectedRevision: stream.revision });
                  await onChanged();
                },
              }),
          },
        ]
      : []),
  ];
  const closeEditor = () => {
    if (busy) return;
    const close = () => {
      setRaw(initial);
      setResult(undefined);
      setFailed(false);
      route.go(stream ? { view: 'stream', streamId: stream.id } : { view: 'stream' }, true);
    };
    if (!dirty) close();
    else
      openConfirmModal(modal, {
        title: t('SourceStream.unsaved'),
        content: t('SourceStream.discardHint'),
        okText: t('SourceStream.discard'),
        cancelText: t('common.cancel'),
        onOk: close,
      });
  };
  const editor = (
    <div className={styles.section}>
      <div className={styles.row}>
        <Segmented
          value={mode}
          onChange={(value) => setMode(value as 'code' | 'json')}
          options={[
            { value: 'code', label: t('SourceStream.code') },
            { value: 'json', label: t('SourceStream.jsonConfig') },
          ]}
        />
      </div>
      {mode === 'code' && doc ? (
        <>
          <label className={styles.field}>
            <span>{t('SourceStream.name')}</span>
            <Input
              disabled={!canEdit || busy}
              value={doc.name}
              onChange={(event) => {
                if (Array.from(event.target.value).length <= MAX_LENGTHS.name) patch({ name: event.target.value });
              }}
            />
          </label>
          <StreamEditorStep
            number={1}
            title={t('SourceStream.sourceReferences')}
            summary={
              doc.sources.length
                ? doc.sources.map((binding) => sourceNames[binding.sourceId] || binding.alias).join(' · ')
                : t('SourceStream.sourcesOptional')
            }
          >
            <Space wrap>
              {doc.sources.map((binding) => (
                <Tag
                  key={binding.alias}
                  closable={canEdit && !busy}
                  onClose={(event) => {
                    event.preventDefault();
                    patch({ sources: doc!.sources.filter((item) => item.alias !== binding.alias) });
                  }}
                >
                  {sourceNames[binding.sourceId] || binding.sourceId} · {binding.alias}
                </Tag>
              ))}
              <ComButton
                auth={ButtonPermission['uns.sources']}
                size="small"
                icon={<Add />}
                disabled={!canEdit || busy}
                onClick={() => setPicker(true)}
              >
                {t('SourceStream.addSource')}
              </ComButton>
              {!!doc.sources.length && (
                <ComButton
                  size="small"
                  disabled={!canEdit || busy}
                  icon={<Renew />}
                  onClick={() =>
                    void perform(async () => {
                      const sources = await Promise.all(
                        doc!.sources.map(async (binding) => ({
                          ...binding,
                          configRevision: (await sourceStreamApi.source(binding.sourceId)).configRevision,
                        }))
                      );
                      patch({ sources });
                    })
                  }
                >
                  {t('SourceStream.refreshReferences')}
                </ComButton>
              )}
            </Space>
          </StreamEditorStep>
          <StreamEditorStep
            number={2}
            title={t('SourceStream.script')}
            defaultOpen
            summary={t(subscription ? 'SourceStream.mqttSubscription' : 'SourceStream.manualTrigger')}
          >
            <ProCodemirror
              value={doc.script.code}
              extensions={[javascript()]}
              height="240px"
              editable={canEdit && !busy}
              showHint={false}
              onChange={(code) => patch({ script: { ...doc!.script, code } })}
            />
            <Collapse
              ghost
              items={[
                {
                  key: 'advanced',
                  label: t('SourceStream.advanced'),
                  children: (
                    <label className={styles.field}>
                      <span>{t('SourceStream.scriptTimeout')}</span>
                      <InputNumber
                        min={100}
                        max={30000}
                        step={100}
                        value={doc.script.timeoutMs}
                        disabled={!canEdit || busy}
                        onChange={(value) => value && patch({ script: { ...doc!.script, timeoutMs: value } })}
                        addonAfter="ms"
                      />
                    </label>
                  ),
                },
              ]}
            />
          </StreamEditorStep>
        </>
      ) : (
        <ProCodemirror
          value={raw}
          extensions={[json()]}
          height="440px"
          editable={canEdit && !busy}
          showHint={false}
          onChange={setRaw}
        />
      )}
      {!doc && <Alert type="error" message={t('SourceStream.invalidConfiguration')} />}
      <StreamEditorStep
        number={3}
        title={t('SourceStream.testRun')}
        summary={t(
          busy
            ? 'SourceStream.status.processing'
            : testFailed
              ? 'SourceStream.status.failed'
              : currentResult
                ? 'SourceStream.testCompleted'
                : 'SourceStream.testNotRun'
        )}
      >
        <div className={styles.section}>
          {!subscription && (
            <>
              <label className={styles.field}>
                <span>{t('SourceStream.input')}</span>
                <ProCodemirror
                  value={input}
                  editable={!busy}
                  extensions={[json()]}
                  height="100px"
                  showHint={false}
                  onChange={(value) => {
                    setInput(value);
                    setRunKey(uuid());
                    setResult(undefined);
                  }}
                />
              </label>
              {!validInput && <Alert type="error" message={t('SourceStream.invalidJson')} />}
            </>
          )}
          <Space>
            <ComButton
              auth={ButtonPermission['uns.streamManage']}
              className={styles['manage-button']}
              disabled={!writeDocument || (!subscription && !validInput) || busy}
              onClick={() =>
                void perform(async () => {
                  if (!writeDocument) return;
                  setResult(undefined);
                  setInvocation(undefined);
                  setTestFailed(false);
                  try {
                    const response = await sourceStreamApi.test(
                      writeDocument,
                      { data: subscription ? {} : JSON.parse(input) },
                      0,
                      'input'
                    );
                    setResult({ response, raw, input });
                  } catch {
                    setTestFailed(true);
                  }
                })
              }
            >
              {t(subscription ? 'SourceStream.testMessage' : 'SourceStream.testScript')}
            </ComButton>
            {!subscription && (
              <ComButton
                auth={ButtonPermission['uns.streamExecute']}
                disabled={!stream || stream.status !== 'enabled' || dirty || busy || !validInput}
                onClick={() =>
                  void perform(async () => {
                    if (!stream) return;
                    setResult(undefined);
                    setInvocation(
                      await sourceStreamApi.invoke(stream.id, stream.activeVersion, JSON.parse(input), runKey)
                    );
                    setRunKey(uuid());
                    setRunRefresh((v) => v + 1);
                  })
                }
              >
                {t('SourceStream.runSavedVersion')}
              </ComButton>
            )}
          </Space>
          <HelpTooltip
            title={t(subscription ? 'SourceStream.subscriptionTestHint' : 'SourceStream.scriptReadOnlyHint')}
          />
          {testFailed && <Alert type="error" showIcon message={t('SourceStream.testFailed')} />}
          {currentResult && (
            <StreamTestResultView
              result={currentResult.response}
              input={subscription ? undefined : JSON.parse(currentResult.input)}
            />
          )}
          {invocation !== undefined && <pre className={styles.code}>{JSON.stringify(invocation, null, 2)}</pre>}
        </div>
      </StreamEditorStep>
    </div>
  );
  return (
    <div className={styles.section}>
      <div className={styles.row}>
        <div>
          <span className={styles.muted}>{t('SourceStream.stream')}</span>
          <h2>{stream?.name || t('SourceStream.addStream')}</h2>
        </div>
        {stream && !!menu.length && (
          <Dropdown trigger={['click']} menu={{ items: menu }}>
            <ComButton className={styles['manage-button']} loading={busy}>
              {t('SourceStream.manage')} <ChevronDown />
            </ComButton>
          </Dropdown>
        )}
      </div>
      {failed && !editing && <Alert type="error" showIcon message={t('SourceStream.scriptActionFailed')} />}
      {stream ? (
        <Tabs
          activeKey={['versions', 'runs'].includes(route.tab) ? route.tab : 'overview'}
          onChange={(tab) => route.go({ view: 'stream', streamId: stream.id, tab })}
          items={[
            {
              key: 'overview',
              label: t('common.detail'),
              children: (
                <div className={styles.section}>
                  <span>
                    {t('SourceStream.status')}: {t(`SourceStream.status.${stream.status}`)}
                  </span>
                  <section className={styles['detail-section']}>
                    <h3>{t('SourceStream.draftCode')}</h3>
                    <ProCodemirror
                      value={doc?.script.code || ''}
                      extensions={[javascript()]}
                      readOnly
                      height="440px"
                      showHint={false}
                    />
                  </section>
                </div>
              ),
            },
            {
              key: 'versions',
              label: t('SourceStream.versions'),
              children: <Versions key={stream.revision} stream={stream} onChanged={onChanged} />,
            },
            {
              key: 'runs',
              label: t('SourceStream.runs'),
              children: <RunsPanel key={runRefresh} streamId={stream.id} />,
            },
          ]}
        />
      ) : (
        <Empty description={t('SourceStream.addStream')} />
      )}
      <ComDrawer
        open={editing && canEdit}
        width="min(1000px, 100vw)"
        title={t(stream ? 'SourceStream.configureStream' : 'SourceStream.addStream')}
        closable={!busy}
        extra={null}
        keyboard={!busy}
        maskClosable={!busy}
        onClose={closeEditor}
        styles={{ body: { padding: 24 }, footer: { padding: '16px 24px' } }}
        footer={
          <div className={styles['wizard-footer']}>
            <ComButton
              disabled={!doc || busy}
              onClick={() =>
                void perform(async () => {
                  if (writeDocument) {
                    await sourceStreamApi.validate(writeDocument, 0);
                    message.success(t('SourceStream.validationPassed'));
                  }
                })
              }
            >
              {t('SourceStream.validate')}
            </ComButton>
            <Space>
              <ComButton disabled={busy} onClick={closeEditor}>
                {t('common.cancel')}
              </ComButton>
              <ComButton
                className={styles['manage-button']}
                loading={busy}
                disabled={!doc || (!dirty && !!stream)}
                onClick={() => void perform(save)}
              >
                {t('SourceStream.saveStream')}
              </ComButton>
            </Space>
          </div>
        }
      >
        {failed && <Alert type="error" showIcon message={t('SourceStream.scriptActionFailed')} />}
        {editor}
      </ComDrawer>
      {picker && (
        <SourcePicker
          selectedIds={doc?.sources.map((binding) => binding.sourceId)}
          onSelect={add}
          onClose={() => setPicker(false)}
        />
      )}
    </div>
  );
}
