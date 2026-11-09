import { useEffect, useMemo, useState, type FC, type ReactNode } from 'react';
import { Alert, Checkbox, Collapse, Input, InputNumber, Segmented, Select } from 'antd';
import { json } from '@codemirror/lang-json';
import ProCodemirror from '@/components/pro-codemirror';
import HelpTooltip from '@/components/help-tooltip';
import { useTranslate } from '@/hooks';
import { MAX_LENGTHS } from '@/utils/limits';
import type { SourceDocument, SourceTypeDefinition } from '@/apis/core-api/source-stream';
import styles from './index.module.scss';
import OPCUAServerNodes from './OPCUAServerNodes';
import HTTPParameters from './HTTPParameters';
import HTTPHeaders from './HTTPHeaders';
import VendorConfigFields from './VendorConfigFields';
import { validSourceType } from './source-types';
import SourceRegisterRanges from './SourceRegisterRanges';
import HIDDevicePicker from './HIDDevicePicker';
import { useSourceStreamScope } from '@/apis/core-api/source-stream-scope';
import { readMqttEndpoint, updateMqttEndpoint } from './mqtt-endpoint';

export function readSourceDocument(text: string): SourceDocument | undefined {
  try {
    const d = JSON.parse(text);
    if (!d || Array.isArray(d) || typeof d !== 'object') return;
    if (['sdkVersion', 'configVersion', 'supportedVersions'].some((key) => key in d)) return;
    if (!validSourceType(d)) return;
    if (d.description === undefined) d.description = '';
    if (
      typeof d.name !== 'string' ||
      typeof d.description !== 'string' ||
      typeof d.endpoint !== 'string' ||
      d.runtimeLocation !== 'local'
    )
      return;
    const configCount = ['mqtt', 'opcua', 'modbus', 'http', 'sql', 'file', 'openProtocol'].filter(
      (key) => d[key] !== undefined
    ).length;
    if (d.protocol === 'usb-hid') {
      if (!configCount && (!d.role || d.role === 'client') && (!d.endpoint || d.endpoint.startsWith('usb-hid://')))
        return d;
      return;
    }
    if (configCount !== 1) return;
    if (d.role !== undefined && !['client', 'server'].includes(d.role)) return;
    if (d.role === 'server' && !['mqtt', 'opcua', 'http-api'].includes(d.protocol)) return;
    const nativeKey = { 'edge-sql': 'sql', 'file-read': 'file', 'open-protocol': 'openProtocol' }[d.protocol as string];
    if (nativeKey) {
      const options = d[nativeKey];
      return options && typeof options === 'object' && !Array.isArray(options) ? d : undefined;
    }
    const managedEndpoint =
      d.protocol === 'mqtt' ? 'mqtt://managed' : d.protocol === 'opcua' ? 'opc.tcp://managed' : 'http://managed';
    if (d.role === 'server' && d.endpoint !== managedEndpoint) return;
    if (
      d.protocol === 'oauth2' &&
      d.http?.oauth &&
      ['tokenUrl', 'clientId', 'clientAuthentication'].every((key) => typeof d.http.oauth[key] === 'string') &&
      (d.http.oauth.grantType === 'client_credentials' || typeof d.http.oauth.authorizationUrl === 'string') &&
      (d.http.oauth.scopes === undefined || typeof d.http.oauth.scopes === 'string')
    )
      return d;
    if (
      d.protocol === 'http-api' &&
      d.http &&
      !d.http.oauth &&
      ['none', 'basic', 'bearer', 'custom', 'http'].includes(
        d.http.authentication ?? (d.role === 'server' ? 'bearer' : 'none')
      ) &&
      (d.http.parameters === undefined ||
        (d.http.parameters && typeof d.http.parameters === 'object' && !Array.isArray(d.http.parameters))) &&
      (d.http.anonymous === undefined || typeof d.http.anonymous === 'boolean') &&
      (d.http.port === undefined ||
        (Number.isInteger(d.http.port) && (d.http.port === 0 || (d.http.port >= 1024 && d.http.port <= 65535)))) &&
      (d.role !== 'server' ||
        (d.http.anonymous ? d.http.authentication === 'none' : (d.http.authentication ?? 'bearer') === 'bearer'))
    )
      return d;
    if (
      d.protocol === 'mqtt' &&
      d.mqtt &&
      typeof d.mqtt.clientId === 'string' &&
      (d.mqtt.qos === undefined || [0, 1, 2].includes(d.mqtt.qos)) &&
      [d.mqtt.topicFilters, d.mqtt.serverSubscribeTopics].every(
        (list) => list === undefined || (Array.isArray(list) && list.every((item) => typeof item === 'string'))
      )
    )
      return d;
    if (
      d.protocol === 'opcua' &&
      d.opcua &&
      typeof d.opcua.securityMode === 'string' &&
      typeof d.opcua.securityPolicy === 'string' &&
      (d.opcua.samplingIntervalMs === undefined || typeof d.opcua.samplingIntervalMs === 'number') &&
      (d.opcua.nodeIds === undefined ||
        (Array.isArray(d.opcua.nodeIds) && d.opcua.nodeIds.every((id: unknown) => typeof id === 'string')))
    ) {
      if (
        d.opcua.serverNodes !== undefined &&
        (!Array.isArray(d.opcua.serverNodes) ||
          !d.opcua.serverNodes.every(
            (node: Record<string, unknown>) =>
              node &&
              typeof node.id === 'string' &&
              typeof node.name === 'string' &&
              ['Variable', 'Method'].includes(String(node.nodeClass)) &&
              ['Double', 'Int32', 'Boolean', 'String'].includes(String(node.dataType)) &&
              ['ingress', 'egress', 'bidirectional'].includes(String(node.direction))
          ))
      )
        return;
      return d;
    }
    if (
      d.protocol === 'modbus-tcp' &&
      d.modbus &&
      typeof d.modbus.unitId === 'number' &&
      (d.modbus.pollIntervalMs === undefined || typeof d.modbus.pollIntervalMs === 'number') &&
      (d.modbus.ranges === undefined ||
        (Array.isArray(d.modbus.ranges) &&
          d.modbus.ranges.every(
            (range: Record<string, unknown>) =>
              range &&
              typeof range.id === 'string' &&
              typeof range.table === 'string' &&
              typeof range.address === 'number' &&
              typeof range.quantity === 'number' &&
              (range.unitId === undefined ||
                (Number.isInteger(range.unitId) && Number(range.unitId) >= 1 && Number(range.unitId) <= 247)) &&
              (range.writable === undefined || typeof range.writable === 'boolean')
          )))
    )
      return d;
  } catch {
    /* Keep invalid JSON in the editor until the user fixes it. */
  }
}

