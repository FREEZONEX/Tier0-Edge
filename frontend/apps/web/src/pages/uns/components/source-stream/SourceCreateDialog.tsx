import { useSourceStreamApi, useSourceStreamScope } from '@/apis/core-api/source-stream-scope';
import { useEffect, useRef, useState, type FC } from 'react';
import { Alert, App, Button, Spin } from 'antd';
import { ArrowLeft } from '@carbon/icons-react';
import { v4 as uuid } from 'uuid';
import ProModal from '@/components/pro-modal';
import { hasPermission } from '@/utils/auth';
import { ButtonPermission } from '@/common-types/button-permission';
import { useTranslate } from '@/hooks';
import { MAX_LENGTHS } from '@/utils/limits';
import {
  type ProtocolDefinition,
  type Source,
  type SourceDocument,
  type SourceTypeDefinition,
} from '@/apis/core-api/source-stream';
import SourceConfigEditor, { readSourceDocument } from './SourceConfigEditor';
import SourceProtocolCatalog from './SourceProtocolCatalog';
import SourceCredentialsFields, {
  EMPTY_SOURCE_CREDENTIALS,
  type SourceCredentialsDraft,
} from './SourceCredentialsFields';
import styles from './index.module.scss';
import { authorizeSource } from './SourceOAuth';
import { credentialKindForDocument, credentialPayload, namedSecretValues } from './source-credentials';
import { vendorCredentialDraft } from './source-types';
import SourceTLSFields from './SourceTLSFields';
import { credentialDraftEntered, supportsTLSCredentials } from './source-credentials';

export function initialSourceDocument(definition: ProtocolDefinition, role?: 'client' | 'server'): SourceDocument {
  const doc = definition.document;
  return {
    ...doc,
    ...(doc.protocol === 'usb-hid' ? { endpoint: '' } : {}),
    ...(role ? { role } : {}),
    ...(doc.mqtt ? { mqtt: { ...doc.mqtt, clientId: doc.mqtt.clientId || 'tier0-' + uuid() } } : {}),
    ...(doc.protocol === 'http-api' && role
      ? {
          endpoint: role === 'server' ? 'http://managed' : doc.endpoint,
          http: {
            ...doc.http,
            ...(role === 'server'
              ? { authentication: 'bearer' as const, anonymous: false, port: 0 }
              : { authentication: 'none' as const, anonymous: undefined, port: undefined }),
          },
        }
      : {}),
  };
}
function connectionError(doc: SourceDocument | undefined): string | undefined {
  if (!doc) return 'invalidConfiguration';
  if (
    (doc.protocol !== 'oauth2' || doc.sourceType) &&
    (!doc.name.trim() || Array.from(doc.name).length > MAX_LENGTHS.name)
  )
    return 'nameRequired';
  if (Array.from(doc.description).length > MAX_LENGTHS.description) return 'descriptionTooLong';
  if (doc.protocol === 'usb-hid') return doc.endpoint.startsWith('usb-hid://') ? undefined : 'hid.confirmRequired';
  if (doc.role === 'server') return;
  try {
    const url = new URL(doc.endpoint);
    const schemes = doc.http
      ? doc.protocol === 'http-api'
        ? ['http:', 'https:']
        : ['https:']
      : doc.protocol === 'mqtt'
        ? ['mqtt:', 'mqtts:']
        : doc.protocol === 'opcua'
          ? ['opc.tcp:']
          : doc.protocol === 'edge-sql'
            ? ['postgresql:', 'postgres:', 'mysql:', 'sqlserver:']
            : doc.protocol === 'file-read'
              ? ['http:', 'https:', 'sftp:', 'smb:']
              : ['tcp:'];
    if (
      !url.hostname ||
      !schemes.includes(url.protocol) ||
      url.username ||
      url.password ||
      url.search ||
      url.hash ||
      (url.port && (+url.port < 1 || +url.port > 65535)) ||
      (!['opcua', 'edge-sql', 'file-read'].includes(doc.protocol) && !doc.http && url.pathname && url.pathname !== '/')
    )
      return 'endpointRequired';
  } catch {
    return 'endpointRequired';
  }
  if (doc.http?.oauth) {
    const o = doc.http.oauth;
    if (!o.clientId.trim()) return 'oauth.configRequired';
    for (const value of o.grantType === 'client_credentials' ? [o.tokenUrl] : [o.authorizationUrl, o.tokenUrl]) {
      try {
        const u = new URL(value);
        if (u.protocol !== 'https:' || u.username || u.password || u.search || u.hash) return 'oauth.configRequired';
      } catch {
        return 'oauth.configRequired';
      }
    }
  }
  if (doc.mqtt && !doc.mqtt.clientId.trim()) return 'clientIdRequired';
}

