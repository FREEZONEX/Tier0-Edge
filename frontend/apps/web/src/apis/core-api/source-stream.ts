import { coreApi } from './core-adapter';
import { ApiWrapper, type AxiosWrapperRequestConfig } from '@/utils/request';
const sourceDocumentApi = new ApiWrapper('/api/core/v2/uns/sources/documents');
const sourceCheckApi = new ApiWrapper('/api/core/v2/uns/sources');
export interface SourceCheckResult { mode: 'connection' | 'data'; health: 'healthy' | 'unhealthy'; stage: string; errorCode: string; message: string; checkedAt: number; latencyMs: number }

export type Protocol =
  'mqtt' | 'opcua' | 'modbus-tcp' | 'usb-hid' | 'http-api' | 'oauth2' | 'edge-sql' | 'file-read' | 'open-protocol';
export type AccessModel = 'publish' | 'subscribe' | 'request-reply';
export type JsonObject = Record<string, unknown>;
export interface OPCUAServerNode extends JsonObject {
  id: string;
  name: string;
  nodeClass: 'Variable' | 'Method';
  dataType: 'Double' | 'Int32' | 'Boolean' | 'String';
  direction: 'ingress' | 'egress' | 'bidirectional';
}
export interface SourceDocument extends JsonObject {
  sourceType?: string;
  name: string;
  description: string;
  protocol: Protocol;
  role?: 'client' | 'server';
  endpoint: string;
  runtimeLocation: 'local';
  credentialRef?: string;
  http?: {
    authentication?: 'none' | 'basic' | 'bearer' | 'custom' | 'http';
    caCertificate?: string;
    parameters?: JsonObject;
    headers?: Record<string, string>;
    port?: number;
    anonymous?: boolean;
    oauth?: {
      grantType?: 'authorization_code' | 'client_credentials';
      authorizationUrl: string;
      tokenUrl: string;
      clientId: string;
      clientAuthentication: 'none' | 'client_secret_basic' | 'client_secret_post';
      scopes: string;
    };
  };
  mqtt?: {
    clientId: string;
    qos?: number;
    topicFilters?: string[];
    requestTopic?: string;
    replyTopic?: string;
    replyTopicField?: string;
    correlationField?: string;
    serverSubscribeTopics?: string[];
  };
  opcua?: {
    securityMode: string;
    securityPolicy: string;
    serverCertificate?: string;
    samplingIntervalMs?: number;
    nodeIds?: string[];
    serverNodes?: OPCUAServerNode[];
  };
  modbus?: {
    unitId: number;
    pollIntervalMs?: number;
    ranges?: { id: string; table: string; address: number; quantity: number; unitId?: number; writable?: boolean }[];
  };
}
export interface NativeResource {
  id: string;
  name: string;
  models: AccessModel[];
  schema?: unknown;
  metadata?: JsonObject;
}
export interface Source {
  oauthStatus?: 'required' | 'authorized';
  credentialKeys?: string[];
  httpServer?: {
    port: number;
    state: 'stopped' | 'starting' | 'listening' | 'failed';
    message?: string;
    url?: string;
  };
  id: number;
  name: string;
  protocol: Protocol;
  revision: number;
  configRevision: number;
  health: 'unverified' | 'healthy' | 'unhealthy';
  healthMessage: string;
  connectionCheck?: SourceCheckResult;
  dataCheck?: SourceCheckResult;
  resources: NativeResource[];
  document: SourceDocument;
  lastDataAt: number;
  checkedAt: number;
  createdAt: number;
  updatedAt: number;
  mqttServer?: {
    topicPrefix: string;
    usernamePrefix: string;
    clientIdPrefix: string;
    webSocketPath: string;
  };
  opcuaServer?: {
    namespaceUri: string;
    nodeIdPrefix: string;
    usernamePrefix: string;
    access: 'private-listener';
  };
}
export interface Script {
  code: string;
  timeoutMs: number;
}
export interface StreamCollection {
  mqtt?: {
    qos: number;
    requestTopic?: string;
    replyTopic?: string;
    replyTopicField?: string;
    correlationField?: string;
  };
  opcua?: { samplingIntervalMs: number };
  modbus?: {
    pollIntervalMs: number;
    ranges: { id: string; table: string; address: number; quantity: number; unitId?: number; writable?: boolean }[];
  };
}
export interface SourceDependency {
  alias: string;
  sourceId: number;
  readResources: string[];
}
export interface ProgramTrigger {
  kind: 'manual' | 'interval' | 'source' | 'uns';
  intervalMs?: number;
  sourceAlias?: string;
  resourceRefs?: string[];
  qos?: number;
  topicIds?: number[];
}
export interface StreamProgram {
  sources: SourceDependency[];
  trigger: ProgramTrigger;
}
export interface StreamDocument extends JsonObject {
  collection?: StreamCollection;
  metadataTarget?: MetadataTarget;
  name: string;
  description: string;
  sourceId: number;
  model: AccessModel;
  nativeResourceRef: string;
  nativeResourceRefs?: string[];
  topicIds: number[];
  script: Script;
  responseScript?: Script;
  program?: StreamProgram;
}
export interface SourceBinding {
  alias: string;
  sourceId: number;
  configRevision: number;
}
export interface ScriptStreamDocument extends JsonObject {
  name: string;
  description: string;
  model: 'script';
  sourceId: 0;
  sources: SourceBinding[];
  script: Script;
  topicIds: [];
  collection?: never;
  metadataTarget?: never;
  nativeResourceRef?: never;
  nativeResourceRefs?: never;
  responseScript?: never;
  program?: never;
}
export interface MetadataTarget {
  topicId: string;
  collectionId: string;
  recordId?: string;
  recordIdPath?: string;
  recordPath?: string;
  sampleEnabled?: boolean;
  samplePath?: string;
  timePath?: string;
  qualityPath?: string;
}
export interface ProgramDocument extends JsonObject {
  name: string;
  description: string;
  script: string;
  timeoutMs: number;
  collection?: never;
  metadataTarget?: never;
  nativeResourceRef?: never;
  nativeResourceRefs?: never;
  topicIds?: never;
  responseScript?: never;
  program?: never;
  sources?: {
    alias: string;
    sourceId: string;
    configRevision: number;
    resources?: string[];
    acquisition?: JsonObject;
  }[];
  sourceBindings?: { sourceId: string; configRevision: number; acquisition?: JsonObject }[];
}
export interface ProgramEntry {
  id: string;
  kind: 'interval' | 'source-event' | 'uns-event' | 'request' | 'http-request';
  intervalMs?: number;
  sourceId?: string;
  resourceRefs?: string[];
  topicIds?: string[];
  qos?: number;
  method?: string;
  path?: string;
  request?: Omit<NonNullable<ProgramEntry['control']>, 'sideEffect' | 'idempotency'> & {
    sideEffect: 'read' | 'write';
    idempotency: 'none' | 'optional' | 'required';
  };
  control?: {
    code: string;
    name: string;
    description?: string;
    contractVersion: string;
    inputSchema: JsonObject;
    outputSchema: JsonObject;
    sideEffect: 'none' | 'write';
    idempotency: 'required';
    timeoutMs: number;
  };
}
export interface ProgramManifest {
  sources: { id: string; resources: string[] }[];
  topics: (({ topicId: string; collectionId?: never } | { collectionId: string; topicId?: never }) & {
    access?: 'read' | 'write' | 'read-write';
    space?: 'metadata' | 'legacy';
  })[];
  entries: ProgramEntry[];
}
export type AnyStreamDocument = StreamDocument | ScriptStreamDocument | ProgramDocument;
export type StreamConfiguration = AnyStreamDocument;
export const isProgramDocument = (document: StreamConfiguration): document is ProgramDocument =>
  typeof document.script === 'string' &&
  Object.keys(document).every((key) =>
    ['name', 'description', 'script', 'timeoutMs', 'extensions', 'sources', 'sourceBindings'].includes(key)
  );

