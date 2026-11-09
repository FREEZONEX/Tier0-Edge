import type { useCapabilityReadiness } from './useCapabilityReadiness';
import { useSourceStreamApi } from '@/apis/core-api/source-stream-scope';
import { useEffect, useMemo, useRef, useState, type FC } from 'react';
import { Alert, App, Space } from 'antd';
import { type ProgramManifest, type Source, type Stream } from '@/apis/core-api/source-stream';
import { ButtonPermission } from '@/common-types/button-permission';
import { hasPermission } from '@/utils/auth';
import { MAX_LENGTHS } from '@/utils/limits';
import { useTranslate } from '@/hooks';
import ComButton from '@/components/com-button';
import ProModal from '@/components/pro-modal';
import ProgramConfigEditor from './ProgramConfigEditor';
import ProgramExecutionModal from './ProgramExecutionModal';
import ProgramManifestView from './ProgramManifestView';
import { initialProgram, readProgramDocument } from './program-document';
import { readmeManifest } from './stream-readme';
import { useIntegrationDirty } from './IntegrationTabs';
import styles from './index.module.scss';

const ProgramDraftModal: FC<{
  stream?: Stream;
  initialSource?: Source;
  initialTopic?: string;
  canManage: boolean;
  capability?: ReturnType<typeof useCapabilityReadiness>;
  onSaved: (stream: Stream) => void;
  onClose: () => void;
}> = ({ stream, initialSource, initialTopic, canManage, capability, onSaved, onClose }) => {
  const sourceStreamApi = useSourceStreamApi();
  const t = useTranslate();
  const { message } = App.useApp();
  // 冻结打开时的版本；后台刷新不能使旧草稿悄悄获得新 revision 的覆盖权。
  const [baseline] = useState(() => ({
    id: stream?.id,
    revision: stream?.revision ?? 0,
    raw: JSON.stringify(stream?.document ?? initialProgram(initialSource, initialTopic), null, 2),
  }));
  const [raw, setRaw] = useState(baseline.raw);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<'save' | 'validate' | 'conflict'>();
  const [validated, setValidated] = useState<{ raw: string; manifest: ProgramManifest }>();
  const [testing, setTesting] = useState(false);
  const document = useMemo(() => readProgramDocument(raw), [raw]);
  const dirty = raw !== baseline.raw;
  const markSaved = useIntegrationDirty(`program-draft-${baseline.id ?? 'new'}`, dirty);
  const manifest = validated?.raw === raw ? validated.manifest : undefined;
  const allowed = canManage && hasPermission(ButtonPermission['uns.streamManage']);
  const live = useRef({ raw, allowed, mounted: true, running: false });
  live.current.raw = raw;
  live.current.allowed = allowed;
  useEffect(() => {
    live.current.mounted = true;
    return () => {
      live.current.mounted = false;
    };
  }, []);
  useEffect(() => {
    if (!dirty) return;
    const guard = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = '';
    };
    window.addEventListener('beforeunload', guard);
    return () => window.removeEventListener('beforeunload', guard);
  }, [dirty]);
  const valid =
    !!document &&
    !!document.name.trim() &&
    Array.from(document.name.trim()).length <= MAX_LENGTHS.name &&
    Array.from(document.description ?? '').length <= MAX_LENGTHS.description;
  const perform = async (action: 'save' | 'validate' | 'test') => {
    if (
      !document ||
      !valid ||
      !live.current.allowed ||
      live.current.running ||
      capability?.blocked(action === 'save' ? 'configure' : 'validate')
    )
      return;
    const snapshot = raw;
    live.current.running = true;
    setBusy(true);
    setFailed(undefined);
    try {
      if (action === 'save') {
        const value = { ...document, name: document.name.trim() };
        const saved = baseline.id
          ? await sourceStreamApi.updateStream(baseline.id, value, { expectedRevision: baseline.revision })
          : await sourceStreamApi.createStream(value);
        if (!live.current.mounted || !live.current.allowed || live.current.raw !== snapshot) return;
        markSaved();
        message.success(t('SourceStream.saved'));
        onSaved(saved);
      } else {
        const result = await sourceStreamApi.validate(document, 0);
        if (!live.current.mounted || !live.current.allowed || live.current.raw !== snapshot) return;
        const registered = readmeManifest(result.manifest);
        if (!registered) {
          message.warning(t('SourceStream.manifestUnavailable'));
          return;
        }
        setValidated({ raw: snapshot, manifest: registered });
        if (action === 'test') setTesting(true);
        else message.success(t('SourceStream.validationPassed'));
      }
    } catch (error) {
      if (live.current.mounted && live.current.allowed && live.current.raw === snapshot) {
        const conflict = typeof error === 'object' && error !== null && 'code' in error && error.code === 409;
        setFailed(action === 'save' ? (conflict ? 'conflict' : 'save') : 'validate');
      }
    } finally {
      live.current.running = false;
      if (live.current.mounted) setBusy(false);
    }
  };
  return (
    <>
      <ProModal
        open
        className={styles['editor-modal']}
        size="lg"
        title={t(baseline.id ? 'SourceStream.editProgram' : 'SourceStream.addStream')}
        maskClosable={!busy}
        keyboard={!busy}
        closable={!busy}
        onCancel={() => !busy && onClose()}
        footer={
          <Space wrap>
            <ComButton disabled={busy} onClick={onClose}>
              {t('common.cancel')}
            </ComButton>
            <ComButton
              disabled={!allowed || !valid || busy || capability?.blocked('validate')}
              onClick={() => perform('validate')}
            >
              {t('SourceStream.validate')}
            </ComButton>
            <ComButton
              disabled={!allowed || !valid || busy || capability?.blocked('validate')}
              onClick={() => perform('test')}
            >
              {t('SourceStream.testRun')}
            </ComButton>
            <ComButton
              type="primary"
              disabled={!allowed || !valid || busy || capability?.blocked('configure')}
              onClick={() => perform('save')}
            >
              {t('SourceStream.saveDraft')}
            </ComButton>
          </Space>
        }
      >
        <div className={styles.section}>
          <Alert type="info" showIcon message={t('SourceStream.deployHint')} />
          {!allowed && <Alert type="warning" showIcon message={t('SourceStream.forbidden')} />}
          {failed && (
            <Alert
              type="error"
              showIcon
              message={t(
                failed === 'conflict'
                  ? 'SourceStream.versionConflict'
                  : failed === 'save'
                    ? 'SourceStream.programSaveFailed'
                    : 'SourceStream.programCheckFailed'
              )}
            />
          )}
          <ProgramConfigEditor
            value={raw}
            onChange={(value) => {
              if (live.current.running || !live.current.allowed) return;
              setRaw(value);
              setFailed(undefined);
              setTesting(false);
            }}
            readOnly={!allowed || busy}
          />
          {manifest && <ProgramManifestView manifest={manifest} label={t('SourceStream.draftManifest')} compact />}
        </div>
      </ProModal>
      <ProgramExecutionModal
        mode={testing && allowed ? 'test' : undefined}
        document={document}
        manifest={manifest}
        onClose={() => setTesting(false)}
      />
    </>
  );
};

export default ProgramDraftModal;
