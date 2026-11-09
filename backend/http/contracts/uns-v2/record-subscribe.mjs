const object=(properties,required=[])=>({type:'object',additionalProperties:false,properties,required});
const text={type:'string'};
export function applyRecordSubscribe(effective){
 const schemas=effective.schemas;
 const ids=maxItems=>({type:'array',maxItems,uniqueItems:true,items:{type:'string',minLength:1,maxLength:128}});
 schemas.UnsV2RecordSubscribeReq=object({collectionIds:ids(64),recordIds:ids(100)});
 schemas.RecordChangeEvent=object({eventId:text,collectionId:text,recordId:text,revision:{type:'integer'},change:{type:'string',enum:['record','delete']},occurredAt:{type:'integer'},restricted:{type:'boolean'}},['eventId','collectionId','recordId','revision','change','occurredAt','restricted']);
 schemas.UnsV2RecordSubscribeResponse={type:'string',description:'SSE change frames contain RecordChangeEvent identity and revision only. heartbeat frames carry the safe cursor as id and {at}; reset frames carry {reason,message} without an id. Resume with Last-Event-ID. Re-read Records after change notifications.'};
 effective.operations.push({name:'recordSubscribe',method:'GET',transport:'sse',suffix:'/uns/records:subscribe',handler:'UnsV2RecordSubscribe',request:'UnsV2RecordSubscribeReq',response:'UnsV2RecordSubscribeResponse',resource:'record',resourceId:212,permission:'uns.read',appAllowed:true,mcp:false,summary:'Subscribe to authorized Record changes using SSE. Supply repeated collectionIds/recordIds query parameters, Accept: text/event-stream and optional Last-Event-ID. Notifications contain no business fields. Current key and App permissions are rechecked on every poll.'});
}
