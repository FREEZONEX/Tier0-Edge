import { useEffect, useState, type FC } from 'react';
import { Alert, App, Empty, Select, Space, Dropdown, Tabs, type MenuProps } from 'antd';
import { Play, Stop, Save, Checkmark, TrashCan, ChevronDown, Settings } from '@carbon/icons-react';
import { json } from '@codemirror/lang-json';
import { v4 as uuid } from 'uuid';
import { ButtonPermission } from '@/common-types/button-permission';
import { hasPermission } from '@/utils/auth';
import ComButton from '@/components/com-button';
import ProTable from '@/components/pro-table';
import ProModal from '@/components/pro-modal';
import ProCodemirror from '@/components/pro-codemirror';
import { openConfirmModal } from '@/components/confirm-modal';
import { usePagination, useTranslate } from '@/hooks';
import {
  sourceStreamApi,
  type Source,
  type Stream,
  type StreamVersion,
  type MetadataTarget,
} from '@/apis/core-api/source-stream';
import StreamConfigEditor, { initialStream, readStreamDocument } from './StreamConfigEditor';
import RunsPanel from './RunsPanel';
import StreamOverview from './StreamOverview';
import { useIntegrationDirty, useIntegrationRoute } from './IntegrationTabs';
import { readProgramDocument } from './program-document';
import styles from './index.module.scss';

export const Versions: FC<{ stream: Stream; source?: Source; onChanged: (value: Stream) => void | Promise<void> }> = ({
  stream,
  source,
  onChanged,
}) => {
  const t = useTranslate();
  const { modal } = App.useApp();
  const [failed, setFailed] = useState(false);
  const { data, loading, pagination } = usePagination<StreamVersion>({
    fetchApi: async ({ pageNo }: { pageNo: number }) => {
      try {
        const r = await sourceStreamApi.versions(stream.id, pageNo);
        setFailed(false);
        return { data: r.list, total: r.total, pageNo };
      } catch (error) {
        setFailed(true);
        throw error;
      }
    },
  });
  const [selected, setSelected] = useState<StreamVersion>();
  return (
    <div className={styles.section}>
      {failed && <Alert type="error" showIcon message={t('SourceStream.loadFailed')} />}
      <ProTable
        loading={loading}
        rowKey="id"
        dataSource={data}
        pagination={pagination}
        columns={[
          { title: t('SourceStream.version'), dataIndex: 'id', width: 200 },
          ...(source
            ? [{ title: t('SourceStream.configurationRevision'), dataIndex: 'sourceRevision', width: 180 }]
            : []),
          {
            title: t('SourceStream.createdAt'),
            dataIndex: 'createdAt',
            width: 180,
            render: (time: number) => new Date(time).toLocaleString(),
          },
        ]}
        onRow={(version: StreamVersion) => ({ onClick: () => setSelected(version) })}
      />
      <ProModal
        open={Boolean(selected)}
        width={720}
        title={t('SourceStream.version')}
        onCancel={() => setSelected(undefined)}
        footer={
          selected && (
            <Space>
              <ComButton onClick={() => setSelected(undefined)}>{t('common.close')}</ComButton>
              <ComButton
                auth={ButtonPermission['uns.streamDeploy']}
                type="primary"
                onClick={() =>
                  openConfirmModal(modal, {
                    title: t('SourceStream.rollback'),
                    content: t('SourceStream.rollbackHint'),
                    okText: t('common.confirm'),
                    cancelText: t('common.cancel'),
                    onOk: async () => {
                      await onChanged(
                        await sourceStreamApi.rollback(
                          stream.id,
                          selected.id,
                          stream.revision,
                          source?.configRevision || 0
                        )
                      );
                      setSelected(undefined);
                    },
                  })
                }
              >
                {t('SourceStream.rollback')}
              </ComButton>
            </Space>
          )
        }
      >
        {selected && <pre className={styles.code}>{JSON.stringify(selected, null, 2)}</pre>}
      </ProModal>
    </div>
  );
};

