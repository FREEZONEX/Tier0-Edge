import { useCapabilityReadiness, CapabilityReadinessNotice } from './useCapabilityReadiness';
import { useSourceStreamApi, useSourceStreamScope } from '@/apis/core-api/source-stream-scope';
import { useEffect, useRef, useState, type FC } from 'react';
import { Alert, App, Dropdown, Space, Tabs, Tag, type MenuProps } from 'antd';
import { ChevronDown, ArrowLeft, Edit, Renew } from '@carbon/icons-react';
import { isProgramDocument, type Source, type Stream, type ProgramManifest } from '@/apis/core-api/source-stream';
import { ButtonPermission } from '@/common-types/button-permission';
import { hasPermission } from '@/utils/auth';
import { useTranslate } from '@/hooks';
import ComButton from '@/components/com-button';
import { openConfirmModal } from '@/components/confirm-modal';
import ProgramExecutionModal from './ProgramExecutionModal';
import ProgramDraftModal from './ProgramDraftModal';
import ProgramVersions from './ProgramVersions';
import RunsPanel from './RunsPanel';
import StreamReadme from './StreamReadme';
import { useIntegrationRoute } from './IntegrationTabs';
import { useStreamRuntimeStatus } from './useStreamRuntimeStatus';
export { Versions } from './LegacyStreamDetail';
import styles from './index.module.scss';