export type SourceConfigSection = 'connection';

const SourceConfigEditor: FC<{
  value: string;
  onChange: (text: string) => void;
  readOnly?: boolean;
  section?: SourceConfigSection;
  heading?: ReactNode;
  connectionExtra?: ReactNode;
  creating?: boolean;
  sourceId?: number;
  onValidityChange?: (valid: boolean) => void;
  profile?: SourceTypeDefinition;
}> = ({
  value,
  onChange,
  readOnly = false,
  heading,
  connectionExtra,
  creating = false,
  sourceId,
  onValidityChange,
  profile,
}) => {
  const t = useTranslate();
  const scope = useSourceStreamScope();
  const [mode, setMode] = useState('form');
  const [parametersValid, setParametersValid] = useState(true);
  const [headersValid, setHeadersValid] = useState(true);
  const [hidValid, setHidValid] = useState(!creating);
  const doc = useMemo(() => readSourceDocument(value), [value]);
  const mqttEndpoint = doc?.protocol === 'mqtt' && doc.role !== 'server' ? readMqttEndpoint(doc.endpoint) : undefined;
  const isHid = doc?.protocol === 'usb-hid';
  const jsonOnly = Boolean(doc && ['edge-sql', 'file-read', 'open-protocol'].includes(doc.protocol));
  const parametersValidity = setParametersValid;
  useEffect(() => {
    onValidityChange?.(parametersValid && headersValid && (!isHid || hidValid));
  }, [parametersValid, headersValid, isHid, hidValid, onValidityChange]);
  const change = (patch: Partial<SourceDocument>) => doc && onChange(JSON.stringify({ ...doc, ...patch }, null, 2));
  const mqtt = (patch: Partial<NonNullable<SourceDocument['mqtt']>>) =>
    doc?.mqtt && change({ mqtt: { ...doc.mqtt, ...patch } });
  const opcua = (patch: Partial<NonNullable<SourceDocument['opcua']>>) =>
    doc?.opcua && change({ opcua: { ...doc.opcua, ...patch } });
  const modbus = (patch: Partial<NonNullable<SourceDocument['modbus']>>) =>
    doc?.modbus && change({ modbus: { ...doc.modbus, ...patch } });
  const field = (key: string, input: ReactNode, wide = false) => (
    <label className={wide ? styles.field + ' ' + styles['full-width'] : styles.field}>
      <span>{t('SourceStream.' + key)}</span>
      {input}
    </label>
  );
  const connection = doc && (
    <div className={styles.editor}>
      {!(creating && doc.http?.oauth && !doc.sourceType) &&
        field(
          'name',
          <Input
            value={doc.name}
            placeholder={t(doc.http ? 'SourceStream.oauth.namePlaceholder' : 'SourceStream.namePlaceholder')}
            maxLength={MAX_LENGTHS.name}
            readOnly={readOnly}
            onChange={(e) => change({ name: e.target.value })}
          />
        )}
      {isHid ? (
        scope.nodeId ? (
          <span>{t('SourceStream.hid.localOnly')}</span>
        ) : (
          <HIDDevicePicker
            endpoint={doc.endpoint}
            creating={creating}
            disabled={readOnly}
            onValidityChange={setHidValid}
            onConfirm={(device) =>
              change({
                endpoint: device.endpoint,
                name: doc.name.trim() || Array.from(device.label).slice(0, MAX_LENGTHS.name).join(''),
              })
            }
          />
        )
      ) : mqttEndpoint ? (
        <>
          {field(
            'mqttHost',
            <Input
              value={mqttEndpoint.host}
              placeholder="broker.example.com"
              readOnly={readOnly}
              addonBefore={
                <Select
                  aria-label={t('SourceStream.mqttTransport')}
                  value={mqttEndpoint.scheme}
                  disabled={readOnly}
                  options={[
                    { value: 'mqtt', label: 'mqtt://' },
                    { value: 'mqtts', label: 'mqtts://' },
                  ]}
                  onChange={(scheme) => change({ endpoint: updateMqttEndpoint(doc.endpoint, { scheme }) })}
                />
              }
              onChange={(event) => change({ endpoint: updateMqttEndpoint(doc.endpoint, { host: event.target.value }) })}
            />
          )}
          {field(
            'mqttPort',
            <InputNumber
              min={1}
              max={65535}
              precision={0}
              value={mqttEndpoint.port}
              disabled={readOnly}
              onChange={(port) => port !== null && change({ endpoint: updateMqttEndpoint(doc.endpoint, { port }) })}
            />
          )}
        </>
      ) : (
        field(
          doc.http ? 'apiBaseUrl' : 'endpoint',
          <Input
            value={doc.endpoint}
            placeholder={
              doc.http
                ? 'https://api.example.com/v1'
                : doc.protocol === 'mqtt'
                  ? 'mqtt://broker:1883'
                  : doc.protocol === 'opcua'
                    ? 'opc.tcp://device:4840'
                    : 'tcp://device:502'
            }
            readOnly={readOnly || doc.role === 'server'}
            onChange={(e) => change({ endpoint: e.target.value })}
          />,
          Boolean(doc.http?.oauth)
        )
      )}
      {profile && (
        <VendorConfigFields
          profile={profile}
          value={doc.http?.parameters ?? {}}
          readOnly={readOnly}
          onChange={(parameters) => change({ http: { ...doc.http, parameters } })}
        />
      )}
      {doc.protocol === 'http-api' && doc.role === 'server' && (
        <>
          {field(
            'httpPort',
            <InputNumber
              min={0}
              max={65535}
              precision={0}
              value={doc.http?.port ?? 0}
              disabled={readOnly || Boolean(sourceId)}
              onChange={(port) => port !== null && change({ http: { ...doc.http, port } })}
            />
          )}
          <div className={styles['full-width']}>
            <HelpTooltip title={t('SourceStream.httpPortHint')} />
            {Boolean(sourceId) && <HelpTooltip title={t('SourceStream.httpPortImmutable')} />}
            <Checkbox
              checked={Boolean(doc.http?.anonymous)}
              disabled={readOnly}
              onChange={(event) =>
                change({
                  http: {
                    ...doc.http,
                    anonymous: event.target.checked,
                    authentication: event.target.checked ? 'none' : 'bearer',
                  },
                })
              }
            >
              {t('SourceStream.httpAnonymous')}
            </Checkbox>
            {doc.http?.anonymous && <Alert type="warning" showIcon message={t('SourceStream.httpAnonymousRisk')} />}
          </div>
        </>
      )}
      {doc.http?.oauth &&
        field(
          'oauth.grantType',
          <Select
            value={doc.http.oauth.grantType || 'authorization_code'}
            disabled={readOnly}
            options={['authorization_code', 'client_credentials'].map((value) => ({
              value,
              label: t('SourceStream.oauth.' + value),
            }))}
            onChange={(grantType) =>
              change({
                http: {
                  ...doc.http!,
                  oauth: {
                    ...doc.http!.oauth!,
                    grantType,
                    authorizationUrl: grantType === 'client_credentials' ? '' : doc.http!.oauth!.authorizationUrl,
                    clientAuthentication:
                      doc.http!.oauth!.clientAuthentication === 'none' && grantType === 'client_credentials'
                        ? 'client_secret_basic'
                        : doc.http!.oauth!.clientAuthentication,
                  },
                },
              })
            }
          />,
          true
        )}
      {doc.http?.oauth ? (
        <Collapse
          className={styles['config-advanced']}
          defaultActiveKey={[]}
          items={[
            {
              key: 'integration',
              label: (
                <>
                  {t('SourceStream.oauth.setup')} <HelpTooltip title={t('SourceStream.oauth.setupHint')} />
                </>
              ),
              children: (
                <div className={styles.editor}>
                  {(['authorizationUrl', 'tokenUrl', 'clientId'] as const)
                    .filter((key) => key !== 'authorizationUrl' || doc.http!.oauth!.grantType !== 'client_credentials')
                    .map((key) => (
                      <div key={key}>
                        {field(
                          'oauth.' + key,
                          <Input
                            value={doc.http!.oauth![key]}
                            readOnly={readOnly}
                            onChange={(e) =>
                              change({ http: { ...doc.http!, oauth: { ...doc.http!.oauth!, [key]: e.target.value } } })
                            }
                          />
                        )}
                      </div>
                    ))}
                  {field(
                    'oauth.clientAuthentication',
                    <Select
                      value={doc.http.oauth.clientAuthentication}
                      disabled={readOnly}
                      options={[
                        'client_secret_basic',
                        'client_secret_post',
                        ...(doc.http.oauth.grantType === 'client_credentials' ? [] : ['none']),
                      ].map((value) => ({
                        value,
                        label: t('SourceStream.oauth.' + value),
                      }))}
                      onChange={(clientAuthentication) =>
                        change({ http: { ...doc.http!, oauth: { ...doc.http!.oauth!, clientAuthentication } } })
                      }
                    />
                  )}
                  {connectionExtra}
                  {field(
                    'oauth.scopes',
                    <Input
                      value={doc.http.oauth.scopes}
                      readOnly={readOnly}
                      onChange={(e) =>
                        change({ http: { ...doc.http!, oauth: { ...doc.http!.oauth!, scopes: e.target.value } } })
                      }
                    />
                  )}
                  {doc.http.oauth.grantType !== 'client_credentials' &&
                    field('oauth.redirectUri', <Input readOnly value={window.location.origin + '/uns'} />)}
                  {field(
                    'caCertificate',
                    <Input.TextArea
                      value={doc.http.caCertificate}
                      rows={3}
                      placeholder={'-----BEGIN CERTIFICATE-----'}
                      readOnly={readOnly}
                      onChange={(e) => change({ http: { ...doc.http!, caCertificate: e.target.value } })}
                    />,
                    true
                  )}
                </div>
              ),
            },
          ]}
        />
      ) : (
        connectionExtra
      )}
      {doc.opcua && (
        <>
          {doc.role === 'server' && (
            <div className={styles['full-width']}>
              <OPCUAServerNodes
                value={doc.opcua.serverNodes || []}
                readOnly={readOnly}
                onChange={(serverNodes) => opcua({ serverNodes })}
              />
              <Alert type="info" showIcon message={t('SourceStream.opcuaPrivateListener')} />
            </div>
          )}
          {field(
            'securityMode',
            <Select
              value={doc.opcua.securityMode}
              disabled={readOnly}
              options={['None', 'Sign', 'SignAndEncrypt'].map((value) => ({ value }))}
              onChange={(securityMode) =>
                opcua({ securityMode, securityPolicy: securityMode === 'None' ? 'None' : 'Basic256Sha256' })
              }
            />
          )}
          {field(
            'securityPolicy',
            <Select
              value={doc.opcua.securityPolicy}
              disabled={readOnly || doc.opcua.securityMode === 'None'}
              options={['None', 'Basic256Sha256', 'Aes128_Sha256_RsaOaep', 'Aes256_Sha256_RsaPss'].map((value) => ({
                value,
              }))}
              onChange={(securityPolicy) => opcua({ securityPolicy })}
            />
          )}
          {doc.opcua.securityMode !== 'None' &&
            field(
              'serverCertificate',
              <Input.TextArea
                value={doc.opcua.serverCertificate}
                rows={3}
                placeholder={'-----BEGIN CERTIFICATE-----'}
                readOnly={readOnly}
                onChange={(e) => opcua({ serverCertificate: e.target.value })}
              />,
              true
            )}
          {doc.opcua.securityMode === 'None' && (
            <div className={styles['full-width']}>
              <Alert type="warning" showIcon message={t('SourceStream.opcuaNoSecurity')} />
            </div>
          )}
        </>
      )}
      {doc.modbus &&
        field(
          'unitId',
          <InputNumber
            value={doc.modbus.unitId}
            disabled={readOnly}
            min={1}
            max={247}
            onChange={(value) => value !== null && modbus({ unitId: value })}
          />
        )}
      {!(creating && doc.http?.oauth) && (
        <Collapse
          ghost
          className={styles['config-advanced']}
          items={[
            {
              key: 'connection',
              label: t('SourceStream.advanced'),
              children: (
                <div className={styles.editor}>
                  {field(
                    'description',
                    <Input.TextArea
                      value={doc.description}
                      maxLength={MAX_LENGTHS.description}
                      readOnly={readOnly}
                      onChange={(e) => change({ description: e.target.value })}
                    />,
                    true
                  )}
                  {doc.http &&
                    !doc.http.oauth &&
                    field(
                      'caCertificate',
                      <Input.TextArea
                        value={doc.http.caCertificate}
                        rows={3}
                        placeholder={'-----BEGIN CERTIFICATE-----'}
                        readOnly={readOnly}
                        onChange={(e) => change({ http: { ...doc.http!, caCertificate: e.target.value } })}
                      />,
                      true
                    )}
                  {doc.http && doc.role !== 'server' && (
                    <div className={styles['full-width']}>
                      <HTTPHeaders
                        value={doc.http.headers ?? {}}
                        readOnly={readOnly}
                        onValidityChange={setHeadersValid}
                        onChange={(headers) => change({ http: { ...doc.http, headers } })}
                      />
                    </div>
                  )}
                  {doc.protocol === 'http-api' && (
                    <div className={styles['full-width']}>
                      <h4>{t('SourceStream.publicParameters')}</h4>
                      <HTTPParameters
                        value={doc.http?.parameters ?? {}}
                        readOnly={readOnly}
                        onValidityChange={parametersValidity}
                        onChange={(parameters) => change({ http: { ...doc.http, parameters } })}
                      />
                    </div>
                  )}
                  {doc.mqtt &&
                    field(
                      'clientId',
                      <Input
                        value={doc.mqtt.clientId}
                        readOnly={readOnly}
                        onChange={(e) => mqtt({ clientId: e.target.value })}
                      />
                    )}
                  {doc.modbus && (
                    <div className={styles['full-width']}>
                      <SourceRegisterRanges
                        value={doc.modbus.ranges ?? []}
                        readOnly={readOnly}
                        onChange={(ranges) => modbus({ ranges })}
                      />
                    </div>
                  )}
                  {doc.mqtt &&
                    doc.role === 'server' &&
                    field(
                      'serverSubscribeTopics',
                      <Select
                        mode="tags"
                        value={doc.mqtt.serverSubscribeTopics || []}
                        disabled={readOnly}
                        onChange={(serverSubscribeTopics) => mqtt({ serverSubscribeTopics })}
                      />,
                      true
                    )}
                </div>
              ),
            },
          ]}
        />
      )}
    </div>
  );
  return (
    <div className={styles.section}>
      {!isHid && (
        <div className={styles['config-mode']}>
          <Segmented
            value={jsonOnly ? 'json' : mode}
            options={[
              { value: 'form', label: t('SourceStream.form'), disabled: !doc || jsonOnly },
              { value: 'json', label: t('SourceStream.jsonConfig'), disabled: !parametersValid || !headersValid },
            ]}
            onChange={(next) => setMode(String(next))}
          />
          <HelpTooltip title={t('SourceStream.modeSyncHint')} />
        </div>
      )}
      {heading}
      {!doc && <Alert type="error" showIcon message={t('SourceStream.invalidConfiguration')} />}
      {jsonOnly || (!isHid && mode === 'json') || !doc ? (
        <ProCodemirror
          value={value}
          onChange={onChange}
          extensions={[json()]}
          height="360px"
          readOnly={readOnly}
          showHint={false}
        />
      ) : (
        <>{connection}</>
      )}
      {jsonOnly && connectionExtra}
    </div>
  );
};

export default SourceConfigEditor;
