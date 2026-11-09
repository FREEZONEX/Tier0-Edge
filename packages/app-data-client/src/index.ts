import { validateCapabilityDiscovery, CapabilityError, type CapabilityDiscoverResponse } from './capabilities.ts';
export * from './capabilities.ts';
/** Server-side data access for Tier0 Apps. No SQL, database connection or migrations. */
export interface DataSchema {
  type: string;
  properties?: Record<string, DataSchema>;
  required?: string[];
  items?: DataSchema;
  [keyword: string]: unknown;
}
export type Grant = 'record:read' | 'record:write' | 'record:delete' | 'sample:read' | 'sample:write';
export type PlatformPermission = 'openapi.base' | 'platform.users.read' | 'platform.projects.members.read' | 'notifications.send' | 'notifications.read' | 'asset.file.upload' | 'asset.file.download' | 'asset.file.delete' | 'control.read' | 'control.execute' | 'control.write' | 'stream.requests.read' | 'stream.requests.invoke' | 'stream.requests.write';
export interface AppContext {
  appId: string;
  name: string;
  revision: number;
  /** Current App capabilities intersected with this API key's scope. */
  platformPermissions?: PlatformPermission[];
  collections: {alias: string; collectionId: string; permissions: Grant[]; recordSchema: DataSchema; sampleSchema?: DataSchema; indexes?: IndexSpec[]}[];
}
export interface RecordRow<T> {
  id: string;
  collectionId: string;
  data: T;
  revision: number;
  createdAt: number;
  updatedAt: number;
  sourceId: string | null;
  sourceTime: number | null;
  lock?: {label: string; expiresAt: number; fence: number; since: number};
}
export interface SampleRow<T> {
  id: string;
  collectionId: string;
  recordId: string;
  time: number;
  receivedAt: number;
  quality: string;
  data: T;
  sourceId: string | null;
  streamId?: string;
  bindingId?: string;
}
export interface Page<T> { list: T[]; total: number; page: number; size: number }
export interface PlatformIdentity { userId: string; userName: string; authType: string; apiKeyId: string; appId?: string; capabilities: string[] }
export interface PlatformInfo { name: string; version: string; apiVersion: 'v2'; capabilities: string[] }
export interface PlatformRole { roleId: string; roleKey: string; roleName?: string; description?: string }
export interface PlatformUser { userId: string; userName?: string; nickName?: string; email?: string; status: string; roles: PlatformRole[]; updatedAt: string }
export interface PlatformUsersQuery { search?: string; page?: number; size?: number; roleKeys?: string[]; statuses?: ('active' | 'disabled')[]; updatedAtStart?: string; updatedAtEnd?: string }
export interface ProjectMember { memberId: string; projectId: string; userId: string; userName?: string; email?: string; status: string; roles: PlatformRole[] }
export interface NotificationInput { recipientUserId: string; title: string; content: string; idempotencyKey: string; type?: 'inbox'; mode?: 'live' | 'test'; link?: string; channels?: ('web' | 'mobile')[] }
export interface NotificationReceipt { messageId: string; status: string; createdAt: string }
export interface NotificationStatus extends NotificationReceipt { type: string; completedAt?: string; errorCode?: string; message?: string }
export interface FileUploadInput { fileName: string; contentType: string; sizeBytes: number; visibility?: 'private' | 'public'; sessionId?: string }
/** JSON control-plane response. Send the file directly to upload.url without the API key. */
export interface FileUpload { fileId: string; filePath: string; upload: {method: 'POST'; url: string; fields: Record<string, string>; fileField: 'file'}; expiresAt: number; sizeBytes: number; visibility: 'private' | 'public' }
export interface FileDownload { filePath: string; url: string; expiresAt: number }
export interface MultipartSession { filePath: string; uploadId: string }
export interface MultipartUpload extends MultipartSession { partSizeBytes: number; partCount: number; expiresAt: number }
export interface CompletedPart { partNumber: number; etag: string }
export interface CompletedUpload { fileId: string; filePath: string; url: string; sizeBytes: number; expiresAt: number; status: 'completed' }
/** Bare business fields or _id / _meta.*; resource responses keep business values under data. */
export type DataFilter = Record<string, unknown>;
export type ExpressionScalar = string | number | boolean | null;
export type PipelineExpression = ExpressionScalar
  | {$literal: ExpressionScalar}
  | { [K in '$ifNull' | '$add' | '$subtract' | '$multiply' | '$divide' | '$eq' | '$ne' | '$gt' | '$gte' | '$lt' | '$lte']: {[P in K]: [PipelineExpression, PipelineExpression]} }['$ifNull' | '$add' | '$subtract' | '$multiply' | '$divide' | '$eq' | '$ne' | '$gt' | '$gte' | '$lt' | '$lte']
  | {$and: PipelineExpression[]} | {$or: PipelineExpression[]} | {$not: [PipelineExpression]}
  | {$cond: [PipelineExpression, PipelineExpression, PipelineExpression] | {if: PipelineExpression; then: PipelineExpression; else: PipelineExpression}}
  | {$concat: PipelineExpression[]}
  | { [K in '$toLower' | '$toUpper' | '$strLenCP' | '$toString' | '$toDecimal' | '$toLong' | '$toBool']: {[P in K]: PipelineExpression} }['$toLower' | '$toUpper' | '$strLenCP' | '$toString' | '$toDecimal' | '$toLong' | '$toBool']
  | {$trim: {input: PipelineExpression; chars?: PipelineExpression}}
  | {$convert: {input: PipelineExpression; to: 'string' | 'decimal' | 'long' | 'bool'}}
  | {$dateTrunc: {date: PipelineExpression; unit: 'millisecond' | 'second' | 'minute' | 'hour' | 'day' | 'week' | 'month' | 'quarter' | 'year'; timezone?: string; binSize?: 1; startOfWeek?: 'sunday' | 'monday' | 'tuesday' | 'wednesday' | 'thursday' | 'friday' | 'saturday'}};
