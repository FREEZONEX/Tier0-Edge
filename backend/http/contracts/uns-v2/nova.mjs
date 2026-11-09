// Nova Collection-scoped resource paths; authentication prefixes stay platform-specific.
const ref=n=>({$ref:`#/components/schemas/${n}`});
const object=(properties,required=[])=>({type:'object',additionalProperties:false,properties,required});
const text={type:'string'};
const id={type:'string',minLength:1,maxLength:128};
const array=(items,min=0,max=10000)=>({type:'array',items,minItems:min,maxItems:max});
export function novaContract(schemas){
 const operations=[];
 const filter=ref('UnsV2DataFilter'),sort=ref('UnsV2PipelineSort');
 const guard={expectedRevision:{type:'integer',minimum:1},dryRun:{type:'boolean',default:false},idempotencyKey:{type:'string',minLength:1,maxLength:96}};
 const update={update:{type:'object',minProperties:1,description:'Nova update operators: $set/$unset/$inc/$mul/$min/$max/$rename/$currentDate/$setOnInsert/$push/$addToSet/$pop/$pull/$pullAll. Business paths omit data.; _id and _meta are read-only.'},arrayFilters:array({type:'object'},0,8)};
 const insert=object({id,data:{type:'object',additionalProperties:true}},['data']);
 const counts=object({matchedCount:{type:'integer'},modifiedCount:{type:'integer'},deletedCount:{type:'integer'},insertedCount:{type:'integer'},upsertedCount:{type:'integer'},upsertedId:id,upsertedIds:array(id),insertedIds:array(id),document:{type:'object',nullable:true},writeErrors:array({type:'object'}),hasMore:{type:'boolean'},resumeAfter:id,status:text,replayed:{type:'boolean'}},['matchedCount','modifiedCount','deletedCount','insertedCount','upsertedCount']);
 const add=(action,fields,required,write=false,summary='')=>{
  const name='record'+action[0].toUpperCase()+action.slice(1),handler='UnsV2'+name[0].toUpperCase()+name.slice(1),request=handler+'Req',response=handler+'Response';
  schemas[request]=object({collectionId:id,...(write?guard:{}),...fields},['collectionId',...required]);
  schemas[response]=object({code:{type:'integer'},msg:text,requestId:text,data:write?counts:{type:'object',additionalProperties:true}},['code','msg','requestId','data']);
  const deletion=action.toLowerCase().includes('delete');
  operations.push({name,handler,request,response,suffix:action==='insertOne'?'/uns/collections/:collectionId/records':'/uns/collections/:collectionId/records:'+(action==='aggregatePipeline'?'aggregate':action),pathParams:['collectionId'],resource:'record',novaAction:action==='aggregatePipeline'?'aggregate':action,resourceId:write?(deletion?215:214):212,permission:write?(deletion?'uns.delete':'uns.write'):'uns.read',mutation:write?(deletion?'record.delete':action.startsWith('insert')?'record.create':'record.update'):'',destructive:deletion,summary:summary||`Nova ${action}: bare business fields, _id and _meta.* system fields`,example:{collectionId:'example-collection',...(write?{dryRun:true}:{}),...Object.fromEntries(required.map(k=>[k,k==='filter'?{_id:'example-record'}:k==='update'?{$inc:{quantity:1}}:k==='data'||k==='replacement'?{name:'Example'}:k==='field'?'name':k==='pipeline'?[]:k==='documents'?[{id:'example-record',data:{name:'Example'}}]:[]]))}});
 };
 add('find',{filter,sort,projection:{type:'object',additionalProperties:{oneOf:[{type:'integer',enum:[0,1]},{type:'boolean'}]},description:'Include or exclude declared fields; computed expressions use aggregate-pipeline. System identity is retained.'},limit:{type:'integer',minimum:1,maximum:200},cursor:text},[]);
 add('count',{filter},[]);
 add('distinct',{filter,field:text,limit:{type:'integer',minimum:1,maximum:200}},['field']);
 add('aggregatePipeline',{pipeline:array(ref('UnsV2PipelineStage'),0,32),limit:{type:'integer',minimum:1,maximum:200}},['pipeline']);
 add('explain',{filter,sort},[],false,'Validate the document query and authorization without executing it');
 add('insertOne',{id,data:insert.properties.data},['data'],true);
 add('insertMany',{documents:array(insert,1),ordered:{type:'boolean',default:true},atomic:{type:'boolean',default:false}},['documents'],true);
 for(const action of ['updateOne','findOneAndUpdate'])add(action,{filter,sort,...update,upsert:{type:'boolean',default:false},...(action.startsWith('find')?{returnDocument:{type:'string',enum:['before','after'],default:'before'}}:{})},['filter','update'],true);
 for(const action of ['replaceOne','findOneAndReplace'])add(action,{filter,sort,replacement:{type:'object',additionalProperties:true},upsert:{type:'boolean',default:false},...(action.startsWith('find')?{returnDocument:{type:'string',enum:['before','after'],default:'before'}}:{})},['filter','replacement'],true);
 for(const action of ['deleteOne','findOneAndDelete'])add(action,{filter,sort},['filter'],true);
 for(const action of ['updateMany','deleteMany'])add(action,{filter,...(action==='updateMany'?update:{}),maxDocs:{type:'integer',minimum:1,maximum:10000,description:'Default 1000; atomic requests default to 200.'},resumeAfter:text,atomic:{type:'boolean',default:false,description:'All selected writes commit together; maxDocs must be <= 200 and the complete match set must fit.'},ordered:{type:'boolean',default:true}},['filter',...(action==='updateMany'?['update']:[])],true);
 const bulkEntry={oneOf:[object({insertOne:insert},['insertOne']),...['updateOne','replaceOne','deleteOne'].map(action=>object({[action]:object({filter,sort,...(action==='updateOne'?update:action==='replaceOne'?{replacement:{type:'object',additionalProperties:true}}:{}),...(action!=='deleteOne'?{upsert:{type:'boolean'}}:{})},['filter',...(action==='updateOne'?['update']:action==='replaceOne'?['replacement']:[])])},[action]))]};
 add('bulkWrite',{operations:array(bulkEntry,1),ordered:{type:'boolean',default:true},atomic:{type:'boolean',default:false}},['operations'],true);
 const sampleBase={collectionId:id,recordIds:array(id,1,20)};
 for(const [action,fields,required,write] of [
  ['querySeries',{...sampleBase,from:{type:'integer',minimum:0},to:{type:'integer',minimum:1},bucket:{type:'string',description:'Fixed UTC duration, e.g. 5m/1h/1d; 1ms to 365d.'},fields:array(text,0,8),quality:array({type:'string',maxLength:64},0,32),limit:{type:'integer',minimum:1,maximum:200},cursors:{type:'object',maxProperties:20,additionalProperties:text}},['collectionId','recordIds','from','to'],false],
  ['latestValues',sampleBase,['collectionId','recordIds'],false],
  ['deleteRange',{...sampleBase,from:{type:'integer',minimum:0},to:{type:'integer',minimum:1},dryRun:{type:'boolean',default:false}},['collectionId','recordIds','from','to'],true],
 ]){
  const name='sample'+action[0].toUpperCase()+action.slice(1),handler='UnsV2'+name[0].toUpperCase()+name.slice(1),request=handler+'Req',response=handler+'Response';
  schemas[request]=object(fields,required);schemas[response]=object({code:{type:'integer'},msg:text,requestId:text,data:{type:'object',additionalProperties:true}},['code','msg','requestId','data']);
  operations.push({name,handler,request,response,suffix:action==='querySeries'?'/uns/samples:query':action==='latestValues'?'/uns/latest':'/uns/collections/:collectionId/samples:delete',...(action==='deleteRange'?{pathParams:['collectionId']}:{}),resource:'sample',novaAction:action==='querySeries'?'sample.query':action==='latestValues'?'sample.latest':'sample.delete',resourceId:write?215:212,permission:write?'uns.delete':'uns.read',mutation:write?'sample.delete':'',...(write?{mcp:false,destructive:true}:{}),summary:action==='querySeries'?'Nova Sample query: [from,to), per-record raw cursors or numeric buckets (avg/min/max/count/sum/first/last); empty buckets stay absent':action==='latestValues'?'Latest real Sample per Record; null means no Sample; never substitute Record data':'Physically delete Samples inside an explicit Record and [from,to) range; Record tombstones/history outside the range are retained',example:{collectionId:'example-collection',recordIds:['example-record'],...(action!=='latestValues'?{from:1700000000000,to:1700003600000}:{}),...(write?{dryRun:true}:{})}});
 }
 return operations;
}

export function novaTransactionContract(schemas){
 for(const name of ['UnsV2Change','UnsV2RecordChange']){
  const schema=schemas[name];if(!schema)continue;
  Object.assign(schema.properties,{update:{type:'object',minProperties:1,description:'Nova update document, mutually exclusive with legacy operations.'},arrayFilters:array({type:'object'},0,8),upsert:{type:'boolean',default:false}});
  for(const variant of schema.oneOf||[]){if(variant.properties?.action?.enum?.some(a=>a==='record.update'||a==='record.modify')){variant.required=variant.required?.filter(k=>k!=='operations');variant.anyOf=[{required:['operations'],not:{required:['update']}},{required:['update'],properties:{action:{enum:['record.update']}},not:{required:['operations']}}]}}
 }
}
