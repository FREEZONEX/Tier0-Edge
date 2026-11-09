import type { SourceDocument } from '@/apis/core-api/source-stream';

export interface SourceCredentialsDraft {
  kind: 'none' | 'basic' | 'bearer' | 'custom' | 'oauth2';
  username: string;
  secret: string;
  namedSecrets?: { name: string; value: string }[];
  tls?: Partial<Record<'caCertificatePem' | 'clientCertificatePem' | 'clientPrivateKeyPem', string>>;
}
export const EMPTY_SOURCE_CREDENTIALS: SourceCredentialsDraft = { kind: 'none', username: '', secret: '' };

export function credentialKindForDocument(
  document: SourceDocument,
  fallback: SourceCredentialsDraft['kind'] = 'none'
): SourceCredentialsDraft['kind'] {
  if (document.http?.oauth) return 'oauth2';
  if (document.http?.authentication)
    return document.http.authentication === 'http' ? 'custom' : document.http.authentication;
  if (document.protocol === 'http-api') return document.role === 'server' ? 'bearer' : 'none';
  return document.credentialRef ? 'basic' : fallback;
}

/** Empty inputs never invent values for the public names returned by the API. */
export function namedSecretValues(draft: SourceCredentialsDraft): Record<string, string> | undefined {
  if ((draft.namedSecrets?.length ?? 0) > 128) return;
  const values: Record<string, string> = Object.create(null);
  for (const item of draft.namedSecrets ?? []) {
    if (
      !item.name.trim() ||
      new TextEncoder().encode(item.name).length > 128 ||
      ['\u0000', '\r', '\n'].some((character) => item.name.includes(character)) ||
      !item.value ||
      new TextEncoder().encode(item.value).length > 65536 ||
      item.name in values
    )
      return;
    values[item.name] = item.value;
  }
  for (const [name, value] of Object.entries(draft.tls ?? {})) {
    if (!value) continue;
    if (name in values || new TextEncoder().encode(value).length > 65536) return;
    values[name] = value;
  }
  if (Boolean(values.clientCertificatePem) !== Boolean(values.clientPrivateKeyPem)) return;
  if (Object.keys(values).length > 128) return;
  return values;
}

export function supportsTLSCredentials(document: SourceDocument) {
  if (document.role === 'server') return false;
  if (document.protocol === 'opcua') return !!document.opcua && document.opcua.securityMode !== 'None';
  if (document.protocol === 'mqtt') return document.endpoint.startsWith('mqtts://');
  if (document.protocol === 'http-api' || document.protocol === 'file-read')
    return document.endpoint.startsWith('https://');
  if (document.protocol === 'edge-sql') {
    const sql = document.sql as { tlsMode?: string } | undefined;
    return !!sql?.tlsMode && sql.tlsMode !== 'disable';
  }
  return false;
}

export function credentialPayload(document: SourceDocument, draft: SourceCredentialsDraft) {
  const values = namedSecretValues(draft);
  if (!values) return;
  if (Object.values(draft.tls ?? {}).some(Boolean) && !supportsTLSCredentials(document)) return;
  if (document.protocol === 'http-api' && document.role === 'server' && !document.http?.anonymous) {
    if (draft.secret && values.token && values.token !== draft.secret) return;
    if (draft.secret) values.token = draft.secret;
    if (!values.token) return;
    return { kind: 'custom' as const, values };
  }
  if (draft.kind === 'none') return Object.keys(values).length ? { kind: 'custom' as const, values } : undefined;
  if (draft.kind === 'custom') return Object.keys(values).length ? { kind: 'custom' as const, values } : undefined;
  if (draft.kind === 'basic' && (!draft.username.trim() || !draft.secret)) return;
  if (draft.kind === 'bearer' && !draft.secret) return;
  if (draft.kind === 'oauth2' && document.http?.oauth?.clientAuthentication !== 'none' && !draft.secret) return;
  return {
    kind: draft.kind,
    username: draft.username,
    secret: draft.secret,
    ...(Object.keys(values).length ? { values } : {}),
  };
}

export const credentialDraftEntered = (draft: SourceCredentialsDraft) =>
  Boolean(draft.username || draft.secret || draft.namedSecrets?.length || Object.values(draft.tls ?? {}).some(Boolean));
