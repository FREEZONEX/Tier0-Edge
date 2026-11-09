const object = (properties, required) => ({type:'object', additionalProperties:false, properties, required});
const ref = name => ({$ref:`#/components/schemas/${name}`});

// Cloud's public shape; native installation identity and policy persistence
// remain in the Enterprise domain. Both authenticated entrances share it.
export function appStreamPolicyContract(schemas) {
  const appId = {type:'string', format:'uuid', minLength:36, maxLength:36};
  const status = {type:'string', enum:['active','disabled']};
  const permissions = {type:'array', maxItems:5, uniqueItems:true, items:{type:'string', enum:[
    'stream.requests.read','stream.requests.invoke','stream.requests.write','stream.messages.publish','stream.messages.subscribe',
  ]}};
  schemas.AppStreamMessageTopicPermission = object({
    topic:{type:'string', minLength:1, maxLength:128, pattern:'^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,127}$'},
    publish:{type:'boolean'}, subscribe:{type:'boolean'},
  }, ['topic','publish','subscribe']);
  const topics = {type:'array', maxItems:32, items:ref('AppStreamMessageTopicPermission')};
  schemas.UnsV2AppStreamPolicyGetReq = object({appId}, ['appId']);
  schemas.UnsV2AppStreamPolicyUpdateReq = object({appId,
    expectedRevision:{type:'integer', minimum:0, description:'Shared App metadata revision; zero only when no metadata exists. Read the current revision before updating.'},
    status, platformPermissions:permissions, messageTopics:topics,
    idempotencyKey:{type:'string', minLength:1, maxLength:128}, dryRun:{type:'boolean'},
  }, ['appId','expectedRevision','status','platformPermissions','idempotencyKey','dryRun']);
  schemas.AppStreamPolicyView = object({appId, revision:{type:'integer', minimum:0},
    version:{type:'string', enum:['cloud-hosted-app-stream/v1']}, status,
    platformPermissions:permissions, messageTopics:topics,
    authorityStatus:{type:'string', enum:['available','notReady','notFound','unavailable']}, appName:{type:'string'},
  }, ['appId','revision','version','status','platformPermissions','authorityStatus','appName']);
  const envelope = data => object({code:{type:'integer'}, msg:{type:'string'}, requestId:{type:'string'}, data}, ['code','msg','requestId','data']);
  schemas.UnsV2AppStreamPolicyGetResponse = envelope(ref('AppStreamPolicyView'));
  schemas.UnsV2AppStreamPolicyUpdateResponse = envelope(ref('Outcome'));
  return ['Get','Update'].map(action => ({
    name:`appStreamPolicy${action}`, handler:`UnsV2AppStreamPolicy${action}`,
    suffix:`/uns/app-stream-policy/${action.toLowerCase()}`, request:`UnsV2AppStreamPolicy${action}Req`, response:`UnsV2AppStreamPolicy${action}Response`,
    resource:'app', resourceId:3383, permission:'stream.requests.manage', mcp:false, appAllowed:false,
    mutation:action === 'Update' ? 'meta.resourcePatch' : '',
    summary:`${action} native App Stream policy; current manager session or full_access personal/Agent key requires uns.write and stream.requests.manage; preserves data grants and does not upgrade credentials or enable message transport`,
  }));
}
