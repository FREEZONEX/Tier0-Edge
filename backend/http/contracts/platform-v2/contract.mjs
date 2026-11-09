import {readFileSync} from 'node:fs';
import {discoveryOpenAPI30Schema} from './discovery-openapi.mjs';
// Platform OpenAPI v2 source of truth. Generated HTTP/Swagger files must not be edited.
const str = description => ({type:'string',...(description?{description}:{})});
const integer = (description, minimum, maximum) => ({type:'integer',format:'int64',...(description?{description}:{}),...(minimum!==undefined?{minimum}:{}),...(maximum!==undefined?{maximum}:{})});
const ref = name => ({$ref:`#/components/schemas/PlatformV2${name}`});
const array = items => ({type:'array',items});
const object = (properties,required=Object.keys(properties)) => ({type:'object',additionalProperties:false,properties,required});
const page = {page:{...integer('页码；省略或 0 使用第 1 页。',0,1000000),default:1},size:{...integer('每页条数；最多 100，省略或 0 使用 20。',0,100),default:20}};
const filePath = str('上传接口返回的受管理文件标识；不是本机文件路径或任意对象存储 key。权限按创建身份和业务绑定检查。');
const uploadId = str('分片初始化返回的会话标识；与 filePath 一起原样使用。');
const upload = {fileName:{...str('非空原始文件名；受危险扩展名限制，例如 html/php 不允许。'),minLength:1},contentType:str('可选文件 MIME 类型，例如 application/pdf。'),sizeBytes:integer('声明的文件大小，单位字节；直传与分片均最多 1 GiB。',1,1073741824),visibility:{type:'string',enum:['private','public'],default:'private',description:'private 仅授权主体可获取签名链接；public 允许公开访问，请按业务需要显式选择。'},sessionId:{...str('可选业务会话标识；去除首尾空白后最多 64 UTF-8 字节。'),maxLength:64}};
export const platformTags = [
  {name:'实例身份与用户',description:'平台与业务 App 按权限共用；App 需要对应 platformPermissions。identity/get 确认当前 Key 的可信身份；info/get 查询平台信息。users/list 和 projects/members/list 受平台权限约束；App Key 不能以枚举用户代替 App 数据授权。'},
  {name:'通用通知',description:'平台与业务 App 按权限共用；App 需要对应 platformPermissions。send 向用户发送站内通知，使用稳定幂等键；status/get 读取同一调用身份发起的消息状态。创建成功不表示用户已读，通知结果不属于 UNS 批量事务。'},
  {name:'通用文件与附件',description:'平台与业务 App 按权限共用；App 需要对应 platformPermissions。所有接口为 JSON 控制面。单文件：uploads/create 获取表单签名 → 直接向 upload.url 上传二进制，并附带 upload.fields。大文件：multipart/create → parts 获取分片 PUT URL → 上传分片 → complete 提交 ETag；失败可 abort。downloads/create 返回短期下载 URL，不返回文件二进制。文件权限依调用身份检查，不能由客户端伪造 App 归属。'},
];
export const platformSchemas = {
  PlatformV2Envelope:object({code:integer(),msg:str(),requestId:str(),data:{}}),
  PlatformV2CompletedPart:object({partNumber:integer('从 1 开始的分片序号。',1,10000),etag:{...str('对象存储上传该分片返回的非空 ETag，原样提交。'),minLength:1}}),
  PlatformV2ErrorResponse:object({code:integer('真实 HTTP 状态码。'),msg:str(),requestId:str(),data:{nullable:true},error:object({code:str('可编程判断的错误码。'),retryable:{type:'boolean',description:'当前业务错误仅 429 标为可重试。500/504 结果可能不明，应先按 recommendedAction 核查。'},details:{type:'array',items:{type:'object',additionalProperties:true},description:'结构化错误明细，当前平台业务错误为空数组。'},path:str('有定位信息时返回的参数路径。'),recommendedAction:str('500/504 时说明先检查操作状态；通知保留原幂等键。'),retryAdvice:{type:'object',additionalProperties:true,description:'认证中间件可附带的结构化恢复建议。'}},['code','retryable','details'])}),
};
export const platformOperations = [];
function add(name,path,group,summary,description,properties,required,response,example){
  const request=`PlatformV2${name}Req`, responseName=`PlatformV2${name}Response`;
  platformSchemas[request]=object(properties,required);
  platformSchemas[responseName]=object({code:integer(),msg:str(),requestId:str(),data:response});
  platformOperations.push({name:name[0].toLowerCase()+name.slice(1),handler:`PlatformV2${name}`,suffix:path,request,response:responseName,tags:[platformTags[group].name],summary,description:description+(name==='CapabilitiesDiscover'?'':' 请求必须为 Content-Type: application/json 的单个 JSON 对象，最大 1 MiB；不接受未知字段。'),example});
}
const loose = {type:'object',additionalProperties:true};
const role = object({roleId:str(),roleKey:str(),roleName:str(),description:str()},['roleId','roleKey']);
const paged = row => object({list:array(row),total:integer(),page:integer(),size:integer()});
const user = object({userId:str(),userName:str(),nickName:str(),email:str(),status:str(),roles:array(role),updatedAt:str()},['userId','status','roles','updatedAt']);
const responseData = {
  identityGet:object({userId:str(),userName:str(),authType:str(),apiKeyId:str(),appId:str('普通 Key 为空字符串；App Key 返回绑定的 App ID。'),capabilities:array(str())}),
  infoGet:object({name:str(),version:str(),apiVersion:{type:'string',enum:['v2']},capabilities:array(str())}),
  usersList:paged(user),
  projectMembersList:paged(object({memberId:str(),projectId:str(),userId:str(),userName:str(),email:str(),status:str(),roles:array(object({roleId:str(),roleKey:str(),roleName:str(),description:str()}))})),
  fileUploadCreate:object({fileId:str(),filePath,upload:object({method:{type:'string',enum:['POST']},url:str(),fields:{type:'object',additionalProperties:str()},fileField:{type:'string',enum:['file']}}),expiresAt:integer('有效期截止时间，Unix 毫秒。'),sizeBytes:integer(),visibility:{type:'string',enum:['private','public']}}),
  fileDownloadCreate:object({filePath,url:str('下载 URL；私有文件使用短期签名 URL，公开文件可为永久 URL。'),expiresAt:integer('有效期截止时间，Unix 毫秒；公开永久链接为 0。')}),
  fileDelete:object({filePath,deleted:{type:'boolean'}}),
  multipartCreate:object({filePath,uploadId,partSizeBytes:integer(),partCount:integer(),expiresAt:integer()}),
  multipartParts:object({list:array(object({partNumber:integer(),url:str('使用 HTTP PUT 上传此分片。'),expiresAt:integer()}))}),
  multipartComplete:object({fileId:str(),filePath,url:str(),sizeBytes:integer(),expiresAt:integer(),status:{type:'string',enum:['completed']}}),
  multipartAbort:object({filePath,aborted:{type:'boolean'}}),
  notificationSend:object({messageId:str(),type:str(),status:str(),createdAt:{type:'string',format:'date-time'},completedAt:{type:'string',format:'date-time'},errorCode:str(),message:str()},['messageId','type','status','createdAt']),
  notificationStatusGet:object({messageId:str(),type:str(),status:str(),createdAt:{type:'string',format:'date-time'},completedAt:{type:'string',format:'date-time'},errorCode:str(),message:str()},['messageId','type','status','createdAt']),
};
add('IdentityGet','/platform/identity/get',0,'读取当前调用身份','返回 API Key 关联的可信主体和授权信息；不回显密钥。',{},[],loose,{});
add('InfoGet','/platform/info/get',0,'读取平台信息','读取当前实例信息，不修改平台配置。',{},[],loose,{});
add('UsersList','/platform/users/list',0,'分页查找平台用户','按搜索文本查找用户；只能使用当前 Key 和所属用户共同拥有的权限。',{search:str('用户名、昵称等支持字段的搜索文本。'),...page},[],loose,{search:'生产',page:1,size:20});
add('ProjectMembersList','/platform/projects/members/list',0,'查询项目成员','projectId 为正整数形式的项目 ID 字符串；API Key 所属用户必须能访问此项目，App Key 还必须获 platform.projects.members.read 能力。此接口不添加成员或授予权限。',{projectId:{type:'string',pattern:'^[1-9][0-9]*$',description:'目标项目 ID，正整数字符串。'},...page},['projectId'],loose,{projectId:'123',page:1,size:20});
add('NotificationSend','/notifications/send',1,'发送站内通知','sender 由服务端可信身份确定，禁止自报。重试使用相同 idempotencyKey 和请求；返回发送状态不代表接收者已读。',{recipientUserId:{type:'string',pattern:'^[1-9][0-9]*$',description:'接收者正整数用户 ID，使用字符串避免大整数精度丢失。'},type:{type:'string',enum:['inbox'],default:'inbox'},title:{type:'string',minLength:1,maxLength:50},content:{type:'string',maxLength:800},idempotencyKey:{type:'string',minLength:1,maxLength:128},mode:{type:'string',enum:['live','test'],default:'live'},link:{type:'string',maxLength:500,description:'http://、https:// 或 / 开头的业务链接；禁止凭据、协议相对地址和控制字符。'},channels:{type:'array',items:{type:'string',enum:['web','mobile']},description:'省略或空数组表示静默站内信。'}},['recipientUserId','title','content','idempotencyKey'],object({messageId:str(),status:str(),createdAt:str()}),{recipientUserId:'123',title:'班次报告待确认',content:'请核对本班次报告。',idempotencyKey:'replace-with-message-intent',channels:[]});
add('NotificationStatusGet','/notifications/status/get',1,'读取通知发送状态','仅查询当前调用身份发起的 messageId；无权访问的消息不暴露存在性。',{messageId:str('send 返回的消息 ID。')},['messageId'],object({messageId:str(),type:str(),status:str(),createdAt:str(),completedAt:str(),errorCode:str(),message:str()},['messageId','type','status','createdAt']),{messageId:'example-message'});
add('FileUploadCreate','/files/uploads/create',2,'申请文件上传凭据','只创建上传凭据；调用方还须按返回 upload.fields 向 upload.url 提交文件，文件字段名取 upload.fileField，不能将本响应视为文件已上传。',upload,['fileName','sizeBytes'],loose,{fileName:'shift-report.pdf',contentType:'application/pdf',sizeBytes:1024,visibility:'private'});
add('FileDownloadCreate','/files/downloads/create',2,'申请文件下载链接','按文件访问权限创建有时限的下载 URL；URL 是临时访问凭据，不公开记录到日志。',{filePath,expiresInSeconds:integer('期望链接有效秒数；省略或 0 使用服务端默认，最大 7 天。',0,604800)},['filePath'],loose,{filePath:'example-managed-file',expiresInSeconds:300});
add('FileDelete','/files/delete',2,'删除文件','删除当前调用身份有权管理的文件；使用 uploads/create 或 multipart/complete 返回的 filePath。',{filePath},['filePath'],object({deleted:{type:'boolean'}}),{filePath:'example-managed-file'});
add('MultipartCreate','/files/uploads/multipart/create',2,'初始化分片上传','申请分片会话；使用返回的 partSizeBytes/partCount 分片，尚未上传文件。',{...upload,partSizeBytes:integer('期望分片大小（字节）；省略、0 或小于 5 MiB 时使用 10 MiB，实际值以响应为准。',0,1073741824)},['fileName','sizeBytes'],loose,{fileName:'telemetry.csv',contentType:'text/csv',sizeBytes:20000000,visibility:'private'});
add('MultipartParts','/files/uploads/multipart/parts',2,'申请分片上传链接','每个 partNumber 获取独立签名 URL；上传后保存每片 ETag，供 complete 使用。',{filePath,uploadId,partNumbers:{type:'array',items:integer('',1,10000),minItems:1,maxItems:1000,uniqueItems:true}},['filePath','uploadId','partNumbers'],loose,{filePath:'example-managed-file',uploadId:'example-upload',partNumbers:[1,2]});
add('MultipartComplete','/files/uploads/multipart/complete',2,'完成分片上传','仅在所有分片上传完成后提交分片号和 ETag；成功后再将 filePath 关联业务记录。',{filePath,uploadId,parts:{type:'array',items:ref('CompletedPart'),minItems:1,maxItems:10000}},['filePath','uploadId','parts'],loose,{filePath:'example-managed-file',uploadId:'example-upload',parts:[{partNumber:1,etag:'example-storage-etag'}]});
add('MultipartAbort','/files/uploads/multipart/abort',2,'取消分片上传','取消未完成的分片会话；不用于删除已完成文件，后者使用 files/delete。',{filePath,uploadId},['filePath','uploadId'],object({aborted:{type:'boolean'}}),{filePath:'example-managed-file',uploadId:'example-upload'});
// Discovery is a raw business response, independent of platform/info/get.
add('CapabilitiesDiscover','/capabilities/discover',0,'发现当前身份可用能力','只读评估已注册接口和平台运行前提，不替代目标资源检查。仅接受空 JSON 对象（最多 4 KiB），不接受查询参数。',{},[],loose,{});
const sharedDiscovery = JSON.parse(readFileSync(new URL('../../../../packages/app-data-client/contracts/capability-discovery/response.schema.json', import.meta.url),'utf8'));
for (const [name,schema] of Object.entries(sharedDiscovery.$defs)) platformSchemas['PlatformV2Discovery_'+name]=discoveryOpenAPI30Schema(schema);
platformSchemas.PlatformV2CapabilitiesDiscoverResponse={
  ...discoveryOpenAPI30Schema(sharedDiscovery),
  description:'OpenAPI 3.0 wire schema. Array contains/cardinality annotations and cross-field proof ordering require the shared JSON Schema plus SDK semantic checks; this projection alone is not full contract acceptance.',
  'x-tier0-json-schema':sharedDiscovery,
};
platformSchemas.PlatformV2DiscoveryError=object({errorCode:str(),message:str()});
const permissions={capabilitiesDiscover:[500,'openapi.base'],identityGet:[500,'openapi.base'],infoGet:[500,'openapi.base'],usersList:[3340,'platform.users.read'],projectMembersList:[3341,'platform.projects.members.read'],notificationSend:[3230,'notifications.send'],notificationStatusGet:[3231,'notifications.read'],fileUploadCreate:[700,'asset.file.upload'],fileDownloadCreate:[730,'asset.file.download'],fileDelete:[720,'asset.file.delete'],multipartCreate:[700,'asset.file.upload'],multipartParts:[700,'asset.file.upload'],multipartComplete:[700,'asset.file.upload'],multipartAbort:[700,'asset.file.upload']};
for(const op of platformOperations){
  [op.resourceId,op.permission]=permissions[op.name];
  if(responseData[op.name])platformSchemas[op.response].properties.data=responseData[op.name];
}
Object.assign(platformSchemas.PlatformV2UsersListReq.properties,{
  roleKeys:{type:'array',items:str('平台角色 key，不是角色 ID。'),description:'匹配任一指定平台角色。'},
  statuses:{type:'array',items:{type:'string',enum:['active','disabled']}},
  updatedAtStart:{type:'string',format:'date-time',description:'最后更新时间起点，RFC3339。'},
  updatedAtEnd:{type:'string',format:'date-time',description:'最后更新时间终点，RFC3339。'},
});
export function platformOpenAPI(){
  return {tags:platformTags,paths:Object.fromEntries(platformOperations.map(op=>[op.suffix,{post:{operationId:op.handler,tags:op.tags,summary:op.summary,description:op.description,requestBody:{required:true,content:{'application/json':{schema:{$ref:`#/components/schemas/${op.request}`},example:op.example}}},responses:{200:{description:'操作结果。文件上传凭据和通知受理不等于文件已上传或用户已读。',content:{'application/json':{schema:{$ref:`#/components/schemas/${op.response}`}}}},...Object.fromEntries([400,401,403,404,409,413,415,422,429,500,503,504].map(status=>[status,{description:'请求失败；根据 code/msg 处理，权限失败不得绕过。',content:{'application/json':{schema:{$ref:op.name==='capabilitiesDiscover'?'#/components/schemas/PlatformV2DiscoveryError':'#/components/schemas/PlatformV2ErrorResponse'}}}}]))}}}])),schemas:platformSchemas};
}