const LegacyStreamDetail: FC<{
  source: Source;
  stream?: Stream;
  initialTarget?: MetadataTarget;
  onChanged: (stream?: Stream) => void | Promise<void>;
}> = ({ source, stream, onChanged, initialTarget }) => {
  const t = useTranslate();
  const { modal, message } = App.useApp();
  const { tab, topicId, go } = useIntegrationRoute();
  const initial = JSON.stringify(
    stream?.document || { ...initialStream(source, topicId), metadataTarget: initialTarget },
    null,
    2
  );
  const [raw, setRaw] = useState(initial);
  const [managing, setManaging] = useState(false);
  const [testPhase, setTestPhase] = useState<'input' | 'request' | 'response'>();
  const [testInput, setTestInput] = useState('{}');
  const [testResourceRef, setTestResourceRef] = useState('');
  const [testResult, setTestResult] = useState<unknown>();
  const [testFailed, setTestFailed] = useState(false);
  const [testing, setTesting] = useState(false);
  const [invokeOpen, setInvokeOpen] = useState(false);
  const [invokeInput, setInvokeInput] = useState('{}');
  const [invokeKey, setInvokeKey] = useState<string>(() => uuid());
  const [invokeResult, setInvokeResult] = useState<unknown>();
  const doc = readStreamDocument(raw, stream ? source : undefined);
  const writeDocument = readProgramDocument(raw);
  const dirty = raw !== initial;
  useIntegrationDirty(`stream-${stream?.id || `new-${source.id}`}`, dirty);
  const editing = !stream || tab === 'editor';
  const canEdit = hasPermission(ButtonPermission['uns.streamManage']) && !!writeDocument;
  useEffect(() => {
    setRaw(initial);
  }, [stream?.id, stream?.revision, source.id]);
  useEffect(() => {
    const warn = (e: BeforeUnloadEvent) => {
      if (dirty) {
        e.preventDefault();
        e.returnValue = '';
      }
    };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [dirty]);
  const save = async () => {
    if (!writeDocument) return;
    const saved = stream
      ? await sourceStreamApi.updateStream(stream.id, writeDocument, { expectedRevision: stream.revision })
      : await sourceStreamApi.createStream(writeDocument);
    await onChanged(saved);
    message.success(t('SourceStream.saved'));
  };
  const deploy = () =>
    stream &&
    openConfirmModal(modal, {
      title: t('SourceStream.deploy'),
      content: t('SourceStream.deployHint'),
      okText: t('common.confirm'),
      cancelText: t('common.cancel'),
      onOk: async () => {
        await onChanged(await sourceStreamApi.deploy(stream.id, stream.revision, source.configRevision));
        message.success(t('SourceStream.deployed'));
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
  const validate = async () => {
    if (writeDocument) {
      await sourceStreamApi.validate(writeDocument, source.configRevision);
      message.success(t('SourceStream.validationPassed'));
    }
  };
  const parseInput = (value: string) => {
    try {
      JSON.parse(value);
      return true;
    } catch {
      return false;
    }
  };
  const test = async () => {
    if (!writeDocument || !testPhase) return;
    setTesting(true);
    setTestResult(undefined);
    setTestFailed(false);
    try {
      const input = JSON.parse(testInput);
      setTestResult(await sourceStreamApi.test(writeDocument, { data: input }, source.configRevision, testPhase));
    } catch {
      setTestFailed(true);
    } finally {
      setTesting(false);
    }
  };
  const editor = (
    <div className={styles.section}>
      {dirty && <Alert type="warning" showIcon message={t('SourceStream.unsaved')} />}
      {doc && doc.sourceId !== source.id && (
        <Alert type="error" showIcon message={t('SourceStream.crossSourceForbidden')} />
      )}
      <StreamConfigEditor value={raw} onChange={setRaw} source={source} creating={!stream} readOnly={!canEdit} />
    </div>
  );
  const manageAction = async (action: () => Promise<void>) => {
    setManaging(true);
    try {
      await action();
    } catch {
      /* API adapter 已显示错误。 */
    } finally {
      setManaging(false);
    }
  };
  const openTest = (phase: 'input' | 'request' | 'response') => {
    setTestPhase(phase);
    setTestResourceRef(doc?.nativeResourceRefs?.[0] || doc?.nativeResourceRef || '');
    setTestResult(undefined);
    setTestFailed(false);
  };
  const manageItems: MenuProps['items'] = [
    ...(canEdit
      ? [
          ...(stream && !editing
            ? [
                {
                  key: 'edit',
                  label: t('SourceStream.editMapping'),
                  icon: <Settings />,
                  onClick: () => go({ view: 'stream', sourceId: source.id, streamId: stream.id, tab: 'editor' }),
                },
              ]
            : []),
          ...(editing
            ? [
                {
                  key: 'validate',
                  label: t('SourceStream.validate'),
                  icon: <Checkmark />,
                  disabled: !writeDocument,
                  onClick: () => {
                    void manageAction(validate);
                  },
                },
                {
                  key: 'save',
                  label: t('SourceStream.saveDraft'),
                  icon: <Save />,
                  disabled: !writeDocument || (Boolean(stream) && !dirty),
                  onClick: () => {
                    void manageAction(save);
                  },
                },
              ]
            : []),
          {
            key: 'test',
            label: t('SourceStream.testRun'),
            disabled: !writeDocument,
            onClick: () => openTest(doc?.model === 'request-reply' ? 'request' : 'input'),
          },
          ...(doc?.model === 'request-reply'
            ? [{ key: 'test-response', label: t('SourceStream.testResponse'), onClick: () => openTest('response') }]
            : []),
        ]
      : []),
    ...(stream
      ? [
          ...(hasPermission(ButtonPermission['uns.streamDeploy'])
            ? [
                { key: 'deploy', label: t('SourceStream.deploy'), icon: <Play />, disabled: dirty, onClick: deploy },
                {
                  key: 'disable',
                  label: t('SourceStream.disableStream'),
                  icon: <Stop />,
                  disabled: stream.status !== 'enabled',
                  onClick: disable,
                },
              ]
            : []),
          ...(stream.model === 'request-reply' && hasPermission(ButtonPermission['uns.streamExecute'])
            ? [
                {
                  key: 'invoke',
                  label: t('SourceStream.invoke'),
                  disabled: stream.status !== 'enabled' || stream.executionState !== 'ready' || dirty,
                  onClick: () => {
                    setInvokeKey(uuid());
                    setInvokeResult(undefined);
                    setInvokeOpen(true);
                  },
                },
              ]
            : []),
          ...(canEdit
            ? [
                { type: 'divider' as const },
                {
                  key: 'delete',
                  label: t('SourceStream.deleteStream'),
                  icon: <TrashCan />,
                  danger: true,
                  disabled: dirty,
                  onClick: remove,
                },
              ]
            : []),
        ]
      : []),
  ];
  return (
    <div className={styles.section}>
      <div className={styles.row}>
        <div>
          <div className={styles.muted}>
            {t('SourceStream.stream')} / {source.name}
          </div>
          <h2>{stream?.name || t('SourceStream.addStream')}</h2>
        </div>
        {manageItems.length > 0 && (
          <Dropdown menu={{ items: manageItems }} trigger={['click']} placement="bottomRight" disabled={managing}>
            <ComButton className={styles['manage-button']} loading={managing}>
              {t('SourceStream.manage')} <ChevronDown size={16} />
            </ComButton>
          </Dropdown>
        )}
      </div>
      {stream?.executionState === 'needs-redeploy' ? (
        <Alert type="warning" showIcon message={t('SourceStream.execution.needs-redeploy')} />
      ) : source.health !== 'healthy' ? (
        <Alert type="warning" showIcon message={t('SourceStream.sourceNotReady')} />
      ) : null}
      {stream ? (
        <Tabs
          activeKey={tab === 'versions' || tab === 'runs' ? tab : 'overview'}
          onChange={(next) => go({ view: 'stream', sourceId: source.id, streamId: stream.id, tab: next })}
          items={[
            {
              key: 'overview',
              label: t('common.detail'),
              children: editing ? editor : <StreamOverview source={source} stream={stream} />,
            },
            {
              key: 'versions',
              label: t('SourceStream.versions'),
              children: <Versions key={stream.revision} stream={stream} source={source} onChanged={onChanged} />,
            },
            { key: 'runs', label: t('SourceStream.runs'), children: <RunsPanel streamId={stream.id} /> },
          ]}
        />
      ) : (
        editor
      )}
      <ProModal
        open={Boolean(testPhase)}
        footer={undefined}
        confirmLoading={testing}
        width={800}
        title={t('SourceStream.testRun')}
        okText={t('SourceStream.testRun')}
        cancelText={t('common.close')}
        onCancel={() => setTestPhase(undefined)}
        onOk={test}
        okButtonProps={{
          disabled: !parseInput(testInput) || !doc || Boolean(doc.nativeResourceRefs?.length && !testResourceRef),
        }}
      >
        <div className={styles.section}>
          <Alert type="info" showIcon message={t('SourceStream.dryRunHint')} />
          {doc?.model === 'publish' && (
            <ComButton
              loading={testing}
              auth={ButtonPermission['uns.streamManage']}
              onClick={async () => {
                setTesting(true);
                setTestFailed(false);
                try {
                  const { input } = await sourceStreamApi.sample(doc, source.configRevision);
                  setTestInput(JSON.stringify(input.data, null, 2));
                  setTestResourceRef(input.resourceRef);
                  setTestResult(undefined);
                } catch {
                  setTestFailed(true);
                } finally {
                  setTesting(false);
                }
              }}
            >
              {t('SourceStream.fetchSample')}
            </ComButton>
          )}
          {Boolean(doc?.nativeResourceRefs?.length) && (
            <Select
              aria-label={t('SourceStream.nativeResource')}
              showSearch
              value={testResourceRef || undefined}
              onChange={setTestResourceRef}
              options={Array.from(new Set([...(doc?.nativeResourceRefs || []), testResourceRef].filter(Boolean))).map(
                (value) => ({
                  value,
                  label: source.resources.find((item) => item.id === value)?.name || value,
                })
              )}
            />
          )}
          <ProCodemirror
            value={testInput}
            onChange={setTestInput}
            extensions={[json()]}
            height="200px"
            showHint={false}
          />
          {!parseInput(testInput) && <Alert type="error" showIcon message={t('SourceStream.invalidJson')} />}
          {testFailed && <Alert type="error" showIcon message={t('SourceStream.testFailed')} />}
          {testResult !== undefined && <pre className={styles.code}>{JSON.stringify(testResult, null, 2)}</pre>}
        </div>
      </ProModal>
      <ProModal
        open={invokeOpen}
        footer={undefined}
        title={t('SourceStream.invoke')}
        width={760}
        onCancel={() => setInvokeOpen(false)}
        okText={t('SourceStream.invoke')}
        cancelText={t('common.close')}
        okButtonProps={{ disabled: !parseInput(invokeInput) || invokeResult !== undefined }}
        onOk={() =>
          openConfirmModal(modal, {
            title: t('SourceStream.invoke'),
            content: t('SourceStream.invokeWarning'),
            okText: t('common.confirm'),
            cancelText: t('common.cancel'),
            onOk: async () => {
              if (!stream) return;
              try {
                setInvokeResult(
                  await sourceStreamApi.invoke(stream.id, stream.activeVersion, JSON.parse(invokeInput), invokeKey)
                );
              } catch {
                setInvokeResult({ status: t('SourceStream.invokeFailed'), idempotencyKey: invokeKey });
              }
            },
          })
        }
      >
        <div className={styles.section}>
          <Alert type="warning" showIcon message={t('SourceStream.invokeWarning')} />
          <span>
            {t('SourceStream.idempotencyKey')}: {invokeKey}
          </span>
          <ProCodemirror
            value={invokeInput}
            onChange={setInvokeInput}
            extensions={[json()]}
            height="200px"
            showHint={false}
          />
          {invokeResult !== undefined && <pre className={styles.code}>{JSON.stringify(invokeResult, null, 2)}</pre>}
        </div>
      </ProModal>
      {!canEdit && !stream && <Empty description={t('SourceStream.forbidden')} />}
    </div>
  );
};

export default LegacyStreamDetail;
