import { useState, type FC } from 'react';
import { Alert, App, Collapse, Input, InputNumber, Segmented, Select, Tabs } from 'antd';
import { json } from '@codemirror/lang-json';
import { javascript } from '@codemirror/lang-javascript';
import { EditorView } from '@codemirror/view';
import { v4 as uuid } from 'uuid';
import { ButtonPermission } from '@/common-types/button-permission';
import { hasPermission } from '@/utils/auth';
import { type ProgramDocument } from '@/apis/core-api/source-stream';
import ComButton from '@/components/com-button';
import HelpTooltip from '@/components/help-tooltip';
import ProCodemirror from '@/components/pro-codemirror';
import { useTranslate } from '@/hooks';
import { MAX_LENGTHS } from '@/utils/limits';
import { useThemeStore } from '@/stores/theme-store';
import InternalResourceSelect from './InternalResourceSelect';
import {
  appendProgramSnippet,
  entryInsertionSnippet,
  readProgramDocument,
  topicInsertionSnippet,
} from './program-document';
import StreamEditorStep from './StreamEditorStep';
import ProgramSourceDeclaration from './ProgramSourceDeclaration';
import { formatProgramScript } from './program-format';
import { getStreamCodemirrorTheme } from './stream-codemirror-theme';
import { editedProgramDocument } from './program-workflow';
import sdkTypes from './stream-sdk.d.ts?raw';
import styles from './index.module.scss';

