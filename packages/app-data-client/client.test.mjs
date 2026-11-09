import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createAppDataClient, AppDataError} from './src/index.ts';

test('Cloud requests use flat responses, nullable input and body idempotency keys', async () => {
  const calls = [];
  const receipt = {code:'device.echo',contractVersion:'1',result:null,runID:'opaque',status:'succeeded',replayed:false};
  const client = createAppDataClient({apiBase:'https://example.test/openapi/v2',apiKey:'test-only',fetch:async (url, init) => {
    calls.push({path:new URL(url).pathname,body:JSON.parse(init.body),headers:init.headers});
    assert.equal(init.redirect,'error');
    return new Response(JSON.stringify(String(url).endsWith('/invoke') ? receipt : String(url).endsWith('/describe') ? {code:'device.echo',inputSchema:{},sideEffect:'read'} : {total:1,list:[]}));
  }});
  assert.equal((await client.streamRequests.discover({size:20})).total,1);
  const request = client.streamRequests.use({code:'device.echo'});
  assert.equal((await request.describe()).sideEffect,'read');
  assert.deepEqual(await request.invoke(null),receipt);
  await request.invoke({requestCode:'business-only'}, {idempotencyKey:'持久键'});
  assert.deepEqual(calls.map(c=>c.path),['/openapi/v2/stream-requests/discover','/openapi/v2/stream-requests/device.echo/describe','/openapi/v2/stream-requests/device.echo/invoke','/openapi/v2/stream-requests/device.echo/invoke']);
  assert.deepEqual(calls[2].body,{input:null});
  assert.deepEqual(calls[3].body,{input:{requestCode:'business-only'},idempotencyKey:'持久键'});
  assert.equal(calls[3].headers['Idempotency-Key'],undefined);
  for (const code of ['../other','Device.echo','a..b','a/describe', 'a'.repeat(129)]) assert.throws(()=>client.streamRequests.use({code}),/stable/);
  assert.throws(()=>request.invoke(undefined),/use null/);
  for (const idempotencyKey of [' leading','trailing ', '界'.repeat(43), 1, null]) assert.throws(()=>request.invoke(null,{idempotencyKey}),/128 UTF-8/);
  assert.equal(calls.length,4);
});

test('Cloud request errors remain typed and unknown outcomes never trigger retries', async () => {
  for (const [status,code] of [[400,'STREAM_REQUEST_INVALID_INPUT'],[403,'STREAM_REQUEST_FORBIDDEN'],[409,'IDEMPOTENCY_CONFLICT'],[422,'STREAM_REQUEST_OUTPUT_INVALID'],[504,'STREAM_REQUEST_RESULT_UNKNOWN']]) {
    let calls=0;
    const client=createAppDataClient({apiBase:'https://example.test/openapi/v2',apiKey:'test-only',fetch:async()=>{calls++;return new Response(JSON.stringify({errorCode:code,message:'stable message'}),{status});}});
    await assert.rejects(client.streamRequests.use({code:'device.echo'}).invoke(null),error=>error instanceof AppDataError && error.code===code && error.status===status && !error.retryable);
    assert.equal(calls,1);
  }
  let calls=0;
  const client=createAppDataClient({apiBase:'https://example.test/openapi/v2',apiKey:'test-only',fetch:async()=>{calls++;throw new TypeError('lost response');}});
  await assert.rejects(client.streamRequests.use({code:'device.echo'}).invoke(null,{idempotencyKey:'same-intent'}),error=>error.code==='STREAM_REQUEST_RESULT_UNKNOWN' && error.status===504 && !error.retryable);
  assert.equal(calls,1);
});

test('Cloud requests reject incomplete success receipts', async () => {
  for (const result of [{code:200,data:{}},{code:'device.echo',contractVersion:'1',runID:'run',status:'succeeded',replayed:false},{code:'device.echo',contractVersion:'1',runID:'run',status:'running',replayed:false,result:null}]) {
    const client=createAppDataClient({apiBase:'https://example.test/openapi/v2',apiKey:'test-only',fetch:async()=>new Response(JSON.stringify(result))});
    await assert.rejects(client.streamRequests.use({code:'device.echo'}).invoke(null),error=>error.code==='INVALID_RESPONSE' && !error.retryable);
  }
});

