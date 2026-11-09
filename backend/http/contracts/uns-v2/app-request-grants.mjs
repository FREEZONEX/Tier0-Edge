const object=(properties,required=[])=>({type:'object',additionalProperties:false,properties,required});
const text={type:'string'};
const flag={type:'integer',enum:[1,2]};
const actions={type:'array',maxItems:3,uniqueItems:true,items:{type:'string',enum:['stream.requests.read','stream.requests.invoke','stream.requests.write']}};
export function appRequestGrantContract(schemas){
 const principal={code:{type:'string',minLength:1,maxLength:128,pattern:'^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$'},principalKind:{type:'string',enum:['app']},principalID:{type:'string',format:'uuid'}};
 schemas.UnsV2StreamRequestGrantGetReq=object(principal,Object.keys(principal));
 schemas.UnsV2StreamRequestGrantSetReq=object({...principal,active:flag,actions,expectedSnapshotToken:{type:'string',pattern:'^cgs1\\.[a-f0-9]{64}\\.[a-f0-9]{64}$'}},[...Object.keys(principal),'active','expectedSnapshotToken']);
 schemas.AppRequestGrantView=object({...principal,status:text,bindingDigest:text,snapshotToken:text,recipientEligible:flag,recipientActions:actions,activationSupported:flag,registered:flag,revision:{type:'integer',minimum:0},active:flag,actions,targets:{type:'array',items:object({digest:text,action:text,revision:{type:'integer',minimum:0},active:flag,current:flag,kind:text,reference:text},['digest','action','revision','active','current'])},createdPrincipal:text,updatedPrincipal:text,createdTime:{type:'integer'},updatedTime:{type:'integer'}},[...Object.keys(principal),'status','bindingDigest','snapshotToken','recipientEligible','recipientActions','activationSupported','registered','revision','active','actions','targets','createdPrincipal','updatedPrincipal','createdTime','updatedTime']);
 const operations=['Get','Set'].map(action=>{
  const handler=`UnsV2StreamRequestGrant${action}`,response=handler+'Response';
  schemas[response]=object({code:{type:'integer'},msg:text,requestId:text,data:{$ref:'#/components/schemas/AppRequestGrantView'}},['code','msg','requestId','data']);
  return {name:`streamRequestGrant${action}`,handler,suffix:`/uns/stream-requests/grants/${action.toLowerCase()}`,request:handler+'Req',response,resource:'app',resourceId:3383,permission:'stream.requests.manage',mcp:false,appAllowed:false,summary:'Manage an App grant for one frozen Request release and its exact targets. Local App identity and current Collection grants remain independent. Snapshot tokens are concurrency versions, never authority.'};
 });
 schemas.UnsV2AppStreamAccessListReq=object({appId:principal.principalID,streamId:{type:'string',pattern:'^[1-9][0-9]*$',maxLength:19},page:{type:'integer',minimum:1,maximum:100000},size:{type:'integer',minimum:1,maximum:100},includeInactive:flag},['appId']);
 const item=object({code:text,streamId:text,streamName:text,entryId:text,entryName:text,actions,configuredActive:flag,revision:{type:'integer',minimum:1},state:{type:'string',enum:['available','blocked']},blockedReasons:{type:'array',items:text}},['code','streamId','streamName','entryId','entryName','actions','configuredActive','revision','state','blockedReasons']);
 schemas.UnsV2AppStreamAccessListResponse=object({code:{type:'integer'},msg:text,requestId:text,data:object({items:{type:'array',items:item},page:{type:'integer'},size:{type:'integer'},total:{type:'integer'}},['items','page','size','total'])},['code','msg','requestId','data']);
 operations.push({name:'appStreamAccessList',handler:'UnsV2AppStreamAccessList',suffix:'/uns/app-stream-access/list',request:'UnsV2AppStreamAccessListReq',response:'UnsV2AppStreamAccessListResponse',resource:'app',resourceId:3383,permission:'stream.requests.manage',mcp:false,appAllowed:false,summary:'List configured App Request grants with current blockers. includeInactive=1 retains revoked or missing-publication grants for review and revocation.'});
 return operations;
}
