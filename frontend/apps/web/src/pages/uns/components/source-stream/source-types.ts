import type { SourceDocument, SourceTypeDefinition } from '@/apis/core-api/source-stream';
import type { SourceCredentialsDraft } from './source-credentials';

export const vendorSourceTypes = [
  'pi-web-api',
  'plex-mes',
  'wms-rest',
  'sap-odata',
  'yonyou-yonbip',
  'kingdee-jdy',
  'ngp',
  'kingdee-k3cloud',
  'salesforce',
  'teamcenter',
  'hikvision-isapi',
] as const;

/** Vendor profiles are client-only; generic protocol documents remain unchanged. */
export function validSourceType(document: SourceDocument): boolean {
  if (document.sourceType === undefined) return true;
  return (
    typeof document.sourceType === 'string' &&
    vendorSourceTypes.some((id) => id === document.sourceType) &&
    ['http-api', 'oauth2'].includes(document.protocol) &&
    document.role !== 'server'
  );
}

export function sourceTypeLabelKey(document: Pick<SourceDocument, 'sourceType' | 'protocol' | 'role'>): string {
  return document.sourceType && vendorSourceTypes.some((id) => id === document.sourceType)
    ? `SourceStream.vendorName.${document.sourceType}`
    : document.protocol === 'http-api'
      ? `SourceStream.catalogName.http-${document.role || 'client'}`
      : `SourceStream.catalogName.${document.protocol}`;
}

export function vendorParameterKeys(profile: SourceTypeDefinition): string[] {
  const schema = profile.configSchema as
    | {
        properties?: { http?: { properties?: { parameters?: { properties?: Record<string, unknown> } } } };
      }
    | undefined;
  return (
    profile.parameterKeys ||
    Object.keys(schema?.properties?.http?.properties?.parameters?.properties ?? profile.document.http?.parameters ?? {})
  );
}

/** Keep the visible draft; omit only unused alternatives in a declared OR group. */
export function vendorCredentialDraft(profile: SourceTypeDefinition | undefined, draft: SourceCredentialsDraft) {
  const alternatives = profile?.credentialAnyOf || [];
  if (!alternatives.some((name) => draft.namedSecrets?.some((item) => item.name === name && item.value))) return draft;
  return {
    ...draft,
    namedSecrets: draft.namedSecrets?.filter((item) => !alternatives.includes(item.name) || Boolean(item.value)),
  };
}