test('data requests use collection actions, canonical rows and explicit multi Record sample scope', async () => {
  const calls=[];
  const record={id:'sensor-1',collectionId:'readings',data:{temperature:25},revision:1,createdAt:1,updatedAt:1,sourceId:null,sourceTime:null};
  const sample={id:'s1',collectionId:'readings',recordId:'sensor-1',time:1,receivedAt:2,quality:'good',data:{temperature:25},sourceId:null};
  const api=createAppDataClient({apiBase:'https://example.test/openapi/v2',apiKey:'test-only',fetch:async (url,init)=>{
    const body=JSON.parse(init.body); calls.push({url:String(url),body});
    const pathname=new URL(url).pathname;
    const data=pathname.endsWith('/apps/context') ? {collections:[{alias:'sensors',collectionId:'readings'}]}
      : pathname.endsWith('samples:latest') ? {series:[{recordId:'sensor-1',sample}]}
      : pathname.endsWith('records:find') ? {items:[record]}
      : pathname.endsWith('samples:find') ? {items:[sample]}
      : pathname.endsWith('records:findOneAndUpdate') ? {value:record,status:'committed',replayed:false}
      : pathname.endsWith('records:bulkWrite') ? {insertedIds:{'0':'r1','2':'r3'},upsertedIds:{'3':'r4'},writeErrors:[{index:1,error:{code:'CONFLICT'}}]}
      : {status:'committed',results:[]};
    return new Response(JSON.stringify({code:200,data}));
  }});
  const collection=await api.collection('sensors');
  assert.deepEqual(await collection.get('sensor-1'),record);
  const samples=await collection.samplesFind({recordIds:['sensor-1','sensor-2'],from:0,to:3,filter:{temperature:{$gt:20}}});
  assert.deepEqual(samples.items,[sample]);
  await collection.append('sensor-1',[{id:'sample',time:1,data:{value:2}}]);
  assert.deepEqual((await collection.latest(['sensor-1','sensor-2']))[0].sample,sample);
  assert.deepEqual((await collection.findOneAndUpdate({_id:'sensor-1'},{$inc:{temperature:1}})).value,record);
  const bulk=await collection.bulkWrite([{insertOne:{data:{temperature:1}}}]);
  assert.deepEqual(bulk.insertedIds,{'0':'r1','2':'r3'});
  assert.deepEqual(bulk.upsertedIds,{'3':'r4'});
  await collection.restore(['sensor-1','sensor-2']);
  await collection.lock({ids:['sensor-1'],ttlMs:1000});
  await collection.renew([{id:'sensor-1',token:'lease'}]);
  await collection.unlock([{id:'sensor-1',token:'lease'}]);
  await api.samples.aggregate('readings',[{$group:{_id:'$_meta.recordId',total:{$count:{}}}}],{recordIds:['sensor-1','sensor-2'],from:0,to:3});
  await api.samples.delete('readings',['sensor-1'],0,3,true);
  const requests=calls.slice(1);
  assert.ok(requests.every(request=>request.url.includes('/uns/collections/readings/')));
  assert.deepEqual(requests[1].body.recordIds,['sensor-1','sensor-2']);
  assert.ok(requests[2].url.endsWith('/records/sensor-1/samples'));
  assert.ok(!('recordId' in requests[2].body.items[0]));
  assert.ok(requests[3].url.endsWith('/samples:latest'));
  assert.deepEqual(requests[6].body.ids,['sensor-1','sensor-2']);
  assert.throws(()=>api.samples.append('readings','',[]),/recordId/);
  assert.equal('query' in api.records,false);
  assert.equal('querySeries' in api.samples,false);
  assert.equal('appendBatch' in api.samples,false);
  const before=calls.length;
  assert.throws(()=>api.transact([{opId:'sample',action:'sample.append',collectionId:'readings',recordId:'sensor-1',time:1,data:{value:2}}]),/record operations only/);
  assert.equal(calls.length,before);
});

test('client preserves non-JSON HTTP status without inferring write retry safety', async () => {
  for (const [status, body] of [[413, 'Request too large'], [401, '<html>Unauthorized</html>'], [429, ''], [503, 'Service unavailable']]) {
    let calls = 0;
    const client = createAppDataClient({apiBase:'https://example.test/openapi/v2', apiKey:'test-only', fetch:async () => { calls++; return new Response(body, {status}); }});
    await assert.rejects(client.transact([], {idempotencyKey:'same-intent'}), error => error instanceof AppDataError && error.status === status && error.code === 'HTTP_ERROR' && error.retryable === false && /inspect business state/.test(error.message));
    assert.equal(calls, 1);
  }
});

test('client distinguishes malformed success from network failure', async () => {
  for (const body of ['not-json', 'null', '[]']) {
    const client = createAppDataClient({apiBase:'https://example.test/openapi/v2', apiKey:'test-only', fetch:async () => new Response(body, {status:200})});
    await assert.rejects(client.context(), error => error.status === 200 && error.code === 'INVALID_RESPONSE' && error.retryable === false);
  }
});

test('client does not retry an ambiguous write or follow redirects with its credential', async () => {
  let calls = 0;
  const data = createAppDataClient({apiBase:'https://example.test/openapi/v2',apiKey:'test-only',fetch:async (_url,init) => {
    calls++;
    assert.equal(init.redirect,'error');
    assert.ok(init.signal);
    throw new TypeError('connection dropped');
  }});
  await assert.rejects(data.transact([{action:'record.delete',opId:'remove',collectionId:'orders',filter:{id:'1'},expectedRevision:2}],{idempotencyKey:'preserved'}),
    error => error instanceof AppDataError && error.code==='RESPONSE_UNAVAILABLE' && error.retryable === false);
  assert.equal(calls,1);
});

