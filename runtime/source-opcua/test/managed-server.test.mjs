import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { PassThrough } from 'node:stream';
import { once } from 'node:events';
import { setTimeout as delay } from 'node:timers/promises';
import {
  OPCUAClient, AttributeIds, DataType, Variant, UserTokenType,
  MessageSecurityMode, SecurityPolicy, ClientSubscription, ClientMonitoredItem, TimestampsToReturn, OPCUACertificateManager,
} from 'node-opcua';
import { ManagedOPCUAServer, validateManagedSource } from '../managed-server.mjs';

const body = (sourceId, ref = `credential-${sourceId}`) => ({ sourceId,
  definition: { name: `Source ${sourceId}`, protocol: 'opcua', role: 'server', endpoint: 'opc.tcp://managed', credentialRef: ref,
    opcua: { securityMode: 'None', securityPolicy: 'None', samplingIntervalMs: 100, nodeIds: ['temperature', 'command', 'update'], serverNodes: [
      { id: 'temperature', name: 'Temperature', nodeClass: 'Variable', dataType: 'Double', direction: 'ingress' },
      { id: 'command', name: 'Command', nodeClass: 'Variable', dataType: 'String', direction: 'egress' },
      { id: 'update', name: 'Update', nodeClass: 'Method', dataType: 'String', direction: 'ingress' },
    ] } }, credential: { kind: 'basic', username: 'device', secret: `source-${sourceId}-test-password` },
});
const username = (entry) => `source:${entry.sourceId}:${entry.definition.credentialRef}:${entry.credential.username}`;