export type QuerySort = [field: string, direction: 1 | -1];
/** Bounded server-validated Mongo-style stages, never arbitrary SQL or JavaScript. */
export type PipelineStage =
  | {$match: DataFilter} | {$project: Record<string, unknown>} | {$set: Record<string, unknown>}
  | {$sort: QuerySort[]} | {$group: Record<string, unknown>}
  | {$distinct: {by: Record<string, `$${string}`>}} | {$frequencies: {by: Record<string, `$${string}`>}}
  | {$lookup: {collectionId: string; kind?: 'records' | 'samples'; localField: string; foreignField: string; as: string; pipeline?: PipelineStage[]}}
  | {$unwind: string | {path: string; preserveNullAndEmptyArrays?: boolean}}
  | {$skip: number} | {$limit: number} | {$count: string} | {$exists: string};
export type QueryPage = {size?: number} & (
  {mode?: 'offset'; page?: number; cursor?: never}
  | {mode: 'cursor'; cursor?: string; page?: never}
);
export type UpdateDocument = Partial<Record<'$set'|'$unset'|'$inc'|'$mul'|'$min'|'$max'|'$rename'|'$currentDate'|'$setOnInsert'|'$push'|'$addToSet'|'$pop'|'$pull'|'$pullAll',Record<string,unknown>>>;
export interface FindOptions {filter?:DataFilter; sort?:QuerySort[]; projection?:Record<string,0|1|boolean>; limit?:number;cursor?:string}
export interface Lease {id:string;token:string}
export interface LeaseAssertion extends Lease {collectionId:string}
export type LockOptions = ({ids:string[];filter?:never;sort?:never;limit?:never}|{ids?:never;filter:DataFilter;sort?:QuerySort[];limit:number}) & {ttlMs?:number;label?:string};
export interface DocumentWriteOptions {expectedRevision?:number; upsert?:boolean;arrayFilters?:DataFilter[];sort?:QuerySort[];projection?:Record<string,0|1|boolean>;returnDocument?:'before'|'after';dryRun?:boolean;idempotencyKey?:string;lockToken?:string;release?:boolean;assertLocks?:LeaseAssertion[]}
export interface ManyWriteOptions {maxDocs?:number;resumeAfter?:string;atomic?:boolean;ordered?:boolean;dryRun?:boolean;idempotencyKey?:string;lockTokens?:Record<string,string>;release?:boolean;assertLocks?:LeaseAssertion[]}
export interface WriteReceipt {status:string;replayed:boolean}
export interface BatchWriteReceipt extends WriteReceipt {writeErrors?:{index:number;id?:string;error:DataFault}[];hasMore?:boolean;resumeAfter?:string}
export interface InsertResult extends WriteReceipt {insertedId:string}
export interface UpdateResult extends WriteReceipt {matchedCount:number;modifiedCount:number;upsertedId?:string}
export interface DeleteResult extends WriteReceipt {deletedCount:number}
export interface DocumentWriteResult<T=unknown> extends WriteReceipt {value:RecordRow<T>|null;upsertedId?:string}
export interface BulkWriteResult extends BatchWriteReceipt {insertedCount:number;matchedCount:number;modifiedCount:number;deletedCount:number;upsertedCount:number;insertedIds:Record<string,string>;upsertedIds:Record<string,string>}
export interface DataPage<T> {items:T[];pageInfo:QueryPageInfo}
export interface SampleFindOptions extends FindOptions {recordIds?:string[];from:number;to:number;page?:QueryPage}
export interface SampleAggregateOptions {recordIds?:string[];from:number;to:number;page?:QueryPage;limit?:number}
export type BulkDocumentWrite<T> = {insertOne:RecordCreateItem<T>} | {updateOne:{filter:DataFilter;update:UpdateDocument;upsert?:boolean;arrayFilters?:DataFilter[];sort?:QuerySort[]}} | {replaceOne:{filter:DataFilter;replacement:T;upsert?:boolean;sort?:QuerySort[]}} | {deleteOne:{filter:DataFilter;sort?:QuerySort[]}};

