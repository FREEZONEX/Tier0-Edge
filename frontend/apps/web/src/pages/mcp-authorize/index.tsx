import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router';
import { Alert, Button, Radio, Spin } from 'antd';
import { Connect } from '@carbon/icons-react';
import HelpTooltip from '@/components/help-tooltip';
import { getMcpOAuthRequest, decideMcpOAuth, type McpOAuthRequest } from '@/apis/core-api/mcp';
import { LOGIN_URL } from '@/common-types/constans';
import { getToken } from '@/utils/auth';
import { useTranslate } from '@/hooks';
import { mcpPresetScopes, type McpAccessPreset } from '@/utils/mcp-scopes';
import styles from './index.module.scss';

export default function McpAuthorizePage() {
  const t = useTranslate('MCPOAuth');
  const [params] = useSearchParams();
  const requestId = params.get('requestId') || '';
  const [request, setRequest] = useState<McpOAuthRequest>();
  const [preset, setPreset] = useState<McpAccessPreset>('read');
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [expired, setExpired] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    const controller = new AbortController();
    if (!getToken()) {
      window.location.replace(
        `${LOGIN_URL}?redirectUri=${encodeURIComponent(window.location.pathname + window.location.search)}`
      );
      return () => controller.abort();
    }
    const prepare = async () => {
      try {
        const value = await getMcpOAuthRequest(requestId, controller.signal);
        if (!controller.signal.aborted) {
          setRequest(value);
          setPreset('read');
          setExpired(false);
        }
      } catch (err) {
        if (controller.signal.aborted) return;
        if ((err as { response?: { status?: number } }).response?.status === 401) {
          window.location.replace(
            `${LOGIN_URL}?redirectUri=${encodeURIComponent(window.location.pathname + window.location.search)}`
          );
          return;
        }
        setError(t('invalidRequest'));
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    };
    void prepare();
    return () => controller.abort();
  }, [requestId, t]);

  useEffect(() => {
    if (!request) return;
    const timer = window.setTimeout(() => setExpired(true), Math.max(0, request.expiresAt - Date.now()));
    return () => window.clearTimeout(timer);
  }, [request]);

  const selected = request ? mcpPresetScopes(preset, request.requestedScopes, request.availableScopes) : [];
  const writeScopes = request ? mcpPresetScopes('write', request.requestedScopes, request.availableScopes) : [];
  const readScopes = request ? mcpPresetScopes('read', request.requestedScopes, request.availableScopes) : [];
  const canWrite = writeScopes.length > readScopes.length;
  const requestedWrite = request
    ? mcpPresetScopes('write', request.requestedScopes, request.requestedScopes).length >
      mcpPresetScopes('read', request.requestedScopes, request.requestedScopes).length
    : false;
  const canDelete = !!request?.requestedScopes.includes('uns.delete') && request.availableScopes.includes('uns.delete');

  const decide = async (allow: boolean) => {
    if (!request || submitting) return;
    setSubmitting(true);
    setError('');
    try {
      const value = await decideMcpOAuth(requestId, allow, selected);
      const redirect = new URL(value.redirectUri);
      const expected = new URL(request.redirectUri);
      // 服务端验证并签发的 callback 才可打开，不使用页面查询参数当跳转目标。
      if (redirect.origin !== expected.origin || redirect.pathname !== expected.pathname)
        throw new Error('callback mismatch');
      window.location.assign(redirect.href);
    } catch {
      setError(t('decisionFailed'));
      setSubmitting(false);
    }
  };

  return (
    <main className={styles.page}>
      <section className={styles.card} aria-label={t('title')}>
        <header>
          <Connect size={28} />
          <span>Tier0 · MCP</span>
        </header>
        <h1>{t('title')}</h1>
        {loading && <Spin aria-label={t('loading')} />}
        {error && <Alert type="error" showIcon message={error} />}
        {request && (
          <>
            <dl>
              <dt>
                {t('client')} <HelpTooltip title={t('unverified')} />
              </dt>
              <dd>{request.clientName}</dd>
              <dt>{t('instance')}</dt>
              <dd>
                <code>{request.resource}</code>
              </dd>
              <dt>{t('account')}</dt>
              <dd>{request.userName}</dd>
            </dl>
            <h2>
              {t('scopeTitle')} <HelpTooltip title={t('presetHelp')} />
            </h2>
            <Radio.Group
              className={styles.scopes}
              value={preset}
              disabled={submitting}
              onChange={(event) => setPreset(event.target.value)}
              optionType="button"
              buttonStyle="solid"
              options={[
                { value: 'read', label: t('preset.read') },
                { value: 'write', label: t('preset.write'), disabled: !canWrite },
                { value: 'all', label: t('preset.all'), disabled: !canDelete },
              ]}
            />
            <p>{t(`presetDesc.${preset}`)}</p>
            {preset === 'all' && canDelete && <Alert type="warning" showIcon message={t('deleteIncluded')} />}
            {!canWrite && <p className={styles.hint}>{t(requestedWrite ? 'accountReadOnly' : 'requestReadOnly')}</p>}
            <details className={styles.details}>
              <summary>{t('scopeDetails')}</summary>
              <ul>
                {selected.map((scope) => (
                  <li key={scope}>
                    {t(`scope.${scope}`)} <code>{scope}</code>
                  </li>
                ))}
              </ul>
              <p>
                {t('callback')} · <code>{request.redirectUri}</code>
              </p>
              {request.requestedScopes.includes('offline_access') && <p>{t('refreshBrief')}</p>}
            </details>
            {!request.availableScopes.includes('uns.read') && <Alert type="warning" showIcon message={t('noRead')} />}
            <div className={styles.actions}>
              <Button onClick={() => decide(false)} disabled={submitting}>
                {t('deny')}
              </Button>
              <Button
                type="primary"
                onClick={() => decide(true)}
                loading={submitting}
                disabled={!request.availableScopes.includes('uns.read') || expired}
              >
                {t('allow')}
              </Button>
            </div>
          </>
        )}
      </section>
    </main>
  );
}