// The code editor keeps its text/timeout controls locally; every request uses
// the common public script object. Native binding declarations remain intact.
function nativeProgramDocument(document: ProgramDocument) {
  const { script, timeoutMs, ...fields } = document;
  return { ...fields, script: { code: script, timeoutMs } };
}
function editorProgramDocument(document: AnyStreamDocument): AnyStreamDocument {
  if (isProgramDocument(document)) return document;
  const value = document as JsonObject;
  const script = value.script;
  if (
    script &&
    typeof script === 'object' &&
    'code' in script &&
    'timeoutMs' in script &&
    typeof script.code === 'string' &&
    typeof script.timeoutMs === 'number' &&
    typeof value.name === 'string' &&
    !('collection' in value) &&
    !('program' in value)
  ) {
    return {
      ...value,
      name: value.name,
      description: typeof value.description === 'string' ? value.description : '',
      script: script.code,
      timeoutMs: script.timeoutMs,
    };
  }
  return document;
}
async function editorStream(response: Promise<Stream>): Promise<Stream> {
  const stream = await response;
  return stream.model === 'program'
    ? {
        ...stream,
        document: editorProgramDocument(stream.document),
        ...(stream.activeDocument ? { activeDocument: editorProgramDocument(stream.activeDocument) } : {}),
      }
    : stream;
}

