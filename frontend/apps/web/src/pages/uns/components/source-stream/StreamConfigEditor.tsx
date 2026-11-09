import { useMemo, useState, type FC, type ReactNode } from 'react';
import { Alert, Input, InputNumber, Segmented, Select, Space } from 'antd';
import { json } from '@codemirror/lang-json';
import { javascript } from '@codemirror/lang-javascript';
import ProCodemirror from '@/components/pro-codemirror';
import HelpTooltip from '@/components/help-tooltip';
import ComButton from '@/components/com-button';
import SearchSelect from '../use-create-modal/components/SearchSelect';
import { useTranslate } from '@/hooks';
import { MAX_LENGTHS } from '@/utils/limits';
import type { AccessModel, Source, StreamDocument } from '@/apis/core-api/source-stream';
import { useIntegrationRoute } from './IntegrationTabs';
import styles from './index.module.scss';
import StreamCollectionEditor, { initialCollection } from './StreamCollectionEditor';
import TargetEditor from '../metadata/target-editor';

export const identityScript = 'return { data: input, drop: false, logs: [] };';
export function initialStream(source: Source, topicId?: number): StreamDocument {
  return {
    name: '',
    description: '',
    sourceId: source.id,
    collection: initialCollection(source.protocol),
    model: 'publish',
    nativeResourceRef: '',
    topicIds: topicId ? [topicId] : [],
    script: { code: identityScript, timeoutMs: 1000 },
  };
}
export function readStreamDocument(raw: string, legacySource?: Source): StreamDocument | undefined {
  try {
    const d = JSON.parse(raw);
    if (
      !d ||
      typeof d !== 'object' ||
      Array.isArray(d) ||
      typeof d.name !== 'string' ||
      (d.description !== undefined && typeof d.description !== 'string')
    )
      return;
    if (
      !Number.isSafeInteger(d.sourceId) ||
      !['publish', 'subscribe', 'request-reply'].includes(d.model) ||
      (d.nativeResourceRef !== undefined && typeof d.nativeResourceRef !== 'string')
    )
      return;
    if (
      !Array.isArray(d.topicIds) ||
      !d.topicIds.every((id: unknown) => typeof id === 'number' && Number.isSafeInteger(id) && id > 0)
    )
      return;
    if (d.program === undefined) {
      const c = d.collection;
      if (c === undefined) {
        // Historic Source-backed documents retain their original wire shape. New
        // drafts never receive this context and must declare Stream collection.
        if (!legacySource || legacySource.id !== d.sourceId) return;
        const source = legacySource.document;
        if (!(
          (source.protocol === 'mqtt' && Array.isArray(source.mqtt?.topicFilters)) ||
          (source.protocol === 'opcua' &&
            Number.isInteger(source.opcua?.samplingIntervalMs) &&
            Number(source.opcua?.samplingIntervalMs) > 0) ||
          (source.protocol === 'modbus-tcp' &&
            Number.isInteger(source.modbus?.pollIntervalMs) &&
            Number(source.modbus?.pollIntervalMs) > 0)
        ))
          return;
      } else if (!c || typeof c !== 'object' || Array.isArray(c) || Object.keys(c).length !== 1) return;
      else if (c.mqtt) {
        if (!Number.isInteger(c.mqtt.qos) || c.mqtt.qos < 0 || c.mqtt.qos > 2) return;
      } else if (c.opcua) {
        if (!Number.isInteger(c.opcua.samplingIntervalMs)) return;
      } else if (c.modbus) {
        if (!Number.isInteger(c.modbus.pollIntervalMs) || !Array.isArray(c.modbus.ranges)) return;
      } else return;
    }
    if (!d.script || typeof d.script.code !== 'string' || typeof d.script.timeoutMs !== 'number') return;
    if (d.nativeResourceRefs !== undefined) {
      if (
        !Array.isArray(d.nativeResourceRefs) ||
        !d.nativeResourceRefs.every((ref: unknown) => typeof ref === 'string' && ref.trim()) ||
        new Set(d.nativeResourceRefs).size !== d.nativeResourceRefs.length
      )
        return;
      if (d.nativeResourceRefs.length && (d.model !== 'publish' || d.nativeResourceRef)) return;
    }
    if (
      d.responseScript &&
      (typeof d.responseScript.code !== 'string' || typeof d.responseScript.timeoutMs !== 'number')
    )
      return;
    if (d.program !== undefined) {
      const p = d.program;
      const ids = (items: unknown) =>
        Array.isArray(items) &&
        items.every((id) => Number.isSafeInteger(id) && id > 0) &&
        new Set(items).size === items.length;
      const refs = (items: unknown) =>
        Array.isArray(items) &&
        items.length <= 1000 &&
        items.every((ref) => typeof ref === 'string' && ref.trim()) &&
        new Set(items).size === items.length;
      if (
        !p ||
        typeof p !== 'object' ||
        d.sourceId !== 0 ||
        d.nativeResourceRef ||
        d.nativeResourceRefs?.length ||
        !Array.isArray(p.sources) ||
        p.sources.length > 64
      )
        return;
      if (
        !p.sources.every(
          (s: { alias?: unknown; sourceId?: unknown; readResources?: unknown }) =>
            s &&
            typeof s.alias === 'string' &&
            /^[A-Za-z_][A-Za-z0-9_]{0,62}$/.test(s.alias) &&
            !['__proto__', 'prototype', 'constructor'].includes(s.alias) &&
            Number.isSafeInteger(s.sourceId) &&
            Number(s.sourceId) > 0 &&
            refs(s.readResources)
        )
      )
        return;
      if (
        new Set(p.sources.map((s: { alias: string }) => s.alias)).size !== p.sources.length ||
        new Set(p.sources.map((s: { sourceId: number }) => s.sourceId)).size !== p.sources.length
      )
        return;
      const trigger = p.trigger;
      if (
        !trigger ||
        !['manual', 'interval', 'source', 'uns'].includes(trigger.kind) ||
        (trigger.resourceRefs !== undefined && !refs(trigger.resourceRefs)) ||
        (trigger.topicIds !== undefined && !ids(trigger.topicIds)) ||
        (trigger.qos !== undefined && ![0, 1, 2].includes(trigger.qos))
      )
        return;
      if (trigger.kind === 'manual' ? d.model !== 'request-reply' : d.model !== 'publish') return;
      if (
        trigger.kind === 'interval'
          ? !Number.isInteger(trigger.intervalMs) || trigger.intervalMs < 1000 || trigger.intervalMs > 86400000
          : Boolean(trigger.intervalMs)
      )
        return;
      if (trigger.kind !== 'source' && (trigger.sourceAlias || trigger.resourceRefs?.length || trigger.qos)) return;
      if (
        trigger.kind === 'source' &&
        trigger.sourceAlias &&
        !p.sources.some((s: { alias: string }) => s.alias === trigger.sourceAlias)
      )
        return;
      if (trigger.kind !== 'uns' && trigger.topicIds?.length) return;
    } else if (d.sourceId <= 0) return;
    return { ...d, description: d.description ?? '', nativeResourceRef: d.nativeResourceRef ?? '' };
  } catch {
    /* Invalid documents remain editable and cannot be submitted. */
  }
}

