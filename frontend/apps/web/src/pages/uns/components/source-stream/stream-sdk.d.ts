/** Editor reference only. No general Node/npm, files or direct network access. */
export type HTTPRequestOptions = {
  method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE' | 'HEAD' | 'OPTIONS';
  /** Relative path resolved against the Source endpoint, not an arbitrary URL. */
  path: string;
  /** 单次请求时限：默认 5000 ms，允许 100–45000 ms，仍受 Program 总时限约束。 */
  timeoutMs?: number;
  headers?: Record<string, string>;
  query?: Record<string, string>;
} & (
  | { json: unknown; body?: never; form?: never }
  | { body: string; json?: never; form?: never }
  | { form: Record<string, string>; json?: never; body?: never }
  | { json?: never; body?: never; form?: never }
);
export interface HTTPResponse {
  status: number;
  headers: Record<string, string[]>;
  data: unknown;
}
export interface IncomingRequest {
  method: string;
  path: string;
  params: Record<string, string>;
  query: Record<string, string[]>;
  headers: Record<string, string[]>;
  body: unknown;
  requestId: string;
}
export interface SourceHandle {
  parameters(): Promise<Record<string, unknown>>;
  /** Real execution only; dry-run resolves explicit mocks, never live secrets. Do not log or output secrets. */
  secrets(): Promise<Record<string, string>>;
  request(options: HTTPRequestOptions): Promise<HTTPResponse>;
  read(resourceRef: string, options?: unknown): Promise<unknown>;
  write(resourceRef: string, data: unknown, options?: unknown): Promise<unknown>;
  invoke(resourceRef: string, input: unknown, options?: unknown): Promise<unknown>;
  publish(resourceRef: string, data: unknown, options?: unknown): Promise<unknown>;
}
export interface TopicHandle {
  read(options?: unknown): Promise<unknown>;
  /** Buffered until successful execution; this receipt is not a committed Topic write. */
  write(data: unknown): Promise<{ stage: 'buffered'; topicId?: string; collectionId?: string }>;
}
export interface InternalResourceWrite {
  recordId: string;
  record: Record<string, unknown>;
  /** Default replace: replaces the full record. upsert/update set only supplied top-level fields; nested objects are whole-field values. create rejects existing IDs; update rejects missing IDs. */
  mode?: 'create' | 'upsert' | 'update' | 'replace';
  /** Explicitly remove top-level fields in upsert/update. Null in record is a value, not deletion. */
  unset?: string[];
  /** Omit for no revision precondition; 0 requires absence, positive values match the current Record revision. Conflict rolls back all outputs and Checkpoint. */
  expectedRevision?: number;
  sample?: Record<string, unknown>;
  time?: number;
  quality?: string;
}
export interface RecordQueryFilter {
  field: string;
  op: 'eq' | 'ne' | 'gt' | 'gte' | 'lt' | 'lte' | 'in' | 'contains' | 'exists';
  value: string | number | boolean | null | (string | number | boolean)[];
}
export type UNSQueryOptions = {
  /** Defaults to 50, maximum 100. Cursor is bound to the resource and these options. */
  limit?: number;
  cursor?: string;
  recordIds?: string[];
} & (
  | {
      kind: 'record';
      filters?: RecordQueryFilter[];
      orderBy?: { field: string; direction: 'asc' | 'desc' }[];
      timeRange?: never;
    }
  | {
      kind: 'sample';
      /** Epoch milliseconds: from included, to excluded; time/id/recordId ascending. */
      timeRange: { from: number; to: number };
      filters?: never;
      orderBy?: never;
    }
);
export interface UNSQueryItem {
  id: string;
  collectionId: string;
  data: Record<string, unknown>;
  sourceId?: string | null;
  revision?: number;
  createdAt?: number;
  updatedAt?: number;
  recordId?: string;
  time?: number;
  receivedAt?: number;
  quality?: string;
  streamId?: string;
  epoch?: string;
  seq?: number;
  producerId?: string;
}
export interface UNSQueryPage {
  items: UNSQueryItem[];
  nextCursor: string;
  hasMore: boolean;
}
export interface InternalResourceHandle {
  read(options?: { recordId?: string; limit?: number; offset?: number }): Promise<unknown>;
  /** Declared read access required. Dry-run uses explicit query fixtures, never live data. */
  query(options: UNSQueryOptions): Promise<UNSQueryPage>;
  write(value: InternalResourceWrite): Promise<{ stage: 'buffered'; topicId?: string; collectionId?: string }>;
}
/** 每次入口执行只传一个冻结的上下文对象；输入位于 context.input。 */
export interface ProgramContext {
  input: unknown;
  entryId: string;
  dryRun?: boolean;
  request?: IncomingRequest;
  [metadata: string]: unknown;
}
export type Handler = (context: ProgramContext) => unknown | Promise<unknown>;