export interface QueryPageInfo {mode: 'cursor' | 'offset'; hasMore: boolean; nextCursor: string | null; nextPage: number | null; snapshotScope: 'thisRequestOnly'}
export interface DiscoveryQuery { search?: string; status?: 'active' | 'disabled'; page?: number; size?: number }
export interface OperationResource {
  code: string;
  kind: 'control';
  model: 'request-reply';
  name: string;
  description: string;
  revision: number;
  contractVersion: string;
  status: 'active' | 'disabled';
  contract: {
    inputSchema: Partial<DataSchema>;
    outputSchema: Partial<DataSchema>;
    sideEffect: 'none' | 'read' | 'write';
    idempotency: 'none' | 'optional' | 'required';
    timeoutMs: number;
  };
}
export interface OperationInvocation<T> {
  controlCode: string;
  contractVersion: string;
  result: T;
  /** Stream completion is not proof that a physical device completed a command. */
  execution: {runId: string; status: string; replayed: boolean; usesNATS: false; operationRepeated: false};
}
export interface OperationInvokeOptions { idempotencyKey: string }
export interface StreamRequestContract {
  code: string;
  name: string;
  description: string;
  contractVersion: string;
  inputSchema: Record<string, unknown>;
  outputSchema: Record<string, unknown>;
  sideEffect: 'read' | 'write';
  idempotency: 'none' | 'optional' | 'required';
  timeoutMs: number;
  status: 'active';
}
export interface StreamRequestResult<T> {
  code: string;
  contractVersion: string;
  result: T;
  runID: string;
  status: 'succeeded';
  replayed: boolean;
}
export interface StreamRequestInvokeOptions { idempotencyKey?: string }
/** Discovery and validation metadata follows the current server contract. */
export type DiscoveryResult = Record<string, unknown>;
export type CollectionDescription = Record<string, unknown> & {indexes: IndexSpec[]; indexStatus: IndexState[]};
export interface IndexSpec {path?: string; paths?: string[]; kind?: 'btree' | 'gin'; unique?: boolean; target?: 'records' | 'samples'}
export interface IndexState {name: string; spec: IndexSpec; state: 'pending' | 'building' | 'ready' | 'failed' | 'removing' | 'managedAtSource'; error?: string}
export interface RetryAdvice { recommendedAction: string; canRetrySameRequest: boolean; keepIdempotencyKey: boolean }
export interface BatchValidation {
  status: 'validated';
  plan: {opId: string; action: string; kind?: string; collectionId?: string; targetId?: string; dependsOn?: string[]; changedFields?: string[]; conditionCount: number; checksRevision: boolean}[];
  readCollections: string[];
  writeCollections: string[];
  writesCommitted: boolean;
  locksHeldAfterResponse: boolean;
  executionGuaranteed: boolean;
  warnings: string[];
}
export interface BatchStatus { status: 'unknown' | 'committed' | 'queued' | 'running' | 'succeeded' | 'failed'; jobId?: string; itemCount?: number; committedAt?: number; retryAdvice: RetryAdvice }
/** Compatibility type aliases; new callers use the Batch names. */
export type TransactionValidation = BatchValidation;
export type TransactionStatus = BatchStatus;
/** Values may be supplied by explicit result bindings. append/addToSet accept 1..200 elements; pull accepts an element predicate. */
export type FieldOperation = {op: 'set' | 'increment'; path: string; value?: unknown}
  | {op: 'append' | 'addToSet'; path: string; value?: unknown[]}
  | {op: 'pull'; path: string; value?: DataFilter}
  | {op: 'unset'; path: string; value?: never};
