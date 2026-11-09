import { test } from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { OPCUAServer, Variant, DataType, StatusCodes, MessageSecurityMode, SecurityPolicy } from 'node-opcua';
import { start } from '../server.mjs';
import { SourceSessionPool } from '../session-pool.mjs';

function request(socketPath, path, body) {
  return new Promise((resolve, reject) => {
    const req = http.request({ socketPath, path, method: 'POST', headers: { 'Content-Type': 'application/json' } }, (res) => {
      let raw = '';
      res.on('data', (chunk) => { raw += chunk; });
      res.on('end', () => { try { resolve(JSON.parse(raw)); } catch (error) { reject(error); } });
    });
    req.on('error', reject); req.setTimeout(10000, () => req.destroy(new Error('test request timed out')));
    req.end(JSON.stringify(body));
  });
}

async function eventually(assertion) {
  let last;
  for (let i = 0; i < 160; i++) {
    try { assertion(); return; } catch (error) { last = error; }
    await delay(25);
  }
  throw last;
}

test('real OPC UA Source session is reused by Invoke, discovery and watch; revisions and stop release it', { timeout: 30000 }, async (t) => {
  const directory = await mkdtemp(join(tmpdir(), 'tier0-opcua-reuse-'));
  const ua = new OPCUAServer({ port: 0, securityModes: [MessageSecurityMode.None], securityPolicies: [SecurityPolicy.None] });
  await ua.initialize();
  let value = 12;
  ua.engine.addressSpace.getOwnNamespace().addVariable({ organizedBy: ua.engine.addressSpace.rootFolder.objects,
    browseName: 'Value', nodeId: 'ns=1;s=Value', dataType: 'Double', minimumSamplingInterval: 100,
    value: { get: () => new Variant({ dataType: DataType.Double, value }), set: (variant) => { value = variant.value; return StatusCodes.Good; } } });
  ua.engine.addressSpace.getOwnNamespace().addVariable({ organizedBy: ua.engine.addressSpace.rootFolder.objects,
    browseName: 'Second', nodeId: 'ns=1;s=Second', dataType: 'Double', minimumSamplingInterval: 100,
    value: { get: () => new Variant({ dataType: DataType.Double, value: 99 }) } });
  await ua.start();
  const socket = join(directory, 'runtime.sock');
  // Reuse/isolation must use the production idle policy. A deliberately short
  // idle policy is tested independently, not raced against PKI/session setup.
  const runtime = await start(socket, { pkiRoot: join(directory, 'client-pki') });
  t.after(async () => {
    await runtime.closeClients(); runtime.closeAllConnections(); await new Promise((resolve) => runtime.close(resolve));
    await ua.shutdown(0); await rm(directory, { recursive: true, force: true });
  });
  const body = { sourceId: 11, configRevision: 1, definition: { name: 'first label', protocol: 'opcua', endpoint: ua.getEndpointUrl(), opcua: {
    securityMode: 'None', securityPolicy: 'None', samplingIntervalMs: 100, nodeIds: ['ns=1;s=Value', 'ns=1;s=Second'],
  } } };
  assert.equal((await request(socket, '/probe', body)).data.health, 'healthy');
  const sourceSession = Object.keys(ua.engine._sessions)[0];
  for (let i = 0; i < 3; i++) assert.equal((await request(socket, '/invoke', { ...body, resourceRef: 'opcua:ns=1;s=Value', input: { operation: 'read' } })).data.value, value);
  assert.equal((await request(socket, '/discover', { ...body, definition: { ...body.definition, name: 'renamed label' } })).data.length, 2);
  assert.equal(ua.engine.currentSessionCount, 1);
  assert.equal(Object.keys(ua.engine._sessions)[0], sourceSession, 'same real session remains open across operations and display rename');
  const parallel = await Promise.all(Array.from({ length: 6 }, () => request(socket, '/invoke', { ...body, resourceRef: 'opcua:ns=1;s=Value', input: { operation: 'read' } })));
  assert.ok(parallel.every((item) => item.data.value === 12));
  assert.equal(ua.engine.currentSessionCount, 1);
  assert.equal((await request(socket, '/probe', { ...body, sourceId: 12 })).data.health, 'healthy');
  assert.equal(ua.engine.currentSessionCount, 2, 'different Source gets a different real session');
  const otherSourceSession = Object.keys(ua.engine._sessions).find(id => id !== sourceSession);
  assert.equal((await request(socket, '/probe', { ...body, sourceId: 0 })).data.health, 'healthy');
  assert.equal(ua.engine.currentSessionCount, 2, 'unsaved Probe does not retain a pooled session');
  const current = { ...body, configRevision: 2 };
  assert.equal((await request(socket, '/probe', current)).data.health, 'healthy');
  assert.equal(ua.engine.currentSessionCount, 2);
  assert.ok(!Object.keys(ua.engine._sessions).includes(sourceSession), 'changed connection revision retires the old real session');
  const currentSourceSession = Object.keys(ua.engine._sessions).find(id => id !== otherSourceSession);
  let watchRequest;
  await new Promise((resolve, reject) => {
    watchRequest = http.request({ socketPath: socket, path: '/watch', method: 'POST' }, (res) => {
      try { assert.equal(res.headers['x-tier0-watch-ready'], '1'); } catch (error) { reject(error); return; }
      res.on('data', () => resolve()); res.on('error', () => {});
    });
    const scopedWatch = { ...current, definition: { ...current.definition, opcua: { ...current.definition.opcua, nodeIds: ['ns=1;s=Value'] } } };
    watchRequest.on('error', () => {}); watchRequest.end(JSON.stringify(scopedWatch));
  });
  assert.ok(Object.keys(ua.engine._sessions).includes(currentSourceSession), 'Stream-scoped watch borrows the same Source session');
  assert.equal(ua.engine.currentSessionCount, 2, 'another Source remains isolated during the live watch');
  let secondWatch;
  await new Promise((resolve, reject) => {
    secondWatch = http.request({ socketPath: socket, path: '/watch', method: 'POST' }, (res) => {
      try { assert.equal(res.headers['x-tier0-watch-ready'], '1'); } catch (error) { reject(error); return; }
      res.on('data', () => resolve()); res.on('error', () => {});
    });
    secondWatch.on('error', () => {});
    secondWatch.end(JSON.stringify({ ...current, definition: { ...current.definition, opcua: { ...current.definition.opcua, nodeIds: ['ns=1;s=Second'], samplingIntervalMs: 200 } } }));
  });
  assert.equal(ua.engine.currentSubscriptionCount, 2, 'independent Stream scopes coexist on one Source session');
  const wrote = await request(socket, '/invoke', { ...current, resourceRef: 'opcua:ns=1;s=Value', input: { operation: 'write', value: 27 } });
  assert.equal(wrote.data.receipt, 'operation-accepted'); assert.equal(value, 27);
  watchRequest.destroy();
  await eventually(() => assert.equal(ua.engine.currentSubscriptionCount, 1, 'cancel one scope leaves the other subscription live'));
  assert.equal((await request(socket, '/invoke', { ...current, resourceRef: 'opcua:ns=1;s=Value', input: { operation: 'read' } })).data.value, 27);
  assert.ok(Object.keys(ua.engine._sessions).includes(currentSourceSession), 'cancel one scope does not replace its Source session');
  secondWatch.destroy();
  await eventually(() => assert.equal(ua.engine.currentSubscriptionCount, 0));
  assert.equal((await request(socket, '/release', { sourceId: 12 })).data.released, true);
  await eventually(() => assert.equal(ua.engine.currentSessionCount, 0));
  assert.equal((await request(socket, '/probe', current)).data.health, 'healthy');
  assert.equal(ua.engine.currentSessionCount, 1);
  assert.equal((await request(socket, '/release', { sourceId: 11 })).data.released, true);
  assert.equal(ua.engine.currentSessionCount, 0, 'explicit stop closes the retained protocol session');
});

