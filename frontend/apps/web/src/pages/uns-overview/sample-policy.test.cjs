const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { stripTypeScriptTypes } = require('node:module');

// Load the real service with only its transport dependencies replaced.
async function service(options = {}) {
  const calls = [];
  const list = ['r1', 'r2'].map((id) => ({ id, collectionId: 'c1', data: { value: 42 } }));
  const mocks = {
    unsV2Api: {
      async collectionGet(collectionId, signal) {
        calls.push({ operation: 'definition', collectionId, signal });
        if (options.definitionError) throw options.definitionError;
        if (options.missingDefinition) return undefined;
        return { kind: 'collection', data: options.enabled ? { sampleSchema: { type: 'object' } } : {} };
      },
      async dataAction(collectionId,resource,action,query,signal) {
        calls.push({operation:`${resource}:${action}`,collectionId,query,signal});
        return {items:list.filter(record=>record.id===query.filter._id)};
      },
    },
    async recordPage(collectionId,query,signal) {
      calls.push({operation:'recordPage',collectionId,query,signal});
      const records=query.recordIds?list.filter(record=>query.recordIds.includes(record.id)):list;
      return {list:records,total:records.length,page:query.page||1,size:query.size};
    },
    async latestSamples(collectionId,recordIds,signal) {
      calls.push({operation:'samples:latest',collectionId,recordIds,signal});
      if(options.sampleError)throw options.sampleError;
      return recordIds.map(recordId=>({recordId,sample:{id:'s1'}}));
    },
    metadataApi: {
      async samples(collectionId, query, signal) {
        calls.push({ operation: 'history', collectionId, query, signal });
        if (options.sampleError) throw options.sampleError;
        return { list: [{ id: 's1', data: { value: 42 } }], total: 1, page: query.page || 1, size: query.size };
      },
    },
  };
  const key = `__overview_test_${crypto.randomUUID().replaceAll('-', '')}`;
  globalThis[key] = mocks;
  const source = fs
    .readFileSync(path.join(__dirname, '../../apis/core-api/uns-graph.ts'), 'utf8')
    .replace(/import \{ latestSamples, recordPage, unsV2Api \} from '\.\/uns-v2';/, `const { latestSamples, recordPage, unsV2Api } = globalThis['${key}'];`)
    .replace(
      /import \{ metadataApi, type Page, type UnsRecord, type UnsSample \} from '\.\/uns-metadata';/,
      `const { metadataApi } = globalThis['${key}'];`
    );
  try {
    const imported = await import(
      `data:text/javascript;base64,${Buffer.from(stripTypeScriptTypes(source)).toString('base64')}`
    );
    return { api: imported.unsGraphApi, calls, list };
  } finally {
    delete globalThis[key];
  }
}

test('record-only Collection checks formal definition and never requests latest Samples', async () => {
  const { api, calls, list } = await service();
  const result = await api.records('c1', 1);
  assert.deepEqual(result.list, list);
  assert.equal(result.samplesEnabled, false);
  assert.equal(result.historyFailed, undefined);
  assert.deepEqual(result.latestSamples, [
    { recordId: 'r1', sample: null },
    { recordId: 'r2', sample: null },
  ]);
  assert.deepEqual(
    calls.map((call) => call.operation),
    ['recordPage', 'definition']
  );
  assert.equal(calls[1].collectionId, 'c1');
});

test('record-only deep link preserves current Record and returns disabled history without a Sample request', async () => {
  const { api, calls } = await service();
  const result = await api.recordHistory('c1', 'r1', { startTime: 100, endTime: 200, page: 3 });
  assert.equal(result.record.id, 'r1');
  assert.equal(result.record.data.value, 42);
  assert.equal(result.samplesEnabled, false);
  assert.equal(result.hasHistory, false);
  assert.deepEqual(result.history, { list: [], total: 0, page: 3, size: 50 });
  assert.ok(calls.every((call) => call.operation !== 'history' && call.operation !== 'samples:latest'));
});

test('Sample-enabled Collection still checks a full Record page in one latest batch', async () => {
  const { api, calls } = await service({ enabled: true });
  const result = await api.records('c1', 2);
  assert.equal(result.samplesEnabled, true);
  assert.equal(result.latestSamples.length, 2);
  const samples = calls.filter((call) => call.operation === 'samples:latest');
  assert.equal(samples.length, 1);
  assert.deepEqual(samples[0].recordIds, ['r1', 'r2']);
});