/** 有界 JSON Schema 子集；不支持远程引用、format 或任意扩展关键字。 */
export interface OperationSchema {
  type: 'object' | 'array' | 'string' | 'number' | 'integer' | 'boolean' | 'null';
  title?: string;
  description?: string;
  properties?: Record<string, OperationSchema>;
  required?: string[];
  additionalProperties?: boolean;
  items?: OperationSchema;
  enum?: unknown[];
  minimum?: number;
  maximum?: number;
}
export interface ControlDeclaration {
  code: string;
  name: string;
  description?: string;
  contractVersion: string;
  inputSchema: OperationSchema | Record<string, never>;
  outputSchema: OperationSchema | Record<string, never>;
  sideEffect: 'none' | 'write';
  idempotency: 'required';
  /** 100–5000 ms，且仍受 Stream 自身和调用方的更短时限约束。 */
  timeoutMs: number;
}
/** Cloud 请求合同；只读入口不能写数据、修改 checkpoint 或调用有副作用的 Source 操作。 */
export interface RequestDeclaration extends Omit<ControlDeclaration, 'sideEffect' | 'idempotency' | 'timeoutMs'> {
  sideEffect: 'read' | 'write';
  idempotency: 'none' | 'optional' | 'required';
  /** 1–30000 ms，仍受 Stream 自身及调用方更短时限约束。 */
  timeoutMs: number;
}
export interface StreamAPI {
  sources: { use(config: { id: string; resources: string[] }): SourceHandle };
  uns: {
    /** Meta records Topic or Collection; fields Topics are read-only. Numeric Meta Topic IDs require space: 'metadata'. */
    use(
      config: ({ topicId: string } | { collectionId: string }) & {
        access: 'read' | 'write' | 'read-write';
        space?: 'metadata';
      }
    ): InternalResourceHandle;
    /** Historical numeric Topic references only; never resolves a same-number Meta Topic. */
    use(config: { topicId: string; space: 'legacy'; access?: 'read' | 'write' | 'read-write' }): TopicHandle;
    use(config: { topicId: string; space?: 'legacy' }): TopicHandle;
  };
  stream: {
    checkpoint: {
      /** Per Stream+entry persistent cursor. Preview uses mocks and never saves. */
      get(): Promise<unknown | null>;
      /** 16 KiB maximum; commits atomically with all successful UNS outputs. */
      set(value: unknown): Promise<{ stage: 'buffered' }>;
    };
    onTimer(config: { id: string; intervalMs: number }, handler: Handler): void;
    onMessage(
      config: { id: string; source: SourceHandle; resourceRefs: string[]; qos?: 0 | 1 | 2 },
      handler: Handler
    ): void;
    onEvent(config: { id: string; topics: (TopicHandle | InternalResourceHandle)[] }, handler: Handler): void;
    /** 兼容旧脚本；新脚本使用 onTimer。 */
    every(config: { id: string; intervalMs: number }, handler: Handler): void;
    /** 兼容旧脚本；新脚本使用 onMessage。 */
    onSource(
      config: { id: string; source: SourceHandle; resourceRefs: string[]; qos?: 0 | 1 | 2 },
      handler: Handler
    ): void;
    /** 兼容旧脚本；新脚本使用 onEvent。 */
    onTopic(config: { id: string; topics: (TopicHandle | InternalResourceHandle)[] }, handler: Handler): void;
    /**
     * 未声明 request 时，新发布自动生成 write/required 合同，身份随 Stream 和入口保持稳定。
     * 请求必须返回 JSON；无结果时显式 return null，不能返回 undefined。
     */
    onRequest(config: { id: string; request?: RequestDeclaration; control?: never }, handler: Handler): void;
    /** 历史 control 合同兼容；新脚本使用 request。 */
    onRequest(config: { id: string; control: ControlDeclaration; request?: never }, handler: Handler): void;
    onRequest(
      config: { id: string; source: SourceHandle; method: HTTPRequestOptions['method']; path: string },
      handler: Handler
    ): void;
  };
  log: {
    info(message: string): void;
    warn(message: string): void;
    error(message: string): void;
    debug(message: string): void;
  };
}
declare const module: { exports: (api: StreamAPI) => void };
export interface RestrictedHash {
  update(data: string): RestrictedHash;
  digest(encoding: 'hex' | 'base64'): string;
}
/** Only this explicit built-in is available, not arbitrary require/npm modules. */
export declare function require(name: 'node:crypto'): {
  createHash(algorithm: 'sha256'): RestrictedHash;
  createHmac(algorithm: 'sha256', key: string): RestrictedHash;
};
