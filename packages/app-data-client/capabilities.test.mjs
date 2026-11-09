import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {createAppDataClient,validateCapabilityDiscovery} from './src/index.ts';
const vectors=JSON.parse(readFileSync(new URL('./contracts/capability-discovery/vectors.json',import.meta.url)));
for(const v of vectors)test(`shared discovery: ${v.name}`,()=>{
 if(v.valid){const result=validateCapabilityDiscovery(v.response);if(v.expected)assert.deepEqual(result,v.expected);}
 else assert.throws(()=>validateCapabilityDiscovery(v.response));
});
test('capabilities.discover uses raw v2 JSON once per explicit call, without platform/control prerequisite',async()=>{
 const calls=[];
 const response=vectors.find(v=>v.valid&&v.response.product==='enterprise').response;
 const api=createAppDataClient({apiBase:'https://example.test/openapi/v2',apiKey:'test-only',fetch:async(url,init)=>{
  calls.push(String(url));assert.equal(init.method,'POST');assert.equal(init.body,'{}');assert.equal(init.redirect,'error');
  assert.equal(init.headers['X-API-Key'],'test-only');return Response.json(response);
 }});
 assert.deepEqual(await api.capabilities.discover(),validateCapabilityDiscovery(response));
 await api.capabilities.discover();assert.equal(calls.length,2);assert.ok(calls.every(p=>p==='https://example.test/openapi/v2/capabilities/discover'));
 await assert.rejects(api.capabilities.discover({scope:'all'}),TypeError);assert.equal(calls.length,2);
});
test('discovery never retries HTTP failure or accepts an old envelope',async()=>{
 for(const status of [401,403,503]){let calls=0;const api=createAppDataClient({apiBase:'https://example.test/openapi/v2',apiKey:'test-only',fetch:async()=>{calls++;return new Response('rejected',{status});}});await assert.rejects(api.capabilities.discover(),e=>e.status===status&&!e.retryable);assert.equal(calls,1);}
 const api=createAppDataClient({apiBase:'https://example.test/openapi/v2',apiKey:'test-only',fetch:async()=>Response.json({code:200,data:vectors[0].response})});await assert.rejects(api.capabilities.discover());
});
