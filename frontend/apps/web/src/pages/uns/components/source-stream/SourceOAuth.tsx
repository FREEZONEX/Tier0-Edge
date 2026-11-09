import { useEffect, useRef } from 'react';
import { App } from 'antd';
import { useLocation, useNavigate } from 'react-router';
import { sourceStreamApi } from '@/apis/core-api/source-stream';
import { useTranslate } from '@/hooks';

import { fleetNativeSources } from '@/apis/core-api/fleet-native-sources';
const key = 'tier0-source-oauth';
export async function authorizeSource(sourceId: number, api = sourceStreamApi, nodeId?: string) {
  const result = await api.oauthStart(sourceId, window.location.origin + '/uns');
  // The browser tab must have initiated this authorization, even when the
  // callback is opened by another tab logged in as the same user.
  sessionStorage.setItem(key, JSON.stringify({ sourceId, state: result.state, nodeId }));
  window.location.assign(result.authorizationUrl);
}

export default function SourceOAuthCallback() {
  const location = useLocation();
  const navigate = useNavigate();
  const { message } = App.useApp();
  const t = useTranslate();
  const started = useRef(false);
  useEffect(() => {
    const params = new URLSearchParams(location.search);
    if (started.current || (!params.has('code') && !params.has('error'))) return;
    started.current = true;
    const stored = sessionStorage.getItem(key);
    sessionStorage.removeItem(key);
    let pending: { sourceId: number; state: string; nodeId?: string } | undefined;
    try {
      pending = stored ? JSON.parse(stored) : undefined;
    } catch {
      /* Treat invalid state as unsolicited. */
    }
    // Remove the authorization code before loading other page content.
    navigate('/uns?view=source', { replace: true });
    if (!pending || pending.state !== params.get('state') || !Number.isSafeInteger(pending.sourceId)) {
      message.error(t('SourceStream.oauthStateInvalid'));
      return;
    }
    const destination = pending.nodeId
      ? `/edge-connection/nodes/${encodeURIComponent(pending.nodeId)}`
      : `/connect?view=source&sourceId=${pending.sourceId}&tab=connection`;
    if (params.has('error')) {
      message.error(t('SourceStream.oauth.cancelled'));
      navigate(destination, { replace: true });
      return;
    }
    (pending.nodeId ? fleetNativeSources(pending.nodeId).api : sourceStreamApi)
      .oauthComplete(pending.sourceId, pending.state, params.get('code') || '')
      .then(() => message.success(t('SourceStream.oauth.authorized')))
      .catch(() => {
        /* API adapter displays the safe backend error. */
      })
      .finally(() => navigate(destination, { replace: true }));
  }, [location.search, message, navigate, t]);
  return null;
}