test('real retained Source session closes at the independently configured 1000ms idle policy', { timeout: 10000 }, async (t) => {
  const directory = await mkdtemp(join(tmpdir(), 'tier0-opcua-idle-policy-'));
  const ua = new OPCUAServer({ port: 0, securityModes: [MessageSecurityMode.None], securityPolicies: [SecurityPolicy.None] });
  await ua.initialize(); await ua.start();
  const socket = join(directory, 'runtime.sock');
  const runtime = await start(socket, { pkiRoot: join(directory, 'pki'), clientIdleMs: 1000 });
  t.after(async () => { await runtime.closeClients(); runtime.closeAllConnections();
    await new Promise(resolve => runtime.close(resolve)); await ua.shutdown(0); await rm(directory, { recursive: true, force: true }); });
  const body = { sourceId: 77, configRevision: 1, definition: { protocol: 'opcua', endpoint: ua.getEndpointUrl(),
    opcua: { securityMode: 'None', securityPolicy: 'None', nodeIds: [], samplingIntervalMs: 100 } } };
  assert.equal((await request(socket, '/probe', body)).data.health, 'healthy');
  assert.equal(ua.engine.currentSessionCount, 1, 'completed probe retains exactly one Source session');
  await eventually(() => assert.equal(ua.engine.currentSessionCount, 0, 'configured idle deadline closes the real session'));
});

