// Executed by TestAppClientHTTP against generated handlers and an isolated real database.
import assert from 'node:assert/strict';
import {createAppDataClient, AppDataError} from './src/index.ts';
import {workOrdersService} from '../../examples/app-api/work-orders.server.ts';

const data = createAppDataClient({apiBase: process.env.APP_API_BASE, apiKey: process.env.APP_API_KEY});
const checks = [];
const orders = workOrdersService(data, async action => {checks.push(action);});
const created = await orders.create({title: 'Inspect pump', priority: 2}, {id: 'sdk-order'});
assert.equal(created.status, 'committed');
const page = await orders.list(1, false);
assert.equal(page.items.length, 1);
assert.equal(page.items[0].data.title, 'Inspect pump');
const completed = await orders.complete(page.items[0], {});
assert.equal(completed.modifiedCount, 1);
await assert.rejects(orders.complete(page.items[0], {}), error => error instanceof AppDataError && error.code === 'VERSION_CONFLICT');
assert.equal((await orders.list(1, true)).items.length, 1);
assert.ok(checks.includes('read') && checks.includes('create') && checks.includes('complete'));

const forbidden = workOrdersService(data, async () => { throw new Error('Business permission denied'); });
await assert.rejects(forbidden.create({title: 'Denied', priority: 1}, {idempotencyKey: 'forbidden'}), /Business permission denied/);
assert.equal((await orders.list()).items.length, 1);
await assert.rejects(data.collection('private'), error => error.code === 'FORBIDDEN');

const collection = await data.collection('orders');
const time = Date.now();
const items = [
  collection.changes.create('new', {title: 'Sensor inspection', priority: 10, done: false}, 'sdk-sensor'),
];
assert.equal((await data.transact(items, {idempotencyKey: 'sdk-batch'})).status, 'committed');
assert.equal((await data.transact(items, {idempotencyKey: 'sdk-batch'})).replayed, true);
const appended = await collection.append('sdk-sensor', [{id:'sdk-sample',time,data:{temperature:25}}], {idempotencyKey:'sdk-sample-append'});
assert.equal(appended.status,'committed');
assert.equal((await collection.append('sdk-sensor', [{id:'sdk-sample',time,data:{temperature:25}}], {idempotencyKey:'sdk-sample-append'})).replayed,true);
assert.equal((await collection.latest(['sdk-sensor']))[0].sample.data.temperature, 25);
assert.equal((await collection.samplesFind({recordIds:['sdk-sensor'],from:time,to:time+1,filter:{temperature:{$gt:20}}})).items.length, 1);
assert.equal((await collection.find({sort:[['priority',-1]],limit:1})).items[0].id, 'sdk-sensor');

const original = await collection.get('sdk-order');
await collection.deleteOne({_id:original.id});
assert.equal(await collection.get('sdk-order'), null);
console.log('App service -> typed client -> real OpenAPI -> database: CRUD, query, atomic batch, sample history and conflicts passed');
