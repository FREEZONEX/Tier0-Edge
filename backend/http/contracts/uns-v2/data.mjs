// 共享业务数据契约；内部接口与 OpenAPI 使用同一操作及 Schema。
const ref = name => ({$ref: `#/components/schemas/${name}`});
const object = (properties, required = []) => ({type: 'object', additionalProperties: false, properties, required});
const id = {type: 'string', minLength: 1, maxLength: 128};
const ids = max => ({type: 'array', items: id, minItems: 1, maxItems: max, uniqueItems: true});
const bool = {type: 'boolean', default: false};
const string = {type: 'string'};
const array = (items, max = 100) => ({type: 'array', items, maxItems: max});
const count = {type: 'integer', minimum: 0};
const doc = {type: 'object', additionalProperties: true};
const envelope = data => object({code: {type: 'integer'}, msg: string, requestId: string, data}, ['code', 'msg', 'requestId', 'data']);
const prefix = '/uns/collections/:collectionId/';
const retired = new Set(['records', 'samples', 'collections', 'recordCreate', 'recordGet', 'recordUpdate', 'recordReplace', 'recordDelete', 'sampleQuerySeries', 'recordTransactionValidate']);

export function applyDataContract(source, openapi) {
  const changed = new Set();
  source.operations = source.operations.filter(op => !retired.has(op.name) && !op.deprecated);
  const schemas = source.schemas;
  const operation = name => {
    const op = source.operations.find(item => item.name === name);
    if (!op) throw new Error(`Missing data operation: ${name}`);
    changed.add(name);
    return op;
  };
  const add = (name, action, resource, fields, required, permission, summary) => {
    const handler = 'UnsV2' + name[0].toUpperCase() + name.slice(1);
    const request = handler + 'Req', response = handler + 'Response';
    schemas[request] = object({collectionId: id, ...fields}, ['collectionId', ...required]);
    schemas[response] = envelope(doc);
    const write = permission !== 'uns.read';
    source.operations.push({name, handler, request, response, suffix: prefix + resource + 's:' + action,
      pathParams: ['collectionId'], resource, novaAction: resource === 'sample' ? 'sample.' + action : action,
      resourceId: permission === 'uns.delete' ? 215 : write ? 214 : 212, permission,
      mutation: write ? resource + '.' + (action === 'restore' ? 'update' : action === 'delete' ? 'delete' : 'update') : '',
      destructive: permission === 'uns.delete', summary,
      example: {collectionId: 'devices', ...Object.fromEntries(required.map(key => [key, key === 'ids' || key === 'recordIds' ? ['device-1', 'device-2'] : key === 'from' ? 1792368000000 : key === 'to' ? 1792454400000 : key === 'pipeline' ? [] : []]))}});
    changed.add(name);
    return source.operations.at(-1);
  };
  const timeRange = {from: {type: 'integer', minimum: 0, description: '包含的起始时间，Unix 毫秒。'}, to: {type: 'integer', minimum: 1, description: '不包含的结束时间，Unix 毫秒，必须大于 from。'}};
  const filter = ref('UnsV2DataFilter');
  const sort = ref('UnsV2PipelineSort');
  const projection = structuredClone(schemas.UnsV2RecordFindReq.properties.projection);
  const limit = {type: 'integer', minimum: 1, maximum: 200, default: 20};
  const page = ref('UnsV2PipelinePage');
  const pipeline = {type: 'array', minItems: 0, maxItems: 32, items: ref('UnsV2PipelineStage')};
  add('sampleFind', 'find', 'sample', {recordIds: ids(20), ...timeRange, filter, sort, projection, limit, cursor: string, page}, ['from', 'to'], 'uns.read', '查询采样');
  add('sampleAggregate', 'aggregate', 'sample', {recordIds: ids(20), ...timeRange, pipeline, limit, page}, ['from', 'to', 'pipeline'], 'uns.read', '聚合采样');
  for (const name of ['sampleFind', 'sampleAggregate']) {
    const shape = schemas[operation(name).request];
    shape.description = 'recordIds 支持多个记录；省略时查询当前授权集合。时间范围和记录范围独立于 filter/pipeline，不能被条件扩大。结果统一分页。';
    shape.allOf = [{not: {required: ['page', 'limit']}}, ...(name === 'sampleFind' ? [{not: {required: ['page', 'cursor']}}] : [])];
  }
  const latest = operation('sampleLatestValues');
  latest.suffix = prefix + 'samples:latest'; latest.pathParams = ['collectionId'];
  latest.summary = '读取每条记录的最新采样';
  const deletion = operation('sampleDeleteRange');
  deletion.summary = '删除指定记录在时间范围内的采样';
  schemas[deletion.request].description = '物理删除显式 recordIds 在 [from,to) 内的采样，需删除权限。dryRun 返回预计删除数量；不删除父 Record。';
  schemas[latest.request].description = 'recordIds 必填，最多 20 个。每个记录返回最新一条真实采样；没有采样返回 null，不以 Record 数据替代。';

  const lock = object({id, token: {type: 'string', minLength: 1, maxLength: 256}}, ['id', 'token']);
  const locks = {...array(lock), minItems: 1};
  const ttlMs = {type: 'integer', minimum: 1000, maximum: 300000, default: 30000};
  const recordLock = add('recordLock', 'lock', 'record', {ids: ids(100), filter, sort, limit: {type: 'integer', minimum: 1, maximum: 100}, ttlMs, label: {type: 'string', maxLength: 64}}, [], 'uns.write', '加锁或认领记录');
  schemas[recordLock.request].oneOf = [{required: ['ids'], not: {anyOf: [{required: ['filter']}, {required: ['sort']}, {required: ['limit']}]}}, {required: ['filter', 'limit'], not: {required: ['ids']}}];
  schemas[recordLock.request].description = 'ids 模式全部成功或全部失败；filter 模式按排序认领可用记录。租约到期失效，读取不受影响。';
  recordLock.example = {collectionId: 'devices', ids: ['device-1'], ttlMs: 30000};
  add('recordRenew', 'renew', 'record', {locks, ttlMs}, ['locks'], 'uns.write', '续租记录锁');
  const unlock = add('recordUnlock', 'unlock', 'record', {locks, ids: ids(100), force: bool}, [], 'uns.write', '释放记录锁');
  schemas[unlock.request].oneOf = [{required: ['locks'], not: {required: ['ids']}, properties: {force: {enum: [false]}}}, {required: ['ids', 'force'], properties: {force: {enum: [true]}}, not: {required: ['locks']}}];
  schemas[unlock.request].description = '正常释放需持有者与令牌匹配；force=true 强制释放需要额外管理权限。';
  for (const name of ['recordRenew', 'recordUnlock']) operation(name).example = {collectionId: 'devices', locks: [{id: 'device-1', token: 'replace-with-returned-lease-token'}]};
  const get = add('recordGet', 'get', 'record', {ids: ids(100), projection}, ['ids'], 'uns.read', '按 ID 读取记录');
  schemas[get.request].description = '最多 100 个 ID；items 按请求顺序返回，missing 不区分不存在、已删除或无权读取。';
  schemas[operation('recordCount').request].properties.maxCount = {type:'integer',minimum:1,maximum:100000,default:100000};
  schemas[operation('recordInsertOne').request].properties.returnDocument = {type:'string',enum:['after'],description:'在同一事务返回新增快照；需要读取权限，只适用于本地存储。'};
  const restore = add('recordRestore', 'restore', 'record', {ids: ids(100), dryRun: bool, idempotencyKey: {type: 'string', minLength: 1, maxLength: 96}}, ['ids'], 'uns.write', '按 ID 批量恢复记录');
  schemas[restore.request].description = '最多 100 个 ID，整批原子恢复。重新校验当前 Schema 和有效记录唯一索引；冲突整批回滚。恢复递增 revision，不改写历史 Sample。';
  const lockValue = object({id, token: string, expiresAt: {type: 'integer'}, fence: {type: 'integer'}}, ['id', 'token', 'expiresAt', 'fence']);
  for (const name of ['recordLock', 'recordRenew']) schemas[operation(name).response] = envelope(object({locks: array(lockValue)}, ['locks']));
  schemas[unlock.response] = envelope(object({unlockedCount: count}, ['unlockedCount']));
  schemas[restore.response] = envelope(object({restoredCount: count, restoredIds: array(id), status: string, replayed: {type: 'boolean'}}, ['restoredCount', 'restoredIds', 'status']));
  const assertion = object({collectionId: id, id, token: {type: 'string', minLength: 1, maxLength: 256}}, ['collectionId', 'id', 'token']);
  const writeGuard = {lockToken: {type: 'string', minLength: 1, maxLength: 256}, release: bool};
  const assertLocks = array(assertion);
  for (const op of source.operations.filter(op => op.resource === 'record' && op.novaAction)) {
    changed.add(op.name);
    const shape = schemas[op.request];
    if (op.mutation && !['lock', 'renew', 'unlock', 'restore'].includes(op.novaAction)) {
      Object.assign(shape.properties, writeGuard, {assertLocks});
      if (['insertMany', 'updateMany', 'deleteMany', 'bulkWrite'].includes(op.novaAction)) shape.properties.lockTokens = {type: 'object', maxProperties: 10000, additionalProperties: string};
    }
    if (op.novaAction.startsWith('findOneAnd')) shape.properties.projection = projection;
    if (shape.properties.upsert) shape.properties.upsert.description = '未匹配时新增；filter 必须包含 _id 等值，或完整的 ready 唯一索引等值键。并发冲突不会创建重复记录。';
  }
  const aggregate = operation('recordAggregatePipeline');
  schemas[aggregate.request].properties.page = page;
  schemas[aggregate.request].allOf = [{not: {required: ['page', 'limit']}}];
  const explain = operation('recordExplain');
  Object.assign(schemas[explain.request].properties, {op: {type: 'string', enum: ['find', 'updateMany', 'deleteMany'], default: 'find'}, limit: {type: 'integer', minimum: 1, maximum: 10000, default: 1000}});
  schemas[explain.response] = envelope(object({op: string, writesCommitted: {type: 'boolean'}, executionGuaranteed: {type: 'boolean'}, matchedCount: count, capped: {type: 'boolean'}, sampleIds: array(id), plan: {type: 'object', additionalProperties: true}}, ['matchedCount', 'capped', 'sampleIds', 'plan']));
  const transaction = operation('recordTransactionExecute');
  transaction.suffix = '/uns/records:transact'; transaction.summary = '执行跨集合业务事务';
  Object.assign(schemas[transaction.request].properties, {dryRun: bool, assertLocks});
  for (const name of ['UnsV2Change', 'UnsV2RecordChange']) if (schemas[name]) Object.assign(schemas[name].properties, writeGuard, {lockTokens: {type: 'object', additionalProperties: string}});
  const bulk = schemas.UnsV2RecordBulkWriteReq.properties.operations.items;
  for (const variant of bulk.oneOf || []) for (const shape of Object.values(variant.properties || {})) Object.assign(shape.properties, writeGuard);
  // 时间桶能力使用同一个表达式契约，避免独立且功能不完整的采样查询。
  const expandBuckets = value => {
    if (!value || typeof value !== 'object') return;
    if (value.properties?.binSize && value.properties?.unit && value.properties?.date) value.properties.binSize = {type: 'integer', minimum: 1, maximum: 1000000, default: 1, description: '大于 1 时仅支持 UTC 的 millisecond/second/minute/hour/day 固定时长桶，以 Unix epoch 对齐；1 支持日历单位和时区。'};
    for (const child of Object.values(value)) expandBuckets(child);
  };
  expandBuckets(schemas);
  // 返回类型与实际 pipeline 查询结果保持一致，供 BFF 和生成客户端使用。
  schemas.CloudDataPage = object({items: array(doc, 200), pageInfo: object({hasMore: {type: 'boolean'}, nextCursor: {type: 'string', nullable: true}, nextPage: {type: 'integer', nullable: true}, mode: string, snapshotScope: string}, ['hasMore'])}, ['items', 'pageInfo']);
  for (const op of source.operations) op.summary = summaries[op.name] || op.summary;
  for (const name of ['recordFind', 'recordAggregatePipeline', 'sampleFind', 'sampleAggregate']) schemas[operation(name).response] = envelope(ref('CloudDataPage'));
  for (const op of source.operations) op.appAllowed = ['record', 'sample'].includes(op.resource) || ['recordTransactionExecute', 'dataDiscover', 'collectionDescribe', 'collectionGet', 'collectionList', 'jobs', 'appContext', 'appSessionInspect'].includes(op.name);
  source.cloudContractRevision = (source.cloudContractRevision || 'cloud') + '+data/2';
  cleanDescriptions(source);
  cleanDescriptions(openapi);
  Object.assign(openapi.components.schemas, structuredClone(schemas));
  for (const [path, item] of Object.entries(openapi.paths)) {
    if (!source.operations.some(op => op.handler === item.post?.operationId) || changed.has(source.operations.find(op => op.handler === item.post?.operationId)?.name)) delete openapi.paths[path];
  }
}