test('slow unsaved probe permits independent Source idle retirement and closes its own session before returning', async () => {
  let releaseProbe;
  const closed = [];
  const pool = new SourceSessionPool({ idleMs: 10, connect: async body => ({ session: body.sourceId,
    close: async () => { closed.push(body.sourceId); } }) });
  const signal = new AbortController().signal;
  const definition = { protocol: 'opcua' };
  await pool.run({ sourceId: 1, definition }, signal, async session => session);
  const pending = pool.run({ sourceId: 0, definition }, signal, async session => {
    await new Promise(resolve => { releaseProbe = resolve; }); return session;
  });
  await eventually(() => assert.equal(typeof releaseProbe, 'function'));
  await eventually(() => assert.deepEqual(closed, [1], 'idle Source may close while a temporary probe is active'));
  assert.equal(pool.entries.has('1:client'), false);
  assert.equal(pool.entries.size, 1, 'only temporary probe remains while its lease is live');
  releaseProbe(); assert.equal(await pending, 0);
  assert.deepEqual(closed, [1, 0], 'unsaved probe cleanup finishes before the response');
  assert.equal(pool.entries.size, 0, 'no unsaved session is retained');
  await pool.close();
});

test('session pool bounds, cancelled generation cleanup and serialization contract', async () => {
  let serial = 0; let overlapping = false; let closed = 0; let connects = 0;
  const pool = new SourceSessionPool({ limit: 1, idleMs: 20, connect: async () => ({ session: ++connects, close: async () => { closed++; } }) });
  const body = { sourceId: 1, definition: { protocol: 'opcua' } };
  const signal = new AbortController().signal;
  const action = async (session) => { serial++; if (serial > 1) overlapping = true; await delay(10); serial--; return session; };
  assert.deepEqual(await Promise.all([pool.run(body, signal, action), pool.run(body, signal, action)]), [1, 1]);
  assert.equal(overlapping, false);
  await assert.rejects(pool.run({ ...body, sourceId: 2 }, signal, action), /protocolFailed/);
  await assert.rejects(pool.run({ ...body, sourceId: 0 }, signal, action), /protocolFailed/, 'unsaved probes also obey the connection bound');
  const old = pool.entries.get('1:client');
  assert.equal(await pool.run({ ...body, configRevision: 2 }, signal, async (session) => session), 2);
  await pool.retire(old);
  assert.equal(await pool.run({ ...body, configRevision: 2 }, signal, async (session) => session), 2, 'old generation cleanup leaves current session intact');
  await eventually(() => assert.equal(pool.entries.size, 0));
  assert.equal(closed, connects);
  await pool.close();
  await assert.rejects(pool.run(body, signal, action), /protocolFailed/);
});