export interface ResultBinding { path: 'id' | `data.${string}` | `operations.${number}.value`; ref: string; resultPath: 'id' | 'revision' | 'collectionId' | `data.${string}` | 'query.total' | `query.records.${number}.${'id' | 'revision' | 'collectionId' | `data.${string}`}` }
export interface RecordConditionOptions { expectedRevision?: number }
export type RecordSelectionOptions = {returnDocument?: 'before' | 'after'} & (
  {selection?: 'exactOne'; sort?: never} | {selection: 'first'; sort?: QuerySort[]}
);
export type RecordUpdateOptions = RecordConditionOptions & RecordSelectionOptions;
export interface TransactionQuery {
  filter?: DataFilter;
  sort?: QuerySort[];
  limit?: number;
  expectCount?: {min?: number; max?: number};
}
export interface DataChange {
  opId: string;
  action: 'record.create' | 'record.replace' | 'record.delete' | 'record.check' | 'record.read' | 'record.query' | 'record.modify' | 'record.update' | 'sample.append';
  collectionId: string;
  id?: string | {ref: string};
  recordId?: string | {ref: string};
  expectedRevision?: number;
  data?: unknown;
  time?: number;
  quality?: string;
  dependsOn?: string[];
  exists?: boolean;
  filter?: DataFilter;
  update?: UpdateDocument;
  arrayFilters?: DataFilter[];
  upsert?: boolean;
  /** Only record.update/modify/replace. Omitted means exactOne. */
  selection?: 'exactOne' | 'first';
  sort?: QuerySort[];
  returnDocument?: 'before' | 'after';
  operations?: FieldOperation[];
  bindings?: ResultBinding[];
  query?: TransactionQuery;
  lockToken?: string;
  lockTokens?: Record<string,string>;
  release?: boolean;
}
export type RecordChange = Omit<DataChange, 'action' | 'recordId' | 'time' | 'quality'> & {action: Exclude<DataChange['action'], 'sample.append'>};
export interface SampleAppendItem<S = unknown> {id?: string; time: number; quality?: string; data: S}
export interface RecordCreateItem<T> {id?: string; data: T}
export type RecordUpdateItem = RecordUpdateOptions & {filter: DataFilter; operations: FieldOperation[]};
export type RecordReplaceItem<T> = RecordUpdateOptions & {filter: DataFilter; data: T};
export interface RecordDeleteItem {filter: DataFilter; expectedRevision?: number}
export interface ResourceWriteResult {status: Outcome['status']; replayed: boolean; jobId?: string; result?: ChangeResult}
export interface WriteOptions {
  /** Optional request key; reuse with the unchanged request to recover a retry. */
  idempotencyKey?: string;
  mode?: 'auto' | 'sync' | 'async';
  dryRun?: boolean;
}
export interface ArrayWriteOptions extends WriteOptions {
  /** Independently commit items and return per-item errors. Defaults to false. */
  allowPartial?: boolean;
}
export interface ChangeResult { opId: string; id: string; collectionId?: string; revision?: number; action: string; status: string; data?: Record<string, unknown>; document?: RecordRow<Record<string, unknown>>; error?: DataFault; query?: {records: RecordRow<Record<string, unknown>>[]; total: number; hasMore: boolean} }
export interface FaultDetail { opId?: string; path?: string; conditionIndex?: number; [key: string]: unknown }
export interface DataFault { code: string; retryable?: boolean; retryAdvice?: RetryAdvice; rolledBack?: boolean; opId?: string; path?: string; conditionIndex?: number; reason?: string; repair?: string; details?: FaultDetail[] }
export interface OutcomeSummary {total: number; succeeded: number; failed: number; notExecuted: number; unknown: number}
export interface Outcome { status: 'committed' | 'validated' | 'accepted' | 'partiallyCommitted' | 'failed' | 'unknown' | 'validationFailed'; replayed: boolean; jobId?: string; results?: ChangeResult[]; summary?: OutcomeSummary }
export type JobExecutionStatus = 'queued' | 'running' | 'finished';
export type JobOutcome = 'pending' | 'committed' | 'partiallyCommitted' | 'rolledBack' | 'unknown';
export interface Job {
  jobId: string;
  /** Legacy worker status; failed does not imply rollback. */
  status: 'queued' | 'running' | 'succeeded' | 'failed';
  /** Optional only for compatibility with servers before these fields were introduced. */
  executionStatus?: JobExecutionStatus;
  outcome?: JobOutcome;
  result?: Outcome | null;
  error?: DataFault | null;
  progress: {total: number; completed: number};
}
/** Resolve old/new server receipts conservatively; unknown never authorizes a whole-job retry. */
export function getJobOutcome(job: Job): JobOutcome {
  if (job.executionStatus === 'queued' || job.executionStatus === 'running' || job.status === 'queued' || job.status === 'running') return 'pending';
  if (job.outcome) return job.outcome;
  if (job.result) {
    const {status, results} = job.result;
    if (status === 'committed' || status === 'partiallyCommitted') return status;
    if (status === 'failed' && results?.length && results.every(item => item.status === 'notExecuted' || item.status === 'failed' && item.error?.rolledBack === true)) return 'rolledBack';
    return 'unknown';
  }
  return job.error?.rolledBack === true ? 'rolledBack' : 'unknown';
}
export class AppDataError extends Error {
  readonly status: number;
  readonly code: string;
  readonly requestId?: string;
  readonly retryable: boolean;
  readonly retryAdvice?: RetryAdvice;
  readonly rolledBack?: boolean;
  readonly details?: FaultDetail[];
  readonly opId?: string;
  readonly path?: string;
  readonly conditionIndex?: number;
  readonly fault?: DataFault;
  constructor(message: string, status: number, code: string, requestId?: string, retryable = false, fault?: DataFault) {
    super(message);
    this.name = 'AppDataError'; this.status = status; this.code = code;
    this.requestId = requestId; this.retryable = retryable;
    this.retryAdvice = fault?.retryAdvice;
    this.rolledBack = fault?.rolledBack; this.details = fault?.details;
    this.opId = fault?.opId; this.path = fault?.path; this.conditionIndex = fault?.conditionIndex; this.fault = fault;
  }
}

