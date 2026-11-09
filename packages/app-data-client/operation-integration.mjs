// Runs against the isolated Go test HTTP server; never defaults to a live host.
import assert from 'node:assert/strict';
import {createAppDataClient} from './src/index.ts';

const apiBase = process.env.OPERATION_TEST_API_BASE;
const apiKey = process.env.OPERATION_TEST_API_KEY;
if (!apiBase?.startsWith('http://127.0.0.1:') || !apiKey) throw new Error('isolated fixture required');
const api = createAppDataClient({apiBase, apiKey});
const identity = await api.context();
assert.equal(identity.appId, 'device-control-app');
assert.deepEqual(new Set(identity.platformPermissions), new Set(['control.read', 'control.execute', 'control.write', 'stream.requests.read', 'stream.requests.invoke', 'stream.requests.write']));
const page = await api.controls.discover({search: 'test.', status: 'active', size: 10});
assert.equal(page.total, 3);
for (const name of ['start', 'status', 'stop']) {
  const operation = api.controls.use({code: `test.${name}`});
  const contract = await operation.describe();
  assert.equal(contract.kind, 'control');
  assert.equal(contract.status, 'active');
  assert.equal(contract.binding, undefined);
  assert.equal(contract.contract.inputSchema.type, 'object');
  const body = {deviceId: 'fixture-1', streamId: 'business-field', entryId: 'not-routing'};
  const key = `app-${name}`;
  const first = await operation.invoke(body, {idempotencyKey: key});
  assert.deepEqual(first.result, {operation: name, deviceId: 'fixture-1'});
  assert.equal(first.execution.usesNATS, false);
  assert.equal(first.execution.replayed, false);
  const second = await operation.invoke(body, {idempotencyKey: key});
  assert.deepEqual(second.result, first.result);
  assert.equal(second.execution.replayed, true);
  assert.equal(second.execution.runId, first.execution.runId);
  await assert.rejects(operation.invoke({...body, deviceId: 'different'}, {idempotencyKey: key}), error => error.status === 409);
}
const requests = await api.streamRequests.discover({search: 'request.', size: 20});
assert.equal(requests.total, 5);
assert.equal(requests.list.some(item => item.kind === 'control' || item.binding), false);
for (const name of ['lookup', 'command', 'null']) {
  const request = api.streamRequests.use({code: `request.${name}`});
  const contract = await request.describe();
  assert.equal(contract.sideEffect, name === 'command' ? 'write' : 'read');
  assert.deepEqual(contract.inputSchema, {});
  assert.equal(contract.binding, undefined);
  const input = {deviceId: 'fixture-1', streamId: 'business-value'};
  const options = {idempotencyKey: `app-request-${name}`};
  const first = await request.invoke(input, options);
  assert.deepEqual(first.result, name === 'null' ? null : input);
  assert.equal(first.code, `request.${name}`);
  assert.equal(first.contractVersion, '1');
  assert.equal(first.replayed, false);
  assert.equal(first.status, 'succeeded');
  assert.equal(first.execution, undefined);
  const second = await request.invoke(input, options);
  assert.equal(second.runID, first.runID);
  assert.equal(second.replayed, true);
  await assert.rejects(request.invoke({...input, changed: true}, options), error => error.status === 409 && error.code === 'IDEMPOTENCY_CONFLICT' && !error.retryable);
}
assert.equal((await api.streamRequests.use({code: 'request.null'}).invoke(null)).result, null);
await assert.rejects(api.streamRequests.use({code: 'request.command'}).invoke(null), error => error.status === 400 && error.code === 'STREAM_REQUEST_INVALID_INPUT');
console.log('Bound App key -> App SDK -> authenticated v1 controls / Cloud v2 requests -> isolated executor: passed');