test('real disconnected cached session: first read after 53s idle reconnects; write is dispatched once', { timeout: 75000 }, async (t) => {
  const directory = await mkdtemp(join(tmpdir(), 'tier0-opcua-idle-loss-'));
  const ua = new OPCUAServer({ port: 0, securityModes: [MessageSecurityMode.None], securityPolicies: [SecurityPolicy.None] });
  await ua.initialize();
  let value = 17, writes = 0;
  ua.engine.addressSpace.getOwnNamespace().addVariable({ organizedBy: ua.engine.addressSpace.rootFolder.objects,
    browseName: 'Value', nodeId: 'ns=1;s=Value', dataType: 'Double', minimumSamplingInterval: 100,
    value: { get: () => new Variant({ dataType: DataType.Double, value }),
      set: (variant) => { writes++; value = variant.value; return StatusCodes.Good; } } });
  await ua.start();
  const socket = join(directory, 'runtime.sock');
  const runtime = await start(socket, { pkiRoot: join(directory, 'client-pki') });
  t.after(async () => {
    await runtime.closeClients(); runtime.closeAllConnections(); await new Promise(resolve => runtime.close(resolve));
    await ua.shutdown(0); await rm(directory, { recursive: true, force: true });
  });
  const body = { sourceId: 801, configRevision: 1, definition: { protocol: 'opcua', endpoint: ua.getEndpointUrl(), opcua: {
    securityMode: 'None', securityPolicy: 'None', samplingIntervalMs: 100, nodeIds: ['ns=1;s=Value'],
  } } };
  assert.equal((await request(socket, '/probe', body)).data.health, 'healthy');
  const oldChannel = ua.endpoints[0].getChannels()[0];
  const other = { ...body, sourceId: 802 };
  assert.equal((await request(socket, '/probe', other)).data.health, 'healthy');
  const otherSession = Object.values(ua.engine._sessions).at(-1);
  // A real peer transport close, not an HTTP failure or fake SDK response.
  await new Promise(resolve => oldChannel.close(resolve));
  await eventually(() => assert.ok(ua.endpoints[0].getChannels().every(channel => channel !== oldChannel)));
  assert.equal((await request(socket, '/invoke', { ...other, resourceRef: 'opcua:ns=1;s=Value', input: { operation: 'read' } })).data.value, 17);
  assert.ok(Object.values(ua.engine._sessions).includes(otherSession), 'lost Source must not retire another Source lease');
  await delay(53000);
  assert.equal((await request(socket, '/invoke', { ...other, resourceRef: 'opcua:ns=1;s=Value', input: { operation: 'read' } })).data?.value, 17,
    'the naturally idle peer also recovers on its first real read');
  const read = await request(socket, '/invoke', { ...body, resourceRef: 'opcua:ns=1;s=Value', input: { operation: 'read' } });
  assert.equal(read.data?.value, 17, 'first operation must acquire a live generation, not fail before reconnecting');
  const write = await request(socket, '/invoke', { ...body, resourceRef: 'opcua:ns=1;s=Value', input: { operation: 'write', value: 17 } });
  assert.equal(write.data?.receipt, 'operation-accepted');
  assert.equal(writes, 1, 'a device write must never be replayed by connection recovery');
});

test('invalidated generation never closes an active lease or a replacement generation', async () => {
  let finish, invalidate, closed = 0, connects = 0;
  const pool = new SourceSessionPool({ connect: async (_, __, lease) => {
    invalidate = lease.invalidate;
    return { session: ++connects, close: async () => { closed++; } };
  } });
  const body = { sourceId: 1, definition: { protocol: 'opcua' } };
  const running = pool.run(body, new AbortController().signal, async session => {
    await new Promise(resolve => { finish = resolve; }); return session;
  });
  await eventually(() => assert.equal(typeof finish, 'function'));
  const oldInvalidator = invalidate;
  oldInvalidator();
  assert.equal(closed, 0, 'invalidate must not close a borrowed operation in progress');
  assert.equal(await pool.run(body, new AbortController().signal, async session => session), 2);
  oldInvalidator();
  finish(); assert.equal(await running, 1);
  assert.equal(await pool.run(body, new AbortController().signal, async session => session), 2);
  assert.equal(closed, 1, 'old generation cleanup must not close the replacement');
  await pool.close();
});