const ProgramConfigEditor: FC<{
  value: string;
  onChange: (value: string) => void;
  readOnly?: boolean;
  hideHeader?: boolean;
  editorMode?: 'code' | 'json';
}> = ({ value, onChange, readOnly = false, hideHeader = false, editorMode }) => {
  const t = useTranslate();
  const theme = useThemeStore((state) => state.theme);
  const { message } = App.useApp();
  const [localMode, setMode] = useState('code');
  const mode = editorMode ?? localMode;
  const [topics, setTopics] = useState<string[]>([]);
  const [outputResources, setOutputResources] = useState<string[]>([]);
  const [entryKind, setEntryKind] = useState<'request' | 'interval' | 'uns-event'>('request');
  const doc = readProgramDocument(value);
  const emitDocument = (next: ProgramDocument) =>
    doc && onChange(JSON.stringify(editedProgramDocument(doc, next), null, 2));
  const change = (patch: Partial<ProgramDocument>) => doc && emitDocument({ ...doc, ...patch });
  const changeJSON = (next: string) => {
    const parsed = readProgramDocument(next);
    if (doc && parsed) {
      const edited = editedProgramDocument(doc, parsed);
      onChange(edited === parsed ? next : JSON.stringify(edited, null, 2));
    } else onChange(next);
  };
  const insert = (snippet: string) => {
    if (!doc || readOnly) return;
    emitDocument(appendProgramSnippet(doc, snippet));
  };
  return (
    <div className={styles.section}>
      {!hideHeader && (
        <div className={styles.row}>
          <Segmented
            value={mode}
            onChange={(next) => setMode(String(next))}
            options={[
              { value: 'code', label: t('SourceStream.code'), disabled: !doc },
              { value: 'json', label: 'JSON' },
            ]}
          />
        </div>
      )}
      {!doc && <Alert type="error" showIcon message={t('SourceStream.invalidConfiguration')} />}
      {mode === 'json' || !doc ? (
        <ProCodemirror
          theme={getStreamCodemirrorTheme(theme)}
          value={value}
          onChange={changeJSON}
          extensions={[json()]}
          readOnly={readOnly}
          height="520px"
          showHint={false}
        />
      ) : (
        <>
          {!hideHeader && (
            <label className={styles.field}>
              <span>{t('SourceStream.name')}</span>
              <Input
                value={doc.name}
                count={{ max: MAX_LENGTHS.name, strategy: (value) => Array.from(value).length }}
                readOnly={readOnly}
                onChange={(e) => {
                  if (Array.from(e.target.value).length <= MAX_LENGTHS.name) change({ name: e.target.value });
                }}
              />
            </label>
          )}
          {!readOnly && (
            <StreamEditorStep
              compact={hideHeader}
              number={1}
              title={t('SourceStream.scriptTools')}
              summary={t('SourceStream.scriptToolsSummary')}
            >
              <div>
                <Tabs
                  items={[
                    ...(hasPermission(ButtonPermission['uns.sources'])
                      ? [
                          {
                            key: 'source',
                            label: t('SourceStream.source'),
                            children: <ProgramSourceDeclaration />,
                          },
                        ]
                      : []),
                    {
                      key: 'entry',
                      label: t('SourceStream.trigger'),
                      children: (
                        <div className={styles.section}>
                          <Select
                            aria-label={t('SourceStream.trigger')}
                            value={entryKind}
                            onChange={setEntryKind}
                            options={['request', 'interval', 'uns-event'].map((kind) => ({
                              value: kind,
                              label: t(`SourceStream.entry.${kind}`),
                            }))}
                          />
                          {entryKind === 'uns-event' && (
                            <InternalResourceSelect topicsOnly value={topics} onChange={setTopics} />
                          )}
                          <ComButton
                            disabled={entryKind === 'uns-event' && topics.length === 0}
                            onClick={() => insert(entryInsertionSnippet(entryKind, topics, `${entryKind}-${uuid()}`))}
                          >
                            {t('SourceStream.insertEntry')}
                          </ComButton>
                        </div>
                      ),
                    },
                    {
                      key: 'topic',
                      label: t('SourceStream.output'),
                      children: (
                        <div className={styles.section}>
                          <InternalResourceSelect write value={outputResources} onChange={setOutputResources} />
                          <ComButton
                            disabled={!outputResources.length}
                            onClick={() => insert(topicInsertionSnippet(outputResources, `write-${uuid()}`))}
                          >
                            {t('SourceStream.insertOutputs')}
                          </ComButton>
                        </div>
                      ),
                    },
                  ]}
                />
              </div>
            </StreamEditorStep>
          )}
          <StreamEditorStep
            compact={hideHeader}
            number={2}
            title={t('SourceStream.script')}
            summary="JavaScript"
            defaultOpen
          >
            <div className={styles.row}>
              <span>
                <HelpTooltip title={t('SourceStream.fullProgramHint')} />
                <HelpTooltip title={t('SourceStream.httpSDKHint')} />
              </span>
              <ComButton
                size="small"
                disabled={readOnly}
                onClick={() => {
                  const formatted = formatProgramScript(doc.script);
                  if (!formatted.valid) {
                    message.warning(t('SourceStream.formatFailed'));
                    return;
                  }
                  change({ script: formatted.code });
                }}
              >
                {t('SourceStream.formatCode')}
              </ComButton>
            </div>
            <ProCodemirror
              theme={getStreamCodemirrorTheme(theme)}
              value={doc.script}
              onChange={(script) => change({ script })}
              extensions={[javascript(), EditorView.lineWrapping]}
              readOnly={readOnly}
              height="280px"
              showHint={false}
            />
            <Collapse
              ghost
              className={styles['config-advanced']}
              items={[
                {
                  key: 'advanced',
                  label: t('SourceStream.advanced'),
                  children: (
                    <div className={styles.section}>
                      <label className={styles.field}>
                        <span>{t('SourceStream.scriptTimeout')}</span>
                        <InputNumber
                          min={1}
                          max={5000}
                          addonAfter="ms"
                          value={doc.timeoutMs}
                          disabled={readOnly}
                          onChange={(timeoutMs) => timeoutMs !== null && change({ timeoutMs })}
                        />
                      </label>
                      <label className={styles.field}>
                        <span>{t('SourceStream.description')}</span>
                        <Input.TextArea
                          value={doc.description}
                          readOnly={readOnly}
                          maxLength={MAX_LENGTHS.description}
                          onChange={(e) => change({ description: e.target.value })}
                        />
                      </label>
                    </div>
                  ),
                },
              ]}
            />
          </StreamEditorStep>
        </>
      )}
      <details>
        <summary>{t('SourceStream.sdkReference')}</summary>
        <HelpTooltip title={t('SourceStream.sdkBoundary')} />
        <pre className={styles.code}>{sdkTypes}</pre>
      </details>
    </div>
  );
};
export default ProgramConfigEditor;