test('Sample-enabled history preserves range, page and separate latest check', async () => {
  const { api, calls } = await service({ enabled: true });
  const result = await api.recordHistory('c1', 'r1', { startTime: 100, endTime: 200, page: 3 });
  assert.equal(result.samplesEnabled, true);
  assert.equal(result.hasHistory, true);
  const requests = calls.filter((call) => call.operation === 'history');
  assert.deepEqual(
    requests.map((call) => call.query),
    [
      { recordId: 'r1', startTime: 100, endTime: 200, page: 3, size: 50 },
      { recordId: 'r1', size: 1 },
    ]
  );
});

test('arbitrary 422 Sample failure is not mistaken for disabled or empty history', async () => {
  const error = Object.assign(new Error('INVALID_TIME_RANGE'), { status: 422 });
  const { api } = await service({ enabled: true, sampleError: error });
  const records = await api.records('c1', 1);
  assert.equal(records.historyFailed, true);
  assert.equal(records.samplesEnabled, true);
  await assert.rejects(api.recordHistory('c1', 'r1', { page: 1 }), (received) => received === error);
});

test('missing definition and access errors remain failures instead of a no-history state', async () => {
  const missing = await service({ missingDefinition: true });
  const records = await missing.api.records('c1', 1);
  assert.equal(records.historyFailed, true);
  assert.equal(records.samplesEnabled, undefined);
  await assert.rejects(missing.api.recordHistory('c1', 'r1', { page: 1 }), /definition is unavailable/);
  const forbidden = await service({ definitionError: new Error('FORBIDDEN') });
  await assert.rejects(forbidden.api.recordHistory('c1', 'r1', { page: 1 }), /FORBIDDEN/);
});

test('an aborted request is never converted to a history warning', async () => {
  const controller = new AbortController();
  controller.abort();
  const error = new Error('ABORTED');
  const { api } = await service({ enabled: true, sampleError: error });
  await assert.rejects(api.records('c1', 1, controller.signal), (received) => received === error);
});

test('table and history adapters send collection actions and preserve nested business data', async () => {
  const calls = [];
  const sample = {id:'s1',collectionId:'c1',recordId:'r1',time:10,receivedAt:11,quality:'good',sourceId:null,data:{data:{value:42}}};
  const record = {id:'r1',collectionId:'c1',revision:1,createdAt:1,updatedAt:2,sourceId:null,sourceTime:null,data:{data:{value:42}}};
  const key=`__data_test_${crypto.randomUUID().replaceAll('-','')}`;
  globalThis[key] = {ApiWrapper: class {
    async post(route,body) {
      calls.push({route,body});
      if(route.endsWith('records:aggregate')) {
        const {id,data,...meta}=record;
        return {items:[{_id:id,_meta:meta,...data}]};
      }
      if(route.endsWith('records:count')) return {count:21};
      if(route.endsWith('samples:aggregate')) return {items:[{total:25}]};
      if(route.endsWith('samples:latest')) return {series:body.recordIds.map(recordId=>({recordId,sample}))};
      return {items:[sample]};
    }
  }};
  const source=fs.readFileSync(path.join(__dirname,'../../apis/core-api/uns-v2.ts'),'utf8')
    .replace("import { ApiWrapper } from '@/utils/request';",`const {ApiWrapper}=globalThis['${key}'];`)
    .replace("import { message } from 'antd';",'')
    .replace("import { getIntl } from '@/stores/i18n-store';",'');
  try {
    const api=await import(`data:text/javascript;base64,${Buffer.from(stripTypeScriptTypes(source,{mode:'transform'})).toString('base64')}`);
    const page=await api.recordPage('c1',{page:2,size:20,search:'r1'});
    assert.deepEqual(page.list,[record]);
    assert.equal(page.total,21);
    assert.deepEqual(calls[0].body.page,{mode:'offset',page:2,size:20});
    assert.deepEqual(calls[0].body.pipeline[0],{$match:{_id:'r1'}});
    const history=await api.sampleHistory('c1','r1',{startTime:1,endTime:100,page:2,size:20});
    assert.deepEqual(history.list,[sample]);
    assert.equal(history.total,25);
    assert.deepEqual(calls[2].body.recordIds,['r1']);
    assert.equal(calls[2].body.from,1);
    assert.equal(calls[2].body.to,100);
    assert.ok(calls[2].route.endsWith('/collections/c1/samples:find'));
    await api.latestSamples('c1',Array.from({length:21},(_,index)=>`r${index}`));
    assert.equal(calls.at(-2).body.recordIds.length,20);
    assert.equal(calls.at(-1).body.recordIds.length,1);
    assert.ok(calls.every(call=>call.route.startsWith('/collections/c1/')));
  } finally {
    delete globalThis[key];
  }
});