export type LegacyStream = Omit<Stream, 'document' | 'model'> & { document: StreamDocument; model: AccessModel };
export interface Stream {
  activeDocument?: AnyStreamDocument;
  id: number;
  sourceId: number;
  name: string;
  model: AccessModel | 'script' | 'program';
  revision: number;
  activeVersion: number;
  pendingVersion?: number;
  manifest?: ProgramManifest;
  draftManifest?: ProgramManifest;
  pendingManifest?: ProgramManifest;
  status: 'draft' | 'enabled' | 'disabled';
  executionMode?: 'manual' | 'mqtt-subscription';
  executionState?:
    | 'draft'
    | 'disabled'
    | 'ready'
    | 'starting'
    | 'source-watch-failed'
    | 'needs-redeploy'
    | 'source-disabled'
    | 'source-unhealthy';
  document: StreamConfiguration;
  createdAt: number;
  updatedAt: number;
}
export interface StreamVersion {
  id: number;
  streamId: number;
  sourceRevision: number;
  document: StreamConfiguration;
  manifest?: ProgramManifest;
  activatedAt?: number;
  createdAt: number;
}
export interface StreamRun {
  id: string;
  sourceId: number;
  streamId: number;
  versionId: number;
  model: AccessModel | 'script' | 'program';
  entryId?: string;
  status: string;
  summary: JsonObject;
  messageId: string;
  durationMs: number;
  createdAt: number;
}
export interface Page<T> {
  list: T[];
  total: number;
}
export interface ProtocolDefinition {
  id: Protocol;
  name: string;
  runtime: 'go' | 'node';
  models: AccessModel[];
  document: SourceDocument;
  credentialKinds: string[];
  roles?: ('client' | 'server')[];
}
export interface SourceTypeDefinition {
  id: string;
  protocol: 'http-api' | 'oauth2';
  name: string;
  description: string;
  document: SourceDocument;
  credentialKinds: string[];
  credentialKeys?: string[];
  credentialAnyOf?: string[];
  parameterKeys?: string[];
  configSchema?: JsonObject;
  operations?: { id: string; name?: string; method?: string; path?: string; readOnly?: boolean }[];
}
export interface SourceProtocolCatalog extends Page<ProtocolDefinition> {
  sourceTypes?: SourceTypeDefinition[];
}
export interface SourceImpact {
  sourceId: number;
  count: number;
  streams: Stream[];
  topics: { streamId: number; topicId: number }[];
  impactRevision: string;
}
export interface StreamTestResult {
  input?: { resourceRef: string; data: unknown; receivedAt: number };
  dryRun: true;
  sideEffects: false;
  result: {
    data: unknown;
    outputs: { topicId: number | string; data: unknown }[] | null;
    drop?: boolean;
    manifest?: ProgramManifest;
    logs: { level: string; message: string }[];
    durationMs: number;
    checkpoint?: { revision: number; value: unknown };
  };
  targets:
    | {
        topicId?: number | string;
        collectionId?: string;
        data: unknown;
        preview?: {
          recordId: string;
          mode: string;
          status: 'created' | 'updated' | 'unchanged' | 'error';
          revision: number;
          error?: string;
          changes: { field: string; beforeExists: boolean; afterExists: boolean; before: unknown; after: unknown }[];
        };
      }[]
    | null;
  manifest?: ProgramManifest;
}
export type SourceFilter = {
  search?: string;
  protocol?: string;
  health?: string;
  page?: number;
  size?: number;
};
export type StreamFilter = {
  sourceId?: number;
  topicId?: number;
  search?: string;
  model?: string;
  status?: string;
  page?: number;
  size?: number;
};
export type RunFilter = {
  sourceId?: number;
  streamId?: number;
  topicId?: number;
  model?: string;
  status?: string;
  page?: number;
  size?: number;
};
type SourceMutation = { expectedRevision: number; confirmedImpactRevision?: string };
type StreamMutation = { expectedRevision: number };