const StreamDetail: FC<{
  source?: Source;
  stream?: Stream;
  initialTopicName?: string;
  onChanged: (stream?: Stream) => void;
}> = ({ source, stream, initialTopicName, onChanged }) => {
  const sourceStreamApi = useSourceStreamApi();
  const capability = useCapabilityReadiness('stream');
  const remoteReadOnly = useSourceStreamScope().readOnly;
  const t = useTranslate();
  const { modal, message } = App.useApp();
  const { tab, topicId, metaId, create, edit, go } = useIntegrationRoute();
  const document = stream && isProgramDocument(stream.document) ? stream.document : undefined;
  const legacy = !!stream && !document;
  const canManage = !remoteReadOnly && hasPermission(ButtonPermission['uns.streamManage']) && !legacy;
  const canExecute = !remoteReadOnly && hasPermission(ButtonPermission['uns.streamExecute']) && !legacy;
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<'load' | 'validate'>();
  const [validated, setValidated] = useState<{ revision: number; manifest: ProgramManifest }>();
  const [execution, setExecution] = useState<'test' | 'invoke'>();
  const current = useRef({ id: stream?.id, revision: stream?.revision, canManage });
  current.current = { id: stream?.id, revision: stream?.revision, canManage };
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  useEffect(() => {
    setValidated(undefined);
    setExecution(undefined);
    setFailure(undefined);
  }, [stream?.id, stream?.revision, canManage, canExecute]);
  const runtime = useStreamRuntimeStatus(stream, hasPermission(ButtonPermission['uns.streams']) && !busy, onChanged);
  const manifest = validated?.revision === stream?.revision ? validated?.manifest : stream?.draftManifest;
  const refresh = async () => {
    if (!stream || busy) return;
    const id = stream.id;
    setBusy(true);
    setFailure(undefined);
    try {
      const latest = await sourceStreamApi.stream(id);
      if (mounted.current && current.current.id === id) onChanged(latest);
    } catch {
      if (mounted.current && current.current.id === id) setFailure('load');
    } finally {
      if (mounted.current) setBusy(false);
    }
  };
  const validate = async (test = false) => {
    if (!document || !stream || !canManage || busy || capability.blocked('validate')) return;
    const snapshot = { id: stream.id, revision: stream.revision };
    setBusy(true);
    setFailure(undefined);
    try {
      // 使用保存的 JS；不从旧的可视化步骤重新编译并覆盖 Agent 的修改。
      const result = await sourceStreamApi.validate(document, 0);
      if (
        !mounted.current ||
        current.current.id !== snapshot.id ||
        current.current.revision !== snapshot.revision ||
        !current.current.canManage
      )
        return;
      if (!result.manifest) {
        message.warning(t('SourceStream.manifestUnavailable'));
        return;
      }
      setValidated({ revision: snapshot.revision, manifest: result.manifest });
      if (test) setExecution('test');
      else message.success(t('SourceStream.validationPassed'));
    } catch {
      if (mounted.current && current.current.id === snapshot.id && current.current.revision === snapshot.revision)
        setFailure('validate');
    } finally {
      if (mounted.current) setBusy(false);
    }
  };
  const deploy = () =>
    stream &&
    openConfirmModal(modal, {
      title: t('SourceStream.publish'),
      content: t('SourceStream.deployHint'),
      okText: t('common.confirm'),
      cancelText: t('common.cancel'),
      onOk: async () => {
        const result = await sourceStreamApi.deploy(stream.id, stream.revision, 0);
        onChanged(result);
        message.info(t(result.pendingVersion ? 'SourceStream.deployPending' : 'SourceStream.deployed'));
      },
    });
  const disable = () =>
    stream &&
    openConfirmModal(modal, {
      title: t('SourceStream.disableStream'),
      content: t('SourceStream.disableHint'),
      okText: t('common.confirm'),
      cancelText: t('common.cancel'),
      onOk: async () => onChanged(await sourceStreamApi.disable(stream.id, { expectedRevision: stream.revision })),
    });
  const remove = () =>
    stream &&
    openConfirmModal(modal, {
      title: t('SourceStream.deleteStream'),
      content: t('SourceStream.deleteStreamHint'),
      danger: true,
      okText: t('common.confirm'),
      cancelText: t('common.cancel'),
      onOk: async () => {
        await sourceStreamApi.deleteStream(stream.id, { expectedRevision: stream.revision });
        onChanged();
      },
    });
  const state = stream?.executionState ?? stream?.status ?? 'draft';
  const manageItems: MenuProps['items'] = [
    ...(document && canManage
      ? [
          {
            key: 'validate',
            label: t('SourceStream.validate'),
            disabled: busy || capability.blocked('validate'),
            onClick: () => validate(),
          },
        ]
      : []),
    ...(stream && !legacy && !remoteReadOnly && hasPermission(ButtonPermission['uns.streamDeploy'])
      ? [
          {
            key: 'deploy',
            label: t('SourceStream.publish'),
            disabled: busy || capability.blocked('deploy') || !!stream.pendingVersion,
            onClick: deploy,
          },
        ]
      : []),
    ...(stream && canManage
      ? [
          {
            key: 'disable',
            label: t('SourceStream.disableStream'),
            disabled: busy || capability.blocked('disable') || (stream.status !== 'enabled' && !stream.pendingVersion),
            onClick: disable,
          },
        ]
      : []),
    ...(stream && canExecute && stream.manifest?.entries.some((entry) => entry.kind === 'request')
      ? [
          {
            key: 'invoke',
            label: t('SourceStream.invoke'),
            disabled:
              busy ||
              capability.blocked('execute') ||
              stream.status !== 'enabled' ||
              stream.executionState !== 'ready' ||
              !!runtime.problem,
            onClick: () => setExecution('invoke'),
          },
        ]
      : []),
    ...(stream && canManage
      ? [
          {
            key: 'delete',
            label: t('SourceStream.deleteStream'),
            danger: true,
            disabled: busy,
            onClick: remove,
          },
        ]
      : []),
  ];
  const overview = legacy ? (
    <pre className={styles.code}>{JSON.stringify(stream?.document, null, 2)}</pre>
  ) : (
    <StreamReadme
      document={document}
      manifest={manifest}
      canManage={canManage}
      initialSourceName={source?.name}
      initialTopicName={initialTopicName}
    />
  );
  return (
    <div className={styles['stream-editor']}>
      <div className={styles['stream-editor-actions']}>
        <Space wrap>
          <ComButton
            type="text"
            icon={<ArrowLeft />}
            onClick={() => go({ view: 'stream', topicId: topicId || undefined })}
          >
            {t('StreamRegistry.allStreams')}
          </ComButton>
          {stream && (
            <Tag>{t(stream.executionState ? 'SourceStream.execution.' + state : 'SourceStream.status.' + state)}</Tag>
          )}
        </Space>
        <Space wrap>
          {canManage && (
            <ComButton
              icon={<Edit />}
              disabled={busy}
              onClick={() =>
                go({
                  view: 'stream',
                  streamId: stream?.id,
                  sourceId: source?.id,
                  create: stream ? undefined : 'stream',
                  topicId: topicId || undefined,
                  edit: 'stream',
                })
              }
            >
              {t('SourceStream.editProgram')}
            </ComButton>
          )}
          {stream && (
            <ComButton icon={<Renew />} aria-label={t('SourceStream.refresh')} loading={busy} onClick={refresh} />
          )}
          {document && canManage && (
            <ComButton loading={busy} disabled={capability.blocked('validate')} onClick={() => validate(true)}>
              {t('SourceStream.testRun')}
            </ComButton>
          )}
          {manageItems.length > 0 && (
            <Dropdown menu={{ items: manageItems }} trigger={['click']} placement="bottomRight">
              <ComButton>
                {t('SourceStream.manage')} <ChevronDown size={16} />
              </ComButton>
            </Dropdown>
          )}
        </Space>
      </div>
      <CapabilityReadinessNotice value={capability} />
      {legacy && <Alert type="info" showIcon message={t('SourceStream.legacyReadOnly')} />}
      {!!stream?.pendingVersion && <Alert type="info" showIcon message={t('SourceStream.deployPending')} />}
      {runtime.problem && (
        <Alert
          type="warning"
          showIcon
          message={t('SourceStream.runtimeRefresh.' + runtime.problem)}
          action={
            <ComButton onClick={refresh} disabled={busy}>
              {t('SourceStream.refresh')}
            </ComButton>
          }
        />
      )}
      {['starting', 'source-watch-failed', 'needs-redeploy', 'source-disabled', 'source-unhealthy'].includes(state) && (
        <Alert
          type={state === 'starting' ? 'info' : 'warning'}
          showIcon
          message={t('SourceStream.execution.' + state)}
        />
      )}
      {failure && (
        <Alert
          type="error"
          showIcon
          message={t(failure === 'load' ? 'SourceStream.loadFailed' : 'SourceStream.programCheckFailed')}
        />
      )}
      {stream ? (
        <Tabs
          className={styles['stream-editor-tabs']}
          activeKey={['versions', 'runs'].includes(tab) ? tab : 'overview'}
          onChange={(next) => go({ view: 'stream', streamId: stream.id, topicId: topicId || undefined, tab: next })}
          items={[
            { key: 'overview', label: t('common.detail'), children: overview },
            {
              key: 'versions',
              label: t('SourceStream.versions'),
              children: (
                <ProgramVersions
                  key={stream.revision}
                  stream={stream}
                  readOnly={legacy || busy}
                  onChanged={onChanged}
                />
              ),
            },
            { key: 'runs', label: t('SourceStream.runs'), children: <RunsPanel streamId={stream.id} /> },
          ]}
        />
      ) : (
        overview
      )}
      {canManage && (edit === 'stream' || (!stream && create === 'stream')) && (
        <ProgramDraftModal
          key={stream?.id ?? 'new'}
          stream={stream}
          initialSource={source}
          initialTopic={metaId ? `topic:${metaId}` : undefined}
          canManage={canManage}
          capability={capability}
          onClose={() => go({ view: 'stream', streamId: stream?.id, topicId: topicId || undefined, tab })}
          onSaved={(saved) => {
            onChanged(saved);
            go({ view: 'stream', streamId: saved.id, topicId: topicId || undefined, tab }, true);
          }}
        />
      )}
      <ProgramExecutionModal
        mode={execution}
        document={
          execution === 'invoke' && stream?.activeDocument && isProgramDocument(stream.activeDocument)
            ? stream.activeDocument
            : document
        }
        stream={stream}
        manifest={execution === 'invoke' ? stream?.manifest : manifest}
        onClose={() => setExecution(undefined)}
      />
    </div>
  );
};
export default StreamDetail;