const summaries = {
  recordFind: '查询业务记录', recordCount: '统计记录数量', recordDistinct: '查询字段去重值', recordAggregatePipeline: '聚合业务记录', recordExplain: '评估查询与写入范围',
  recordInsertOne: '新增一条记录', recordInsertMany: '批量新增记录', recordUpdateOne: '更新一条记录', recordUpdateMany: '批量更新记录', recordReplaceOne: '替换一条记录', recordDeleteOne: '软删除一条记录', recordDeleteMany: '批量软删除记录', recordFindOneAndUpdate: '查找更新并返回记录', recordFindOneAndReplace: '查找替换并返回记录', recordFindOneAndDelete: '软删除并返回原记录', recordBulkWrite: '执行集合内混合写入',
  sampleAppend: '追加采样', sourceDelete: '删除数据源', streamProgramList: '列出处理流程序', streamProgramDelete: '删除处理流程序', streamRuntimeCapabilitiesGet: '读取运行能力', controlDiscover: '发现控制能力', controlDescribe: '读取控制定义', controlInvoke: '执行控制', sourceDocumentGet: '读取数据源文档', sourceDocumentCreate: '创建数据源文档', sourceDocumentUpdate: '更新数据源文档', sourceDocumentList: '列出数据源文档', streamDocumentGet: '读取处理流文档', streamDocumentCreate: '创建处理流文档', streamDocumentUpdate: '更新处理流文档', streamDocumentDisable: '停用处理流文档', streamDocumentValidate: '校验处理流文档', sourceImpact: '评估数据源变更影响', sourceHistory: '读取数据源变更历史', sourceObservationsGet: '读取数据源观测', streamObservationGet: '读取处理流观测', streamRunsList: '列出处理流运行记录', streamSampleTest: '测试处理流采样',
};
export function dataOperationTag(op) {
  if (op.metadataAction || op.name === 'references') return '元数据管理';
  if (op.name === 'catalog') return '接口能力';
  if (op.resource === 'record' || op.name === 'recordTransactionExecute') return '业务数据';
  if (op.resource === 'sample') return '时序数据';
  if (/^source/.test(op.name)) return '数据源';
  if (/^stream/.test(op.name)) return '处理流';
  if (/^control/.test(op.name)) return '控制';
  if (/^(collection|dataDiscover)/.test(op.name)) return '集合管理';
  if (/^(directory|topic|namespace)/.test(op.name)) return '命名空间';
  if (/^app/.test(op.name)) return 'App 与授权';
  if (op.name === 'jobs') return '任务';
  return '元数据';
}
export function decorateDataSpec(spec, operations) {
  for (const op of operations) {
    const path = op.suffix.replace(/\/:([A-Za-z][A-Za-z0-9]*)(?=\/|$)/g, '/{$1}');
    const item = spec.paths[path]?.[(op.method || 'POST').toLowerCase()];
    if (!item) continue;
    item.tags = [dataOperationTag(op)];
    item['x-tier0-readonly'] = !op.mutation;
    item['x-tier0-destructive'] = !!op.destructive;
    item.summary = summaries[op.name] || op.summary || item.summary;
    for (const [status, description] of Object.entries({'401': '身份凭证无效', '403': '无操作或集合权限', '404': '资源不存在', '409': '版本、唯一键、幂等请求或租约冲突', '413': '请求或响应超过限制', '503': '服务暂时不可用'})) {
      item.responses[status] ||= {description, content: {'application/json': {schema: ref('UnsV2ErrorResponse')}}};
    }
    if (op.metadataAction || ['namespaceCopy','sourceReplace','streamReplace','appReplace'].includes(op.name)) {
      item.responses = {'200': {...item.responses['200'], description: op.mutation?'操作计数或对象，以及提交或预校验状态。':'查询结果。'},
        default:{description:'操作失败；具体原因、回滚状态和恢复建议见 error。',content:{'application/json':{schema:ref('UnsV2ErrorResponse')}}}};
    }
    if (op.example) item.requestBody.content['application/json'].example = Object.fromEntries(Object.entries(op.example).filter(([key]) => !op.pathParams?.includes(key)));
    if (op.pathParams?.length) {
      item.parameters = op.pathParams.map(name => ({name, in: 'path', required: true, schema: id}));
      const body = structuredClone(spec.components.schemas[op.request]);
      for (const key of op.pathParams) { delete body.properties[key]; body.required = (body.required || []).filter(name => name !== key); }
      item.requestBody.content['application/json'].schema = body;
    }
  }
  const descriptions = {'业务数据': '业务记录查询、写入、软删除、恢复、租约锁与跨集合事务。业务字段直接使用字段名，系统字段使用 _id 和 _meta.*。', '时序数据': '采样追加、多记录查询、聚合、最新值与显式时间范围删除。最新值仅指真实 Sample。'};
  spec.tags = [...new Set(operations.filter(op => !op.coreOnly).map(dataOperationTag))].map(name => ({name, description: descriptions[name] || name + '接口'}));
  cleanDescriptions(spec);
  for (const path of Object.values(spec.paths)) for (const [method,item] of Object.entries(path)) if (['get','post'].includes(method) && (item.tags?.length !== 1 || item.tags[0] === 'default')) throw new Error('Every public operation must have exactly one category');
}
function cleanDescriptions(value) {
    if (!value || typeof value !== 'object') return;
    for (const [key, child] of Object.entries(value)) {
      if (typeof child === 'string' && ['description', 'summary', 'title'].includes(key)) value[key] = child.replace(/\bNova\s*/gi, '').replace(/only binSize=1/g, 'positive binSize').replace(/binSize=1/g, 'binSize 为正整数');
      else cleanDescriptions(child);
    }
}