test('invalidating a lost session wakes all watches without cancelling an in-flight operation or its replacement', async (t) => {
  let invalidate, finish, watchReady = 0, aborted = 0, closed = 0, generation = 0;
  const watchControllers = [new AbortController(), new AbortController()];
  const pool = new SourceSessionPool({ connect: async (_, __, lease) => {
    invalidate = lease.invalidate;
    return { session: ++generation, close: async () => { closed++; } };
  } });
  const body = { sourceId: 19, definition: { protocol: 'opcua' } };
  const watches = watchControllers.map(controller => pool.run(body, controller.signal, async (_, signal) => {
    watchReady++;
    await new Promise(resolve => signal.addEventListener('abort', () => { aborted++; resolve(); }, { once: true }));
  }, { watch: true }));
  const running = pool.run(body, new AbortController().signal, async session => {
    await new Promise(resolve => { finish = resolve; }); return session;
  });
  t.after(async () => {
    watchControllers.forEach(controller => controller.abort()); finish?.();
    await Promise.allSettled([...watches, running]); await pool.close();
  });
  await eventually(() => { assert.equal(watchReady, 2); assert.equal(typeof finish, 'function'); });
  const oldInvalidator = invalidate;
  oldInvalidator();
  await eventually(() => assert.equal(aborted, 2, 'both long-lived watches must exit so the Go worker can reconnect'));
  await Promise.all(watches);
  assert.equal(closed, 0, 'ordinary operation is still borrowed; never cancel or re-dispatch its side effect');
  assert.equal(await pool.run(body, new AbortController().signal, async session => session), 2);
  oldInvalidator();
  finish(); assert.equal(await running, 1);
  assert.equal(closed, 1);
  assert.equal(await pool.run(body, new AbortController().signal, async session => session), 2);
});

test('real Server restart closes the stale watch and accepts a fresh watch for the same Source', { timeout: 30000 }, async (t) => {
  const directory = await mkdtemp(join(tmpdir(), 'tier0-opcua-watch-recovery-'));
  let value = 12;
  async function fixture(port) {
    const server = new OPCUAServer({ port, securityModes: [MessageSecurityMode.None], securityPolicies: [SecurityPolicy.None] });
    await server.initialize();
    server.engine.addressSpace.getOwnNamespace().addVariable({ organizedBy: server.engine.addressSpace.rootFolder.objects,
      browseName: 'Value', nodeId: 'ns=1;s=Value', dataType: 'Double', minimumSamplingInterval: 100,
      value: { get: () => new Variant({ dataType: DataType.Double, value }) } });
    await server.start(); return server;
  }
  let server = await fixture(0);
  const endpoint = server.getEndpointUrl(), port = Number(new URL(endpoint).port);
  const socket = join(directory, 'runtime.sock');
  const runtime = await start(socket, { pkiRoot: join(directory, 'pki') });
  const requests = [];
  t.after(async () => {
    requests.forEach(item => item.request.destroy());
    await runtime.closeClients(); runtime.closeAllConnections(); await new Promise(resolve => runtime.close(resolve));
    await server.shutdown(0); await rm(directory, { recursive: true, force: true });
  });
  const body = { sourceId: 27, configRevision: 1, definition: { protocol: 'opcua', endpoint,
    opcua: { securityMode: 'None', securityPolicy: 'None', nodeIds: ['ns=1;s=Value'], samplingIntervalMs: 100 } } };
  function monitor() {
    const state = { values: [], ended: false, ready: false };
    state.request = http.request({ socketPath: socket, path: '/watch', method: 'POST' }, response => {
      state.ready = response.headers['x-tier0-watch-ready'] === '1';
      let pending = '';
      response.on('data', chunk => {
        pending += chunk;
        const lines = pending.split('\n'); pending = lines.pop();
        for (const line of lines) if (line) state.values.push(JSON.parse(line));
      });
      response.on('error', () => {});
      response.on('close', () => { state.ended = true; });
    });
    state.request.on('error', () => { state.ended = true; });
    state.request.end(JSON.stringify(body)); requests.push(state); return state;
  }
  const old = monitor();
  await eventually(() => { assert(old.ready); assert(old.values.some(item => item.data.value === 12)); });
  await server.shutdown(0);
  await eventually(() => assert(old.ended, 'lost-session NDJSON must end, not wait forever for another data change'));
  value = 42.5; server = await fixture(port);
  const fresh = monitor();
  await eventually(() => { assert(fresh.ready); assert(fresh.values.some(item => item.data.value === 42.5)); });
  assert.equal(server.engine.currentSessionCount, 1, 'one replacement Source session, no old-session resurrection');
});
