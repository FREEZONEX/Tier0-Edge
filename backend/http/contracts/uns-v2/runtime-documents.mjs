// Native Enterprise adapters for cloud Source/Stream document entrypoints.
// Upstream: Tier0-Backend 51a7dc77; keep Enterprise protocol/SDK definitions.
const text = {type:'string'};
const id = {type:'string',pattern:'^[1-9][0-9]*$',maxLength:19};
const revision = {type:'integer',minimum:1};
const ref = name => ({$ref:`#/components/schemas/${name}`});
const object = (properties,required=[]) => ({type:'object',additionalProperties:false,properties,required});
export function runtimeDocumentContract(schemas) {
  // Client credentials have no authorization URL and scopes are optional.
  // Authorization-code URL requirements remain enforced by OAuthConfig.validate.
  schemas.OAuthConfig.required=(schemas.OAuthConfig.required||[]).filter(name=>!['authorizationUrl','scopes'].includes(name));
  const ops=[];
  const page={page:{type:'integer',minimum:1},size:{type:'integer',minimum:1,maximum:200},search:text};
  const guard={id,expectedRevision:revision};
  const intent={dryRun:{type:'boolean',default:false},idempotencyKey:{type:'string',minLength:1,maxLength:128}};
  const view={type:'string',enum:['summary','full'],default:'full'};
  const source=structuredClone(schemas.SourceDefinition);
  source.properties.protocol={type:'string',enum:['http-api','oauth2','mqtt','opcua','modbus-tcp','usb-hid','file-read','edge-sql','open-protocol']};
  source.required=[...new Set([...(source.required||[]),'protocol'])];
  source.allOf=[
    {oneOf:[{properties:{protocol:{enum:['file-read']}},required:['file']},{properties:{protocol:{not:{enum:['file-read']}}},not:{required:['file']}}]},
    {oneOf:[{properties:{protocol:{enum:['edge-sql']}},required:['sql']},{properties:{protocol:{not:{enum:['edge-sql']}}},not:{required:['sql']}}]},
    {oneOf:[{properties:{protocol:{enum:['open-protocol']}},required:['openProtocol']},{properties:{protocol:{not:{enum:['open-protocol']}}},not:{required:['openProtocol']}}]},
  ];
  schemas.SQLConnection.properties.query={type:'string',enum:['sql:tool-inventory','sql:tool-loans','sql:tool-sharpenings','sql:tool-suppliers','sql:quality-inspections','sql:wms-schema','sql:wms-inventory','sql:wms-movements'],description:'Select an Edge-owned read-only query with bound parameters. WMS business queries require a declared table and column mapping.'};
  Object.assign(schemas.FileSourceOptions.properties.resources,{maxItems:128});
  Object.assign(schemas.FileSourceOptions.properties.directories,{maxItems:128});
  schemas.FileSourceOptions.anyOf=[
    {required:['resources'],properties:{resources:{minItems:1}}},
    {required:['directories'],properties:{directories:{minItems:1}}},
  ];
  schemas.FileSelection.properties.tables={type:'array',items:{type:'string',enum:['T_JYD','T_JYD_MX','T_YHB']},uniqueItems:true,description:'Omit for a byte resource, or select QC2000 tables from a SQLite snapshot.'};
  schemas.FileDirectory.properties.format={type:'string',enum:['bytes','edifact']};
  Object.assign(schemas.FileDirectory.properties.extensions,{minItems:1,maxItems:16,uniqueItems:true});
  source.allOf.push(
    {if:{properties:{sourceType:{const:'teamcenter'}},required:['sourceType']},then:{properties:{http:{properties:{parameters:{properties:{fileSelections:{type:'array',minItems:1,maxItems:32,items:ref('TeamcenterFileSelection')}}}}}}}},
    {if:{properties:{sourceType:{const:'salesforce'}},required:['sourceType']},then:{properties:{http:{properties:{parameters:{properties:{salesforceQueries:{type:'array',minItems:1,maxItems:32,items:ref('SalesforceQueryDeclaration')}}}}}}}},
  );
  schemas.NativeSourceDocument=source;
  const stream=ref('RuntimeStreamDefinition');
  function add(name,path,kind,mutating,fields,required,description,extra={}) {
    const handler=`UnsV2${name[0].toUpperCase()}${name.slice(1)}`,request=handler+'Req',response=handler+'Response';
    schemas[request]=object({...mutating?intent:{},...fields},required);
    schemas[response]=object({code:{type:'integer'},msg:text,requestId:text,data:{type:'object',additionalProperties:true}},['code','msg','requestId','data']);
    ops.push({name,handler,suffix:'/uns/'+path,request,response,resource:kind,resourceId:kind==='source'?(mutating?3301:3300):(mutating?3311:3310),permission:`uns.${kind}${mutating?'.manage':''}`,mutation:mutating?'runtime':'',runtime:true,summary:description,...extra,example:mutating?{dryRun:fields.dryRun?.enum?.[0] ?? true}: {}});
  }
  add('sourceDocumentGet','sources/documents/get','source',false,{id,view},['id'],'Read native Source; full view requires Source manage permission and full-access API key. Summary omits connection and credential references.');
  add('sourceDocumentList','sources/documents/list','source',false,{...page,protocol:source.properties.protocol,health:text,includeDocument:{type:'boolean',default:false}},[],'List native Source summaries; includeDocument additionally requires Source manage permission.');
  const sourceFields={document:ref('NativeSourceDocument')};
  const patch=object({name:text,description:text,endpoint:text,http:ref('HTTPConnection'),mqtt:ref('MQTTOptions'),opcua:ref('OPCUAOptions'),modbus:ref('ModbusOptions'),file:ref('FileSourceOptions'),sql:ref('SQLConnection'),openProtocol:ref('OpenProtocolOptions')});
  // Patch is shallow at the top level, merging protocol configuration maps.
  const impactRevision={type:'string',minLength:1,description:'当前依赖清单的确认令牌；无草稿、活跃版本或待部署版本依赖时可省略，历史发布不阻挡删除。仅修改名称或描述无需确认。显式提交的令牌始终校验，不接受空值。'};
  const update={...guard,...sourceFields,patch,confirmedImpactRevision:impactRevision};
  const exclusiveDocument=[{required:['document'],not:{required:['patch']}},{required:['patch'],not:{required:['document']}}];
  add('sourceDocumentCreate','sources/documents/create','source',true,{...sourceFields,credentials:{...object({kind:{type:'string',enum:['basic','bearer','custom','http']},username:{type:'string',maxLength:1024},secret:{type:'string',maxLength:65536},values:{type:'object',maxProperties:128,additionalProperties:{type:'string',maxLength:65536}}},['kind']),writeOnly:true},items:{type:'array',minItems:1,maxItems:200,items:object(sourceFields,['document'])}},[],'Create a native Source or atomic batch. Only configuration is saved; collection does not execute.');
  add('sourceDocumentUpdate','sources/documents/update','source',true,{...update,items:{type:'array',minItems:1,maxItems:200,items:{...object(update,['id','expectedRevision']),oneOf:exclusiveDocument}}},[],'Update one Source or an atomic batch with revision guards. Confirm impact only for referenced Sources.');
  schemas.UnsV2SourceDocumentCreateReq.oneOf=[{required:['document'],not:{required:['items']}},{required:['items'],not:{anyOf:[{required:['document']},{required:['credentials']}]}}];
  schemas.UnsV2SourceDocumentCreateReq.properties.credentials.description='Optional for a single http-api Source with an idempotency key. For mqtt, file-read and edge-sql, create the Source first and use sources/documents/credentials/update on the Edge.';
  schemas.UnsV2SourceDocumentCreateReq.allOf=[{oneOf:[{not:{required:['credentials']}},{required:['credentials','idempotencyKey'],properties:{document:{properties:{protocol:{enum:['http-api']},credentialRef:{maxLength:0}}}}}]}];
  schemas.UnsV2SourceDocumentUpdateReq.oneOf=[{required:['id','expectedRevision'],oneOf:exclusiveDocument,not:{required:['items']}},{required:['items'],not:{anyOf:['id','expectedRevision','document','patch','confirmedImpactRevision'].map(key=>({required:[key]}))}}];
  add('sourceDocumentDelete','sources/documents/delete','source',true,{...guard,confirmedImpactRevision:impactRevision},['id','expectedRevision'],'Delete a native Source using its current revision and dependency impact confirmation. Stops future connections and acquisition without a separate disable step; history is retained.');
  add('sourceDocumentStateUpdate','sources/documents/state/update','source',true,{...guard,desiredState:{type:'string',enum:['enabled','disabled']},confirmedImpactRevision:impactRevision},['id','expectedRevision','desiredState'],'Enable or disable Source connections with revision and dependency confirmation; configuration, Stream documents and history are retained.');
  const credentials=object({kind:{type:'string',enum:['basic','bearer','custom','oauth2']},username:text,secret:{...text,writeOnly:true,maxLength:65536},values:{type:'object',maxProperties:128,additionalProperties:{type:'string',maxLength:65536},writeOnly:true}},['kind']);
  credentials.properties.values.description='MQTT mTLS uses kind=custom with clientCertificatePem, clientPrivateKeyPem and caCertificatePem. SFTP private-key authentication uses kind=custom with username and privateKey. SQL uses kind=basic with username and secret.';
  add('sourceNativeCredentialsUpdate','sources/documents/credentials/update','source',true,{...guard,credentials,confirmedImpactRevision:impactRevision},['id','expectedRevision','credentials'],'Set or rotate encrypted credentials on the native Edge, including MQTT mTLS, file access and SQL login.',{permission:'uns.source.credentials',resourceId:3302});
  // OAuth cannot roll back one-time state or token exchange.
  const oauthIntent = {type:'boolean',enum:[false],default:false,description:'OAuth state creation and token exchange require execution. A preview is rejected.'};
  add('sourceNativeOAuthStart','sources/documents/oauth/start','source',true,{id,redirectUri:text,dryRun:oauthIntent},['id','redirectUri'],'Start native OAuth authorization; secrets remain encrypted on the Source installation.',{permission:'uns.source.credentials',resourceId:3302,mcp:false});
  add('sourceNativeOAuthComplete','sources/documents/oauth/complete','source',true,{id,state:text,dryRun:oauthIntent,code:{...text,writeOnly:true}},['id','state','code'],'Complete the native OAuth flow with its stored one-time state.',{permission:'uns.source.credentials',resourceId:3302,mcp:false});
  add('sourceImpact','sources/impact','source',false,guard,['id','expectedRevision'],'Read current Source references and the impact revision for a subsequent guarded mutation.',{permission:'uns.source.manage',resourceId:3301});
  add('sourceObservationsGet','sources/observations/get','source',false,{items:{type:'array',minItems:1,maxItems:100,items:object(guard,['id','expectedRevision'])}},['items'],'Read observations only for the requested Source revision. No connection document or secret is returned.');
  for(const action of ['create','update','validate']) {
    const mutation=action!=='validate';
    add(`streamDocument${action[0].toUpperCase()+action.slice(1)}`,`streams/documents/${action}`,'stream',mutation,{...action==='update'?guard:{},document:stream,program:stream,expectedSourceRevision:revision},action==='update'?['id','expectedRevision']:[],'Use the existing native Stream program registration and validation. Saving a draft never deploys it.',{permission:'uns.stream.manage',resourceId:3311});
    schemas[`UnsV2StreamDocument${action[0].toUpperCase()+action.slice(1)}Req`].oneOf=[{required:['document'],not:{required:['program']}},{required:['program'],not:{required:['document']}}];
  }
  add('streamDocumentGet','streams/documents/get','stream',false,{id,view},['id'],'Read a native Stream; full program requires Stream manage permission.');
  add('streamProgramList','streams/program/list','stream',false,{...page,sourceId:id,model:text,status:text,includeDocument:{type:'boolean',default:false}},[],'List native Stream summaries; includeDocument requires Stream manage permission.');
  add('streamProgramDelete','streams/program/delete','stream',true,guard,['id','expectedRevision'],'Stop and remove a native Stream using the existing lifecycle.');
  add('streamDocumentDisable','streams/documents/disable','stream',true,{...guard,expectedActiveVersion:id},['id','expectedRevision','expectedActiveVersion'],'Stop a Stream with draft and active version guards. Does not require a healthy execution worker.');
  add('streamObservationGet','streams/observation/get','stream',false,{id},['id'],'Read native deployment and execution observations; no program source is returned.');
  add('streamRunsList','streams/runs/list','stream',false,{id,...page,status:text,sourceId:id,topicId:id,model:text},[],'List native Stream run receipts; an accepted output is distinct from center persistence.');
  add('streamRunGet','streams/runs/get','stream',false,{runId:text},['runId'],'Read a redacted native Stream run receipt.');
  add('sourceDevicesList','sources/devices/list','source',false,{kind:{type:'string',enum:['usb-hid','serial']}},[],'List currently attached physical devices on this Edge without reading input. Returns a fresh snapshot with checkedAt, revision, completeness and existing Source bindings. Serial discovery does not imply a supported serial acquisition driver.');
  return ops;
}
