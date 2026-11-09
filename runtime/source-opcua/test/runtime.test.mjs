import { test } from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { X509Certificate } from 'node:crypto';
import { mkdtemp, rm, readFile, readdir } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { OPCUAServer, OPCUACertificateManager, Variant, DataValue, DataType, StatusCodes, MessageSecurityMode, SecurityPolicy, resolveNodeId, standardUnits } from 'node-opcua';
import { start } from '../server.mjs';

test('secure session uses the supplied client identity and removes temporary key files on stop', {timeout:30000}, async t=>{
  const directory=await mkdtemp(join(tmpdir(),'tier0-opcua-identity-'));
  const issuer=new OPCUACertificateManager({rootFolder:join(directory,'identity')});
  await issuer.initialize();
  const certificateFile=join(directory,'client.pem');
  await issuer.createSelfSignedCertificate({applicationUri:'urn:tier0:acceptance:client',subject:'CN=Tier0Acceptance',dns:['localhost'],outputFile:certificateFile,validity:30});
  const clientCertificatePem=await readFile(certificateFile,'utf8');
  const clientPrivateKeyPem=await readFile(issuer.privateKey,'utf8');
  const ua=new OPCUAServer({port:0,securityModes:[MessageSecurityMode.SignAndEncrypt],securityPolicies:[SecurityPolicy.Basic256Sha256]});
  await ua.initialize();
  await ua.serverCertificateManager.trustCertificate(new X509Certificate(clientCertificatePem).raw);
  await ua.start();
  const pkiRoot=join(directory,'runtime-pki');
  const socket=join(directory,'runtime.sock');
  const runtime=await start(socket,{pkiRoot});
  t.after(async()=>{ await runtime.closeClients();runtime.closeAllConnections();await new Promise(resolve=>runtime.close(resolve));await ua.shutdown(0);await issuer.dispose();await rm(directory,{recursive:true,force:true}); });
  const body={sourceId:901,configRevision:1,definition:{protocol:'opcua',endpoint:ua.getEndpointUrl(),opcua:{securityMode:'SignAndEncrypt',securityPolicy:'Basic256Sha256',serverCertificate:await readFile(ua.certificateFile,'utf8')}},credential:{kind:'custom',values:{clientCertificatePem,clientPrivateKeyPem}}};
  const response=await request(socket,'/probe',body);
  assert.equal(response.error,undefined,JSON.stringify(response));
  assert.equal(response.data.health,'healthy');
  assert.ok((await readdir(pkiRoot)).some(name=>name.startsWith('identity-')));
  await runtime.closeClients();
  assert.ok(!(await readdir(pkiRoot)).some(name=>name.startsWith('identity-')));
});

function request(socketPath, path, body) {
  return new Promise((resolve, reject) => {
    const req = http.request({ socketPath, path, method: 'POST', headers: { 'Content-Type': 'application/json' } }, (res) => {
      let data = '';
      res.on('data', (chunk) => { data += chunk; });
      res.on('end', () => { try { resolve(JSON.parse(data)); } catch (error) { reject(error); } });
    });
    req.on('error', reject);
    req.setTimeout(15000, () => req.destroy(new Error('timeout')));
    req.end(JSON.stringify(body));
  });
}