const StreamConfigEditor: FC<{
  value: string;
  source: Source;
  onChange: (value: string) => void;
  readOnly?: boolean;
  creating?: boolean;
}> = ({ value, source, onChange, readOnly = false, creating = false }) => {
  const t = useTranslate();
  const { go } = useIntegrationRoute();
  const [mode, setMode] = useState('form');
  const [template, setTemplate] = useState('identity');
  const [targetMode, setTargetMode] = useState(() =>
    readStreamDocument(value, creating ? undefined : source)?.topicIds.length ? 'legacy' : 'metadata'
  );
  const doc = useMemo(() => readStreamDocument(value, creating ? undefined : source), [value, source, creating]);
  const change = (patch: Partial<StreamDocument>) => doc && onChange(JSON.stringify({ ...doc, ...patch }, null, 2));
  const field = (key: string, child: ReactNode) => (
    <label className={styles.field}>
      <span>{t(`SourceStream.${key}`)}</span>
      {child}
    </label>
  );
  const templates: Record<string, string> = {
    identity: identityScript,
    opcuaValue:
      'return { data: { value: input.value, time: input.sourceTimestamp, quality: input.statusCode }, drop: false, logs: [] };',
    modbusValue: 'return { data: { value: input.values[0] }, drop: false, logs: [] };',
    filterNull: 'return { data: input, drop: input === null, logs: [] };',
    ...(doc?.model === 'publish' && !doc.metadataTarget
      ? {
          directed: (() => {
            const refs = doc.nativeResourceRefs?.length ? doc.nativeResourceRefs : [doc.nativeResourceRef];
            const targets = Object.fromEntries(
              refs
                .filter(Boolean)
                .map((ref, index) => [ref, doc.topicIds[index]])
                .filter(([, id]) => id)
            );
            const value = source.protocol === 'modbus-tcp' ? 'input.values[0]' : 'input.value';
            return `const targets = ${JSON.stringify(targets, null, 2)};\nconst topicId = targets[context.resourceRef];\nreturn { outputs: topicId ? [{ topicId, data: { value: ${value} } }] : [], drop: false, logs: [] };`;
          })(),
        }
      : {}),
  };
  return (
    <div className={styles.section}>
      <Space>
        <Segmented
          value={mode}
          options={[
            { value: 'form', label: t('SourceStream.form'), disabled: !doc },
            { value: 'json', label: 'JSON' },
          ]}
          onChange={(next) => setMode(String(next))}
        />
        <HelpTooltip title={t('SourceStream.streamEditorHint')} />
      </Space>
      {!doc && <Alert type="error" showIcon message={t('SourceStream.invalidConfiguration')} />}
      {mode === 'json' || !doc ? (
        <ProCodemirror
          value={value}
          onChange={onChange}
          extensions={[json()]}
          readOnly={readOnly}
          height="520px"
          showHint={false}
        />
      ) : (
        <>
          <div className={styles.editor}>
            {field(
              'name',
              <Input
                value={doc.name}
                readOnly={readOnly}
                maxLength={MAX_LENGTHS.name}
                onChange={(e) => change({ name: e.target.value })}
              />
            )}
            {field(
              'source',
              <Space wrap>
                <span>
                  {source.name} · {source.protocol}
                </span>
                <ComButton onClick={() => go({ view: 'source', sourceId: source.id })}>
                  {t('SourceStream.openSource')}
                </ComButton>
              </Space>
            )}
            {field(
              'description',
              <Input.TextArea
                value={doc.description}
                readOnly={readOnly}
                maxLength={MAX_LENGTHS.description}
                onChange={(e) => change({ description: e.target.value })}
              />
            )}
            {field(
              'model',
              <Select
                value={doc.model}
                disabled={readOnly || !creating}
                options={(['publish', 'subscribe', 'request-reply'] as AccessModel[]).map((value) => ({
                  value,
                  label: t(`SourceStream.model.${value}`),
                }))}
                onChange={(model) =>
                  change({
                    model,
                    nativeResourceRef:
                      model === 'request-reply' && source.protocol === 'mqtt' ? 'mqtt-request-reply' : '',
                    nativeResourceRefs: undefined,
                    collection: initialCollection(source.protocol),
                    topicIds: model === 'request-reply' ? doc.topicIds.slice(0, 1) : doc.topicIds,
                    metadataTarget: model === 'publish' ? doc.metadataTarget : undefined,
                    responseScript: model === 'request-reply' ? { code: identityScript, timeoutMs: 1000 } : undefined,
                  })
                }
              />
            )}
            {(doc.model !== 'publish' || targetMode === 'legacy') &&
              field(
                'topics',
                <SearchSelect
                  apiParams={{ type: 2 }}
                  disabled={readOnly}
                  mode={doc.model === 'request-reply' ? undefined : 'multiple'}
                  value={
                    doc.model === 'request-reply'
                      ? doc.topicIds[0]
                        ? String(doc.topicIds[0])
                        : undefined
                      : doc.topicIds.map(String)
                  }
                  onChange={(ids: string | string[] | undefined) =>
                    change({ topicIds: (Array.isArray(ids) ? ids : ids ? [ids] : []).map(Number) })
                  }
                />
              )}
          </div>
          {doc.model === 'publish' && (
            <>
              <Segmented
                value={doc.metadataTarget ? 'metadata' : targetMode}
                disabled={readOnly}
                options={[
                  { value: 'metadata', label: t('UnsMeta.namespace') },
                  { value: 'legacy', label: t('UnsMeta.legacyShort') },
                ]}
                onChange={(value) => {
                  setTargetMode(String(value));
                  change({ topicIds: [], metadataTarget: undefined });
                }}
              />
              {(doc.metadataTarget || targetMode === 'metadata') && (
                <TargetEditor
                  value={doc.metadataTarget}
                  readOnly={readOnly}
                  onChange={(metadataTarget) => change({ metadataTarget, topicIds: [] })}
                />
              )}
            </>
          )}
          <StreamCollectionEditor source={source} document={doc} onChange={change} readOnly={readOnly} />
          <div className={styles.flow}>
            <span>{doc.model === 'publish' ? t('SourceStream.nativeResource') : t('SourceStream.topics')}</span>
            <span aria-hidden>→</span>
            <strong>{t('SourceStream.script')}</strong>
            <span aria-hidden>→</span>
            <span>{doc.model === 'publish' ? t('SourceStream.topics') : t('SourceStream.nativeResource')}</span>
            {doc.model === 'request-reply' && <span>{t('SourceStream.responsePathHint')}</span>}
          </div>
          <Space wrap>
            <strong>{t(doc.model === 'request-reply' ? 'SourceStream.requestScript' : 'SourceStream.script')}</strong>
            <HelpTooltip title={t('SourceStream.scriptContract')} />
            {!readOnly && (
              <>
                <Select
                  aria-label={t('SourceStream.template')}
                  value={template}
                  onChange={setTemplate}
                  options={Object.keys(templates).map((value) => ({
                    value,
                    label: t(`SourceStream.template.${value}`),
                  }))}
                />
                <ComButton
                  disabled={!templates[template]}
                  onClick={() => change({ script: { ...doc.script, code: templates[template] } })}
                >
                  {t('SourceStream.applyTemplate')}
                </ComButton>
              </>
            )}
          </Space>
          <ProCodemirror
            value={doc.script.code}
            onChange={(code) => change({ script: { ...doc.script, code } })}
            extensions={[javascript()]}
            readOnly={readOnly}
            height="240px"
            showHint={false}
          />
          {field(
            'scriptTimeout',
            <InputNumber
              min={1}
              max={5000}
              value={doc.script.timeoutMs}
              disabled={readOnly}
              addonAfter="ms"
              onChange={(timeoutMs) => timeoutMs !== null && change({ script: { ...doc.script, timeoutMs } })}
            />
          )}
          {doc.model === 'request-reply' && (
            <>
              <strong>{t('SourceStream.responseScript')}</strong>
              <ProCodemirror
                value={doc.responseScript?.code || ''}
                onChange={(code) =>
                  change({ responseScript: { timeoutMs: doc.responseScript?.timeoutMs || 1000, code } })
                }
                extensions={[javascript()]}
                readOnly={readOnly}
                height="200px"
                showHint={false}
              />
              {field(
                'scriptTimeout',
                <InputNumber
                  min={1}
                  max={5000}
                  value={doc.responseScript?.timeoutMs || 1000}
                  disabled={readOnly}
                  addonAfter="ms"
                  onChange={(timeoutMs) =>
                    timeoutMs !== null &&
                    change({ responseScript: { code: doc.responseScript?.code || '', timeoutMs } })
                  }
                />
              )}
            </>
          )}
          <Space wrap>
            {doc.topicIds.map((id) => (
              <ComButton key={id} onClick={() => go({ view: 'namespace', namespaceId: id })}>
                {t('SourceStream.openTopic')} · {id}
              </ComButton>
            ))}
          </Space>
        </>
      )}
    </div>
  );
};

export default StreamConfigEditor;
