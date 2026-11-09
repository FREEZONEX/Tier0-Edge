import {readFileSync} from 'node:fs';
const read=name=>JSON.parse(readFileSync(new URL(`./shared/${name}`,import.meta.url),'utf8'));
export function applyUnifiedSourceStream(source){
 // Retire metadata-era Source writes and the duplicate credential envelope.
 // Dedicated lifecycle operations retain impact, snapshot and permission guards.
 source.operations=source.operations.filter(op=>!['sourceArchive','sourceRestore','sourceCredentialsSave'].includes(op.name));
 const shared=read('source-stream-shared.json'), effective=read('source-stream-effective.json');
 Object.assign(source.schemas,structuredClone(shared.schemas));
 source.schemas.NativeSourceDocument=structuredClone(shared.schemas.SourceDefinition);
 source.schemas.RuntimeStreamDefinition=structuredClone(shared.schemas.StreamDefinition);
 // Only common lifecycle requests are imported. Native response observations
 // retain their real target-specific fields; importing schemas never adds a driver.
 for(const operation of source.operations){
  const common=effective.operations.find(item=>item.name===operation.name);
  if(!common)continue;
  operation.request=common.request;
  operation.example=structuredClone(common.example||{});
  if(operation.name.startsWith('sourceNative'))operation.mcp=true;
  const visit=name=>{
   if(shared.schemas[name])return;
   const schema=effective.schemas[name];if(!schema)throw Error(`Missing common request schema ${name}`);
   source.schemas[name]=structuredClone(schema);
   const walk=value=>{if(!value||typeof value!=='object')return;if(value.$ref){const dependency=value.$ref.split('/').at(-1);if(!source.schemas[dependency]||effective.schemas[dependency]){if(!visited.has(dependency)){visited.add(dependency);visit(dependency);}}}for(const child of Object.values(value))walk(child);};
   walk(schema);
  };
  const visited=new Set([common.request]);visit(common.request);
  if(operation.name==='sourceProbe'){operation.response=common.response;visit(common.response);}
 }
 source.schemas.SourceCheckResult=structuredClone(effective.schemas.SourceCheckResult);
 const runtime=source.schemas.UnsV2SourceRuntimeGetResponse?.properties?.data?.properties?.state;
 if(runtime?.properties)Object.assign(runtime.properties,{connectionCheck:{$ref:'#/components/schemas/SourceCheckResult'},dataCheck:{$ref:'#/components/schemas/SourceCheckResult'}});
 for(const name of ['NativeSourceView','CloudSourceDocumentView','CloudSourceDocumentSummary'])if(source.schemas[name]?.properties)Object.assign(source.schemas[name].properties,{connectionCheck:{$ref:'#/components/schemas/SourceCheckResult'},dataCheck:{$ref:'#/components/schemas/SourceCheckResult'}});
 source.cloudContractRevision+='+unified-source-stream/1';
}
