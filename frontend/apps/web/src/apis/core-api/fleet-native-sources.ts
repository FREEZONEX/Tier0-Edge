import { v4 as uuid } from 'uuid';
import { coreApi } from './core-adapter';
import { sourceStreamApi } from './source-stream';
import { metadataApi, type UnsMetadata } from './uns-metadata';
import type { AxiosWrapperRequestConfig } from '@/utils/request';
import { hasPermission } from '@/utils/auth';
import { ButtonPermission } from '@/common-types/button-permission';

// Normalize only native resource envelopes. Protocol resource names and script
// manifests remain strings even when their contents look numeric.
function nativeView<T>(value: T): T {
  if (!value || typeof value !== 'object') return value;
  const row = value as Record<string, unknown>;
  for (const key of ['id', 'sourceId', 'streamId', 'versionId', 'activeVersion', 'pendingVersion']) {
    if (typeof row[key] === 'string' && /^(0|[1-9][0-9]*)$/.test(row[key] as string)) {
      const n = Number(row[key]);
      if (!Number.isSafeInteger(n)) throw new Error('Unsafe native resource identity');
      row[key] = n;
    }
  }
  if (Array.isArray(row.list)) row.list = row.list.map(nativeView);
  return value;
}
export function fleetNativeSources(nodeId: string) {
  const call = async <T>(operation: string, data: object, options?: AxiosWrapperRequestConfig) =>
    nativeView((await coreApi.post('/v2/fleet/runtime/execute', { nodeId, operation, data }, options)) as T);
  const write = <T>(operation: string, data: object) =>
    call<T>(operation, { dryRun: false, idempotencyKey: uuid(), ...data }, { timeout: 60000 });
  const id = (value?: number) => (value ? String(value) : undefined);
  const fullSource = () => hasPermission(ButtonPermission['uns.sourceManage']);
  const fullStream = () => hasPermission(ButtonPermission['uns.streamManage']);
  const api: typeof sourceStreamApi = {
    protocols: () => call('sourceProtocolsList', {}),
    sources: (params = {}, signal) =>
      call('sourceDocumentList', { ...params, includeDocument: fullSource() }, { signal }),
    source: (sourceId) => call('sourceDocumentGet', { id: id(sourceId), view: fullSource() ? 'full' : 'summary' }),
    createSource: (document) => write('sourceDocumentCreate', { document }),
    createSourceDocument: (document, credentials, idempotencyKey) =>
      write('sourceDocumentCreate', { document, credentials, idempotencyKey }),
    updateSource: (sourceId, document, mutation) =>
      write('sourceDocumentUpdate', { id: id(sourceId), document, ...mutation }),
    deleteSource: (sourceId, mutation) => write('sourceDocumentDelete', { id: id(sourceId), ...mutation }),
    impact: async (sourceId) => {
      const source = await api.source(sourceId);
      return call('sourceImpact', { id: id(sourceId), expectedRevision: source.revision });
    },
    probe: (sourceId, _document, mode = 'connection') => write('sourceProbe', { id: id(sourceId), mode, timeoutMs: 10000 }),
    discover: (sourceId, expectedRevision, parentResourceRef = '') =>
      write('sourceDiscover', { id: id(sourceId), expectedRevision, parentResourceRef }),
    credentials: (sourceId, input) => {
      const { expectedRevision, confirmedImpactRevision, ...credentials } = input;
      return write('sourceNativeCredentialsUpdate', {
        id: id(sourceId),
        expectedRevision,
        confirmedImpactRevision,
        credentials,
      });
    },
    oauthStart: (sourceId, redirectUri) => write('sourceNativeOAuthStart', { id: id(sourceId), redirectUri }),
    oauthComplete: (sourceId, state, code) => write('sourceNativeOAuthComplete', { id: id(sourceId), state, code }),
    streams: (params = {}, options) =>
      call(
        'streamProgramList',
        { ...params, sourceId: id(params.sourceId), includeDocument: fullStream() },
        options instanceof AbortSignal ? { signal: options } : options
      ),
    stream: (streamId, options) =>
      call('streamDocumentGet', { id: id(streamId), view: fullStream() ? 'full' : 'summary' }, options),
    createStream: (document) => write('streamDocumentCreate', { document }),
    updateStream: (streamId, document, mutation) =>
      write('streamDocumentUpdate', { id: id(streamId), document, ...mutation }),
    deleteStream: (streamId, mutation) => write('streamProgramDelete', { id: id(streamId), ...mutation }),
    validate: (document, expectedSourceRevision) =>
      call('streamDocumentValidate', { document, expectedSourceRevision }),
    test: (document, input, expectedSourceRevision, phase, entryId) =>
      write('streamProgramTest', { document, input, expectedSourceRevision, phase, entryId }),
    sample: () => Promise.reject(new Error('Legacy sample preview is unavailable for native programs')),
    deploy: (streamId, expectedRevision, expectedSourceRevision) =>
      write('streamProgramDeploy', { id: id(streamId), expectedRevision, expectedSourceRevision }),
    disable: (streamId, mutation) => write('streamDocumentDisable', { id: id(streamId), ...mutation }),
    versions: (streamId, page = 1) => call('streamProgramVersionsList', { id: id(streamId), page, size: 20 }),
    rollback: (streamId, versionId, expectedRevision, expectedSourceRevision) =>
      write('streamProgramRollback', {
        id: id(streamId),
        versionId: id(versionId),
        expectedRevision,
        expectedSourceRevision,
      }),
    invoke: (streamId, expectedActiveVersion, input, idempotencyKey, entryId) =>
      write('streamProgramInvoke', {
        id: id(streamId),
        expectedActiveVersion: id(expectedActiveVersion),
        input,
        idempotencyKey,
        entryId,
        timeoutMs: 30000,
      }),
    runs: (params = {}) => {
      const { streamId, sourceId, topicId, ...rest } = params;
      return call('streamRunsList', { ...rest, id: id(streamId), sourceId: id(sourceId), topicId: id(topicId) });
    },
    run: (runId) => call('streamRunGet', { runId }),
    relations: () => Promise.reject(new Error('Legacy Topic relations are unavailable in Fleet')),
  };
  const loadMetadata = async () => {
    const rows: UnsMetadata[] = [];
    for (let page = 1; ; page++) {
      const result = await call<{ items: UnsMetadata[]; pageInfo: { hasMore: boolean } }>('metadataFind', {
        kinds: ['collection', 'topic', 'directory'],
        state: 'active',
        page: { mode: 'offset', page, size: 200 },
      });
      rows.push(...result.items);
      if (!result.pageInfo.hasMore) return rows;
    }
  };
  const findMetadata: typeof metadataApi.find = async (kind, identity, archived = false, signal) => {
    const result = await call<{ items: UnsMetadata[] }>(
      'metadataFind',
      {
        kind: kind === 'folder' ? 'directory' : kind,
        filter: { _id: identity },
        limit: 1,
        state: archived ? 'deleted' : 'active',
      },
      { signal }
    );
    return result.items[0];
  };
  return { api, nodeId, loadMetadata, findMetadata, readOnly: false };
}
