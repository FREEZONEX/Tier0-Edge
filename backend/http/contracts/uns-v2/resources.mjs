const text = {type: 'string'};
const identity = {type: 'string', minLength: 1, maxLength: 128};
const ref = name => ({$ref: `#/components/schemas/${name}`});
const array = schema => ({type: 'array', items: schema});
const object = (properties, required = []) => ({type: 'object', additionalProperties: false, properties, required});
const page = {page: {type: 'integer', minimum: 1}, size: {type: 'integer', minimum: 1, maximum: 200}, search: text};
export const cursorPage = {
  paginationMode: {type: 'string', enum: ['cursor'], description: 'Opt in to bounded cursor traversal; page must be omitted or 1.'},
  cursor: {type: 'string', maxLength: 2048, description: 'Opaque nextCursor from the preceding response; keep the query unchanged.'},
  asOf: {type: 'integer', minimum: 1, description: 'Traversal cutoff in Unix milliseconds; not a fixed database snapshot.'},
  includeTotal: {type: 'boolean', description: 'In cursor mode, false skips exact counting and returns total=null.'},
};
const write = {idempotencyKey: {...identity, description:'Optional durable intent key, shared across ordinary UNS resource routes for the same user/API key/MCP grant. Use a different key for each new write intent; retry the original identity, route, body and key. Not isolated per route.'}, dryRun: {type: 'boolean'}};
const partial = {allowPartial:{type:'boolean',default:false,description:'Optional. false keeps whole-request rollback; true independently commits each item, continues after business errors and returns per-item errors. Transactions never accept this field. Partial writes retain a jobId for recovery.'}};
const revision = {id: identity, expectedRevision: {type: 'integer', minimum: 1, description:'Optional optimistic concurrency guard; omission updates the current object.'}};
const metadata = {name: text, description: text};
const parentId = {type: 'string', nullable: true, maxLength: 128};
const resources = [
  ['directory', 'directories', 'DirectoryData', 212, 214, 'uns.read', 'uns.write'],
  ['topic', 'topics', 'TopicData', 212, 214, 'uns.read', 'uns.write'],
  ['collection', 'collections', 'CollectionData', 212, 214, 'uns.read', 'uns.write'],
  ['source', 'sources', 'SourceData', 3300, 3301, 'uns.source', 'uns.source.manage'],
  ['stream', 'streams', 'StreamData', 3310, 3311, 'uns.stream', 'uns.stream.manage'],
  ['app', 'apps', 'AppData', 3330, 3331, 'uns.app.read', 'uns.app.manage'],
];
export function resourceContract(schemas) {
  const operations = [];
  const paged = (item, extra = {}) => object({
    ...structuredClone(schemas.PaginationResult.properties), list: array(item), ...extra,
  }, ['list', ...schemas.PaginationResult.required]);
  metadata.name = schemas.Header.properties.name;
  metadata.description = schemas.Header.properties.description;
  for (const [kind, , model] of resources) {
    const view = structuredClone(schemas[model]);
    if (kind === 'source') Object.assign(view.properties, {configRevision: {type: 'integer'}});
    if (kind === 'stream') view.properties.releases = {type: 'object', additionalProperties: ref('Release')};
    schemas[`${model}View`] = view;
  }
  const add = (kind, plural, action, fields, required, options = {}) => {
    const resource = resources.find(row => row[0] === kind);
    const mutation = options.mutation || '';
    const name = `${kind}${action.split(/[/-]/).map(part => part[0].toUpperCase() + part.slice(1)).join('')}`;
    if (['recordGet','recordList','sampleHistory','sampleLatest'].includes(name)) options = {...options, deprecated:true, replacedBy:kind === 'record' ? 'records' : 'samples'};
    const handler = `UnsV2${name[0].toUpperCase()}${name.slice(1)}`;
    let properties = {...(mutation ? write : {}), ...fields};
    required = [...required];
    required = required.filter(key=>key!=='expectedRevision');
    const bulk = (kind==='record' && ['create','update','replace','delete'].includes(action)) || (kind==='sample' && action==='append');
    if (bulk) {
      const item = {...fields}; delete item.collectionId;
      if(kind==='sample') delete item.recordId;
      const itemRequired = required.filter(key=>key!=='collectionId' && key!=='expectedRevision' && !(kind==='sample' && key==='recordId'));
      properties = {...write, ...partial, collectionId:identity, ...(kind==='sample'?{recordId:identity}:{}), mode:{type:'string',enum:['auto','sync','async']}, items:{type:'array',minItems:1,maxItems:10000,items:object(item,itemRequired)}};
      required = ['collectionId',...(kind==='sample'?['recordId']:[]),'items'];
    }
    const request = `${handler}Req`;
    if (mutation === 'runtime') {
      properties.dryRun = {...properties.dryRun, default:false};
      required = required.filter(key => key !== 'dryRun');
    } else if (mutation) {
      required.unshift('dryRun');
    }
    schemas[request] = object(properties, [...new Set(required)]);
    const response = `${handler}Response`;
    const definition = object({kind: {type: 'string', enum: [kind]}, id: identity, revision: {type: 'integer'}, data: ref(resource?.[2] || 'ResourceData'), createdAt: {type: 'integer'}, updatedAt: {type: 'integer'}, deletedAt: {type: 'integer'}}, ['kind', 'id', 'revision', 'data']);
    definition.properties.data = resource ? ref(`${resource[2]}View`) : {type: 'object'};
    if (kind === 'app') definition.properties.launch = object({status: {type:'string', enum:['ready','unconfigured','invalid','environmentMissing','environmentInvalid','disabled']}, url: text}, ['status']);
    let responseData = definition;
    if (mutation) responseData = object({status: {type: 'string', enum: ['committed', 'validated', 'accepted']}, replayed: {type: 'boolean'}, jobId: text, result: ref('Result')}, ['status', 'replayed']);
    else if (action === 'list' || action === 'history') responseData = paged(kind === 'record' || kind === 'sample' ? {type: 'object'} : definition, kind === 'record' ? {latestSamples: array({type: 'object'})} : {});
    else if (action === 'latest') responseData = object({latestSamples: array({type: 'object'})}, ['latestSamples']);
    else if (action === 'session/inspect') responseData = ref('AppSessionIdentity');
    else if (action === 'bindings/list') responseData = object({sourceId: identity, revision: {type: 'integer'}, bindings: {type: 'object', additionalProperties: ref('Binding')}}, ['sourceId', 'revision', 'bindings']);
    else if (action === 'bindings/get') responseData = object({sourceId: identity, revision: {type: 'integer'}, bindingId: identity, binding: ref('Binding')}, ['sourceId', 'revision', 'bindingId', 'binding']);
    else if (action === 'runtime/get') responseData = object({sourceId:identity,revision:{type:'integer'},state:object({schemaVersion:{type:'integer'},health:text,healthMessage:text,lastDataAt:{type:'integer'},checkedAt:{type:'integer'},resources:{type:'array',items:{type:'object'}},streams:{type:'object',additionalProperties:object({revision:{type:'integer'},status:{type:'string',enum:['disabled','starting','running','idle','failed','reconfiguring']},leaseUntil:{type:'integer'},nextRunAt:{type:'integer'},cycleId:text,page:{type:'integer'},cursor:text,lastDataAt:{type:'integer'},records:{type:'integer'},sequence:{type:'integer'},retries:{type:'integer'},phase:text,lastError:text},['status'])}},['streams'])},['sourceId','revision','state']);
    else if (action === 'releases/get') responseData = object({streamId:identity,release:{type:'integer'},publishedAt:{type:'integer'},definition:ref('StreamDefinition')},['streamId','release','publishedAt','definition']);
    else if (action === 'releases/list') responseData = paged(object({streamId:identity,release:{type:'integer'},publishedAt:{type:'integer'},definition:ref('StreamDefinition')},['streamId','release','publishedAt','definition']));
    else if (kind === 'record') responseData = {type: 'object', description: 'Record identity, collectionId, revision, data and timestamps'};
    if(bulk) responseData=ref('Outcome');
    if(options.runtime) responseData={type:'object',additionalProperties:true,description:'Existing Source/Stream runtime domain result. Program create/get/deploy return id, revision, activeVersion, executionState and redacted document; versions/runs return list and total; probe returns health; validation returns valid and sideEffects.'};
    schemas[response] = object({code: {type: 'integer'}, msg: text, requestId: text, data: responseData}, ['code', 'msg', 'requestId', 'data']);
    operations.push({name, handler, suffix: `/uns/${plural}/${action}`, request, response, resourceId: options.resourceId || resource?.[mutation ? 4 : 3] || (mutation ? 214 : 212), resource: kind, mutation, permission: options.permission || resource?.[mutation ? 6 : 5] || (mutation ? 'uns.write' : 'uns.read'), summary: options.summary || `${action} ${kind}`, ...options});
  };
  for (const [kind, plural, model] of resources) {
    add(kind, plural, 'list', {...page, archived: {type: 'boolean'}}, [], {summary: `List ${plural}; archived=true selects archived objects only`});
    add(kind, plural, 'get', {id: identity}, ['id'], {summary: `Get one ${kind} definition and revision, without data expansion`});
    const properties = {...schemas[model]?.properties};
    delete properties.schemaVersion;
    let required = (schemas[model]?.required || []).filter(key => key !== 'schemaVersion');
    if (kind === 'source') {
      delete properties.config;
      properties.connection = ref('SourceDefinition');
      required = ['name', 'adapter', 'connection'];
    }
    if (kind === 'app') {
      for (const key of ['roles', 'members', 'collections', 'navigation']) delete properties[key];
      required = ['name'];
    }
    add(kind, plural, 'create', {id: identity, ...properties}, required, {mutation: 'meta.create', summary: `Create ${kind}; schemaVersion is assigned by the server`});
    add(kind, plural, 'update', {...revision, ...metadata, ...(['directory', 'topic'].includes(kind) ? {parentId} : {}), ...(kind === 'app' ? {status: {type: 'string', enum: ['active', 'disabled']}, entry: {...schemas.AppData.properties.entry, nullable: true}} : {})}, ['id', 'expectedRevision'], {mutation: 'meta.resourcePatch', summary: `Update ${kind} metadata only; omitted fields are preserved`});
    if (['directory', 'topic', 'collection', 'source'].includes(kind)) {
      for (const action of ['archive', 'restore']) add(kind, plural, action, revision, ['id', 'expectedRevision'], {mutation: `meta.${action}`, ...(action === 'archive' ? {permission: 'uns.delete', resourceId: 215} : {})});
    }
  }
  const patch = (kind, plural, action, fields, required, options = {}) => add(kind, plural, action, {...revision, ...fields}, ['id', 'expectedRevision', ...required], {mutation: 'meta.resourcePatch', ...options});
  add('app', 'apps', 'session/inspect', {appId: identity, token: {type: 'string', minLength: 1, maxLength: 16384, writeOnly: true}}, ['appId', 'token'], {permission: 'uns.read', resourceId: 212, mcp: false, summary: 'Verify a human platform session for the caller App; OpenAPI requires an App-bound API Key matching appId; internal callers may inspect only themselves; returns minimal identity and current App roles, never credentials; no implicit administrator membership'});
  patch('app', 'apps', 'platform-permissions/update', {platformPermissions: schemas.AppData.properties.platformPermissions}, ['platformPermissions'], {mcp: false, summary: 'Replace explicit App platform permissions; empty array revokes all; permissions cannot exceed issuer resources; MCP consent is unavailable'});
  patch('collection', 'collections', 'schema/update', {recordSchema: ref('Schema'), sampleSchema: {allOf: [ref('Schema')], nullable: true}, indexes: {...array(ref('IndexSpec')), maxItems:16}}, ['recordSchema'], {summary: 'Replace Collection schemas; omitted sampleSchema and indexes are preserved; indexes=[] removes declarations; indexes build asynchronously; existing data must satisfy constraints; may return a job'});
  patch('topic', 'topics', 'view/update', {viewMode: {type: 'string', enum: ['records', 'fields']}, collectionId: identity, fields: array(ref('FieldBinding'))}, ['viewMode'], {summary: 'Replace Topic view configuration; incompatible previous view fields are cleared'});
  patch('stream', 'streams', 'draft/update', {draft: ref('StreamDefinition')}, ['draft']);
  add('stream', 'streams', 'publish', revision, ['id', 'expectedRevision'], {mutation: 'stream.publish', summary: 'Publish an immutable Stream release on its execution node'});
  patch('source', 'sources', 'connection/update', {connection: ref('SourceDefinition')}, ['connection'], {summary: 'Replace non-secret Source connection configuration; preserve existing credentialRef'});
  add('source','sources','runtime/get',{id:identity},['id'],{summary:'Read observed Source and per-Stream acquisition health, pagination checkpoint, counts and last error; no payloads or credentials'});
  add('stream','streams','releases/list',{id:identity,...page},['id'],{summary:'List immutable shared transform releases in descending release order; this is not a program deployment version'});
  add('stream','streams','releases/get',{id:identity,release:{type:'integer',minimum:1}},['id','release'],{summary:'Read one immutable shared transform release with parameter/output schemas; processor code is redacted'});
  patch('stream','streams','releases/restore',{release:{type:'integer',minimum:1}},['release'],{summary:'Restore a shared release into the draft on the server, retaining hidden processor code; does not change enabled Bindings or publish a new release'});
  // v1 runtime lifecycle reuses the existing sourcestream domain. It is kept
  // separate from shared transforms and never aliases a release to a version.
  const lifecycle=(kind,action,fields,required,permission,writeOperation=false)=>add(kind,kind==='source'?'sources':'streams',action,fields,required,{runtime:true,...(writeOperation?{mutation:'runtime'}:{}),permission,resourceId:permission==='uns.source'?3300:kind==='source'?3301:permission==='uns.stream'?3310:permission==='uns.stream.deploy'?3312:permission==='uns.stream.execute'?3313:3311,summary:`${action}: reuse the existing Source/Stream runtime lifecycle; runtime IDs are positive decimal strings; no shared release is implicitly deployed`});
  lifecycle('source','protocols/list',{},[],'uns.source');
  const edgeId = {type:'string',pattern:'^[1-9][0-9]*$',maxLength:19};
  const edgeRevision = {type:'integer',minimum:1};
  const resourceRef = {type:'string',minLength:1,maxLength:1024};
  const readLimit = {type:'integer',minimum:1,maximum:100};
  const edgeRead = {runtime:true,permission:'uns.source',resourceId:3300};
  add('source','sources','resource/catalog',{id:edgeId,configRevision:edgeRevision},['id'],{
    ...edgeRead,suffix:'/uns/sources/resources/catalog',summary:'Describe the read-only resources declared on this native Source, including mode, parameters, result shape and required permission.'});
  const dateTime = {type:'string',format:'date-time',maxLength:48};
  const sapFilters={material:{type:'string',pattern:'^[0-9]{18}$'},baseUnit:{type:'string',pattern:'^[A-Z0-9]{1,8}$'},selectFields:{type:'array',items:{type:'string',enum:['Material','Description','BaseUnit']},minItems:1,maxItems:3,uniqueItems:true},order:{type:'string',enum:['materialAsc','materialDesc']}};
  add('source','sources','resource/read',{id:edgeId,configRevision:edgeRevision,resourceRef,params:object({...sapFilters,itemId:{type:"string",pattern:"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$"},startTime:dateTime,endTime:dateTime,matnr:{type:'string',pattern:'^[A-Za-z0-9]{1,18}$'},since:dateTime,jobId:{type:'string',format:'uuid'},beginDate:dateTime,endDate:dateTime,limit:readLimit,createdDateTimeBegin:dateTime,createdDateTimeEnd:dateTime}),timeoutMs:{type:'integer',minimum:100,maximum:10000}},
    ['id','configRevision','resourceRef'],{...edgeRead,suffix:'/uns/sources/resources/preview',summary:'Perform one bounded read on an enabled native Source at the specified revision. Use the instance catalog for supported parameters and output fields.'});
  add('source','sources','resource/scan',{id:edgeId,configRevision:edgeRevision,resourceRef,limit:{type:'integer',minimum:1,maximum:200},cursor:{type:'string',maxLength:4096},
    params:object({...sapFilters,maxItems:{type:'integer',minimum:0,maximum:10000,description:'Zero omits the total result budget; every page remains bounded by limit.'},createdDateTimeBegin:dateTime,createdDateTimeEnd:dateTime}),timeoutMs:{type:'integer',minimum:100,maximum:10000}},
    ['id','configRevision','resourceRef','limit'],{...edgeRead,suffix:'/uns/sources/resources/scan',summary:'Read one bounded native page of a resource declared by this Source. Pass the issued opaque cursor unchanged with the same parameters, caller, resource and revision.'});
  const resourceDescriptor = object({resourceRef,name:text,modes:array(text),permission:text,requiredPermissions:array(text),configRevision:edgeRevision,
    inputSchema:{type:'object',additionalProperties:true},outputSchema:{type:'object',additionalProperties:true},metadata:{type:'object',additionalProperties:true}},
    ['resourceRef','name','modes','permission','configRevision','inputSchema','outputSchema']);
  schemas.UnsV2SourceResourceCatalogResponse.properties.data = object({id:edgeId,configRevision:edgeRevision,protocol:text,sourceType:text,resources:array(resourceDescriptor)},
    ['id','configRevision','protocol','sourceType','resources']);
  schemas.UnsV2SourceResourceReadResponse.properties.data = object({resourceRef,configRevision:edgeRevision,collectedAt:{type:'integer'},
    items:array({type:'object',additionalProperties:true}),returned:{type:'integer'},truncated:{type:'boolean'},
    unitId:{type:'integer'},address:{type:'integer'},table:text,values:array({oneOf:[{type:'integer'},{type:'boolean'}]})},
    ['resourceRef','configRevision','collectedAt']);
  schemas.UnsV2SourceResourceScanResponse.properties.data = object({resourceRef,configRevision:edgeRevision,collectedAt:{type:'integer'},
    items:array({type:'object',additionalProperties:true}),nextCursor:{type:'string'},hasMore:{type:'boolean'},truncated:{type:'boolean'},checkpoint:{type:'string',description:'When the resource supports incremental scans, persist this opaque position only after all page items commit. Resume a later scan by supplying it as cursor.'}},
    ['resourceRef','configRevision','collectedAt','items','nextCursor','hasMore']);
  add('source','sources','runtime/read',{
    id:{type:'string',pattern:'^[1-9][0-9]*$',maxLength:19},
    configRevision:{type:'integer',minimum:1},
    range:object({id:{type:'string',minLength:1,maxLength:128},table:{type:'string',enum:['holding-register','input-register','coil','discrete-input']},address:{type:'integer',minimum:0,maximum:65535},quantity:{type:'integer',minimum:1,maximum:2000}},['id','table','address','quantity']),
    timeoutMs:{type:'integer',minimum:100,maximum:10000},
  },['id','configRevision','range','timeoutMs'],{runtime:true,permission:'uns.source',resourceId:3300,mcp:false,summary:'Read one Modbus range on this Edge using an enabled native Source at exactly configRevision; fixed read operation; endpoint, credentials and unit ID remain local; no subscriptions or writes'});
  add('source','sources','credentials/list',{id:identity},['id'],{runtime:true,permission:'uns.source',resourceId:3300,summary:'List Edge-local shared Source credential references; never returns secrets; string Source IDs'});
  add('source','sources','credentials/save',{id:identity,sourceRevision:{type:'integer',minimum:1},credentialRef:identity,expectedCredentialRevision:{type:'integer',minimum:0},kind:{type:'string',enum:['basic','bearer','custom','http']},username:{type:'string',maxLength:1024,writeOnly:true},secret:{type:'string',maxLength:65536,writeOnly:true},values:{type:'object',additionalProperties:{type:'string'},maxProperties:128,writeOnly:true}},['id','sourceRevision','expectedCredentialRevision','kind'],{runtime:true,mutation:'runtime',mcp:false,permission:'uns.source.credentials',resourceId:3302,summary:'Provision or rotate a Source-scoped encrypted HTTP credential or MQTT client basic or mTLS slot, or OPC UA client basic slot; MQTT mTLS uses kind=custom with clientCertificatePem, clientPrivateKeyPem and caCertificatePem in values; rotation reconnects the selected native watch; CAS without configuration selection or deployment; never returns secrets'});

  lifecycle('source','validate',{name:text,adapter:schemas.SourceData.properties.adapter,connection:ref('SourceDefinition')},['name','adapter','connection'],'uns.source.manage');
  lifecycle('source','probe',{id:identity,timeoutMs:{type:'integer',minimum:100,maximum:10000},dryRun:{type:'boolean'}},['id','timeoutMs','dryRun'],'uns.source.manage',true);
  lifecycle('source','discover',{...revision,parentResourceRef:text,dryRun:{type:'boolean'}},['id','expectedRevision','dryRun'],'uns.source.manage',true);
  const programDocument=ref('RuntimeStreamDefinition');
  lifecycle('stream','program/validate',{document:programDocument,expectedSourceRevision:{type:'integer'}},['document'],'uns.stream.manage');
  lifecycle('stream','program/test',{document:programDocument,input:ref('RuntimeProgramTestInput'),phase:{type:'string',enum:['input','fetch']},entryId:text,expectedSourceRevision:{type:'integer'},dryRun:{type:'boolean'}},['document','input','phase','dryRun'],'uns.stream.manage',true);
  lifecycle('stream','program/create',{document:programDocument,dryRun:{type:'boolean'}},['document','dryRun'],'uns.stream.manage',true);
  lifecycle('stream','program/update',{...revision,document:programDocument,dryRun:{type:'boolean'}},['id','expectedRevision','document','dryRun'],'uns.stream.manage',true);
  lifecycle('stream','program/get',{id:identity},['id'],'uns.stream');
  lifecycle('stream','program/versions/list',{id:identity,...page},['id'],'uns.stream');
  for(const action of ['deploy','disable','rollback'])lifecycle('stream',`program/${action}`,{...revision,expectedSourceRevision:{type:'integer'},...(action==='rollback'?{versionId:identity}:{}),dryRun:{type:'boolean'}},['id','expectedRevision','dryRun',...(action==='rollback'?['versionId']:[])],'uns.stream.deploy',true);
  lifecycle('stream','program/runs/list',{id:identity,...page,status:text},['id'],'uns.stream');
  lifecycle('stream','program/runs/get',{id:identity,runId:identity},['id','runId'],'uns.stream');
  lifecycle('stream','program/invoke',{id:identity,expectedActiveVersion:identity,entryId:text,input:{},timeoutMs:{type:'integer',minimum:100,maximum:30000},dryRun:{type:'boolean'}},['id','expectedActiveVersion','input','timeoutMs','dryRun'],'uns.stream.execute',true);
  for (const [field, shape] of [['collections', array(ref('Grant'))], ['navigation', object({topicIds: array(identity)}, ['topicIds'])], ['members', array(ref('Member'))], ['roles', array(ref('Role'))]]) {
    const action = field === 'collections' ? 'grants' : field;
    patch('app', 'apps', `${action}/update`, {[field]: shape}, [field], {mcp: !['members', 'roles'].includes(field), summary: field === 'navigation' ? 'Replace the App associated Topic list (navigation.topicIds); this does not configure page menus or grant data access; all other App fields are preserved' : `Replace App ${field} only; all other App fields are preserved`});
  }
  add('record', 'records', 'list', {...page, ...cursorPage, collectionId: identity, filters: array(ref('UnsV2RecordFilter')), orderBy: array(ref('UnsV2RecordOrder')), includeLatestSample: {type: 'boolean'}}, ['collectionId']);
  add('record', 'records', 'get', {collectionId: identity, id: identity}, ['collectionId', 'id']);
  const selection={selection:{type:'string',enum:['exactOne','first'],default:'exactOne'},sort:{allOf:[ref('UnsV2PipelineSort')],description:'Only selection=first; id ascending is appended for stable ties.'},returnDocument:{type:'string',enum:['before','after'],description:'Return the selected document before/after this write in result.document; omitted by default.'}};
  for (const action of ['create', 'replace', 'delete']) add('record', 'records', action, {collectionId: identity, ...(action === 'create' ? {id:identity} : {filter:ref('UnsV2DataFilter'), expectedRevision: revision.expectedRevision}), ...(action === 'replace' ? selection : {}), ...(action !== 'delete' ? {data: {type: 'object', additionalProperties: true}} : {})}, ['collectionId', ...(action !== 'create' ? ['filter'] : []), ...(action !== 'delete' ? ['data'] : [])], {mutation: `record.${action}`, ...(action === 'delete' ? {permission: 'uns.delete', resourceId: 215, destructive: true} : {})});
  add('sample', 'samples', 'history', {page: page.page, size: page.size, ...cursorPage, collectionId: identity, recordIds: array(identity), timeRange: ref('UnsV2TimeRange')}, ['collectionId']);
  add('sample', 'samples', 'latest', {collectionId: identity, recordIds: {...array(identity), minItems: 1, maxItems: 200}}, ['collectionId', 'recordIds']);
  add('record', 'records', 'update', {collectionId: identity, filter:ref('UnsV2DataFilter'), expectedRevision: revision.expectedRevision, ...selection, operations: {type:'array', minItems:1,maxItems:32,items:{$ref:'#/components/schemas/UnsV2FieldOperation'}}}, ['collectionId','filter','operations'], {mutation:'record.update',summary:'Update one Record selected by shared filter; exactOne by default, explicit first selection with optional sort; optional before/after document'});
  add('sample', 'samples', 'append', {collectionId: identity, recordId: identity, id: identity, time: {type: 'integer'}, quality: text, data: {type: 'object', additionalProperties: true}}, ['collectionId', 'recordId', 'time', 'data'], {mutation: 'sample.append'});
  const recordSchema = {type: 'object', properties: {name: {type: 'string'}}, required: ['name'], additionalProperties: false};
  const draft = {supportedProtocols: ['http-api'], parametersSchema: {type: 'object', properties: {}, additionalProperties: false}, processor: {language: 'javascript', code: 'return input;', timeoutMs: 1000}, output: {recordSchema}};
  const examples = {id: 'example-id', expectedRevision: 1, idempotencyKey: 'unique-request-key', dryRun: true, name: 'Example', description: 'Example metadata update', viewMode: 'records', collectionId: 'example-collection', recordId: 'example-record', recordIds: ['example-record'], bindingId: 'example-binding', adapter: 'http-api', connection: {endpoint: 'https://example.com/data', role: 'client', runtimeLocation: 'local', http: {authentication: 'none'}}, draft, recordSchema, collections: [{alias: 'stations', collectionId: 'example-collection', permissions: ['record:read']}], navigation: {topicIds: []}, members: [], roles: [], time: 1700000000000, data: {name: 'Station 1'}, binding: {name: 'Station binding', streamId: 'example-stream', streamRelease: 1, enabled: false, acquisition: {}, parameters: {}, target: {collectionId: 'example-collection'}, mapping: {recordIdPath: 'id', recordPath: ''}}};
  for (const operation of operations) {
    const itemSchema=schemas[operation.request].properties.items?.items;
    if(itemSchema) examples.items=[Object.fromEntries(itemSchema.required.map(key=>[key,key==='operations'?[{op:'set',path:'data.name',value:'Updated'}]:key==='filter'?{id:'example-record'}:examples[key]]))];
    examples.platformPermissions = [];
    operation.example = Object.fromEntries(schemas[operation.request].required.map(key => [key, examples[key]]));
    if (operation.mutation === 'runtime') operation.example = {dryRun:true, ...operation.example};
    const runtimeExamples={...examples,timeoutMs:5000,enabled:false,release:1,streamId:'example-stream',streamRelease:1,versionId:'123456789',runId:'example-run',expectedActiveVersion:'123456789',entryId:'read',phase:'input',input:{value:1},document:{name:'Example program',script:'module.exports=api=>{api.stream.onRequest({id:"read"},()=>({ok:true}));};',timeoutMs:1000}};
    for(const key of schemas[operation.request].required)if(operation.example[key]===undefined&&runtimeExamples[key]!==undefined)operation.example[key]=runtimeExamples[key];
    if(operation.name==='streamProgramTest')operation.example.input={data:{value:1}};
    if(operation.name==='sourceRuntimeRead')operation.example={id:'123456789',configRevision:1,range:{id:'temperature',table:'holding-register',address:0,quantity:2},timeoutMs:5000};
    if(operation.name==='sourceResourceCatalog')operation.example={id:'123456789'};
    if(operation.name==='sourceResourceRead')operation.example={id:'123456789',configRevision:1,resourceRef:'vendor:plex-mes:jobs',params:{limit:5},timeoutMs:5000};
    if(operation.name==='sourceResourceScan')operation.example={id:'123456789',configRevision:1,resourceRef:'vendor:sap-odata:entity:Materials',limit:2,params:{maxItems:5}};
    if (operation.name === 'appSessionInspect') operation.example = {appId: 'example-app', token: 'PLATFORM_SESSION_TOKEN'};
    if (operation.name.endsWith('Update') && schemas[operation.request].properties.name && Object.keys(operation.example).length === 2) operation.example.name = 'Updated name';
    if (operation.name === 'topicCreate' || operation.name === 'topicViewUpdate') operation.example.collectionId = 'example-collection';
    if (['sourceBindingsCreate', 'sourceBindingsUpdate'].includes(operation.name)) schemas[operation.request].properties.binding = {allOf: [ref('Binding'), {properties: {enabled: {enum: [false]}}}]};
    if (operation.name === 'sourceCreate') schemas[operation.request].oneOf = [
      {properties: {adapter: {enum: ['http-api', 'oauth2']}, connection: {required: ['http']}}},
      {properties: {adapter: {enum: ['modbus-tcp']}, connection: {required: ['modbus']}}},
      {properties: {adapter: {enum: ['mqtt', 'opcua', 'usb-hid']}}},
    ];
  }
  for (const operation of operations) {
    if (operation.runtime || !(operation.mutation.startsWith('meta.') || operation.mutation==='stream.publish')) continue;
    const shape=schemas[operation.request], item=structuredClone(shape);
    for(const name of ['dryRun','idempotencyKey'])delete item.properties[name];
    item.required=item.required.filter(name=>!['dryRun','idempotencyKey'].includes(name));
    schemas[operation.request]=object({...write,...partial,mode:{type:'string',enum:['auto','sync','async']},items:{type:'array',minItems:1,maxItems:10000,items:item}},['dryRun','items']);
    schemas[operation.response].properties.data=ref('Outcome');
    const {dryRun,idempotencyKey,...example}=operation.example;
    operation.example={dryRun,items:[example]};
  }
  return operations;
}