export function createAppDataClient(config: {apiBase: string; apiKey: string; timeoutMs?: number; fetch?: typeof fetch}) {
  if (typeof window !== 'undefined') throw new Error('Tier0 App data must be called on the server');
  const base = new URL(config.apiBase.replace(/\/+$/, '') + '/');
  if (!['http:', 'https:'].includes(base.protocol) || base.username || base.password || base.search || base.hash || !base.pathname.endsWith('/openapi/v2/')) {
    throw new Error('apiBase must be a trusted OpenAPI v2 URL without credentials, query or fragment');
  }
  if (!config.apiKey?.trim()) throw new Error('App API key is required');
  const transport = config.fetch ?? globalThis.fetch;
  const timeout = config.timeoutMs ?? 15_000;
  if (!Number.isFinite(timeout) || timeout < 1) throw new Error('timeoutMs must be positive');
  async function send(path: string, body: unknown, controlHeaders: Record<string, string> = {}): Promise<Response> {
    let response: Response;
    const signal = AbortSignal.timeout(timeout);
    try {
      response = await transport(new URL(path, base), {
        method: 'POST', redirect: 'error', signal,
        headers: {'Content-Type': 'application/json', 'X-API-Key': config.apiKey, ...controlHeaders},
        body: JSON.stringify(body),
      });
    } catch (error) {
      // A network failure does not prove rollback; writes must not be blindly retried.
      throw new AppDataError('Tier0 response unavailable; inspect business state or the returned job before another write', signal.aborted ? 504 : 502, signal.aborted ? 'TIMEOUT' : 'RESPONSE_UNAVAILABLE', undefined, false);
    }
    return response;
  }
  async function postAt<T>(path: string, body: unknown, controlHeaders: Record<string, string> = {}, rawDiscovery = false): Promise<T> {
    const response = await send(path, body, controlHeaders);
    let envelope: {code: number; msg?: string; requestId?: string; data: T; error?: DataFault};
    try {
      envelope = await response.json();
      if (rawDiscovery && !response.ok) {
        const fault = envelope as unknown as {errorCode?: unknown; message?: unknown};
        if (fault && typeof fault.errorCode === 'string' && typeof fault.message === 'string') throw new CapabilityError(fault.message, {status:response.status,errorCode:fault.errorCode});
        throw new CapabilityError('Invalid capability error response.', {status:response.status,errorCode:'CAPABILITY_PROTOCOL_ERROR'});
      }
      if (rawDiscovery && response.ok) return validateCapabilityDiscovery(envelope, response.status) as T;
      if (!envelope || typeof envelope !== 'object' || typeof envelope.code !== 'number') throw new Error('Invalid envelope');
    } catch (error) {
      if (error instanceof CapabilityError) throw error;
      // A received HTTP rejection keeps its status even when a proxy returns text or HTML.
      if (!response.ok) throw new AppDataError(`Tier0 request failed (HTTP ${response.status}); inspect business state before another write`, response.status, 'HTTP_ERROR', undefined, false);
      throw new AppDataError('Invalid Tier0 response; inspect business state before another write', response.status, 'INVALID_RESPONSE', undefined, false);
    }
    if (!response.ok || envelope.code !== 200) {
      throw new AppDataError(envelope.msg ?? 'Tier0 request failed', response.status, envelope.error?.code ?? 'API_ERROR', envelope.requestId, envelope.error?.retryable === true, envelope.error);
    }
    return envelope.data;
  }
  async function postStreamRequest<T>(path: string, body: unknown): Promise<T> {
    const invoking = path.endsWith('/invoke');
    let response: Response;
    try { response = await send(`stream-requests/${path}`, body); }
    catch (error) {
      if (invoking && error instanceof AppDataError) throw new AppDataError('Stream request result is unknown; do not repeat a write with a new idempotency key', 504, 'STREAM_REQUEST_RESULT_UNKNOWN');
      throw error;
    }
    let value: Record<string, unknown>;
    try {
      value = await response.json();
      if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid response');
    } catch {
      throw new AppDataError('Invalid Stream request response; inspect business state before another write', response.status, response.ok ? 'INVALID_RESPONSE' : 'HTTP_ERROR');
    }
    if (!response.ok) {
      throw new AppDataError(typeof value.message === 'string' ? value.message : 'Stream request failed', response.status,
        typeof value.errorCode === 'string' ? value.errorCode : 'HTTP_ERROR');
    }
    if (invoking && (typeof value.code !== 'string' || typeof value.contractVersion !== 'string' || !value.contractVersion ||
      typeof value.runID !== 'string' || !value.runID || value.status !== 'succeeded' || typeof value.replayed !== 'boolean' || !Object.hasOwn(value, 'result'))) {
      throw new AppDataError('Invalid Stream request result; inspect business state before another write', response.status, 'INVALID_RESPONSE');
    }
    return value as T;
  }
  const post = <T>(path: string, body: unknown): Promise<T> => postAt(`uns/${path}`,body);
  const resource = <T>(collectionId:string, kind:'records'|'samples', action:string, body:unknown):Promise<T> => {
    if (!collectionId?.trim()) throw new Error('collectionId is required');
    return post(`collections/${encodeURIComponent(collectionId)}/${kind}${action ? ':' + action : ''}`, body);
  };
  async function query<T>(path:string,value:object):Promise<T> {
    return (await post<{results:T[]}>(path,{queries:[{...value,queryId:'app'}]})).results[0];
  }
  function requireRecordId(recordId:string) {
    if (!recordId?.trim()) throw new Error('recordId is required for Sample append');
  }
  const transact = (items:RecordChange[], options:WriteOptions & {assertLocks?:LeaseAssertion[]}={}) => {
    if (items.some(item=>!item.action.startsWith('record.'))) throw new Error('Record transactions accept record operations only; append Samples separately');
    return post<Outcome>('records:transact',{...options,items});
  };
  async function context(): Promise<AppContext> { return post('apps/context', {}); }
  async function collection<T, S = never>(alias: string) {
    const grant = (await context()).collections.find(item => item.alias === alias);
    if (!grant) throw new AppDataError('Collection alias is not granted to this App', 403, 'FORBIDDEN');
    const collectionId = grant.collectionId;
    const changes = {
      query: (opId: string, query: TransactionQuery): RecordChange => ({opId, action: 'record.query', collectionId, query}),
      update: (opId: string, filter: DataFilter, operations: FieldOperation[], options: RecordUpdateOptions = {}): RecordChange => ({opId, action: 'record.update', collectionId, filter, operations, ...options}),
      check: (opId: string, filter: DataFilter, options: RecordConditionOptions & {exists?: boolean} = {}): RecordChange => ({opId, action: 'record.check', collectionId, filter, ...options}),
      read: (opId: string, filter: DataFilter): RecordChange => ({opId, action: 'record.read', collectionId, filter}),
      modify: (opId: string, filter: DataFilter, operations: FieldOperation[], options: RecordUpdateOptions = {}): RecordChange => ({opId, action: 'record.modify', collectionId, filter, operations, ...options}),
      create: (opId: string, data: T, id?: string): RecordChange => ({opId, action: 'record.create', collectionId, data, ...(id ? {id} : {})}),
      replace: (opId: string, record: Pick<RecordRow<T>, 'id' | 'revision'>, data: T): RecordChange => ({opId, action: 'record.replace', collectionId, filter: {_id: record.id}, expectedRevision: record.revision, data}),
      delete: (opId: string, record: Pick<RecordRow<T>, 'id' | 'revision'>): RecordChange => ({opId, action: 'record.delete', collectionId, filter: {_id: record.id}, expectedRevision: record.revision}),
    };
    return {
      id: collectionId, alias, schema: grant.recordSchema, changes,
      describe: () => post<CollectionDescription>('discovery/describe', {collectionId}),
      get: async(id:string):Promise<RecordRow<T>|null> => (await records.find<T>(collectionId,{filter:{_id:id},limit:1})).items[0] ?? null,
      append: (recordId:string,items:SampleAppendItem<S>[],options:ArrayWriteOptions={})=>samples.append(collectionId,recordId,items,options),
      find: (options:FindOptions={})=>records.find<T>(collectionId,options),
      count: (filter:DataFilter={})=>records.count(collectionId,filter),
      insertOne: (data:T,options:DocumentWriteOptions & {id?:string}={})=>records.insertOne(collectionId,data,options),
      insertMany: (documents:RecordCreateItem<T>[],options:ManyWriteOptions={})=>records.insertMany(collectionId,documents,options),
      updateOne: (filter:DataFilter,update:UpdateDocument,options:DocumentWriteOptions={})=>records.updateOne(collectionId,filter,update,options),
      updateMany: (filter:DataFilter,update:UpdateDocument,options:ManyWriteOptions={})=>records.updateMany(collectionId,filter,update,options),
      replaceOne: (filter:DataFilter,replacement:T,options:DocumentWriteOptions={})=>records.replaceOne(collectionId,filter,replacement,options),
      deleteOne: (filter:DataFilter,options:DocumentWriteOptions={})=>records.deleteOne(collectionId,filter,options),
      deleteMany: (filter:DataFilter,options:ManyWriteOptions={})=>records.deleteMany(collectionId,filter,options),
      bulkWrite: (operations:BulkDocumentWrite<T>[],options:ManyWriteOptions={})=>records.bulkWrite<T>(collectionId,operations,options),
      findOneAndUpdate: (filter:DataFilter,update:UpdateDocument,options:DocumentWriteOptions={})=>records.findOneAndUpdate<T>(collectionId,filter,update,options),
      findOneAndReplace: (filter:DataFilter,replacement:T,options:DocumentWriteOptions={})=>records.findOneAndReplace(collectionId,filter,replacement,options),
      findOneAndDelete: (filter:DataFilter,options:DocumentWriteOptions={})=>records.findOneAndDelete<T>(collectionId,filter,options),
      aggregate: <R=Record<string,unknown>>(pipeline:PipelineStage[],options:{page?:QueryPage;limit?:number}={})=>records.aggregate<R>(collectionId,pipeline,options),
      samplesFind: (options:SampleFindOptions)=>samples.find<S>(collectionId,options),
      samplesAggregate: <R=Record<string,unknown>>(pipeline:PipelineStage[],options:SampleAggregateOptions)=>samples.aggregate<R>(collectionId,pipeline,options),
      restore: (ids:string[],options:WriteOptions={})=>records.restore(collectionId,ids,options),
      lock: (options:LockOptions)=>records.lock<T>(collectionId,options),
      renew: (locks:Lease[],ttlMs?:number)=>records.renew(collectionId,locks,ttlMs),
      unlock: (locks:Lease[])=>records.unlock(collectionId,locks),
      latest: async(recordIds:string[])=>recordIds.length?(await samples.latest<S>(collectionId,recordIds)).series:[],
    };
  }
  const jobs = async (jobIds: string[]) => (await query<{list: Job[]}>('jobs/query', {jobIds})).list;
  const discover = (options: DiscoveryQuery = {}) => post<DiscoveryResult>('discovery/search', options);
  const describeCollection = (collectionId: string) => post<CollectionDescription>('discovery/describe', {collectionId});
  const records = {
    find: <T=Record<string,unknown>>(collectionId:string,options:FindOptions={})=>resource<DataPage<RecordRow<T>>>(collectionId,'records','find',options),
    count: (collectionId:string,filter:DataFilter={})=>resource<{count:number}>(collectionId,'records','count',{filter}),
    distinct: (collectionId:string,field:string,filter:DataFilter={},options:{page?:QueryPage;limit?:number}={})=>resource<{values:unknown[];pageInfo:QueryPageInfo}>(collectionId,'records','distinct',{field,filter,...options}),
    aggregate: <T=Record<string,unknown>>(collectionId:string,pipeline:PipelineStage[],options:{page?:QueryPage;limit?:number}={})=>resource<DataPage<T>>(collectionId,'records','aggregate',{pipeline,...options}),
    explain: (collectionId:string,options:Record<string,unknown>)=>resource<Record<string,unknown>>(collectionId,'records','explain',options),
    insertOne: <T>(collectionId:string,data:T,options:DocumentWriteOptions & {id?:string}={})=>resource<InsertResult>(collectionId,'records','',{data,...options}),
    insertMany: <T>(collectionId:string,documents:RecordCreateItem<T>[],options:ManyWriteOptions={})=>resource<BatchWriteReceipt & {insertedCount:number;insertedIds:Record<string,string>}>(collectionId,'records','insertMany',{documents,...options}),
    updateOne: (collectionId:string,filter:DataFilter,update:UpdateDocument,options:DocumentWriteOptions={})=>resource<UpdateResult>(collectionId,'records','updateOne',{filter,update,...options}),
    replaceOne: <T>(collectionId:string,filter:DataFilter,replacement:T,options:DocumentWriteOptions={})=>resource<UpdateResult>(collectionId,'records','replaceOne',{filter,replacement,...options}),
    deleteOne: (collectionId:string,filter:DataFilter,options:DocumentWriteOptions={})=>resource<DeleteResult>(collectionId,'records','deleteOne',{filter,...options}),
    updateMany: (collectionId:string,filter:DataFilter,update:UpdateDocument,options:ManyWriteOptions={})=>resource<BatchWriteReceipt & {matchedCount:number;modifiedCount:number}>(collectionId,'records','updateMany',{filter,update,...options}),
    deleteMany: (collectionId:string,filter:DataFilter,options:ManyWriteOptions={})=>resource<BatchWriteReceipt & {deletedCount:number}>(collectionId,'records','deleteMany',{filter,...options}),
    findOneAndUpdate: <T=unknown>(collectionId:string,filter:DataFilter,update:UpdateDocument,options:DocumentWriteOptions={})=>resource<DocumentWriteResult<T>>(collectionId,'records','findOneAndUpdate',{filter,update,...options}),
    findOneAndReplace: <T>(collectionId:string,filter:DataFilter,replacement:T,options:DocumentWriteOptions={})=>resource<DocumentWriteResult<T>>(collectionId,'records','findOneAndReplace',{filter,replacement,...options}),
    findOneAndDelete: <T=unknown>(collectionId:string,filter:DataFilter,options:DocumentWriteOptions={})=>resource<DocumentWriteResult<T>>(collectionId,'records','findOneAndDelete',{filter,...options}),
    bulkWrite: <T=unknown>(collectionId:string,operations:BulkDocumentWrite<T>[],options:ManyWriteOptions={})=>resource<BulkWriteResult>(collectionId,'records','bulkWrite',{operations,...options}),
    restore: (collectionId:string,ids:string[],options:WriteOptions={})=>resource<WriteReceipt & {restoredCount:number;restoredIds:string[]}>(collectionId,'records','restore',{ids,...options}),
    lock: <T=unknown>(collectionId:string,options:LockOptions)=>resource<{locks:(Lease & {expiresAt:number;fence:number;record:RecordRow<T>})[]}>(collectionId,'records','lock',options),
    renew: (collectionId:string,locks:Lease[],ttlMs?:number)=>resource<{locks:{id:string;expiresAt:number}[]}>(collectionId,'records','renew',{locks,...(ttlMs===undefined?{}:{ttlMs})}),
    unlock: (collectionId:string,locks:Lease[])=>resource<{unlockedCount:number}>(collectionId,'records','unlock',{locks}),
    forceUnlock: (collectionId:string,ids:string[])=>resource<{unlockedCount:number}>(collectionId,'records','unlock',{ids,force:true}),
    transact,
  };
  const samples = {
    find: <S=unknown>(collectionId:string,options:SampleFindOptions)=>resource<DataPage<SampleRow<S>>>(collectionId,'samples','find',options),
    aggregate: <T=Record<string,unknown>>(collectionId:string,pipeline:PipelineStage[],options:SampleAggregateOptions)=>resource<DataPage<T>>(collectionId,'samples','aggregate',{pipeline,...options}),
    latest: <S=unknown>(collectionId:string,recordIds:string[])=>resource<{series:{recordId:string;sample:SampleRow<S>|null}[]}>(collectionId,'samples','latest',{recordIds}),
    delete: (collectionId:string,recordIds:string[],from:number,to:number,dryRun=false)=>resource<{deletedCount:number;dryRun:boolean}>(collectionId,'samples','delete',{recordIds,from,to,dryRun}),
    append: <S>(collectionId:string,recordId:string,items:SampleAppendItem<S>[],options:ArrayWriteOptions={})=>{
      requireRecordId(recordId);
      return post<Outcome>(`collections/${encodeURIComponent(collectionId)}/records/${encodeURIComponent(recordId)}/samples`,{...options,dryRun:options.dryRun ?? false,items});
    },
  };
  const capabilities = {
    async discover(): Promise<CapabilityDiscoverResponse> {
      if (arguments.length) throw new TypeError('Capability discovery accepts no arguments');
      return postAt<CapabilityDiscoverResponse>('capabilities/discover', {}, {}, true);
    },
  };
  const platform = {
    /** API key identity, never the browser's end-user identity. */
    identity: () => postAt<PlatformIdentity>('platform/identity/get', {}),
    info: () => postAt<PlatformInfo>('platform/info/get', {}),
  };
  const users = {
    list: (options: PlatformUsersQuery = {}) => postAt<Page<PlatformUser>>('platform/users/list', options),
    projectMembers: (projectId: string, options: {page?: number; size?: number} = {}) => postAt<Page<ProjectMember>>('platform/projects/members/list', {...options, projectId}),
  };
  const notifications = {
    send: (input: NotificationInput) => postAt<NotificationReceipt>('notifications/send', input),
    status: (messageId: string) => postAt<NotificationStatus>('notifications/status/get', {messageId}),
  };
  // Only request signed URLs here: never forward this API key to object storage.
  const files = {
    createUpload: (input: FileUploadInput) => postAt<FileUpload>('files/uploads/create', input),
    createDownload: (filePath: string, options: {expiresInSeconds?: number} = {}) => postAt<FileDownload>('files/downloads/create', {...options, filePath}),
    delete: (filePath: string) => postAt<{filePath: string; deleted: boolean}>('files/delete', {filePath}),
    multipart: {
      create: (input: FileUploadInput & {partSizeBytes?: number}) => postAt<MultipartUpload>('files/uploads/multipart/create', input),
      parts: (session: MultipartSession, partNumbers: number[]) => postAt<{list: {partNumber: number; url: string; expiresAt: number}[]}>('files/uploads/multipart/parts', {...session, partNumbers}),
      complete: (session: MultipartSession, parts: CompletedPart[]) => postAt<CompletedUpload>('files/uploads/multipart/complete', {...session, parts}),
      abort: (session: MultipartSession) => postAt<{filePath: string; aborted: boolean}>('files/uploads/multipart/abort', session),
    },
  };
  const controls = {
    discover(query: DiscoveryQuery = {}): Promise<Page<OperationResource>> {
      return postAt('../v1/controls/discover', query);
    },
    use<Input = unknown, Output = unknown>(reference: {code: string}) {
      const id = reference.code;
      if (typeof id !== 'string' || !id || id === '.' || id === '..') throw new Error('A stable control code is required');
      const route = `../v1/controls/${encodeURIComponent(id)}`;
      return {
        describe: (): Promise<OperationResource> => postAt(`${route}/describe`, {}),
        invoke(body: Input, options: OperationInvokeOptions): Promise<OperationInvocation<Output>> {
          if (!options?.idempotencyKey?.trim() || options.idempotencyKey.length > 128) throw new Error('A persisted idempotencyKey is required');
          return postAt(`${route}/invoke`, body, {'Idempotency-Key': options.idempotencyKey});
        },
      };
    },
  };
  const streamRequests = {
    discover(query: {page?: number; size?: number; search?: string} = {}): Promise<{total: number; list: StreamRequestContract[]}> {
      return postStreamRequest('discover', query);
    },
    use<Input = unknown, Output = unknown>({code}: {code: string}) {
      if (typeof code !== 'string' || code.length > 128 || !/^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$/.test(code)) throw new Error('A stable Stream request code is required');
      return {
        describe: (): Promise<StreamRequestContract> => postStreamRequest(`${code}/describe`, {}),
        invoke(input: Input, options: StreamRequestInvokeOptions = {}): Promise<StreamRequestResult<Output>> {
          if (input === undefined) throw new Error('Stream request input must be JSON; use null for no input');
          const key = options.idempotencyKey;
          if (key !== undefined && (typeof key !== 'string' || key.trim() !== key || new TextEncoder().encode(key).length > 128)) throw new Error('idempotencyKey must be at most 128 UTF-8 bytes without surrounding whitespace');
          return postStreamRequest(`${code}/invoke`, {input, ...(key !== undefined ? {idempotencyKey: key} : {})});
        },
      };
    },
  };
  return {context, collection, records, samples, transact, jobs, discover, describeCollection, platform, users, notifications, files, controls, capabilities, streamRequests};
}