test('client validates endpoint and preserves structured conflict details', async () => {
  assert.throws(() => createAppDataClient({apiBase:'https://secret@example.test/openapi/v2',apiKey:'key'}),/trusted/);
  const data = createAppDataClient({apiBase:'https://example.test/openapi/v2',apiKey:'test-only',fetch:async () => new Response(JSON.stringify({code:409,msg:'Revision changed',requestId:'request-1',error:{code:'VERSION_CONFLICT',retryable:false}}),{status:409})});
  await assert.rejects(data.transact([],{idempotencyKey:'intent-1'}),error => error.code==='VERSION_CONFLICT' && error.requestId==='request-1' && !error.retryable);
});

test('client retains explicit server rollback advice without automatically replaying a transaction', async () => {
  const retryAdvice = {recommendedAction:'retrySameTransaction',canRetrySameRequest:true,keepIdempotencyKey:true};
  const details = [{opId:'adjust',path:'data.value',conditionIndex:0}];
  let calls = 0;
  const client = createAppDataClient({apiBase:'https://example.test/openapi/v2',apiKey:'test-only',fetch:async (_url,init) => {
    calls++;
    assert.equal(init.redirect,'error');
    assert.equal(JSON.parse(init.body).idempotencyKey,'original-intent');
    return new Response(JSON.stringify({code:409,msg:'Transaction rolled back',requestId:'rollback-1',error:{code:'CONCURRENT_MODIFICATION',retryable:true,rolledBack:true,retryAdvice,details}}),{status:409});
  }});
  await assert.rejects(client.records.transact([{action:'record.check',opId:'adjust',collectionId:'orders',filter:{id:'1'},expectedRevision:2}],{idempotencyKey:'original-intent'}),error => {
    assert.ok(error instanceof AppDataError);
    assert.equal(error.code,'CONCURRENT_MODIFICATION');
    assert.equal(error.retryable,true);
    assert.equal(error.rolledBack,true);
    assert.equal(error.requestId,'rollback-1');
    assert.deepEqual(error.retryAdvice,retryAdvice);
    assert.deepEqual(error.details,details);
    return true;
  });
  assert.equal(calls,1);
});

test('operation handle sends the business body and control header separately', async () => {
  const calls=[];
  const api=createAppDataClient({apiBase:'https://example.test/enterprise/openapi/v2',apiKey:'test-only',fetch:async (url,init)=>{
    calls.push({url:String(url),init});
    return new Response(JSON.stringify({code:200,data:{controlCode:'device.start',result:{accepted:true},execution:{replayed:false,usesNATS:false}}}));
  }});
  const body={deviceId:'A',speed:100,idempotencyKey:'business-field',entryId:'business-field'};
  const result=await api.controls.use({code:'device.start'}).invoke(body,{idempotencyKey:'saved-intent'});
  assert.equal(calls[0].url,'https://example.test/enterprise/openapi/v1/controls/device.start/invoke');
  assert.deepEqual(JSON.parse(calls[0].init.body),body);
  assert.equal(calls[0].init.headers['Idempotency-Key'],'saved-intent');
  assert.equal(calls[0].init.headers['X-API-Key'],'test-only');
  assert.equal(calls[0].init.redirect,'error');
  assert.equal(result.result.accepted,true);
  assert.equal(result.execution.usesNATS,false);
  assert.throws(()=>api.controls.use({code:'..'}),/stable/);
  assert.throws(()=>api.controls.use({code:'device.start'}).invoke(body,{}),/idempotencyKey/);
});

test('operation discovery and description use the public resource contract', async () => {
  const calls=[];
  const api=createAppDataClient({apiBase:'https://example.test/openapi/v2',apiKey:'test-only',fetch:async (url,init)=>{
    calls.push({url:String(url),body:JSON.parse(init.body)});
    return new Response(JSON.stringify({code:200,data:{}}));
  }});
  await api.controls.discover({search:'device',status:'active',page:2,size:10});
  await api.controls.use({code:'device.start'}).describe();
  assert.deepEqual(calls,[
    {url:'https://example.test/openapi/v1/controls/discover',body:{search:'device',status:'active',page:2,size:10}},
    {url:'https://example.test/openapi/v1/controls/device.start/describe',body:{}},
  ]);
});

test('operation invocation never retries a lost response or follows a redirect', async () => {
  let calls=0;
  const api=createAppDataClient({apiBase:'https://example.test/openapi/v2',apiKey:'test-only',fetch:async (_url,init)=>{
    calls++;assert.equal(init.redirect,'error');assert.equal(init.headers['Idempotency-Key'],'same-intent');throw new TypeError('lost response');
  }});
  await assert.rejects(api.controls.use({code:'device.start'}).invoke({},{idempotencyKey:'same-intent'}),error=>error instanceof AppDataError&&error.code==='RESPONSE_UNAVAILABLE');
  assert.equal(calls,1);
});