const SourceCreateDialog: FC<{ open: boolean; onClose: () => void; onCreated: (source: Source) => void }> = ({
  open,
  onClose,
  onCreated,
}) => {
  const sourceStreamApi = useSourceStreamApi();
  const sourceScope = useSourceStreamScope();
  const t = useTranslate();
  const { message } = App.useApp();
  const [protocols, setProtocols] = useState<ProtocolDefinition[]>([]);
  const [sourceTypes, setSourceTypes] = useState<SourceTypeDefinition[]>([]);
  const [raw, setRaw] = useState('');
  const [selected, setSelected] = useState<string>();
  const [catalogOpen, setCatalogOpen] = useState(true);
  const drafts = useRef<Record<string, string>>({});
  const draftKey = useRef('');
  const [error, setError] = useState<string>();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [readingTLS, setReadingTLS] = useState(false);
  const [failed, setFailed] = useState(false);
  const [credentials, setCredentials] = useState<Record<string, SourceCredentialsDraft>>({});
  const [createdSource, setCreatedSource] = useState<Source>();
  const [parametersValid, setParametersValid] = useState(true);
  const credentialsSaved = useRef(false);
  const createIntent = useRef<{ fingerprint: string; key: string }>();
  const document = readSourceDocument(raw);
  const isOAuth = (document?.protocol || selected) === 'oauth2';
  const isHid = selected === 'usb-hid';
  const protocol = protocols.find((item) => item.id === (document?.protocol || selected));
  const profile = sourceTypes.find((item) => item.id === document?.sourceType);
  const profileName = profile ? t(`SourceStream.vendorName.${profile.id}`) : protocol?.name;
  const canManageCredentials = hasPermission(ButtonPermission['uns.sourceCredentials']);
  const supportsCredentials = Boolean(protocol?.credentialKinds.length);
  const draftCredential =
    credentials[protocol?.id || ''] ||
    (isOAuth
      ? { ...EMPTY_SOURCE_CREDENTIALS, kind: 'oauth2' as const }
      : { ...EMPTY_SOURCE_CREDENTIALS, kind: document ? credentialKindForDocument(document) : 'none' });
  const credential: SourceCredentialsDraft = {
    ...draftCredential,
    kind:
      document?.role === 'server' && document.protocol !== 'http-api'
        ? 'basic'
        : isOAuth
          ? 'oauth2'
          : document
            ? credentialKindForDocument(document, draftCredential.kind)
            : draftCredential.kind,
  };
  const submittedCredential = vendorCredentialDraft(profile, credential);
  const credentialError = !parametersValid
    ? isHid
      ? 'hid.confirmRequired'
      : 'invalidParameters'
    : !namedSecretValues(submittedCredential)
      ? 'invalidNamedSecrets'
      : document?.http?.oauth &&
          (!canManageCredentials || (document.http.oauth!.clientAuthentication !== 'none' && !credential.secret))
        ? 'oauth.secretRequired'
        : supportsCredentials &&
            (credential.kind === 'basic' ||
              credential.kind === 'bearer' ||
              credential.kind === 'custom' ||
              credentialDraftEntered(credential))
          ? !canManageCredentials
            ? 'credentialPermission'
            : !document || !credentialPayload(document, submittedCredential)
              ? 'credentialsRequired'
              : undefined
          : undefined;
  const authenticationFields = supportsCredentials && document?.http?.oauth?.clientAuthentication !== 'none' && (
    <SourceCredentialsFields
      fixedKind={Boolean(profile)}
      allowBearer={document?.protocol === 'http-api'}
      allowAnonymous={document?.role !== 'server' || Boolean(document?.http?.anonymous)}
      allowNamedSecrets={document?.protocol === 'http-api'}
      allowTLS={!!document && supportsTLSCredentials(document)}
      serverBearer={document?.protocol === 'http-api' && document.role === 'server'}
      value={credential}
      canManage={canManageCredentials}
      disabled={saving}
      retry={Boolean(createdSource)}
      onChange={(value) => {
        if (protocol) setCredentials((current) => ({ ...current, [protocol.id]: value }));
        if (document?.protocol === 'http-api')
          update(JSON.stringify({ ...document, http: { ...document.http, authentication: value.kind } }, null, 2));
        setError(undefined);
      }}
    />
  );
  useEffect(() => {
    if (!open) {
      setCredentials({});
      return;
    }
    let alive = true;
    setLoading(true);
    setFailed(false);
    setRaw('');
    setSelected(undefined);
    setCatalogOpen(true);
    setError(undefined);
    setParametersValid(true);
    setCredentials({});
    setCreatedSource(undefined);
    credentialsSaved.current = false;
    createIntent.current = undefined;
    drafts.current = {};
    sourceStreamApi
      .protocols()
      .then((result) => {
        if (alive) {
          setProtocols(result.list);
          setSourceTypes(result.sourceTypes || []);
        }
      })
      .catch(() => alive && setFailed(true))
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [open]);
  const update = (value: string) => {
    const next = readSourceDocument(value);
    if (next?.protocol === 'oauth2' && !next.sourceType) {
      try {
        next.name = Array.from(new URL(next.endpoint).hostname).slice(0, MAX_LENGTHS.name).join('');
        value = JSON.stringify(next, null, 2);
      } catch {
        /* Validate the address on submit. */
      }
    }
    setRaw(value);
    setError(undefined);
    if (draftKey.current) drafts.current[draftKey.current] = value;
  };
  const select = (definition: ProtocolDefinition, role?: 'client' | 'server', sourceType?: SourceTypeDefinition) => {
    const initial = sourceType
      ? { ...sourceType.document, sourceType: sourceType.id, role: 'client' as const }
      : initialSourceDocument(definition, role);
    const key = sourceType?.id || (role ? `${definition.id}:${role}` : definition.id);
    const value = (definition.id !== 'usb-hid' && drafts.current[key]) || JSON.stringify(initial, null, 2);
    draftKey.current = key;
    setSelected(definition.id);
    setRaw(value);
    drafts.current[key] = value;
    setParametersValid(definition.id !== 'usb-hid');
    setError(undefined);
    setCatalogOpen(false);
    if (sourceType) {
      setCredentials((current) => ({
        ...current,
        [definition.id]: {
          ...EMPTY_SOURCE_CREDENTIALS,
          kind: credentialKindForDocument(initial),
          namedSecrets:
            initial.http?.authentication === 'custom'
              ? sourceType.credentialKeys?.map((name) => ({ name, value: '' }))
              : undefined,
        },
      }));
    }
  };
  const save = async (test = false, authorize = false) => {
    if (saving || readingTLS) return;
    const issue = connectionError(document) || credentialError;
    if (issue || !document) {
      setError(issue);
      return;
    }
    setSaving(true);
    let source = createdSource;
    try {
      const credentialInput = credentialPayload(document, submittedCredential);
      if (!source) {
        // 每个创建意图都保留同一个键，包括无需凭据的连接。
        const fingerprint = JSON.stringify({ document, credentialInput });
        if (createIntent.current?.fingerprint !== fingerprint) createIntent.current = { fingerprint, key: uuid() };
        source = await sourceStreamApi.createSourceDocument(document, credentialInput, createIntent.current.key);
        credentialsSaved.current = true;
        createIntent.current = undefined;
        setCreatedSource(source);
      }
      credentialsSaved.current = true;
      setCreatedSource(source);
      if (test) {
        const check=await sourceStreamApi.probe(source.id);
        source = await sourceStreamApi.source(source.id);
        setCreatedSource(source);
        if(check.health!=='healthy'){message.error(`${check.stage} · ${check.errorCode}: ${check.message}`);setError('partialCreate');return;}
      }
      if (
        (isOAuth && document.http?.oauth?.grantType !== 'client_credentials') ||
        (document.sourceType === 'ngp' && authorize)
      ) {
        await authorizeSource(source.id, sourceStreamApi, sourceScope.nodeId);
        return;
      }
      setCredentials({});
      message.success(t('SourceStream.created'));
      onCreated(source);
    } catch {
      if (source)
        setError(test ? 'partialCreate' : credentialsSaved.current ? 'oauth.startFailed' : 'credentialSaveFailed');
      /* 请求层展示服务端错误，保留配置与未完成的凭证供重试。 */
    } finally {
      setSaving(false);
    }
  };
  return (
    <ProModal
      footer={
        catalogOpen ? null : (
          <div className={styles['wizard-footer']}>
            <Button
              disabled={saving || Boolean(createdSource)}
              icon={<ArrowLeft />}
              onClick={() => {
                setCatalogOpen(true);
                setError(undefined);
              }}
            >
              {t('SourceStream.catalogBack')}
            </Button>
            <div className={styles['wizard-actions']}>
              {isHid ? (
                <Button
                  type="primary"
                  loading={saving}
                  disabled={!document || !parametersValid}
                  onClick={() => void save()}
                >
                  {t('SourceStream.saveSource')}
                </Button>
              ) : isOAuth ? (
                <Button
                  type="primary"
                  className={styles['manage-button']}
                  loading={saving}
                  onClick={() => void save()}
                  disabled={!document}
                >
                  {t(
                    document?.http?.oauth?.grantType === 'client_credentials'
                      ? 'SourceStream.saveSource'
                      : 'SourceStream.oauth.continue'
                  )}
                </Button>
              ) : (
                <>
                  <Button disabled={!document || saving || readingTLS} onClick={() => void save(true)}>
                    {t('SourceStream.saveAndTest')}
                  </Button>
                  <Button
                    type="primary"
                    className={styles['manage-button']}
                    loading={saving}
                    onClick={() => void save(false, document?.sourceType === 'ngp')}
                    disabled={!document || readingTLS}
                  >
                    {t(document?.sourceType === 'ngp' ? 'SourceStream.oauth.continue' : 'SourceStream.saveSource')}
                  </Button>
                </>
              )}
            </div>
          </div>
        )
      }
      open={open}
      title={
        <div>
          <strong>
            {t(!catalogOpen && isOAuth ? 'SourceStream.oauth.customIntegration' : 'SourceStream.addSource')}
          </strong>
          {!catalogOpen && <div className={styles['wizard-subtitle']}>{profileName}</div>}
        </div>
      }
      width={catalogOpen ? 1180 : isOAuth || isHid ? 720 : 800}
      styles={
        catalogOpen
          ? undefined
          : {
              content: { padding: 0, borderRadius: 16, overflow: 'hidden', background: 'var(--ui-bg-color)' },
              header: { padding: '24px 32px', margin: 0 },
              footer: { padding: '18px 28px', margin: 0 },
            }
      }
      className={
        catalogOpen
          ? styles['catalog-modal']
          : `${styles['source-wizard']} ${styles['source-create']} ${isOAuth || isHid ? styles['oauth-create'] : ''}`
      }
      fullScreenable={false}
      draggable={false}
      closable={!saving}
      maskClosable={!saving}
      keyboard={!saving}
      onCancel={() => {
        if (saving) return;
        setCredentials({});
        if (createdSource) onCreated(createdSource);
        else onClose();
      }}
      destroyOnHidden
    >
      <Spin spinning={loading}>
        {failed ? (
          <Alert type="error" message={t('SourceStream.loadFailed')} showIcon />
        ) : catalogOpen ? (
          !loading && (
            <SourceProtocolCatalog
              protocols={sourceScope.nodeId ? protocols.filter((item) => item.id !== 'usb-hid') : protocols}
              sourceTypes={sourceTypes}
              onSelect={select}
            />
          )
        ) : (
          <div className={styles['wizard-layout']}>
            <div className={styles['wizard-content']}>
              {error && <Alert type="error" showIcon message={t('SourceStream.' + error, { id: createdSource?.id })} />}
              <SourceConfigEditor
                profile={profile}
                creating
                readOnly={saving || Boolean(createdSource)}
                value={raw}
                onChange={update}
                onValidityChange={setParametersValid}
                section="connection"
                connectionExtra={authenticationFields}
              />
              {document && (
                <SourceTLSFields
                  document={document}
                  value={credential}
                  onReadingChange={setReadingTLS}
                  disabled={saving || !!createdSource || !canManageCredentials}
                  onChange={(value) => {
                    if (protocol) setCredentials((current) => ({ ...current, [protocol.id]: value }));
                    setError(undefined);
                  }}
                />
              )}
            </div>
          </div>
        )}
      </Spin>
    </ProModal>
  );
};
export default SourceCreateDialog;
