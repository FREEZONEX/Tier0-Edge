import { useCapabilityReadiness, CapabilityReadinessNotice } from './useCapabilityReadiness';
import { useSourceStreamApi, useSourceStreamScope } from '@/apis/core-api/source-stream-scope';
import { useEffect, useState, type FC } from 'react';
import { isEqual, omit } from 'lodash-es';
import { Alert, App, Collapse, Descriptions, Space, Dropdown, Input, Tag, type MenuProps } from 'antd';
import { Add, ArrowLeft, TrashCan, Save, Settings, ChevronDown } from '@carbon/icons-react';
import { ButtonPermission } from '@/common-types/button-permission';
import { hasPermission } from '@/utils/auth';
import ComButton from '@/components/com-button';
import ComDrawer from '@/components/com-drawer';
import ProTable from '@/components/pro-table';
import HelpTooltip from '@/components/help-tooltip';
import { openConfirmModal } from '@/components/confirm-modal';
import { usePagination, useTranslate } from '@/hooks';
import {
  isProgramDocument,
  type Source,
  type SourceImpact,
  type SourceTypeDefinition,
  type Stream,
} from '@/apis/core-api/source-stream';
import { authorizeSource } from './SourceOAuth';
import SourceConfigEditor, { readSourceDocument } from './SourceConfigEditor';
import TopicLinks from './TopicLinks';
import { TopicLink } from './resource-links';
import SourceCredentialsFields, { type SourceCredentialsDraft } from './SourceCredentialsFields';
import {
  credentialDraftEntered,
  credentialKindForDocument,
  credentialPayload,
  namedSecretValues,
  supportsTLSCredentials,
} from './source-credentials';
import SourceTLSFields from './SourceTLSFields';
import { useIntegrationDirty, useIntegrationRoute } from './IntegrationTabs';
import styles from './index.module.scss';
import { sourceTypeLabelKey } from './source-types';

const SourceStreams: FC<{ sourceId: number; httpServer?: boolean }> = ({ sourceId, httpServer = false }) => {
  const sourceStreamApi = useSourceStreamApi();
  const t = useTranslate();
  const { go } = useIntegrationRoute();
  const [failed, setFailed] = useState(false);
  const { data, loading, pagination } = usePagination<Stream>({
    fetchApi: async ({ pageNo, pageSize }: { pageNo: number; pageSize: number }) => {
      try {
        const r = await sourceStreamApi.streams({ sourceId, page: pageNo, size: pageSize });
        setFailed(false);
        return { data: r.list, total: r.total, pageNo };
      } catch (error) {
        setFailed(true);
        throw error;
      }
    },
  });
  return (
    <div className={styles.section}>
      {failed && <Alert type="error" message={t('SourceStream.loadFailed')} showIcon />}
      <ProTable
        rowKey="id"
        loading={loading}
        dataSource={data}
        pagination={pagination}
        columns={[
          {
            title: t('SourceStream.name'),
            dataIndex: 'name',
            width: 180,
            render: (name: string, stream: Stream) => (
              <ComButton
                type="link"
                onClick={(event) => {
                  event.stopPropagation();
                  go({ view: 'stream', sourceId, streamId: stream.id });
                }}
              >
                {name}
              </ComButton>
            ),
          },
          ...(httpServer
            ? [
                {
                  title: t('SourceStream.httpRoutes'),
                  key: 'routes',
                  width: 300,
                  render: (_: unknown, stream: Stream) => {
                    const active = stream.manifest;
                    const entries = (active ?? stream.draftManifest)?.entries.filter(
                      (entry) => entry.kind === 'http-request' && entry.sourceId === String(sourceId)
                    );
                    return entries?.length ? (
                      <div className={styles.section}>
                        {!active && <Tag>{t('SourceStream.status.draft')}</Tag>}
                        {entries.map((entry) => (
                          <code key={entry.id}>
                            {entry.method} {entry.path}
                          </code>
                        ))}
                      </div>
                    ) : (
                      <span className={styles.muted}>{t('SourceStream.noHTTPRoutes')}</span>
                    );
                  },
                },
              ]
            : [
                {
                  title: t('SourceStream.topics'),
                  key: 'topics',
                  width: 240,
                  render: (_: unknown, stream: Stream) => (
                    <div onClick={(event) => event.stopPropagation()}>
                      {isProgramDocument(stream.document) ? (
                        (stream.manifest ?? stream.draftManifest)?.topics.map((topic) => (
                          <TopicLink
                            key={topic.topicId || topic.collectionId}
                            id={(topic.topicId || topic.collectionId)!}
                          />
                        ))
                      ) : (
                        <TopicLinks
                          ids={stream.document.topicIds}
                          metadataId={stream.document.metadataTarget?.topicId}
                        />
                      )}
                    </div>
                  ),
                },
              ]),
          {
            title: t('SourceStream.status'),
            dataIndex: 'status',
            width: 120,
            render: (s: string, stream: Stream) => (
              <Tag>
                {t(
                  `SourceStream.${s === 'enabled' && stream.executionState ? 'execution.' + stream.executionState : 'status.' + s}`
                )}
              </Tag>
            ),
          },
        ]}
        onRow={(item: Stream) => ({ onClick: () => go({ view: 'stream', sourceId, streamId: item.id }) })}
      />
    </div>
  );
};

