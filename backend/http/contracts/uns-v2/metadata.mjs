// 元数据动作的共享生成源；内部接口和 OpenAPI 共用，领域校验仍由各 kind 的服务执行。
const ref = name => ({$ref: '#/components/schemas/' + name});
const object = (properties, required = []) => ({type: 'object', additionalProperties: false, properties, required});
const array = items => ({type: 'array', items, minItems: 1, maxItems: 200});
const doc = {type: 'object', additionalProperties: true};
const id = {type: 'string', minLength: 1, maxLength: 128};
const integer = {type: 'integer', minimum: 0};
const kind = {type: 'string', enum: ['directory', 'topic', 'collection']};
const revision = {type: 'integer', minimum: 1, description: '可选乐观锁；管理表单保存时携带读取到的 revision。冲突须重新读取。'};
const flags = {dryRun: {type: 'boolean', default: false, description: '仅校验并回滚，不预留资源。'}, idempotencyKey: {...id, description: '同一写入意图的重试复用此键；内容变化必须换键。'}};
const status = {status: {type: 'string', enum: ['validated', 'committed', 'partiallyCommitted', 'failed']}, replayed: {type: 'boolean'}};
const envelope = data => object({code: {type: 'integer'}, msg: {type: 'string'}, requestId: {type: 'string'}, data}, ['code', 'msg', 'requestId', 'data']);

export const retiredMetadataOperations = new Set([
  'metadata', 'metaBatchExecute', 'metaBatchValidate', 'metaBatchStatus',
  ...['directory', 'topic', 'collection'].flatMap(k => ['List', 'Get', 'Create', 'Update', 'Archive', 'Restore'].map(a => k + a)),
  'collectionSchemaUpdate', 'topicViewUpdate', 'collectionCreateForApp',
]);