export const sourceStreamApi = {
  protocols: () => coreApi.get('/uns/source-protocols') as Promise<SourceProtocolCatalog>,
  sources: (params: SourceFilter = {}, signal?: AbortSignal) =>
    coreApi.get('/uns/sources', { params, signal }) as Promise<Page<Source>>,
  source: (id: number) => coreApi.get(`/uns/sources/${id}`) as Promise<Source>,
  createSource: (document: SourceDocument) =>
    coreApi.post('/uns/sources', { document, expectedRevision: 0 }) as Promise<Source>,
  createSourceDocument: async (
    document: SourceDocument,
    credentials:
      | {
          kind: 'basic' | 'bearer' | 'custom' | 'oauth2';
          username?: string;
          secret?: string;
          values?: Record<string, string>;
        }
      | undefined,
    idempotencyKey: string
  ) => {
    const source = (await sourceDocumentApi.post('/create', {
      document,
      credentials,
      idempotencyKey,
      dryRun: false,
    })) as Omit<Source, 'id'> & { id: string };
    const id = Number(source.id);
    if (!Number.isSafeInteger(id) || id <= 0) throw new Error('Invalid Source identity');
    return { ...source, id };
  },
  updateSource: (id: number, document: SourceDocument, mutation: SourceMutation) =>
    coreApi.put(`/uns/sources/${id}`, { document, ...mutation }) as Promise<Source>,
  deleteSource: (id: number, mutation: SourceMutation) =>
    coreApi.delete(`/uns/sources/${id}`, { data: mutation }) as Promise<{ id: number }>,
  impact: (id: number) => coreApi.get(`/uns/sources/${id}/impact`) as Promise<SourceImpact>,
  probe: (sourceId: number, _document?: SourceDocument, mode: 'connection' | 'data' = 'connection') =>
    sourceCheckApi.post('/probe', { id: String(sourceId), mode, dryRun: false, timeoutMs: 10000 }) as Promise<SourceCheckResult>,
  discover: (sourceId: number, expectedRevision: number, parentResourceRef = '') =>
    coreApi.post(`/uns/sources/${sourceId}/discover`, { expectedRevision, parentResourceRef }) as Promise<
      Page<NativeResource>
    >,
  credentials: (
    id: number,
    input: SourceMutation & {
      kind: 'basic' | 'bearer' | 'oauth2' | 'custom';
      username?: string;
      secret?: string;
      values?: Record<string, string>;
    }
  ) => coreApi.put(`/uns/sources/${id}/credentials`, input) as Promise<Source>,
  oauthStart: (id: number, redirectUri: string) =>
    coreApi.post(`/uns/sources/${id}/oauth/start`, { redirectUri }) as Promise<{
      authorizationUrl: string;
      state: string;
    }>,
  oauthComplete: (id: number, state: string, code: string) =>
    coreApi.post(`/uns/sources/${id}/oauth/complete`, { state, code }) as Promise<Source>,
  streams: (params: StreamFilter = {}, config?: AxiosWrapperRequestConfig | AbortSignal) => {
    const options = config && 'aborted' in config ? { signal: config } : config;
    return (coreApi.get('/uns/streams', { ...options, params }) as Promise<Page<Stream>>).then(async (page) => ({
      ...page,
      list: await Promise.all(page.list.map((stream) => editorStream(Promise.resolve(stream)))),
    }));
  },
  stream: (id: number, config?: AxiosWrapperRequestConfig) =>
    editorStream(coreApi.get(`/uns/streams/${id}`, config) as Promise<Stream>),
  createStream: (document: ProgramDocument) =>
    editorStream(
      coreApi.post('/uns/streams', {
        document: nativeProgramDocument(document),
        expectedRevision: 0,
      }) as Promise<Stream>
    ),
  updateStream: (id: number, document: ProgramDocument, mutation: StreamMutation) =>
    editorStream(
      coreApi.put(`/uns/streams/${id}`, { document: nativeProgramDocument(document), ...mutation }) as Promise<Stream>
    ),
  deleteStream: (id: number, mutation: StreamMutation) =>
    coreApi.delete(`/uns/streams/${id}`, { data: mutation }) as Promise<{ id: number }>,
  validate: (document: ProgramDocument, expectedSourceRevision: number) =>
    coreApi.post('/uns/streams/validate', {
      document: nativeProgramDocument(document),
      expectedSourceRevision,
    }) as Promise<{
      valid: true;
      sideEffects: false;
      manifest?: ProgramManifest;
    }>,
  test: (
    document: ProgramDocument,
    input: unknown,
    expectedSourceRevision: number,
    phase: 'input' | 'request' | 'response' | 'fetch',
    entryId?: string
  ) =>
    coreApi.post(
      '/uns/streams/test',
      { document: nativeProgramDocument(document), input, expectedSourceRevision, phase, entryId },
      { timeout: 60000 }
    ) as Promise<StreamTestResult>,
  sample: (document: AnyStreamDocument, expectedSourceRevision: number) =>
    coreApi.post('/uns/streams/test', { document, expectedSourceRevision, input: {}, phase: 'sample' }) as Promise<{
      input: { resourceRef: string; data: unknown; receivedAt: number };
    }>,
  deploy: (id: number, expectedRevision: number, expectedSourceRevision: number) =>
    editorStream(
      coreApi.post(`/uns/streams/${id}/deploy`, { expectedRevision, expectedSourceRevision }) as Promise<Stream>
    ),
  disable: (id: number, mutation: StreamMutation) =>
    editorStream(coreApi.post(`/uns/streams/${id}/disable`, mutation) as Promise<Stream>),
  versions: (id: number, page = 1) =>
    coreApi.get(`/uns/streams/${id}/versions`, { params: { page, size: 20 } }) as Promise<Page<StreamVersion>>,
  rollback: (id: number, versionId: number, expectedRevision: number, expectedSourceRevision: number) =>
    editorStream(
      coreApi.post(`/uns/streams/${id}/versions/${versionId}/activate`, {
        expectedRevision,
        expectedSourceRevision,
      }) as Promise<Stream>
    ),
  invoke: (id: number, expectedActiveVersion: number, input: unknown, idempotencyKey: string, entryId?: string) =>
    coreApi.post(`/uns/streams/${id}/invoke`, {
      expectedActiveVersion,
      input,
      idempotencyKey,
      entryId,
      timeoutMs: 30000,
    }) as Promise<JsonObject>,
  runs: (params: RunFilter = {}) => coreApi.get('/uns/stream-runs', { params }) as Promise<Page<StreamRun>>,
  run: (id: string) => coreApi.get(`/uns/stream-runs/${encodeURIComponent(id)}`) as Promise<StreamRun>,
  relations: (topicId: number) => coreApi.get(`/uns/topics/${topicId}/streams`) as Promise<Page<Stream>>,
};
