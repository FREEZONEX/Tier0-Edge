const object=(properties,required=[])=>({type:'object',additionalProperties:false,properties,required});
const ref=name=>({$ref:'#/components/schemas/'+name});
export function applyRecordReads(source){
 const {schemas}=source;
 const queryID={type:'string',minLength:1,maxLength:64,pattern:'^[A-Za-z0-9_.-]{1,64}$'};
 const models={find:'UnsV2RecordFindReq',get:'UnsV2RecordGetReq',count:'UnsV2RecordCountReq',distinct:'UnsV2RecordDistinctReq',aggregate:'UnsV2RecordAggregatePipelineReq'};
 const variants=Object.entries(models).map(([op,name])=>{
  const model=structuredClone(schemas[name]);
  model.properties={...model.properties,id:queryID,op:{type:'string',enum:[op]}};
  model.required=[...new Set([...(model.required||[]),'id','op'])];
  return model;
 });
 schemas.UnsV2RecordQueryBatchReq=object({queries:{type:'array',minItems:1,maxItems:20,items:{oneOf:variants}}},['queries']);
 schemas.UnsV2RecordQueryBatchResponse=object({code:{type:'integer'},msg:{type:'string'},requestId:{type:'string'},data:object({results:{type:'object',additionalProperties:{oneOf:[object({ok:{type:'boolean',enum:[true]},data:{type:'object',additionalProperties:true}},['ok','data']),object({ok:{type:'boolean',enum:[false]},error:ref('Fault')},['ok','error'])]}}},['results'])},['code','msg','requestId','data']);
 source.operations.push({name:'recordQueryBatch',handler:'UnsV2RecordQueryBatch',request:'UnsV2RecordQueryBatchReq',response:'UnsV2RecordQueryBatchResponse',suffix:'/uns/records:queryBatch',resource:'record',resourceId:212,permission:'uns.read',appAllowed:true,summary:'批量查询业务记录',description:'最多20项find/count/distinct/aggregate/get共用本地只读快照；每项错误独立返回，整批最多2000行、8MiB，远端center集合不支持跨节点快照。',example:{queries:[{id:'machines',op:'find',collectionId:'machines',limit:20}]}});
 source.cloudContractRevision+='+record-query-batch/1';
}