const SourceDetail: FC<{ source: Source; onChanged: (source?: Source) => void; embedded?: boolean }> = ({
  source,
  onChanged,
  embedded = false,
}) => {
  const sourceStreamApi = useSourceStreamApi();
  const capability = useCapabilityReadiness('source');
  const remoteReadOnly = useSourceStreamScope().readOnly;
  const sourceScope = useSourceStreamScope();
  const t = useTranslate();
  const { modal, message } = App.useApp();
  const { tab, go } = useIntegrationRoute();
  const [sourceTypes, setSourceTypes] = useState<SourceTypeDefinition[]>([]);
  const [profileFailed, setProfileFailed] = useState(false);
  useEffect(() => {
    if (!source.document.sourceType) return;
    let alive = true;
    sourceStreamApi
      .protocols()
      .then((catalog) => {
        if (alive) {
          setSourceTypes(catalog.sourceTypes || []);
          setProfileFailed(false);
        }
      })
      .catch(() => alive && setProfileFailed(true));
    return () => {
      alive = false;
    };
  }, [source.document.sourceType]);
  const profile = sourceTypes.find((item) => item.id === source.document.sourceType);
  const [raw, setRaw] = useState(() => JSON.stringify(source.document, null, 2));
  const [credentialsDraft, setCredentials] = useState<SourceCredentialsDraft>({
    kind: credentialKindForDocument(source.document),
    username: '',
    secret: '',
  });
  const [savingSource, setSavingSource] = useState(false);
  const [readingTLS, setReadingTLS] = useState(false);
  const [credentialSaveFailed, setCredentialSaveFailed] = useState(false);
  const [managing, setManaging] = useState(false);
  const [parentResourceRef, setParentResourceRef] = useState('');
  const [discovering, setDiscovering] = useState(false);
  const [parametersValid, setParametersValid] = useState(true);
  const document = readSourceDocument(raw);
  const credentials: SourceCredentialsDraft = {
    ...credentialsDraft,
    kind: document ? credentialKindForDocument(document, credentialsDraft.kind) : credentialsDraft.kind,
  };
  const httpAuthenticationDirty =
    Boolean(document?.http) &&
    (document?.http?.authentication ?? (document?.role === 'server' ? 'bearer' : 'none')) !==
      (source.document.http?.authentication ?? (source.document.role === 'server' ? 'bearer' : 'none'));
  const configDirty = raw !== JSON.stringify(source.document, null, 2);
  const hasCredentials = Boolean(source.document.credentialRef);
  const canManageCredentials = !remoteReadOnly && hasPermission(ButtonPermission['uns.sourceCredentials']);
  const oauthConfigDirty =
    Boolean(document?.http?.oauth) &&
    (JSON.stringify(document?.http) !== JSON.stringify(source.document.http) ||
      document?.endpoint !== source.document.endpoint);
  const credentialDirty =
    credentialDraftEntered(credentials) ||
    (credentials.kind !== 'none' && (!hasCredentials || oauthConfigDirty || httpAuthenticationDirty));
  const credentialValid =
    Boolean(namedSecretValues(credentials)) &&
    (!credentialDirty || Boolean(document && credentialPayload(document, credentials)));
  const dirty = configDirty || credentialDirty || !parametersValid;
  useIntegrationDirty(`source-${source.id}`, dirty);
  const canEdit = !remoteReadOnly && hasPermission(ButtonPermission['uns.sourceManage']);
  const discover = async () => {
    setDiscovering(true);
    try {
      await sourceStreamApi.discover(source.id, source.revision, parentResourceRef);
      onChanged(await sourceStreamApi.source(source.id));
    } finally {
      setDiscovering(false);
    }
  };
  useEffect(() => {
    setRaw(JSON.stringify(source.document, null, 2));
  }, [source.id, source.revision]);
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
  const confirmImpact = (impact: SourceImpact, action: string): Promise<boolean> =>
    new Promise((resolve) => {
      openConfirmModal(modal, {
        title: t(`SourceStream.${action}`),
        content: (
          <div className={styles.section}>
            {action === 'deleteSource' && <span>{t('SourceStream.deleteSourceHint')}</span>}
            <span>
              {t(action === 'deleteSource' ? 'SourceStream.deleteImpactHint' : 'SourceStream.impactHint', {
                count: impact.count,
              })}
            </span>
            {impact.streams.length > 0 && (
              <ul>
                {impact.streams.map((item) => (
                  <li key={item.id}>
                    {item.name} · {t(`SourceStream.status.${item.status}`)}
                  </li>
                ))}
              </ul>
            )}
          </div>
        ),
        okText: t('common.confirm'),
        cancelText: t('common.cancel'),
        danger: action === 'deleteSource',
        onOk: () => resolve(true),
        onCancel: () => resolve(false),
      });
    });
  const save = async () => {
    if (readingTLS) return;
    if (
      !document ||
      !parametersValid ||
      !credentialValid ||
      savingSource ||
      (configDirty && !canEdit) ||
      (credentialDirty && !canManageCredentials)
    )
      return;
    setSavingSource(true);
    setCredentialSaveFailed(false);
    let saved = source;
    try {
      const connectionChanged = !isEqual(
        omit(source.document, ['name', 'description']),
        omit(document, ['name', 'description'])
      );
      let impact = connectionChanged || credentialDirty ? await sourceStreamApi.impact(source.id) : undefined;
      if (impact?.count && !(await confirmImpact(impact, 'saveSource'))) return;
      if (configDirty)
        saved = await sourceStreamApi.updateSource(source.id, document, {
          expectedRevision: saved.revision,
          confirmedImpactRevision: impact?.impactRevision,
        });
      if (credentialDirty) {
        const credentialInput = credentialPayload(document, credentials);
        if (!credentialInput) return;
        if (configDirty) impact = await sourceStreamApi.impact(source.id);
        saved = await sourceStreamApi.credentials(source.id, {
          ...credentialInput,
          expectedRevision: saved.revision,
          confirmedImpactRevision: impact?.impactRevision,
        });
      }
      setCredentials({
        kind: credentialKindForDocument(saved.document),
        username: '',
        secret: '',
      });
      onChanged(saved);
      message.success(t('SourceStream.saved'));
    } catch {
      if (saved.revision !== source.revision) {
        onChanged(saved);
        setCredentialSaveFailed(true);
      }
      /* 保留未保存的认证输入，重试使用最新的 Source 版本。 */
    } finally {
      setSavingSource(false);
    }
  };
  const probe = async (mode: 'connection' | 'data' = 'connection') => {
    try {
      const result = await sourceStreamApi.probe(source.id, undefined, mode);
      if(result.health === 'healthy') message.success(t(mode === 'data' ? 'SourceStream.dataVerified' : 'SourceStream.connectionVerified'));
      else message.error(`${result.stage} · ${result.errorCode}: ${result.message}`);
    } catch {
      // The request layer presents the protocol/runtime failure. Refresh health
      // below without leaving a rejected click-handler promise in the page.
    } finally {
      try {
        onChanged(await sourceStreamApi.source(source.id));
      } catch {
        // A failed refresh must not replace the primary probe error.
      }
    }
  };
  const remove = async () => {
    const impact = await sourceStreamApi.impact(source.id);
    if (await confirmImpact(impact, 'deleteSource')) {
      await sourceStreamApi.deleteSource(source.id, {
        expectedRevision: source.revision,
        confirmedImpactRevision: impact.impactRevision,
      });
      onChanged();
    }
  };
  const manageAction = async (action: () => Promise<void>) => {
    setManaging(true);
    try {
      await action();
    } catch {
      /* 接口错误由 API adapter 提示，保留当前页面。 */
    } finally {
      setManaging(false);
    }
  };
  const manageItems: MenuProps['items'] = [
    ...(canEdit || canManageCredentials
      ? [
          {
            key: 'edit',
            label: t('SourceStream.connection'),
            icon: <Settings />,
            onClick: () => go({ view: 'source', sourceId: source.id, tab: 'connection' }),
          },
        ]
      : []),
    ...(!remoteReadOnly && hasPermission(ButtonPermission['uns.streamManage'])
      ? [
          {
            key: 'stream',
            label: t('SourceStream.addStream'),
            icon: <Add />,
            disabled: dirty,
            onClick: () =>
              go({
                view: 'stream',
                sourceId: source.id,
                create: source.protocol === 'oauth2' ? 'script' : 'stream',
              }),
          },
        ]
      : []),
    ...(canEdit
      ? [
          { type: 'divider' as const },
          {
            key: 'delete',
            label: t('SourceStream.deleteSource'),
            icon: <TrashCan />,
            danger: true,
            disabled: dirty,
            onClick: () => {
              void manageAction(remove);
            },
          },
        ]
      : []),
  ];
  const time = (value: number) => (value ? new Date(value).toLocaleString() : t('SourceStream.never'));
  const overview = (
    <Descriptions
      layout="vertical"
      column={2}
      items={[
        ...(source.protocol === 'usb-hid'
          ? []
          : [{ key: 'endpoint', label: t('SourceStream.endpoint'), children: source.document.endpoint }]),
        { key: 'protocol', label: t('SourceStream.protocol'), children: source.protocol.toUpperCase() },
        {
          key: 'health',
          label: t('SourceStream.connectionState'),
          children: t(`SourceStream.health.${source.health}`),
        },
        { key: 'checkedAt', label: t('SourceStream.checkedAt'), children: time(source.checkedAt) },
      ]}
    />
  );
  const closeConfig = () => {
    if (savingSource) return;
    if (!dirty) go({ view: 'source', sourceId: source.id });
    else
      openConfirmModal(modal, {
        title: t('SourceStream.unsaved'),
        content: t('SourceStream.discardHint'),
        okText: t('SourceStream.discard'),
        cancelText: t('common.cancel'),
        onOk: () => {
          setRaw(JSON.stringify(source.document, null, 2));
          setCredentials({
            kind: credentialKindForDocument(source.document),
            username: '',
            secret: '',
          });
          setCredentialSaveFailed(false);
          go({ view: 'source', sourceId: source.id });
        },
      });
  };
  const renderResources = (editable: boolean) => (
    <Collapse
      ghost
      items={[
        {
          key: 'resources',
          label: t('SourceStream.nativeResources'),
          children: (
            <>
              {(source.mqttServer || source.opcuaServer) && (
                <section className={styles.card}>
                  <h3>{t('SourceStream.deviceAccess')}</h3>
                  <Alert
                    type="info"
                    showIcon
                    message={t(
                      source.mqttServer ? 'SourceStream.serverAccessHint' : 'SourceStream.opcuaPrivateListener'
                    )}
                  />
                  <pre className={styles.code}>{JSON.stringify(source.mqttServer || source.opcuaServer, null, 2)}</pre>
                </section>
              )}
              <section className={styles.card}>
                <div className={styles.row}>
                  <h3>
                    {t('SourceStream.nativeResources')} · {source.resources?.length || 0}
                  </h3>
                  {editable && canEdit && (
                    <Space>
                      {source.protocol === 'opcua' && source.document.role !== 'server' && (
                        <Input
                          value={parentResourceRef}
                          aria-label={t('SourceStream.parentNode')}
                          placeholder={t('SourceStream.parentNode')}
                          onChange={(event) => setParentResourceRef(event.target.value)}
                        />
                      )}
                      <ComButton
                        auth={ButtonPermission['uns.sourceManage']}
                        loading={discovering}
                        disabled={dirty || managing || capability.blocked('discover')}
                        onClick={discover}
                      >
                        {t('SourceStream.discover')}
                      </ComButton>
                    </Space>
                  )}
                </div>
                <ProTable
                  rowKey="id"
                  dataSource={source.resources || []}
                  pagination={false}
                  columns={[
                    { title: t('SourceStream.name'), dataIndex: 'name' },
                    { title: t('SourceStream.resourceId'), dataIndex: 'id', ellipsis: true },
                    {
                      title: t('SourceStream.model'),
                      dataIndex: 'models',
                      render: (models: string[]) =>
                        models.map((model) => <Tag key={model}>{t(`SourceStream.model.${model}`)}</Tag>),
                    },
                  ]}
                />
              </section>
            </>
          ),
        },
      ]}
    />
  );
  const config = (
    <div className={`${styles.section} ${styles['drawer-content']}`}>
      {dirty && <Alert type="warning" showIcon message={t('SourceStream.unsaved')} />}
      {credentialSaveFailed && <Alert type="error" showIcon message={t('SourceStream.credentialSaveFailed')} />}
      {profileFailed && <Alert type="error" showIcon message={t('SourceStream.vendorLoadFailed')} />}
      {!remoteReadOnly &&
        ((source.document.http?.oauth && source.document.http.oauth.grantType !== 'client_credentials') ||
          source.document.sourceType === 'ngp') && (
          <Space>
            <Tag>
              {t(source.oauthStatus === 'authorized' ? 'SourceStream.oauth.authorized' : 'SourceStream.oauth.required')}
            </Tag>
            <ComButton
              auth={ButtonPermission['uns.sourceCredentials']}
              disabled={dirty || savingSource || !hasCredentials}
              loading={managing}
              onClick={() => {
                void manageAction(() => authorizeSource(source.id, sourceStreamApi, sourceScope.nodeId));
              }}
            >
              {t(
                source.oauthStatus === 'authorized' ? 'SourceStream.oauth.reauthorize' : 'SourceStream.oauth.authorize'
              )}
            </ComButton>
          </Space>
        )}
      <SourceConfigEditor
        profile={profile}
        sourceId={source.id}
        value={raw}
        onChange={setRaw}
        readOnly={!canEdit || savingSource}
        onValidityChange={setParametersValid}
        connectionExtra={
          source.protocol !== 'modbus-tcp' &&
          source.protocol !== 'usb-hid' &&
          document?.http?.oauth?.clientAuthentication !== 'none' && (
            <SourceCredentialsFields
              fixedKind={Boolean(source.document.sourceType)}
              allowBearer={document?.protocol === 'http-api'}
              allowAnonymous={document?.role !== 'server' || Boolean(document?.http?.anonymous)}
              allowNamedSecrets={document?.protocol === 'http-api'}
              allowTLS={!!document && supportsTLSCredentials(document)}
              existingKeys={source.credentialKeys}
              serverBearer={document?.protocol === 'http-api' && document.role === 'server'}
              value={credentials}
              onChange={(value) => {
                setCredentials(value);
                if (document?.protocol === 'http-api')
                  setRaw(
                    JSON.stringify({ ...document, http: { ...document.http, authentication: value.kind } }, null, 2)
                  );
              }}
              existing={hasCredentials}
              canManage={canManageCredentials}
              disabled={savingSource}
            />
          )
        }
      />
      {document && (
        <SourceTLSFields
          document={document}
          value={credentials}
          onReadingChange={setReadingTLS}
          disabled={savingSource || !canManageCredentials}
          onChange={setCredentials}
        />
      )}
      {renderResources(true)}
    </div>
  );
  const configFooter = (
    <div className={styles['wizard-footer']}>
      {!remoteReadOnly && (
        <ComButton
          auth={ButtonPermission['uns.sourceManage']}
          onClick={() => probe('connection')}
          disabled={dirty || savingSource || capability.blocked('test')}
        >
          {t('SourceStream.testConnection')}
        </ComButton>
      )}
      {!remoteReadOnly && <ComButton auth={ButtonPermission['uns.sourceManage']} onClick={() => probe('data')} disabled={dirty || savingSource || capability.blocked('test')}>{t('SourceStream.validateData')}</ComButton>}
      <Space>
        <ComButton onClick={closeConfig} disabled={savingSource}>
          {t('common.cancel')}
        </ComButton>
        {(canEdit || canManageCredentials) && (
          <ComButton
            className={styles['manage-button']}
            icon={<Save />}
            loading={savingSource}
            onClick={save}
            disabled={
              !document ||
              readingTLS ||
              capability.blocked('configure') ||
              !parametersValid ||
              !dirty ||
              !credentialValid ||
              (configDirty && !canEdit) ||
              (credentialDirty && !canManageCredentials)
            }
          >
            {t('SourceStream.saveSource')}
          </ComButton>
        )}
      </Space>
    </div>
  );
  const overviewPanel = (
    <div className={styles.section}>
      <div className={styles.row}>
        <div>
          <div className={styles.muted}>
            {t('SourceStream.source')} / {t(sourceTypeLabelKey(source.document))}
          </div>
          {!embedded && <h2>{source.name}</h2>}
          {source.protocol !== 'usb-hid' && <Tag>{t(`SourceStream.role.${source.document.role || 'client'}`)}</Tag>}
        </div>
        <Space>
          {manageItems.length > 0 && (
            <Dropdown menu={{ items: manageItems }} trigger={['click']} placement="bottomRight" disabled={managing}>
              <ComButton className={styles['manage-button']} loading={managing}>
                {t('SourceStream.manage')} <ChevronDown size={16} />
              </ComButton>
            </Dropdown>
          )}
        </Space>
      </div>
      <CapabilityReadinessNotice value={capability} />
      {tab !== 'streams' && (
        <>
          {source.healthMessage && <Alert type="warning" showIcon message={source.connectionCheck ? `${source.connectionCheck.stage} · ${source.connectionCheck.errorCode}: ${source.connectionCheck.message}` : t(source.healthMessage)} />}
          {source.dataCheck && <Alert type={source.dataCheck.health === 'healthy' ? 'success' : 'warning'} showIcon message={`${t('SourceStream.validateData')}: ${source.dataCheck.message}${source.dataCheck.errorCode ? ` (${source.dataCheck.stage} · ${source.dataCheck.errorCode})` : ''}`} />}
          <section className={styles.card}>
            <h3>{t('common.detail')}</h3>
            {overview}
            {source.document.description && <p className={styles.muted}>{source.document.description}</p>}
          </section>
          {source.protocol === 'http-api' && source.document.role !== 'server' && (
            <section className={styles.card}>
              <Space>
                <h3>{t('SourceStream.httpClientAccess')}</h3>
                <HelpTooltip title={t('SourceStream.httpClientAccessHint')} />
              </Space>
              <Descriptions
                layout="vertical"
                column={2}
                items={[
                  { key: 'url', label: t('SourceStream.endpoint'), children: source.document.endpoint },
                  {
                    key: 'parameters',
                    label: t('SourceStream.publicParameters'),
                    children: Object.keys(source.document.http?.parameters ?? {}).join(', ') || t('common.none'),
                  },
                  {
                    key: 'secrets',
                    label: t('SourceStream.namedSecrets'),
                    children: source.credentialKeys?.join(', ') || t('common.none'),
                  },
                ]}
              />
            </section>
          )}
          {source.protocol === 'http-api' && source.document.role === 'server' && (
            <section className={styles.card}>
              <div className={styles.row}>
                <h3>{t('SourceStream.httpListener')}</h3>
                <ComButton
                  loading={managing}
                  disabled={dirty}
                  onClick={() => void manageAction(async () => onChanged(await sourceStreamApi.source(source.id)))}
                >
                  {t('SourceStream.refresh')}
                </ComButton>
              </div>
              <Tag>{t(`SourceStream.httpState.${source.httpServer?.state ?? 'stopped'}`)}</Tag>
              {source.httpServer?.port ? (
                <p>
                  {t('SourceStream.httpPort')} · {source.httpServer.port}
                </p>
              ) : null}
              <p>{t('SourceStream.httpPublicURL')}</p>
              <pre className={styles.code}>{source.httpServer?.url || t('SourceStream.httpURLUnconfigured')}</pre>
              {source.httpServer?.message && <Alert type="warning" showIcon message={t(source.httpServer.message)} />}
              {source.document.http?.anonymous && (
                <Alert type="warning" showIcon message={t('SourceStream.httpAnonymousRisk')} />
              )}
              <HelpTooltip title={t('SourceStream.httpListenerHint')} />
            </section>
          )}
          {renderResources(false)}
        </>
      )}
      {hasPermission(ButtonPermission['uns.streams']) && (
        <section className={styles.card}>
          <h3>
            {t(
              source.protocol === 'http-api' && source.document.role === 'server'
                ? 'SourceStream.httpRoutes'
                : 'SourceStream.mappingRelations'
            )}
          </h3>
          <SourceStreams
            key={source.revision}
            sourceId={source.id}
            httpServer={source.protocol === 'http-api' && source.document.role === 'server'}
          />
        </section>
      )}
    </div>
  );
  if (embedded) {
    return ['connection', 'resources', 'health'].includes(tab) ? (
      <div className={styles.section}>
        <ComButton type="text" icon={<ArrowLeft />} onClick={closeConfig} disabled={savingSource}>
          {t('common.back')}
        </ComButton>
        {config}
        {configFooter}
      </div>
    ) : (
      overviewPanel
    );
  }
  return (
    <>
      {overviewPanel}
      <ComDrawer
        closable={!savingSource}
        maskClosable={!savingSource}
        keyboard={!savingSource}
        extra={null}
        open={['connection', 'resources', 'health'].includes(tab)}
        title={
          <div>
            {t('SourceStream.connection')}
            <div className={styles['drawer-subtitle']}>
              {source.name} · {source.protocol.toUpperCase()}
            </div>
          </div>
        }
        width={800}
        styles={{
          body: { padding: 24 },
          header: { borderBottom: '1px solid var(--ui-line-color)' },
          footer: { padding: '16px 24px' },
        }}
        footer={configFooter}
        onClose={closeConfig}
      >
        {config}
      </ComDrawer>
    </>
  );
};

export default SourceDetail;
