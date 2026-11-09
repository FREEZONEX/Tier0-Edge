import { useCallback, useEffect, useState } from 'react';
import { Alert, App, Button, Checkbox, Pagination, Space, Tabs, Tag } from 'antd';
import { Copy, Connect, Renew } from '@carbon/icons-react';
import { AuthButton } from '@/components';
import { ButtonPermission } from '@/common-types/button-permission';
import { useTranslate } from '@/hooks';
import {
  getMcpInfo,
  getMcpOAuthGrants,
  revokeMcpOAuthGrant,
  resolveMcpUrl,
  type McpInfo,
  type McpOAuthGrant,
} from '@/apis/core-api/mcp';
import { copyToClipboard } from '@/utils/common';
import { formatTimestamp } from '@/utils/format';
import { mcpGrantPreset } from '@/utils/mcp-scopes';
import styles from './mcp-guide.module.scss';

export default function McpGuide({ onCreateKey }: { onCreateKey: (availableScopes: string[]) => void }) {
  const t = useTranslate('MCP');
  const common = useTranslate();
  const { message } = App.useApp();
  const [info, setInfo] = useState<McpInfo>();
  const [loading, setLoading] = useState(true);
  const [failure, setFailure] = useState<'missing' | 'unknown'>();
  const [tab, setTab] = useState('codex');
  const [grants, setGrants] = useState<McpOAuthGrant[]>([]);
  const [grantFailure, setGrantFailure] = useState(false);
  const [revoking, setRevoking] = useState<string>();
  const [showHistory, setShowHistory] = useState(false);
  const [grantPage, setGrantPage] = useState(1);
  const refresh = useCallback(async (signal?: AbortSignal) => {
    setLoading(true);
    try {
      const data = await getMcpInfo(signal);
      if (signal?.aborted) return;
      setInfo(data);
      setFailure(undefined);
    } catch (error) {
      if (signal?.aborted) return;
      setInfo(undefined);
      const status = (error as { response?: { status?: number } }).response?.status;
      setFailure(status === 404 ? 'missing' : 'unknown');
    } finally {
      if (!signal?.aborted) setLoading(false);
    }
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    void refresh(controller.signal);
    return () => controller.abort();
  }, [refresh]);

  useEffect(() => {
    if (!info?.oauth?.available) return;
    const controller = new AbortController();
    void getMcpOAuthGrants(controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) {
          setGrants(value.list);
          setGrantFailure(false);
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) setGrantFailure(true);
      });
    return () => controller.abort();
  }, [info]);

  const url = info ? resolveMcpUrl(info) : '';
  const available = !!info?.enabled;
  const oauthAvailable = !!info?.oauth?.available;
  const visibleGrants = grants.filter((grant) => showHistory || (!grant.revokedAt && !grant.expired));
  const shownGrants = visibleGrants.slice((grantPage - 1) * 5, grantPage * 5);
  const canRead = info?.availableScopes.includes('uns.read') || false;
  const copy = (value: string) =>
    copyToClipboard(value, (success) => {
      if (success) message.success(common('common.copySuccess'));
      else message.error(common('common.copyFail'));
    });
  const code = (value: string, label: string) => (
    <div className={styles['code-box']}>
      <pre>{value}</pre>
      <Button type="text" aria-label={label} icon={<Copy size={16} />} onClick={() => copy(value)} />
    </div>
  );
  const command = `claude mcp add --transport http --scope local tier0-uns "${url}"`;
  const codexCommand = `codex mcp add tier0-uns --url "${url}"`;
  const config = JSON.stringify({ mcpServers: { 'tier0-uns': { type: 'http', url } } }, null, 2);
  const revoke = async (grantId: string) => {
    setRevoking(grantId);
    try {
      await revokeMcpOAuthGrant(grantId);
      const value = await getMcpOAuthGrants();
      setGrants(value.list);
      setGrantPage(1);
      message.success(t('grantRevoked'));
    } catch {
      message.error(t('grantRevokeFailed'));
    } finally {
      setRevoking(undefined);
    }
  };
  const localAddress =
    url && /^https?:\/\/(localhost|127\.|0\.0\.0\.0|\[::1\]|10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.)/i.test(url);
  const label = loading
    ? t('checking')
    : failure === 'missing'
      ? t('missing')
      : failure
        ? t('unknown')
        : available
          ? t('available')
          : t('disabled');

  return (
    <section className={styles['mcp-guide']} aria-label={t('title')}>
      <header className={styles['guide-header']}>
        <div>
          <h3>
            <Connect size={20} />
            {t('title')}
          </h3>
          <p>{t('purpose')}</p>
        </div>
        <Space wrap>
          <Tag color={available ? 'success' : loading ? 'default' : 'warning'}>{label}</Tag>
          <Button
            size="small"
            icon={<Renew size={16} />}
            loading={loading}
            onClick={() => refresh()}
            aria-label={t('refresh')}
          />
        </Space>
      </header>
      {failure && (
        <Alert type="warning" showIcon message={failure === 'missing' ? t('missingHelp') : t('unknownHelp')} />
      )}
      {info && !available && <Alert type="warning" showIcon message={t('disabledHelp')} />}
      {info && (
        <div className={styles['endpoint-row']}>
          <div className={styles['endpoint-content']}>
            <span>{t('address')}</span>
            <code>{url}</code>
            <small>{t('endpointStatus', { version: info.productVersion })}</small>
          </div>
          <Button icon={<Copy size={16} />} disabled={!available} onClick={() => copy(url)}>
            {t('copyAddress')}
          </Button>
        </div>
      )}
      {available && (
        <>
          {!canRead && <Alert type="warning" showIcon message={t('noRead')} />}
          {!oauthAvailable && <Alert type="warning" showIcon message={t('oauthUnavailable')} />}
          {oauthAvailable && (
            <>
              <Tabs
                activeKey={tab}
                onChange={setTab}
                items={[
                  {
                    key: 'codex',
                    label: 'Codex',
                    children: (
                      <div className={styles['tab-content']}>
                        {code(codexCommand, t('copyCodexCommand'))}
                        <p>{t('codexBrief')}</p>
                        <details className={styles['troubleshooting']}>
                          <summary>{t('connectDetails')}</summary>
                          <p>
                            {t('loginWhenNeeded')} <code>codex mcp login tier0-uns</code>
                          </p>
                          <p>{t('scopeRequestHint')}</p>
                          <p>{t('codexEnvironment')}</p>
                          {localAddress && <p>{t('localOnly')}</p>}
                          <a href="https://developers.openai.com/codex/mcp" target="_blank" rel="noreferrer">
                            {t('officialDocs')}
                          </a>
                        </details>
                      </div>
                    ),
                  },
                  {
                    key: 'claude',
                    label: 'Claude Code',
                    children: (
                      <div className={styles['tab-content']}>
                        {code(command, t('copyCommand'))}
                        <p>{t('claudeBrief')}</p>
                        <details className={styles['troubleshooting']}>
                          <summary>{t('connectDetails')}</summary>
                          <p>{t('scopeRequestHint')}</p>
                          {localAddress && <p>{t('localOnly')}</p>}
                          <a href="https://code.claude.com/docs/en/mcp" target="_blank" rel="noreferrer">
                            {t('officialDocs')}
                          </a>
                        </details>
                      </div>
                    ),
                  },
                  {
                    key: 'generic',
                    label: t('generic'),
                    children: (
                      <div className={styles['tab-content']}>
                        {code(config, t('copyConfig'))}
                        <p>{t('genericBrief')}</p>
                      </div>
                    ),
                  },
                ]}
              />
              <details className={styles['troubleshooting']}>
                <summary>{t('grantsTitle')}</summary>
                <div className={styles['grants']}>
                  <div className={styles['grants-header']}>
                    <Checkbox
                      checked={showHistory}
                      onChange={(event) => {
                        setShowHistory(event.target.checked);
                        setGrantPage(1);
                      }}
                    >
                      {t('showGrantHistory')}
                    </Checkbox>
                  </div>
                  {grantFailure && <Alert type="warning" message={t('grantsFailed')} />}
                  {!grantFailure && visibleGrants.length === 0 && <p>{t('grantsEmpty')}</p>}
                  {shownGrants.map((grant) => (
                    <div key={grant.grantId} className={styles['grant-row']}>
                      <div>
                        <strong>{grant.clientName}</strong>
                        <code>{grant.resource}</code>
                        <small>{t(`access.${mcpGrantPreset(grant.scopes)}`)}</small>
                        <small>
                          {t('authorizedAt')} {formatTimestamp(grant.createdAt)} · {grant.grantId.slice(-8)}
                        </small>
                      </div>
                      {grant.revokedAt > 0 ? (
                        <Tag>{t('revoked')}</Tag>
                      ) : grant.expired ? (
                        <Tag>{t('expired')}</Tag>
                      ) : (
                        <Button
                          loading={revoking === grant.grantId}
                          disabled={!!revoking}
                          onClick={() => revoke(grant.grantId)}
                        >
                          {t('revokeGrant')}
                        </Button>
                      )}
                    </div>
                  ))}
                  {visibleGrants.length > 5 && (
                    <Pagination
                      size="small"
                      current={grantPage}
                      pageSize={5}
                      total={visibleGrants.length}
                      showSizeChanger={false}
                      onChange={setGrantPage}
                    />
                  )}
                </div>
              </details>
            </>
          )}
          <details className={styles['troubleshooting']}>
            <summary>{t('apiKeyAdvanced')}</summary>
            <div className={styles['key-help']}>
              <div>
                <strong>{t('keyTitle')}</strong>
                <p>{t('keyHelp')}</p>
              </div>
              <AuthButton
                auth={ButtonPermission['OpenData.addKey']}
                disabled={!canRead}
                onClick={() => onCreateKey(info!.availableScopes)}
              >
                {t('createKey')}
              </AuthButton>
            </div>
            {code('$env:TIER0_API_KEY = "<API_KEY>"', t('copyKeyEnv'))}
            {code(`${codexCommand} --bearer-token-env-var TIER0_API_KEY`, t('copyCodexCommand'))}
            {code(`${command} --header "X-API-Key: <API_KEY>"`, t('copyCommand'))}
            <p>{t('keyStorage')}</p>
          </details>
          <div className={styles['prompts']}>
            <strong>{t('tryTitle')}</strong>
            {['inspect', 'source', 'build'].map((scene) => (
              <details key={scene} className={styles['scenario']}>
                <summary>{t(`scene.${scene}`)}</summary>
                <div className={styles['prompt-row']}>
                  <span>{t(`scenario.${scene}`)}</span>
                  <Button
                    type="text"
                    aria-label={t('copyPrompt')}
                    icon={<Copy size={16} />}
                    onClick={() => copy(t(`scenario.${scene}`))}
                  />
                </div>
              </details>
            ))}
          </div>
          <details className={styles['troubleshooting']}>
            <summary>{t('trouble')}</summary>
            <ul>
              <li>{t('troubleAuth')}</li>
              <li>{t('troublePermission')}</li>
              <li>{t('troubleNetwork')}</li>
              <li>{t('troubleDisabled')}</li>
            </ul>
          </details>
        </>
      )}
    </section>
  );
}
