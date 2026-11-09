// 资源对象与查询语言分别建模；仅聚合返回管线文档。
const ref = name => ({$ref: `#/components/schemas/${name}`});
const object = (properties, required = Object.keys(properties)) => ({type: 'object', additionalProperties: false, properties, required});
const id = {type: 'string', minLength: 1, maxLength: 128};
const count = {type: 'integer', minimum: 0};
const timestamp = {type: 'integer', description: 'Unix 毫秒。'};
const document = {type: 'object', additionalProperties: true};
const array = items => ({type: 'array', items});
const nullable = schema => ({...structuredClone(schema), nullable: true});
const envelope = data => object({code: {type: 'integer'}, msg: {type: 'string'}, requestId: {type: 'string'}, data});

export function applyResponseContract(source, openapi) {
  const schemas = source.schemas;
  const setResponse = (name, shape) => {
    const operation = source.operations.find(op => op.name === name);
    if (!operation) throw new Error(`Missing response operation ${name}`);
    schemas[operation.response] = envelope(shape);
  };
  schemas.CloudRecordLockView = object({label: {type: 'string'}, expiresAt: timestamp, fence: count, since: timestamp});
  schemas.RecordDocument = object({id, collectionId: id, data: document, revision: count, createdAt: timestamp, updatedAt: timestamp,
    sourceId: nullable(id), sourceTime: nullable(timestamp), lock: ref('CloudRecordLockView')},
    ['id', 'collectionId', 'data', 'revision', 'createdAt', 'updatedAt', 'sourceId', 'sourceTime']);
  schemas.RecordDocument.description = '普通业务记录。业务字段位于 data；系统属性位于同级。filter/projection 使用业务裸字段名及 _id/_meta.*，不按本响应对象路径编写。lock 仅描述有效租约，不返回令牌。';
  schemas.CloudNullableRecordDocument = nullable(schemas.RecordDocument);
  schemas.TransactionQueryResult.properties.records = array(ref('RecordDocument'));
  schemas.Result.properties.data.description = '事务结果绑定使用的业务数据载荷；读取完整资源对象使用 document。';
  schemas.SampleDocument = object({id, collectionId: id, recordId: id, time: timestamp, receivedAt: timestamp,
    quality: {type: 'string'}, data: document, sourceId: nullable(id), streamId: id, bindingId: id},
    ['id', 'collectionId', 'recordId', 'time', 'receivedAt', 'quality', 'data', 'sourceId']);
  schemas.SampleDocument.description = '普通采样对象。业务字段位于 data；时间、质量、记录身份及可用来源信息位于同级。';
  schemas.CloudNullableSampleDocument = nullable(schemas.SampleDocument);
  schemas.CloudPageInfo = object({mode: {type: 'string', enum: ['cursor', 'offset']}, hasMore: {type: 'boolean'},
    nextCursor: nullable({type: 'string'}), nextPage: nullable({type: 'integer', minimum: 1}),
    snapshotScope: {type: 'string', enum: ['thisRequestOnly']}}, ['mode', 'hasMore', 'nextCursor', 'nextPage', 'snapshotScope']);
  schemas.CloudRecordPage = object({items: array(ref('RecordDocument')), pageInfo: ref('CloudPageInfo')});
  schemas.CloudSamplePage = object({items: array(ref('SampleDocument')), pageInfo: ref('CloudPageInfo')});
  schemas.CloudDataPage = object({items: array(document), pageInfo: ref('CloudPageInfo')});
  schemas.CloudDataPage.description = '聚合管线的输出文档，不包装为 Record/Sample。字段由 pipeline 决定。';
  setResponse('recordFind', ref('CloudRecordPage'));
  setResponse('sampleFind', ref('CloudSamplePage'));
  for (const name of ['recordAggregatePipeline', 'sampleAggregate']) setResponse(name, ref('CloudDataPage'));
  setResponse('recordCount', object({count,capped:{type:'boolean'}}));
  setResponse('recordGet',object({items:array(ref('RecordDocument')),missing:array(id)}));
  setResponse('recordDistinct', object({values: array({}), pageInfo: ref('CloudPageInfo')}));
  const distinct = source.operations.find(op => op.name === 'recordDistinct');
  schemas.CloudDistinctPage = object({mode: {type: 'string', enum: ['offset']}, page: {type: 'integer', minimum: 1, default: 1}, size: {type: 'integer', minimum: 1, maximum: 200, default: 20}}, []);
  schemas[distinct.request].properties.page = ref('CloudDistinctPage');
  schemas[distinct.request].allOf = [{not: {required: ['limit', 'page']}}];
  schemas[distinct.request].description = '去重值使用 offset 分页；首屏可用 limit，续页使用 page:{page:nextPage,size}，二者互斥。';

  const state = {delivery: ref('WriteDelivery'), status: {type: 'string', description: '执行状态；validated 为回滚预检，committed 为本机提交成功，accepted 为持久接收待上传（非中心落库），partiallyCommitted/failed 表示部分写入结果。'}, replayed: {type: 'boolean', description: '是否返回同一幂等请求的已保存结果。'}};
  setResponse('recordInsertOne', object({insertedId: id, value:ref('RecordDocument'), acceptedId:id, deliveryState:{type:'string',enum:['pendingUpload'],description:'Durably accepted for delivery, not confirmation of center storage.'},centerCommitted:{type:'boolean'}, ...state}, ['status','replayed']));
  for (const name of ['recordUpdateOne', 'recordReplaceOne']) setResponse(name,
    object({matchedCount: count, modifiedCount: count, upsertedId: id, ...state}, ['matchedCount', 'modifiedCount', ...Object.keys(state).filter(key=>key!=='delivery')]));
  setResponse('recordDeleteOne', object({deletedCount: count, ...state},['deletedCount','status','replayed']));
  for (const name of ['recordFindOneAndUpdate', 'recordFindOneAndReplace', 'recordFindOneAndDelete']) setResponse(name,
    object({value: ref('CloudNullableRecordDocument'), upsertedId: id, ...state}, ['value', ...Object.keys(state).filter(key=>key!=='delivery')]));

  schemas.CloudWriteError = object({index: count, id: {type: 'string'}, error: ref('Fault')}, ['index', 'error']);
  const indexIDs = {type: 'object', additionalProperties: id, description: '键为原始输入中从 0 开始的操作索引，值为对应成功记录 ID；部分失败时不重新编号。'};
  const batchState = {...state, writeErrors: array(ref('CloudWriteError')), hasMore: {type: 'boolean'}, resumeAfter: id};
  const progress = {nextIndex:{...count,description:"确定结果前缀后的首个操作序号；此前未列在 writeErrors 的操作已提交。"},unknownUntil:{...count,description:"半开区间 [nextIndex,unknownUntil) 结局未知；保留原请求和幂等键重试，不直接跳过或换键重写。"}};
  for (const [name, fields] of [
    ['recordInsertMany', {insertedCount: count, insertedIds: indexIDs}],
    ['recordUpdateMany', {matchedCount: count, modifiedCount: count}],
    ['recordDeleteMany', {deletedCount: count}],
    ['recordBulkWrite', {insertedCount: count, matchedCount: count, modifiedCount: count, deletedCount: count, upsertedCount: count, insertedIds: indexIDs, upsertedIds: indexIDs}],
   ]) {
    const scope = {...batchState};
    if (['recordInsertMany','recordBulkWrite'].includes(name)) { delete scope.resumeAfter; Object.assign(scope,progress); }
    else scope.outcomeUnknown={type:'boolean',description:'部分操作结局未知；此时不提供 resumeAfter。保留原意图和幂等键恢复。'};
    setResponse(name, object({...fields,...scope}, [...Object.keys(fields), ...Object.keys(state).filter(key=>key!=='delivery')]));
  }

  schemas.CloudRecordLeaseGrant = object({id, token: {type: 'string'}, expiresAt: timestamp, fence: count, record: ref('RecordDocument')});
  setResponse('recordLock', object({locks: array(ref('CloudRecordLeaseGrant'))}));
  setResponse('recordRenew', object({locks: array(object({id, expiresAt: timestamp}))}));
  setResponse('recordUnlock', object({unlockedCount: count}));
  setResponse('recordRestore', object({restoredCount: count, restoredIds: array(id), ...state}, ['restoredCount','restoredIds','status','replayed']));
  setResponse('sampleLatestValues', object({series: array(object({recordId: id, sample: ref('CloudNullableSampleDocument')}))}));
  setResponse('sampleDeleteRange', object({deletedCount: count, dryRun: {type: 'boolean'}}));
  schemas.Outcome.properties.status.description = 'Local execution lifecycle. accepted means durable upload acceptance only; results with pendingUpload are not center commits.';
  schemas.Result.properties.status.description = 'pendingUpload means durably accepted for remote delivery, not center storage confirmation.';
  // 事务及 Sample 追加保留已有 Outcome 生命周期，document 引用同一 RecordDocument。
  source.cloudContractRevision = source.cloudContractRevision.replace(/data\/2$/, 'data/3');
  Object.assign(openapi.components.schemas, structuredClone(schemas));
}