export function applyMetadataContract(source, openapi) {
  const schemas = source.schemas;
  schemas.CollectionData.properties.parentId = structuredClone(schemas.DirectoryData.properties.parentId);
  const retired = source.operations.filter(o => retiredMetadataOperations.has(o.name));
  source.operations = source.operations.filter(o => !retiredMetadataOperations.has(o.name));
  for (const op of retired) delete openapi.paths[op.suffix];
  const filter = ref('UnsV2DataFilter');
  const sort = ref('UnsV2PipelineSort');
  const projection = structuredClone(schemas.UnsV2RecordFindReq.properties.projection);
  const state = {type: 'string', enum: ['active', 'deleted', 'all'], default: 'active', description: '有效、已软删除或全部对象。'};
  const page = object({mode: {type: 'string', enum: ['offset'], default: 'offset'}, page: {type: 'integer', minimum: 1, default: 1}, size: {type: 'integer', minimum: 1, maximum: 200, default: 20}});
  const limit = {type: 'integer', minimum: 1, maximum: 200};
  const selection = {kind, filter, sort, expectedRevision: revision};
  const update = object({$set: doc, $unset: doc, $setOnInsert: doc});
  update.minProperties = 1;
  update.description = '仅支持 $set/$unset/$setOnInsert；配置路径不带 data.，系统字段只读。数组按整个字段替换。';
  const upsert = {type: 'boolean', default: false, description: '仅单条更新/替换支持；filter 必须明确 _id 等值，新增配置须完整合法。同 ID 已删除对象不自动恢复。'};
  const returnDocument = {type: 'string', enum: ['before', 'after'], default: 'before'};
  const atomic = {type: 'boolean', default: true, description: '默认整批原子提交。false 时按项独立提交；每个对象仍执行完整权限和领域校验。'};
  const ordered = {type: 'boolean', default: true, description: 'atomic=false 时控制失败后停止或继续；原子批次不接受 ordered=false。'};
  const maxDocs = {type: 'integer', minimum: 1, maximum: 200, default: 200, description: '超过匹配上限整体拒绝，不截断写入范围。'};
  const byId = object({id, expectedRevision: revision}, ['id']);
  schemas.MetadataDocument = object({kind, id, originNodeId:{type:'string',description:'Fleet origin node identity for automatically synchronized Collections. Does not change edit permissions.'}, mountId: {type:'string',nullable:true,description:'挂载资源的来源标识；非空时配置只读。'}, revision: integer, createdAt: integer, updatedAt: integer, deletedAt: integer, data: doc}, ['kind', 'id', 'mountId', 'revision', 'createdAt', 'updatedAt', 'deletedAt', 'data']);
  schemas.MetadataNullableDocument = {nullable: true, allOf: [ref('MetadataDocument')]};
  schemas.MetadataPage = object({items: {type: 'array', items: ref('MetadataDocument')}, pageInfo: ref('CloudPageInfo')}, ['items', 'pageInfo']);
  for (const [k, input] of [['Directory','DirectoryData'], ['Topic','TopicData'], ['Collection','CollectionData']]) {
    const schema = structuredClone(schemas[input]);
    schema.required = (schema.required || []).filter(p => p !== 'schemaVersion');
    schema.description = '可写配置；schemaVersion 由服务端默认设为 1，未知字段拒绝。';
    schemas['Metadata' + k + 'Input'] = schema;
  }
  const writeResult = (properties, required) => object({...properties, ...status}, [...required, 'status', 'replayed']);
  const insertResult = {insertedId: id};
  const updateResult = {matchedCount: integer, modifiedCount: integer, upsertedId: id};
  const valueResult = {value: ref('MetadataNullableDocument'), upsertedId: id};
  const idMap = {type: 'object', additionalProperties: id, description: '键为原始输入的零基序号。'};
  const itemError = object({index: integer, error: ref('Fault')}, ['index', 'error']);
  const mixedFields = {insertedId: id, matchedCount: integer, modifiedCount: integer, deletedCount: integer, restoredCount: integer, upsertedId: id, value: ref('MetadataNullableDocument')};
  schemas.MetadataBulkResult = object({index: integer, ...mixedFields, error: ref('Fault')}, ['index']);
  schemas.MetadataTransactionResult = object({opId: id, ...mixedFields}, ['opId']);
  const bulkResult = {insertedCount: integer, matchedCount: integer, modifiedCount: integer, deletedCount: integer, restoredCount: integer, upsertedCount: integer, insertedIds: idMap, upsertedIds: idMap, results: {type:'array',items:ref('MetadataBulkResult')}, errors: {type:'array',items:itemError}};
  const baseDoc = {name: '装配车间', parentId: null};
  const add = (action, fields, required, result, summary, description, example, write = false) => {
    const name = 'metadata' + action[0].toUpperCase() + action.slice(1);
    const handler = 'UnsV2' + name[0].toUpperCase() + name.slice(1);
    const request = handler + 'Req', response = handler + 'Response';
    schemas[request] = object(fields, required);
    schemas[response] = envelope(result);
    const op = {name, handler, request, response, suffix: '/uns/metadata' + (action === 'insertOne' ? '' : ':' + action),
      resource: 'metadata', metadataAction: action, resourceId: write ? 214 : 212,
      mutation: write ? 'metadata.' + action : '', permission: 'uns.' + (write ? 'write' : 'read'),
      destructive: ['deleteOne','deleteMany','findOneAndDelete','bulkWrite','transact'].includes(action),
      appAllowed: ['find', 'findOne'].includes(action), summary, description, example};
    source.operations.push(op);
    return schemas[request];
  };
  const query = {kind, kinds: {...array(kind), maxItems:3, uniqueItems:true}, state, filter};
  const scopeQuery = shape => {shape.oneOf = [{required:['kind'],not:{required:['kinds']}},{required:['kinds'],not:{required:['kind']}}];};
  scopeQuery(add('find', {...query, sort, projection, page, limit}, [], ref('MetadataPage'), '查询元数据', '查询目录、主题、集合。配置字段直接使用字段名；_id 和 _meta.* 访问系统字段。返回 items/pageInfo。', {kinds:['directory','collection'],state:'active',filter:{parentId:null},page:{mode:'offset',page:1,size:20}}));
  add('findOne', {kind, state, filter, sort, projection}, ['kind','filter'], object({value:ref('MetadataNullableDocument')},['value']), '读取一条元数据', '按 filter/sort 选取一个对象，未匹配返回 value:null。', {kind:'directory',filter:{_id:'workshop-1'}});
  scopeQuery(add('count', query, [], object({count:integer},['count']), '统计元数据数量', '统计指定类型和状态内符合条件的对象。', {kinds:['collection'],filter:{}}));
  scopeQuery(add('distinct', {...query, field:id, page, limit}, ['field'], object({values:{type:'array',items:{}},pageInfo:ref('CloudPageInfo')},['values','pageInfo']), '查询元数据去重值', '对单一配置或系统字段取去重值，保留 JSON 类型。', {kinds:['collection'],field:'parentId'}));
  const stageNames=['$match','$project','$group','$sort','$skip','$limit'];
  const pipeline = {type:'array',maxItems:32,items:{oneOf:stageNames.map(name=>object({[name]:name==='$sort'?sort:name==='$skip'?integer:name==='$limit'?{...integer,minimum:1}:doc},[name]))}};
  scopeQuery(add('aggregate', {...query, pipeline, page, limit}, ['pipeline'], object({items:{type:'array',items:doc},pageInfo:ref('CloudPageInfo')},['items','pageInfo']), '聚合元数据', '支持 match/project/group/sort/skip/limit；仅查询允许的元数据类型，不跨表关联或写入。', {kinds:['collection'],pipeline:[{$group:{_id:'$parentId',count:{$sum:1}}}]}));
  const insert = add('insertOne', {kind,id,data:doc,...flags}, ['kind','data'], writeResult(insertResult,['insertedId']), '新增一条元数据', '创建目录、主题或集合，执行当前身份权限和配置校验；App 授权使用专用授权接口。', {kind:'directory',data:baseDoc},true);
  insert.oneOf = ['Directory','Topic','Collection'].map(k=>({properties:{kind:{enum:[k.toLowerCase()]},data:ref('Metadata'+k+'Input')}}));
  add('insertMany',{kind,documents:array(object({id,data:doc},['data'])),atomic,ordered,...flags},['kind','documents'],writeResult({insertedCount:integer,insertedIds:idMap,results:{type:'array',items:ref('MetadataBulkResult')},errors:{type:'array',items:itemError}},['insertedCount','insertedIds']),'批量新增元数据','同一 kind 批量创建，最多 200 项，默认整批原子。insertedIds 按原始输入零基序号对应。',{kind:'directory',documents:[{data:baseDoc}],atomic:true},true);
  for(const action of ['updateOne','findOneAndUpdate','replaceOne','findOneAndReplace','deleteOne','findOneAndDelete']) {
    const replacing=action.includes('Replace')||action==='replaceOne', deleting=action.includes('Delete')||action==='deleteOne', returning=action.startsWith('findOneAnd');
    const fields={...selection,...(!deleting?replacing?{replacement:doc,upsert}:{update,upsert}:{}),...(returning?{projection,...(!deleting?{returnDocument}:{})}:{}),...flags};
    const payload=deleting?{}:replacing?{replacement:baseDoc}:{update:{$set:{name:'装配二车间'}}};
    add(action,fields,['kind','filter',...(!deleting?[replacing?'replacement':'update']:[])],writeResult(returning?valueResult:deleting?{deletedCount:integer}:updateResult,returning?['value']:deleting?['deletedCount']:['matchedCount','modifiedCount']),
      ({updateOne:'更新一条元数据',findOneAndUpdate:'更新并返回元数据',replaceOne:'替换一条元数据',findOneAndReplace:'替换并返回元数据',deleteOne:'软删除一条元数据',findOneAndDelete:'软删除并返回原元数据'})[action],
      (deleting?'回收有效对象；目录连同有效子树回收，Collection 保留 Record/Sample 与引用并拒绝写入。':replacing?'完整替换可写配置并执行 Schema、索引和引用校验；系统字段保持服务端管理。':'选择和更新在同一事务中完成；执行领域校验。')+(returning?(deleting?'返回删除前对象。':'returnDocument 选择 before/after。'):'')+(!deleting?'upsert 新增要求明确 _id 和完整配置。':''),
      {kind:'directory',filter:{_id:'workshop-1'},...payload,...(returning&&!deleting?{returnDocument:'after'}:{})},true);
  }
  for(const action of ['updateMany','deleteMany'])add(action,{kind,filter,...(action==='updateMany'?{update}:{}),atomic,ordered,maxDocs,...flags},['kind','filter',...(action==='updateMany'?['update']:[])],writeResult(action==='updateMany'?{...updateResult,results:{type:'array',items:ref('MetadataBulkResult')},errors:{type:'array',items:itemError}}:{deletedCount:integer,results:{type:'array',items:ref('MetadataBulkResult')},errors:{type:'array',items:itemError}},action==='updateMany'?['matchedCount','modifiedCount']:['deletedCount']),action==='updateMany'?'批量更新元数据':'批量软删除元数据','显式 kind 内条件批量操作。默认原子；匹配数超过 maxDocs 整体拒绝。不同对象需要独立 revision 时使用 bulkWrite。',{kind:'directory',filter:{_id:{$in:['workshop-1','workshop-2']}},...(action==='updateMany'?{update:{$set:{description:'生产车间'}}}:{}),atomic:true,maxDocs:200},true);
  add('restore',{kind,items:array(byId),...flags},['kind','items'],writeResult({restoredCount:integer,items:{type:'array',items:ref('MetadataDocument')}},['restoredCount','items']),'按 ID 批量恢复元数据','整批原子恢复，复验当前名称、父目录和引用，revision 递增；不隐式恢复依赖。',{kind:'directory',items:[{id:'workshop-1',expectedRevision:7}]},true);
  add('purge',{kind,ids:{...array(id),uniqueItems:true},...flags},['kind','ids'],ref('Outcome'),'永久清理回收站','异步清理明确的回收站对象；目录包含已回收后代。记录与采样分批清理后删除元数据；返回 jobId，重试复用 idempotencyKey。',{kind:'collection',ids:['collection-1'],idempotencyKey:'purge-1'},true);
  source.operations.at(-1).destructive = true;
  const bulkItems = [];
  for (const action of ['insertOne','updateOne','replaceOne','deleteOne','restoreOne']) {
    let fields,required;
    if(action==='insertOne'){fields={id,data:doc};required=['data'];}
    else if(action==='restoreOne'){fields={id,expectedRevision:revision};required=['id'];}
    else {fields={filter,sort,expectedRevision:revision,...(action==='updateOne'?{update,upsert}:action==='replaceOne'?{replacement:doc,upsert}:{})};required=['filter',...(action==='updateOne'?['update']:action==='replaceOne'?['replacement']:[])];}
    schemas['MetadataBulk'+action[0].toUpperCase()+action.slice(1)] = object(fields,required);
    bulkItems.push(object({[action]:ref('MetadataBulk'+action[0].toUpperCase()+action.slice(1))},[action]));
  }
  add('bulkWrite',{kind,operations:array({oneOf:bulkItems}),atomic,ordered,...flags},['kind','operations'],writeResult(bulkResult,Object.keys(bulkResult)),'执行元数据混合批量写入','同一 kind 下最多 200 个操作。默认原子；非原子时 ordered 控制失败后是否继续，按原始序号返回逐项结果。',{kind:'directory',operations:[{insertOne:{data:baseDoc}}]},true);
  const bulkResponse=schemas.UnsV2MetadataBulkWriteResponse.properties.data;
  Object.assign(bulkResponse.properties,{nextIndex:{type:'integer',minimum:0,description:'确定结果前缀后的首个操作序号；此前未列在 errors 的项已提交。'},unknownUntil:{type:'integer',minimum:1,description:'[nextIndex,unknownUntil) 结局未知；保留原请求及幂等键重试。'}});
  schemas.MetadataTransactionOperation=object({opId:id,action:{type:'string',enum:['insertOne','updateOne','replaceOne','deleteOne','restoreOne']},kind,id,filter,sort,data:doc,update,replacement:doc,expectedRevision:revision,dependsOn:{type:'array',items:id,maxItems:200}},['opId','action','kind']);
  add('transact',{operations:array(ref('MetadataTransactionOperation')),...flags},['operations'],writeResult({results:{type:'array',items:ref('MetadataTransactionResult')}},['results']),'执行元数据事务','跨目录、主题、集合的原子操作。opId/dependsOn 控制依赖，parentId/collectionId 支持 {ref:opId} 引用创建结果。最多 200 项；dryRun=true 执行预校验后回滚。',{operations:[{opId:'parent',action:'insertOne',kind:'directory',data:baseDoc},{opId:'child',action:'insertOne',kind:'directory',data:{name:'产线一',parentId:{ref:'parent'}},dependsOn:['parent']}]},true);
  for(const op of source.operations.filter(o=>o.metadataAction)) {
    if (['insertMany','updateMany','deleteMany'].includes(op.metadataAction)) {
      const shape=schemas[op.request];
      shape.properties.atomic={type:'boolean',enum:[true],default:true,description:'整批原子提交；需要逐项提交时使用 bulkWrite。'};
      delete shape.properties.ordered;
      op.description = op.description.replace('atomic 控制整批原子性，ordered 控制遇到逐项失败时是否停止；','').replace('默认原子；','整批原子；');
    }
    const shape=schemas[op.request];
    const typedKinds=['Directory','Topic','Collection'];
    if(['replaceOne','findOneAndReplace'].includes(op.metadataAction))shape.oneOf=typedKinds.map(k=>({properties:{kind:{enum:[k.toLowerCase()]},replacement:ref('Metadata'+k+'Input')},required:['kind','replacement']}));
    if(op.metadataAction==='insertMany')shape.oneOf=typedKinds.map(k=>({properties:{kind:{enum:[k.toLowerCase()]},documents:{type:'array',items:object({id,data:ref('Metadata'+k+'Input')},['data'])}},required:['kind','documents']}));
    Object.assign(openapi.components.schemas, {[op.request]:schemas[op.request],[op.response]:schemas[op.response]});
    const request=schemas[op.request];
    if(request.properties.page&&request.properties.limit)request.allOf=[...(request.allOf||[]),{not:{required:['page','limit']}}];
  }
  schemas.MetadataTransactionOperation.oneOf = ['insertOne','updateOne','replaceOne','deleteOne','restoreOne'].map(action=>({
    properties:{action:{enum:[action]}},required:['action',...(action==='insertOne'?['data']:action==='updateOne'?['update']:action==='replaceOne'?['replacement']:action==='restoreOne'?['id']:[])],
    ...(action!=='insertOne'&&action!=='restoreOne'?{oneOf:[{required:['id'],not:{required:['filter']}},{required:['filter'],not:{required:['id']}}]}:{}),
    not:{anyOf:(action==='insertOne'?['filter','update','replacement','expectedRevision','sort']:action==='updateOne'?['data','replacement']:action==='replaceOne'?['data','update']:['data','update','replacement']).map(key=>({required:[key]}))}
  }));
  schemas.MetadataTransactionOperation.allOf=[{oneOf:['Directory','Topic','Collection'].map(k=>({properties:{kind:{enum:[k.toLowerCase()]},data:ref('Metadata'+k+'Input'),replacement:ref('Metadata'+k+'Input')},required:['kind']}))}];
  // 复制层级及专用配置替换保留领域语义，不开放为通用元数据种类。
  const specialized = (name, suffix, resource, permission, resourceId, properties, required, result, summary, description) => {
    const handler='UnsV2'+name[0].toUpperCase()+name.slice(1),request=handler+'Req',response=handler+'Response';
    schemas[request]=object({...properties,...flags},required);
    schemas[response]=envelope(writeResult(result,Object.keys(result)));
    source.operations.push({name,handler,suffix,request,response,resource,permission,resourceId,mutation:name==='namespaceCopy'?'meta.copy':'meta.replace',appAllowed:false,destructive:false,summary,description});
  };
  const placement=schemas.MetadataDirectoryInput.properties;
  specialized('namespaceCopy','/uns/namespace:copy','namespace','uns.write',214,
    {kind:{type:'string',enum:['directory','topic']},id,expectedRevision:revision,data:object({name:placement.name,parentId:placement.parentId,description:placement.description})},['kind','id','data'],{insertedId:id},
    '复制目录或展示主题','目录复制保留树结构，主题复制保留显式引用；不复制 Collection/Record/Sample。挂载资源只读，权限、名称、父目录及引用校验仍执行。');
  for (const [resource,plural,input,permission,resourceId,label] of [['source','sources','SourceData','uns.source.manage',3301,'数据源'],['stream','streams','StreamData','uns.stream.manage',3311,'处理流'],['app','apps','AppData','uns.app.manage',3331,'App']]) {
    specialized(resource+'Replace',`/uns/${plural}:replace`,resource,permission,resourceId,
      {id,expectedRevision:revision,replacement:ref(input)},['id','replacement'],{matchedCount:integer,modifiedCount:integer},
      `完整替换${label}配置`,'按当前领域规则整体替换配置；系统字段不可写，不能绕过凭证、发布版本、关联资源和授权校验。');
    if (resource === 'app') source.operations.at(-1).mcp=false;
  }
  source.cloudContractRevision += '+metadata/1';
}