test('real OPC UA session: probe, browsing, Read/Write/Method and monitored values', { timeout: 60000 }, async (t) => {
  const directory = await mkdtemp(join(tmpdir(), 'tier0-opcua-test-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const ua = new OPCUAServer({ port: 0, securityModes: [MessageSecurityMode.None], securityPolicies: [SecurityPolicy.None] });
  await ua.initialize();
  const namespace = ua.engine.addressSpace.getOwnNamespace();
  const device = namespace.addObject({ organizedBy: ua.engine.addressSpace.rootFolder.objects, browseName: 'ProtocolTestDevice', nodeId: 'ns=1;s=Device' });
  let temperature = 21.5;
  namespace.addVariable({ componentOf: device, browseName: 'Temperature', nodeId: 'ns=1;s=Temperature', dataType: 'Double', minimumSamplingInterval: 100, value: {
    get: () => new Variant({ dataType: DataType.Double, value: temperature }),
    set: (variant) => { temperature = variant.value; return StatusCodes.Good; },
  } });
  const method = namespace.addMethod(device, { browseName: 'Reset', nodeId: 'ns=1;s=Reset', inputArguments: [], outputArguments: [{ name: 'value', dataType: DataType.Double }] });
  method.bindMethod((_, __, callback) => { temperature = 0; callback(null, { statusCode: StatusCodes.Good, outputArguments: [new Variant({ dataType: DataType.Double, value: temperature })] }); });
  await ua.start();
  t.after(() => ua.shutdown(0));
  const endpoint = ua.getEndpointUrl();
  const socket = join(directory, 'runtime.sock');
  const runtime = await start(socket, { pkiRoot: join(directory, 'client-pki') });
  t.after(() => new Promise((resolve) => { runtime.closeAllConnections(); runtime.close(resolve); }));
  const body = { sourceId: 1, definition: { protocol: 'opcua', endpoint, opcua: { securityMode: 'None', securityPolicy: 'None', samplingIntervalMs: 100, nodeIds: ['ns=1;s=Temperature', 'ns=1;s=Reset'] } } };
  const connection = { ...body, definition: { ...body.definition, opcua: { securityMode: 'None', securityPolicy: 'None' } } };
  assert.equal((await request(socket, '/probe', connection)).data.health, 'healthy');
  assert.equal((await request(socket, '/invoke', { ...connection, resourceRef: 'opcua:ns=1;s=Temperature' })).error, 'SourceStream.protocolFailed');
  assert.equal((await request(socket, '/probe', { ...body, sourceId: 0 })).data.health, 'healthy');
  const resources = (await request(socket, '/discover', { ...connection, parent: 'opcua:ns=1;s=Device' })).data;
  assert.equal(resources.length, 2);
  assert.ok(resources.find((item) => item.id === 'opcua:ns=1;s=Temperature').models.includes('publish'));
  assert.equal(resources.find((item) => item.id === 'opcua:ns=1;s=Temperature').metadata.namespaceUri, namespace.namespaceUri);
  assert.ok(resources.find((item) => item.id === 'opcua:ns=1;s=Reset').models.includes('request-reply'));
  const browse = await request(socket, '/discover', { ...body, parent: 'opcua:ns=1;s=Device' });
  assert.ok(browse.data.some((item) => item.id === 'opcua:ns=1;s=Temperature'));
  const read = await request(socket, '/invoke', { ...body, resourceRef: 'opcua:ns=1;s=Temperature', input: { operation: 'read' } });
  assert.equal(read.data.value, 21.5);
  assert.equal(read.data.good, true);
  const write = await request(socket, '/invoke', { ...body, resourceRef: 'opcua:ns=1;s=Temperature', input: { operation: 'write', value: 32.5 } });
  assert.equal(write.data.receipt, 'operation-accepted');
  assert.equal(write.data.deviceExecuted, false);
  assert.equal(temperature, 32.5);
  const forbidden = await request(socket, '/invoke', { ...body, resourceRef: 'opcua:i=2258', input: { operation: 'write', value: 1 } });
  assert.equal(forbidden.error, 'SourceStream.resourceScope');
  const called = await request(socket, '/invoke', { ...body, resourceRef: 'opcua:ns=1;s=Reset', input: { operation: 'call' } });
  assert.deepEqual(called.data.outputArguments, [0]);
  assert.equal(temperature, 0);
  await new Promise((resolve, reject) => {
    const req = http.request({ socketPath: socket, path: '/watch', method: 'POST', headers: { 'Content-Type': 'application/json' } }, (res) => {
      assert.equal(res.headers['x-tier0-watch-ready'], '1');
      let pending = '';
      res.on('data', (chunk) => {
        pending += chunk;
        if (!pending.includes('\n')) return;
        try {
          const event = JSON.parse(pending.split('\n')[0]);
          assert.equal(event.resourceRef, 'opcua:ns=1;s=Temperature');
          assert.equal(event.data.value, 0);
          assert.equal(event.data.statusCode, 'Good');
          req.destroy(); resolve();
        } catch (error) { req.destroy(); reject(error); }
      });
    });
    req.on('error', (error) => { if (error.code !== 'ECONNRESET') reject(error); });
    req.setTimeout(10000, () => req.destroy(new Error('watch timeout')));
    req.end(JSON.stringify(body));
  });
});

test('dynamic Namespace URI and bad process quality survive a successful OPC UA Read', { timeout: 30000 }, async (t) => {
  const directory = await mkdtemp(join(tmpdir(), 'tier0-opcua-quality-'));
  const ua = new OPCUAServer({ port: 0, securityModes: [MessageSecurityMode.None], securityPolicies: [SecurityPolicy.None] });
  await ua.initialize();
  // Allocate another namespace first: discovery must read the namespace array,
  // not assume that a vendor namespace is always ns=1 or ns=2.
  ua.engine.addressSpace.registerNamespace('urn:test:other-vendor');
  const namespace = ua.engine.addressSpace.registerNamespace('urn:test:source-quality');
  const nodeId = `ns=${namespace.index};s=QualityPoint`;
  const timestamp = new Date('2026-10-01T01:02:03.000Z');
  const point = namespace.addVariable({ organizedBy: ua.engine.addressSpace.rootFolder.objects,
    browseName: 'QualityPoint', nodeId, dataType: 'Double', minimumSamplingInterval: 100,
    value: { timestamped_get: () => new DataValue({ value: new Variant({ dataType: DataType.Double, value: 42.5 }),
      statusCode: StatusCodes.BadSensorFailure, sourceTimestamp: timestamp }) },
  });
  namespace.addVariable({ propertyOf: point, browseName: 'EngineeringUnits', dataType: 'EUInformation', minimumSamplingInterval: 100,
    value: { get: () => new Variant({ dataType: DataType.ExtensionObject, value: standardUnits.degree_celsius }) } });
  await ua.start();
  const socket = join(directory, 'runtime.sock');
  const runtime = await start(socket, { pkiRoot: join(directory, 'client-pki') });
  t.after(async () => {
    await runtime.closeClients(); runtime.closeAllConnections();
    await new Promise((resolve) => runtime.close(resolve));
    await ua.shutdown(0); await rm(directory, { recursive: true, force: true });
  });
  const body = { sourceId: 92, definition: { protocol: 'opcua', endpoint: ua.getEndpointUrl(), opcua: {
    securityMode: 'None', securityPolicy: 'None', nodeIds: [nodeId], samplingIntervalMs: 100,
  } } };
  const resource = (await request(socket, '/discover', body)).data[0];
  assert.equal(resource.metadata.namespaceUri, 'urn:test:source-quality');
  assert.equal(resource.id, 'opcua:' + nodeId);
  assert.equal(resource.metadata.engineeringUnit.symbol, '°C');
  assert.equal(resource.metadata.engineeringUnit.unitId, 4408652);
  assert.equal(resource.metadata.dataType, 'ns=0;i=11');
  const read = await request(socket, '/invoke', { ...body, resourceRef: resource.id, input: { operation: 'read' } });
  assert.equal(read.error, undefined, 'process quality is not a fabricated transport error');
  assert.equal(read.data.value, 42.5);
  assert.equal(read.data.good, false);
  assert.equal(read.data.statusCode, 'BadSensorFailure');
  assert.equal(read.data.sourceTimestamp, timestamp.toISOString());
  assert.equal(read.data.namespaceUri, 'urn:test:source-quality');
  assert.equal(read.data.engineeringUnit.symbol, '°C');
  await new Promise((resolve, reject) => {
    const req = http.request({ socketPath: socket, path: '/watch', method: 'POST',
      headers: { 'Content-Type': 'application/json' } }, (res) => {
      assert.equal(res.headers['x-tier0-watch-ready'], '1');
      let pending = '';
      res.on('data', (chunk) => {
        pending += chunk;
        if (!pending.includes('\n')) return;
        try {
          const event = JSON.parse(pending.split('\n')[0]);
          assert.equal(event.data.good, false);
          assert.equal(event.data.statusCode, 'BadSensorFailure');
          assert.equal(event.data.sourceTimestamp, timestamp.toISOString());
          assert.equal(event.data.engineeringUnit.symbol, '°C');
          assert.equal(event.data.namespaceUri, 'urn:test:source-quality');
          req.destroy(); resolve();
        } catch (error) { req.destroy(); reject(error); }
      });
    });
    req.on('error', (error) => { if (error.code !== 'ECONNRESET') reject(error); });
    req.setTimeout(10000, () => req.destroy(new Error('watch timeout')));
    req.end(JSON.stringify(body));
  });
});

test('vendor Unit preserves explicit labels without guessing IDs or accepting invalid properties', { timeout: 30000 }, async (t) => {
  const directory = await mkdtemp(join(tmpdir(), 'tier0-opcua-unit-'));
  const ua = new OPCUAServer({ port: 0, securityModes: [MessageSecurityMode.None], securityPolicies: [SecurityPolicy.None] });
  await ua.initialize();
  const namespace = ua.engine.addressSpace.registerNamespace('urn:test:vendor-unit');
  const cases = [
    ['string', DataType.String, ' rpm ', 'rpm', StatusCodes.Good],
    ['localized', DataType.LocalizedText, { text: '%' }, '%', StatusCodes.Good],
    ['wrong-type', DataType.Double, 12, null, StatusCodes.Good],
    ['oversized', DataType.String, 'x'.repeat(257), null, StatusCodes.Good],
    ['bad-unit-quality', DataType.String, 'rpm', null, StatusCodes.BadSensorFailure],
    ['empty', DataType.String, '', null, StatusCodes.Good],
    ['control', DataType.String, 'rpm\n', null, StatusCodes.Good],
    ['standard-first', DataType.String, 'wrong-vendor-unit', '°C', StatusCodes.Good],
  ];
  for (const [name, type, label, , status] of cases) {
    const point = namespace.addVariable({ organizedBy: ua.engine.addressSpace.rootFolder.objects,
      browseName: name, nodeId: `ns=${namespace.index};s=${name}`, dataType: 'Double', minimumSamplingInterval: 100,
      value: { timestamped_get: () => new DataValue({ value: new Variant({ dataType: DataType.Double, value: 7 }),
        statusCode: StatusCodes.BadSensorFailure, sourceTimestamp: new Date('2026-10-01T00:00:00Z') }) } });
    namespace.addVariable({ propertyOf: point, browseName: 'Unit', dataType: DataType[type], minimumSamplingInterval: 100,
      value: { timestamped_get: () => new DataValue({ value: new Variant({ dataType: type, value: label }), statusCode: status }) } });
    if (name === 'standard-first') namespace.addVariable({ propertyOf: point, browseName: 'EngineeringUnits',
      dataType: 'EUInformation', minimumSamplingInterval: 100,
      value: { get: () => new Variant({ dataType: DataType.ExtensionObject, value: standardUnits.degree_celsius }) } });
  }
  await ua.start();
  const socket = join(directory, 'runtime.sock');
  const runtime = await start(socket, { pkiRoot: join(directory, 'pki') });
  t.after(async () => { await runtime.closeClients(); runtime.closeAllConnections();
    await new Promise(resolve => runtime.close(resolve)); await ua.shutdown(0); await rm(directory, { recursive: true, force: true }); });
  const nodeIds = cases.map(([name]) => `ns=${namespace.index};s=${name}`);
  const body = { sourceId: 94, definition: { protocol: 'opcua', endpoint: ua.getEndpointUrl(),
    opcua: { securityMode: 'None', securityPolicy: 'None', nodeIds, samplingIntervalMs: 100 } } };
  for (const [name, , , expected] of cases) {
    const read = await request(socket, '/invoke', { ...body, resourceRef: `opcua:ns=${namespace.index};s=${name}`, input: { operation: 'read' } });
    assert.equal(read.error, undefined); assert.equal(read.data.statusCode, 'BadSensorFailure'); assert.equal(read.data.good, false);
    if (expected === null) assert.equal(read.data.engineeringUnit, null, name);
    else {
      assert.equal(read.data.engineeringUnit.symbol, expected, name);
      if (name !== 'standard-first') { assert.equal(read.data.engineeringUnit.unitId, null);
        assert.equal(read.data.engineeringUnit.sourceProperty, 'Unit'); }
      else assert.equal(read.data.engineeringUnit.unitId, 4408652);
    }
  }
});

test('Watch advertises readiness only after all real MonitoredItems initialize; failure and cancellation clean up', { timeout: 60000 }, async (t) => {
  const directory = await mkdtemp(join(tmpdir(), 'tier0-opcua-watch-ready-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const ua = new OPCUAServer({ port: 0, securityModes: [MessageSecurityMode.None], securityPolicies: [SecurityPolicy.None] });
  await ua.initialize();
  const namespace = ua.engine.addressSpace.getOwnNamespace();
  for (const [id, value] of [['First', 11], ['Second', 22]]) {
    namespace.addVariable({ organizedBy: ua.engine.addressSpace.rootFolder.objects, browseName: id,
      nodeId: `ns=1;s=${id}`, dataType: 'Double', minimumSamplingInterval: 100,
      value: { get: () => new Variant({ dataType: DataType.Double, value }) } });
  }
  // This real protocol server can reject or stall CreateMonitoredItems after
  // successful discovery, exercising the runtime's initialization boundary.
  let mode = 'good';
  let monitorRequests = 0;
  const createItems = ua._on_CreateMonitoredItemsRequest;
  ua._on_CreateMonitoredItemsRequest = function (message, channel) {
    monitorRequests++;
    if (mode === 'stall') return;
    if (mode === 'bad') {
      message.request.itemsToCreate[0].itemToMonitor.nodeId = resolveNodeId('ns=1;s=DoesNotExist');
    }
    return createItems.call(this, message, channel);
  };
  await ua.start();
  t.after(() => ua.shutdown(0));
  const socket = join(directory, 'runtime.sock');
  const runtime = await start(socket, { pkiRoot: join(directory, 'client-pki') });
  t.after(() => new Promise((resolve) => { runtime.closeAllConnections(); runtime.close(resolve); }));
  const body = { sourceId: 91, definition: { protocol: 'opcua', endpoint: ua.getEndpointUrl(), opcua: {
    securityMode: 'None', securityPolicy: 'None', samplingIntervalMs: 100,
    nodeIds: ['ns=1;s=First', 'ns=1;s=Second'],
  } } };
  const idle = async () => {
    for (let i = 0; i < 100 && (ua.engine.currentSessionCount || ua.engine.currentSubscriptionCount); i++) await delay(25);
    assert.equal(ua.engine.currentSessionCount, 0, 'cancelled initialization must release the real session');
    assert.equal(ua.engine.currentSubscriptionCount, 0, 'cancelled initialization must release the real subscription');
  };

  await new Promise((resolve, reject) => {
    const req = http.request({ socketPath: socket, path: '/watch', method: 'POST' }, (res) => {
      try {
        assert.equal(res.statusCode, 200);
        assert.equal(res.headers['x-tier0-watch-ready'], '1');
        assert.equal(ua.engine.currentSubscriptionCount, 1);
        assert.equal(ua.engine._globalCounter.totalMonitoredItemCount, 2, 'both nodes initialized before response headers');
      } catch (error) { req.destroy(); reject(error); return; }
      let pending = '';
      const events = [];
      res.on('data', (chunk) => {
        pending += chunk;
        while (pending.includes('\n')) {
          const newline = pending.indexOf('\n');
          events.push(JSON.parse(pending.slice(0, newline))); pending = pending.slice(newline + 1);
        }
        if (events.length < 2) return;
        try {
          assert.deepEqual(events.map((event) => [event.resourceRef, event.data.value]).sort(),
            [['opcua:ns=1;s=First', 11], ['opcua:ns=1;s=Second', 22]]);
          assert.ok(events.every((event) => event.data.statusCode === 'Good'));
          req.destroy(); resolve();
        } catch (error) { req.destroy(); reject(error); }
      });
    });
    req.on('error', reject);
    req.setTimeout(10000, () => req.destroy(new Error('ready watch timeout')));
    req.end(JSON.stringify(body));
  });
  await idle();

  mode = 'bad';
  await new Promise((resolve, reject) => {
    const req = http.request({ socketPath: socket, path: '/watch', method: 'POST' }, (res) => {
      let data = '';
      res.on('data', (chunk) => { data += chunk; });
      res.on('end', () => {
        try {
          assert.equal(res.headers['x-tier0-watch-ready'], undefined);
          assert.equal(res.headers['content-type'], 'application/json');
          assert.equal(JSON.parse(data).error, 'SourceStream.protocolFailed'); resolve();
        } catch (error) { reject(error); }
      });
    });
    req.on('error', reject); req.setTimeout(15000, () => req.destroy(new Error('failed initialization timeout')));
    req.end(JSON.stringify(body));
  });
  await idle();

  mode = 'stall';
  const previousRequests = monitorRequests;
  let headersReceived = false;
  const req = http.request({ socketPath: socket, path: '/watch', method: 'POST' }, (res) => { headersReceived = true; res.resume(); });
  req.on('error', () => {});
  req.end(JSON.stringify(body));
  for (let i = 0; i < 100 && monitorRequests === previousRequests; i++) await delay(25);
  assert.ok(monitorRequests > previousRequests, 'real server received an initialization request before cancellation');
  assert.equal(headersReceived, false, 'no success headers while protocol initialization is pending');
  req.destroy();
  await idle();

  const timeoutStarted = Date.now();
  await new Promise((resolve, reject) => {
    const stalled = http.request({ socketPath: socket, path: '/watch', method: 'POST' }, (res) => {
      let data = '';
      res.on('data', (chunk) => { data += chunk; });
      res.on('end', () => {
        try {
          assert.equal(res.headers['x-tier0-watch-ready'], undefined);
          assert.equal(JSON.parse(data).error, 'SourceStream.protocolFailed');
          assert.ok(Date.now() - timeoutStarted < 14000, 'initialization is bounded even when the server never replies');
          resolve();
        } catch (error) { reject(error); }
      });
    });
    stalled.on('error', reject);
    stalled.setTimeout(15000, () => stalled.destroy(new Error('unbounded stalled initialization')));
    stalled.end(JSON.stringify(body));
  });
  await idle();

  // Reuse the same Source ID: cancelled HTTP ownership must also be released.
  mode = 'bad';
  assert.equal((await request(socket, '/watch', body)).error, 'SourceStream.protocolFailed');
  await idle();
});