async function setup(t, leaseMs = 60000) {
  const directory = await mkdtemp(join(tmpdir(), 'tier0-managed-opcua-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const runtime = new ManagedOPCUAServer({ pkiRoot: directory, port: 0, leaseMs });
  t.after(() => runtime.close());
  return runtime;
}

async function connect(t, endpoint, entry, password = entry?.credential.secret) {
  const client = OPCUAClient.create({ securityMode: MessageSecurityMode.None, securityPolicy: SecurityPolicy.None,
    endpointMustExist: true, connectionStrategy: { maxRetry: 0 }, requestedSessionTimeout: 10000 });
  t.after(() => client.disconnect());
  await client.connect(endpoint);
  const session = await client.createSession(entry ? { type: UserTokenType.UserName, userName: username(entry), password }
    : { type: UserTokenType.Anonymous });
  t.after(() => session.close());
  return session;
}

test('managed OPC UA: authenticated Source namespaces, real Read/Write/Method, downlink monitoring and no echo', { timeout: 60000 }, async (t) => {
  const runtime = await setup(t);
  const a = body(11); const b = body(12);
  const status = await runtime.sync([a, b]);
  assert.equal(status.sources, 2);
  for (const [manager, directory] of [
    [runtime.server.serverCertificateManager, 'managed-server'],
    [runtime.server.userCertificateManager, 'managed-users'],
  ]) {
    assert.equal(manager.rootDir, join(runtime.options.pkiRoot, directory));
    assert.equal(manager.automaticallyAcceptUnknownCertificate, false);
  }
  assert.ok(status.endpoint.startsWith('opc.tcp://'));
  await assert.rejects(connect(t, status.endpoint, a, 'wrong-password'), /BadUserAccessDenied/);
  await assert.rejects(connect(t, status.endpoint, undefined), /BadIdentityTokenInvalid|BadUserAccessDenied|Cannot find ANONYMOUS user token policy/);
  const sessionA = await connect(t, status.endpoint, a);
  const sessionB = await connect(t, status.endpoint, b);
  const resourcesA = runtime.resources(a); const resourcesB = runtime.resources(b);
  const temperature = resourcesA.find((entry) => entry.id === 'opcua:temperature').metadata.serverNodeId;
  const command = resourcesA.find((entry) => entry.id === 'opcua:command').metadata.serverNodeId;
  const otherTemperature = resourcesB.find((entry) => entry.id === 'opcua:temperature').metadata.serverNodeId;
  assert.deepEqual(resourcesA[2].models, ['publish']);
  const initial = await sessionA.readVariableValue(temperature);
  assert.equal(initial.statusCode.name, 'BadWaitingForInitialData');
  const wrongRead = await sessionB.readVariableValue(temperature);
  assert.equal(wrongRead.statusCode.name, 'BadUserAccessDenied');
  const wrongWrite = await sessionB.writeSingleNode(temperature, new Variant({ dataType: DataType.Double, value: 9 }));
  assert.equal(wrongWrite.name, 'BadUserAccessDenied');
  const root = runtime.root.nodeId;
  const browse = await sessionA.browse(root);
  assert.ok(browse.references.some((entry) => entry.browseName.name === 'Source 11'));
  assert.ok(!browse.references.some((entry) => entry.browseName.name === 'Source 12'));

  let headers;
  const response = new PassThrough(); response.writeHead = (_, value) => { headers = value; return response; }; response.flushHeaders = () => {};
  const events = []; response.on('data', (chunk) => events.push(...chunk.toString().trim().split('\n').map(JSON.parse)));
  const controller = new AbortController();
  const watchBody = structuredClone(a); watchBody.definition.opcua.nodeIds = ['temperature', 'update'];
  const watching = runtime.watch(watchBody, response, controller.signal);
  assert.equal(headers['X-Tier0-Watch-Ready'], '1');
  assert.ok([...runtime.sources.get(a.sourceId).watchers].some(watcher => watcher.ids.has('temperature')), 'isolated watcher installed before readiness');
  t.after(async () => { controller.abort(); await watching; });
  assert.equal((await sessionA.writeSingleNode(temperature, new Variant({ dataType: DataType.Double, value: 42.5 }))).name, 'Good');
  await delay(30);
  assert.equal(events[0].resourceRef, 'opcua:temperature');
  assert.equal(events[0].data.value, 42.5);
  assert.equal(runtime.invoke({ ...a, resourceRef: 'opcua:temperature', input: { operation: 'read' } }).value, 42.5);
  assert.equal((await sessionA.readVariableValue(temperature)).value.value, 42.5);
  assert.equal((await sessionA.writeSingleNode(command, new Variant({ dataType: DataType.String, value: 'forbidden' }))).name, 'BadUserAccessDenied');
  assert.throws(() => runtime.invoke({ ...a, resourceRef: 'opcua:temperature', input: { operation: 'write', value: 1 } }), /resourceScope/);
  assert.throws(() => runtime.invoke({ ...a, resourceRef: 'opcua:outside', input: { operation: 'read' } }), /resourceScope/);
  assert.throws(() => runtime.invoke({ ...a, resourceRef: 'opcua:command', input: { operation: 'write', value: 1 } }), /resourceScope/);

  const subscription = ClientSubscription.create(sessionA, { requestedPublishingInterval: 100, requestedLifetimeCount: 100,
    requestedMaxKeepAliveCount: 10, maxNotificationsPerPublish: 100, publishingEnabled: true, priority: 1 });
  t.after(() => subscription.terminate());
  const item = ClientMonitoredItem.create(subscription, { nodeId: command, attributeId: AttributeIds.Value },
    { samplingInterval: 100, discardOldest: true, queueSize: 10 }, TimestampsToReturn.Both);
  await once(item, 'initialized');
  const changes = []; item.on('changed', (value) => changes.push(value));
  const receipt = runtime.invoke({ ...a, resourceRef: 'opcua:command', input: { operation: 'write', value: 'reset' } });
  assert.equal(receipt.receipt, 'server-value-updated'); assert.equal(receipt.deviceExecuted, false);
  for (let attempt = 0; attempt < 30 && !changes.some((value) => value.value.value === 'reset'); attempt++) await delay(100);
  assert.ok(changes.some((value) => value.value.value === 'reset'), 'real remote monitored item receives platform downlink');
  assert.equal((await sessionA.readVariableValue(command)).value.value, 'reset');
  assert.ok(!events.some((event) => event.resourceRef === 'opcua:command'), 'downlink must not echo into ingress');

  const method = resourcesA[2].metadata.serverNodeId;
  const called = await sessionA.call({ objectId: runtime.sources.get(a.sourceId).folder.nodeId, methodId: method,
    inputArguments: [new Variant({ dataType: DataType.String, value: '{"event":"alarm"}' })] });
  assert.equal(called.statusCode.name, 'Good');
  assert.equal(JSON.parse(called.outputArguments[0].value).deviceExecuted, false);
  await delay(30);
  assert.ok(events.some((event) => event.resourceRef === 'opcua:update' && event.data.event === 'alarm'));
  const otherMethod = await sessionB.call({ objectId: runtime.sources.get(a.sourceId).folder.nodeId, methodId: method,
    inputArguments: [new Variant({ dataType: DataType.String, value: '{}' })] });
  assert.equal(otherMethod.statusCode.name, 'BadUserAccessDenied');
  assert.equal((await sessionA.readVariableValue(otherTemperature)).statusCode.name, 'BadUserAccessDenied');
});

test('managed Watch rejects cancelled initialization without ready headers and releases Source ownership', { timeout: 60000 }, async (t) => {
  const runtime = await setup(t);
  const entry = body(51);
  entry.definition.opcua.nodeIds = ['temperature'];
  await runtime.sync([entry]);
  const response = new PassThrough();
  let headers;
  response.writeHead = (_, value) => { headers = value; return response; }; response.flushHeaders = () => {};
  const controller = new AbortController(); controller.abort();
  await assert.rejects(runtime.watch(entry, response, controller.signal), /protocolFailed/);
  assert.equal(headers, undefined);
  assert.equal(runtime.sources.get(entry.sourceId).watchers.size, 0);
  const next = new AbortController();
  const watching = runtime.watch(entry, response, next.signal);
  assert.equal(headers['X-Tier0-Watch-Ready'], '1');
  next.abort(); await watching;
  assert.equal(runtime.sources.get(entry.sourceId).watchers.size, 0);
  assert.equal(response.readableLength, 0, 'ready header must not fabricate initial data');
});

test('managed OPC UA: independent Stream watcher scopes and cancellation share one Source safely', { timeout: 60000 }, async (t) => {
  const runtime = await setup(t);
  const entry = body(61);
  const status = await runtime.sync([entry]);
  const session = await connect(t,status.endpoint,entry);
  const temperature = runtime.resources(entry).find(resource => resource.id === 'opcua:temperature').metadata.serverNodeId;
  const createWatch = (ids) => {
    const response = new PassThrough();
    let ready = false;
    response.writeHead = (_,headers) => {ready = headers['X-Tier0-Watch-Ready'] === '1';return response};
    response.flushHeaders = () => {};
    const events=[];response.on('data',chunk=>events.push(...chunk.toString().trim().split('\n').map(JSON.parse)));
    const controller=new AbortController();
    const scoped=structuredClone(entry);scoped.definition.opcua.nodeIds=ids;
    const done=runtime.watch(scoped,response,controller.signal);
    assert.equal(ready,true);
    return {events,controller,done};
  };
  const first=createWatch(['temperature']);
  const second=createWatch(['temperature','update']);
  t.after(async()=>{first.controller.abort();second.controller.abort();await Promise.all([first.done,second.done])});
  assert.equal(runtime.sources.get(entry.sourceId).watchers.size,2);
  assert.equal((await session.writeSingleNode(temperature,new Variant({dataType:DataType.Double,value:81}))).name,'Good');
  await delay(30);
  assert.equal(first.events[0].data.value,81);
  assert.equal(second.events[0].data.value,81);
  first.controller.abort();await first.done;
  assert.equal(runtime.sources.get(entry.sourceId).watchers.size,1);
  assert.equal((await session.writeSingleNode(temperature,new Variant({dataType:DataType.Double,value:82}))).name,'Good');
  await delay(30);
  assert.equal(first.events.length,1);
  assert.equal(second.events.at(-1).data.value,82);
  second.controller.abort();await second.done;
  assert.equal(runtime.sources.get(entry.sourceId).watchers.size,0);
  const third=createWatch(['temperature']);
  const fourth=createWatch(['temperature','update']);
  await runtime.sync([]);
  await Promise.all([third.done,fourth.done]);
  assert.equal(runtime.sources.has(entry.sourceId),false,'Source removal terminates every scoped watcher');
  assert.equal((await session.readVariableValue(temperature)).statusCode.name,'BadNodeIdUnknown');
});

test('managed OPC UA: disable, credential rotation, lease expiry and validated snapshots fail closed', { timeout: 60000 }, async (t) => {
  const runtime = await setup(t, 4000);
  const a = body(21); const b = body(22);
  const status = await runtime.sync([a, b]);
  const session = await connect(t, status.endpoint, a);
  const oldNode = runtime.resources(a)[0].metadata.serverNodeId;
  const rotated = body(21, 'rotated-credential');
  await runtime.sync([rotated, b]);
  const forbidden = await session.readVariableValue(runtime.resources(rotated)[0].metadata.serverNodeId);
  assert.equal(forbidden.statusCode.name, 'BadUserAccessDenied');
  assert.equal((await session.readVariableValue(oldNode)).statusCode.name, 'BadNodeIdUnknown');
  await assert.rejects(connect(t, status.endpoint, a), /BadUserAccessDenied/);
  assert.throws(() => runtime.invoke({ ...a, resourceRef: 'opcua:temperature', input: { operation: 'read' } }), /credentialInvalid/);
  const newSession = await connect(t, status.endpoint, rotated);
  assert.equal((await newSession.readVariableValue(runtime.resources(rotated)[0].metadata.serverNodeId)).statusCode.name, 'BadWaitingForInitialData');
  await assert.rejects(runtime.sync([rotated, rotated]), /protocolFailed/);
  assert.equal(runtime.sources.size, 2, 'invalid full snapshot must not remove live Sources');
  await runtime.sync([b]);
  assert.equal(runtime.sources.size, 1);
  assert.equal((await newSession.readVariableValue(oldNode)).statusCode.name, 'BadNodeIdUnknown');
  await delay(5100);
  assert.equal(runtime.sources.size, 0, 'lost Backend lease must revoke devices even without another sync');
  await assert.rejects(connect(t, status.endpoint, b), /BadUserAccessDenied/);
});

test('managed OPC UA declaration rejects unsupported roles, nodes, methods, bounds and security combinations', () => {
  const valid = body(31);
  assert.equal(validateManagedSource(valid), valid);
  for (const change of [
    (entry) => { entry.definition.role = 'client'; },
    (entry) => { entry.definition.endpoint = 'opc.tcp://0.0.0.0:9999'; },
    (entry) => { entry.credential.secret = ''; },
    (entry) => { entry.definition.opcua.serverNodes[0].id = '../other'; },
    (entry) => { entry.definition.opcua.serverNodes.push(entry.definition.opcua.serverNodes[0]); },
    (entry) => { entry.definition.opcua.serverNodes[2].direction = 'egress'; },
    (entry) => { entry.definition.opcua.serverNodes[0].dataType = 'ExtensionObject'; },
    (entry) => { entry.definition.opcua.nodeIds.push('not-declared'); },
    (entry) => { entry.definition.opcua.securityMode = 'Sign'; },
    (entry) => { entry.definition.opcua.securityPolicy = 'Basic256Sha256'; },
  ]) { const entry = structuredClone(valid); change(entry); assert.throws(() => validateManagedSource(entry)); }
});

test('managed OPC UA secure Source requires encrypted channel and explicitly trusted client certificate', { timeout: 60000 }, async (t) => {
  const runtime = await setup(t);
  const entry = body(41);
  entry.definition.opcua.securityMode = 'SignAndEncrypt';
  entry.definition.opcua.securityPolicy = 'Basic256Sha256';
  const { endpoint } = await runtime.sync([entry]);
  await assert.rejects(connect(t, endpoint, entry), /BadUserAccessDenied/);
  const directory = await mkdtemp(join(tmpdir(), 'tier0-secure-device-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const certificateManager = new OPCUACertificateManager({ rootFolder: directory, automaticallyAcceptUnknownCertificate: false });
  const client = OPCUAClient.create({ clientCertificateManager: certificateManager, securityMode: MessageSecurityMode.SignAndEncrypt,
    securityPolicy: SecurityPolicy.Basic256Sha256, endpointMustExist: true, connectionStrategy: { maxRetry: 0 } });
  t.after(() => client.disconnect());
  await certificateManager.initialize();
  await client.createDefaultCertificate();
  await certificateManager.trustCertificate(runtime.server.getCertificate());
  assert.notEqual((await runtime.server.serverCertificateManager.checkCertificate(client.getCertificate())).name, 'Good');
  await assert.rejects(client.connect(endpoint), /BadCertificateUntrusted|BadSecurityChecksFailed/);
  await client.disconnect();
  await runtime.server.serverCertificateManager.trustCertificate(client.getCertificate());
  await client.connect(endpoint);
  const session = await client.createSession({ type: UserTokenType.UserName, userName: username(entry), password: entry.credential.secret });
  t.after(() => session.close());
  const nodeId = runtime.resources(entry)[0].metadata.serverNodeId;
  assert.equal((await session.writeSingleNode(nodeId, new Variant({ dataType: DataType.Double, value: 73 }))).name, 'Good');
  assert.equal((await session.readVariableValue(nodeId)).value.value, 73);
});
