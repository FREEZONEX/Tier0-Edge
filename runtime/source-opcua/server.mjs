import http from 'node:http';
import { X509Certificate, createHash } from 'node:crypto';
import { chmod, mkdir, lstat, unlink, mkdtemp, writeFile, rm } from 'node:fs/promises';
import { createConnection } from 'node:net';
import { dirname, isAbsolute, join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { ManagedOPCUAServer, validateManagedSource } from './managed-server.mjs';
import { SourceSessionPool } from './session-pool.mjs';
import { exploreCertificate } from 'node-opcua-crypto';
import {
  OPCUAClient, AttributeIds, NodeClass, BrowseDirection, MessageSecurityMode,
  SecurityPolicy, UserTokenType, DataType, Variant, ClientSubscription,
  ClientMonitoredItem, TimestampsToReturn, resolveNodeId,
  OPCUACertificateManager,
} from 'node-opcua';

const MAX_BODY = 1024 * 1024;
const MAX_RESOURCES = 1000;
const WATCH_INITIALIZATION_MS = 10000;
const failure = (code = 'SourceStream.protocolFailed') => Object.assign(new Error(code), { code });

async function readBody(request) {
  let size = 0;
  const chunks = [];
  for await (const chunk of request) {
    size += chunk.length;
    if (size > MAX_BODY) throw failure();
    chunks.push(chunk);
  }
  try { return JSON.parse(Buffer.concat(chunks).toString('utf8')); }
  catch { throw failure(); }
}

function validate(body, collection) {
  const definition = body?.definition;
  if (!Number.isSafeInteger(body?.sourceId) || body.sourceId < 0 || definition?.protocol !== 'opcua' || !definition.opcua
    || (definition.role && definition.role !== 'client')) throw failure();
  const endpoint = new URL(definition.endpoint);
  if (endpoint.protocol !== 'opc.tcp:' || !endpoint.hostname || endpoint.username || endpoint.password || endpoint.search || endpoint.hash) throw failure();
  const config = definition.opcua;
  if (collection && (!Array.isArray(config.nodeIds) || config.nodeIds.length > MAX_RESOURCES || !config.nodeIds.every((id) => typeof id === 'string' && id.length < 1024))) throw failure();
  if (!['None', 'Sign', 'SignAndEncrypt'].includes(config.securityMode) || !SecurityPolicy[config.securityPolicy]) throw failure();
  if ((config.securityMode === 'None') !== (config.securityPolicy === 'None')) throw failure();
  if (collection && (!Number.isInteger(config.samplingIntervalMs) || config.samplingIntervalMs < 100 || config.samplingIntervalMs > 3600000)) throw failure();
  if (body.credential?.kind && body.credential.kind !== 'basic' && body.credential.kind !== 'custom') throw failure('SourceStream.credentialInvalid');
  return definition;
}

async function connectSession(body, signal, pkiRoot, lease) {
  const definition = validate(body);
  const pem = definition.opcua.serverCertificate || '';
  let serverCertificate;
  if (pem) {
    if (typeof pem !== 'string' || pem.length > 65536 || !/^\s*-----BEGIN CERTIFICATE-----[\s\S]+-----END CERTIFICATE-----\s*$/.test(pem) || (pem.match(/-----BEGIN/g) || []).length !== 1) throw failure();
    serverCertificate = new X509Certificate(pem).raw;
  }
  const trustId = serverCertificate ? createHash('sha256').update(serverCertificate).digest('hex') : 'system';
  const manager = new OPCUACertificateManager({ rootFolder: join(pkiRoot, `source-${body.sourceId}-${trustId}`), automaticallyAcceptUnknownCertificate: false });
  let identityDirectory;
  let client;
  const identity = {};
  const certificatePem = body.credential?.values?.clientCertificatePem;
  const privateKeyPem = body.credential?.values?.clientPrivateKeyPem;
  if ((certificatePem || privateKeyPem) && (!certificatePem || !privateKeyPem || definition.opcua.securityMode === 'None')) throw failure('SourceStream.credentialInvalid');
  try {
    await manager.initialize();
    if (serverCertificate) await manager.trustCertificate(serverCertificate);
    if (certificatePem || privateKeyPem) {
      identityDirectory = await mkdtemp(join(pkiRoot, 'identity-'));
      await chmod(identityDirectory, 0o700);
      identity.certificateFile = join(identityDirectory, 'certificate.pem');
      identity.privateKeyFile = join(identityDirectory, 'private-key.pem');
      const uris = exploreCertificate(new X509Certificate(certificatePem).raw).tbsCertificate.extensions?.subjectAltName?.uniformResourceIdentifier;
      if (!Array.isArray(uris) || uris.length !== 1 || !uris[0]) throw failure('SourceStream.credentialInvalid');
      identity.applicationUri = uris[0];
      await writeFile(identity.certificateFile, certificatePem, {mode:0o600,flag:'wx'});
      await writeFile(identity.privateKeyFile, privateKeyPem, {mode:0o600,flag:'wx'});
    }
    client = OPCUAClient.create({
      applicationName: 'Tier0-Source', endpointMustExist: true,
      clientCertificateManager: manager,
      serverCertificate,
      ...identity,
      securityMode: MessageSecurityMode[definition.opcua.securityMode],
      securityPolicy: SecurityPolicy[definition.opcua.securityPolicy],
      connectionStrategy: { maxRetry: 0, initialDelay: 100, maxDelay: 100 },
      requestedSessionTimeout: 30000, keepSessionAlive: true,
      transportTimeout: 6000,
    });
  } catch {
    try { await manager.dispose(); }
    finally { if (identityDirectory) await rm(identityDirectory, {recursive:true,force:true}); }
    throw failure('SourceStream.credentialInvalid');
  }
  let session;
  let closing;
  let sessionReady = false;
  const diagnostic = (code) => console.warn(JSON.stringify({ kind: 'source-session', sourceId: body.sourceId,
    generation: lease.generation, code }));
  const connectionLost = () => { diagnostic('connection-lost'); lease.invalidate(); };
  const sessionClosed = () => { diagnostic('session-closed'); lease.invalidate(); };
  client.on('connection_lost', connectionLost);
  client.on('close', connectionLost);
  const close = () => closing ||= (async () => {
    signal.removeEventListener('abort', abort);
    client.removeListener('connection_lost', connectionLost);
    client.removeListener('close', connectionLost);
    session?.removeListener('session_closed', sessionClosed);
    if (session) await session.close().catch(() => {});
    await client.disconnect().catch(() => {});
    try { await manager.dispose(); }
    finally { if (identityDirectory) await rm(identityDirectory, {recursive:true,force:true}); }
  })();
  // Abort may race createSession. Disconnect the transport now, but defer the
  // memoized full cleanup until connect/createSession has settled so a late
  // session is still explicitly closed.
  const abort = () => {
    if (sessionReady) void close();
    else void client.disconnect().catch(() => {});
  };
  signal.addEventListener('abort', abort, { once: true });
  let failureCode = 'SourceStream.deviceConnectFailed';
  try {
    if (signal.aborted) throw failure();
    await client.connect(definition.endpoint);
    failureCode = 'SourceStream.deviceSessionFailed';
    if (signal.aborted) throw failure();
    session = await client.createSession(body.credential?.kind === 'basic'
      ? { type: UserTokenType.UserName, userName: body.credential.username, password: body.credential.secret }
      : { type: UserTokenType.Anonymous });
    session.on('session_closed', sessionClosed);
    if (signal.aborted) throw failure();
    sessionReady = true;
    return { session, close };
  } catch {
    diagnostic(failureCode);
    await close();
    throw failure(failureCode);
  }
}

async function describe(session, id, namespaces = []) {
  const [name, nodeClass, access, dataType, valueRank] = await session.read([
    { nodeId: id, attributeId: AttributeIds.DisplayName },
    { nodeId: id, attributeId: AttributeIds.NodeClass },
    { nodeId: id, attributeId: AttributeIds.UserAccessLevel },
    { nodeId: id, attributeId: AttributeIds.DataType },
    { nodeId: id, attributeId: AttributeIds.ValueRank },
  ]);
  if (!name.statusCode.isGood() || !nodeClass.statusCode.isGood()) throw failure();
  const models = [];
  if (nodeClass.value.value === NodeClass.Variable && access.statusCode.isGood()) {
    if (access.value.value & 1) models.push('publish', 'request-reply');
    if (access.value.value & 2) models.push('subscribe');
  } else if (nodeClass.value.value === NodeClass.Method) models.push('request-reply', 'subscribe');
  const nodeId = resolveNodeId(id);
  let engineeringUnit = null;
  if (nodeClass.value.value === NodeClass.Variable) {
    const properties = await session.browse({ nodeId: id, browseDirection: BrowseDirection.Forward,
      referenceTypeId: 'HasProperty', includeSubtypes: true, resultMask: 63 });
    if (!properties.statusCode.isGood()) throw failure();
    if (properties.continuationPoint?.length) {
      await session.browseNext(properties.continuationPoint, true);
      throw failure('SourceStream.discoveryLimit');
    }
    const localProperties = (properties.references || []).filter((ref) => !ref.nodeId.serverIndex && !ref.nodeId.namespaceUri);
    const standardUnit = localProperties.find((ref) => ref.browseName.name === 'EngineeringUnits');
    const unit = standardUnit || localProperties.find((ref) => ref.browseName.name === 'Unit');
    if (unit) {
      const info = await session.read({ nodeId: unit.nodeId, attributeId: AttributeIds.Value });
      if (info.statusCode.isGood() && info.value.value) {
        const value = info.value.value;
        if (standardUnit) {
          engineeringUnit = { unitId: value.unitId, namespaceUri: value.namespaceUri,
            symbol: value.displayName?.text || null, description: value.description?.text || null };
        } else {
          // Vendor Unit is an explicit label, not a standard unit ID. Never infer
          // a numerical unit identity or accept arbitrary objects/oversized text.
          const text = info.value.dataType === DataType.String ? value
            : info.value.dataType === DataType.LocalizedText ? value.text : null;
          if (typeof text === 'string' && text.length <= 256 && text.trim()
            && !/[\u0000-\u001f\u007f]/.test(text)) {
            engineeringUnit = { unitId: null, namespaceUri: null, symbol: text.trim(),
              description: null, sourceProperty: 'Unit', sourceNodeId: unit.nodeId.toString() };
          }
        }
      }
    }
  }
  return {
    id: `opcua:${nodeId.toString()}`, name: name.value.value.text || id,
    models, metadata: { nodeId: nodeId.toString(), namespaceUri: namespaces[nodeId.namespace] || null,
      nodeClass: nodeClass.value.value, browsable: nodeClass.value.value === NodeClass.Object,
      dataType: dataType.statusCode.isGood() ? dataType.value.value.toString() : null,
      valueRank: valueRank.statusCode.isGood() ? valueRank.value.value : null, engineeringUnit },
  };
}

async function discover(session, parent) {
  const namespaces = await session.readNamespaceArray();
  const nodeId = parent?.replace(/^opcua:/, '') || 'i=85';
  let result = await session.browse({ nodeId, browseDirection: BrowseDirection.Forward, includeSubtypes: true, referenceTypeId: 'HierarchicalReferences', resultMask: 63 });
  const items = [];
  while (true) {
    if (!result.statusCode.isGood()) throw failure();
    for (const ref of result.references || []) {
      if (items.length >= MAX_RESOURCES) {
        if (result.continuationPoint?.length) await session.browseNext(result.continuationPoint, true);
        throw failure('SourceStream.discoveryLimit');
      }
      if (ref.nodeId.serverIndex || ref.nodeId.namespaceUri) continue;
      items.push(await describe(session, ref.nodeId.toString(), namespaces));
    }
    if (!result.continuationPoint?.length) break;
    result = await session.browseNext(result.continuationPoint, false);
  }
  return items;
}

function selectedNode(body, definition) {
  if (typeof body.resourceRef !== 'string' || !body.resourceRef.startsWith('opcua:')) throw failure();
  const nodeId = resolveNodeId(body.resourceRef.slice(6)).toString();
  if (!definition.opcua.nodeIds.some((id) => resolveNodeId(id).toString() === nodeId)) throw failure('SourceStream.resourceScope');
  return nodeId;
}

async function invoke(session, definition, body) {
  const nodeId = selectedNode(body, definition);
  const operation = body.input?.operation;
  if (operation === 'read') {
    // readVariableValue requests Neither and silently loses source/server timestamps.
    const value = await session.read({ nodeId, attributeId: AttributeIds.Value });
    const resource = await describe(session, nodeId, await session.readNamespaceArray());
    // A bad process value is not a failed protocol exchange. Preserve its
    // quality and timestamps so Stream can apply an explicit quality policy.
    return { value: value.value.value, sourceTimestamp: value.sourceTimestamp, serverTimestamp: value.serverTimestamp,
      statusCode: value.statusCode.name, good: value.statusCode.isGood(), receipt: 'response-received', deviceExecuted: false,
      namespaceUri: resource.metadata.namespaceUri, engineeringUnit: resource.metadata.engineeringUnit };
  }
  if (operation === 'write') {
    const dataType = await session.getBuiltInDataType(nodeId);
    if (!Number.isInteger(dataType) || dataType === DataType.ExtensionObject || dataType === DataType.Null) throw failure();
    const status = await session.write({ nodeId, attributeId: AttributeIds.Value, value: { value: new Variant({ dataType, value: body.input.value }) } });
    if (!status.isGood()) throw failure();
    return { receipt: 'operation-accepted', deviceExecuted: false, statusCode: status.name };
  }
  if (operation === 'call') {
    const parent = await session.browse({ nodeId, browseDirection: BrowseDirection.Inverse, referenceTypeId: 'HasComponent', includeSubtypes: true, resultMask: 63 });
    const objects = parent.references || [];
    if (!parent.statusCode.isGood() || objects.length !== 1 || objects[0].nodeClass !== NodeClass.Object) throw failure();
    const arguments_ = body.input.arguments || [];
    if (!Array.isArray(arguments_) || arguments_.length > 64) throw failure();
    const inputArguments = arguments_.map((arg) => {
      const dataType = typeof arg.dataType === 'string' ? DataType[arg.dataType] : arg.dataType;
      if (!Number.isInteger(dataType) || dataType < 1 || dataType > DataType.LocalizedText) throw failure();
      return new Variant({ dataType, value: arg.value });
    });
    const result = await session.call({ objectId: objects[0].nodeId, methodId: nodeId, inputArguments });
    if (!result.statusCode.isGood()) throw failure();
    return { receipt: 'method-returned', deviceExecuted: false, outputArguments: (result.outputArguments || []).map((value) => value.value) };
  }
  throw failure();
}

async function watch(session, definition, response, signal) {
  if (!definition.opcua.nodeIds.length) throw failure();
  const namespaces = await session.readNamespaceArray();
  const subscription = ClientSubscription.create(session, {
    requestedPublishingInterval: definition.opcua.samplingIntervalMs,
    requestedLifetimeCount: 100, requestedMaxKeepAliveCount: 10,
    maxNotificationsPerPublish: 100, publishingEnabled: true, priority: 1,
  });
  const pending = [];
  let failure_;
  let wake;
  let rejectInitialization;
  const initializationFailed = new Promise((_, reject) => { rejectInitialization = reject; });
  const initializationListeners = [];
  const notify = () => { wake?.(); wake = undefined; };
  const fail = () => { failure_ ||= failure(); rejectInitialization?.(failure_); notify(); };
  subscription.on('error', fail);
  subscription.on('internal_error', fail);
  subscription.on('terminated', () => { if (!signal.aborted) fail(); });
  const onAbort = () => { if (rejectInitialization) fail(); else notify(); };
  signal.addEventListener('abort', onAbort, { once: true });
  const initializationTimer = setTimeout(fail, WATCH_INITIALIZATION_MS);
  try {
    if (signal.aborted) throw failure();
    const initialized = [];
    for (const id of definition.opcua.nodeIds) {
      const resource = await Promise.race([describe(session, id, namespaces), initializationFailed]);
      if (signal.aborted || failure_) throw failure_ || failure();
      if (!resource.models.includes('publish')) continue;
      const item = ClientMonitoredItem.create(subscription,
        { nodeId: id, attributeId: AttributeIds.Value },
        { samplingInterval: definition.opcua.samplingIntervalMs, discardOldest: false, queueSize: 100 },
        TimestampsToReturn.Both);
      item.on('err', fail);
      // create() schedules the protocol request; it does not prove that the
      // server accepted a MonitoredItem. Attach listeners before yielding.
      initialized.push(new Promise((resolve) => {
        const ready = () => { if (item.statusCode?.isGood()) resolve(); else fail(); };
        item.once('initialized', ready);
        initializationListeners.push(() => item.removeListener('initialized', ready));
      }));
      item.on('changed', (value) => {
        if (pending.length >= 1000 || (value.statusCode.value & 0x80)) { fail(); return; }
        pending.push({ resourceRef: resource.id, data: { value: value.value.value, sourceTimestamp: value.sourceTimestamp,
          serverTimestamp: value.serverTimestamp, statusCode: value.statusCode.name, good: value.statusCode.isGood(),
          namespaceUri: resource.metadata.namespaceUri, engineeringUnit: resource.metadata.engineeringUnit }, receivedAt: Date.now() });
        notify();
      });
    }
    if (!initialized.length) throw failure();
    await Promise.race([Promise.all(initialized), initializationFailed]);
    if (signal.aborted || failure_) throw failure_ || failure();
    clearTimeout(initializationTimer);
    rejectInitialization = undefined;
    response.writeHead(200, { 'Content-Type': 'application/x-ndjson', 'Cache-Control': 'no-store', 'X-Tier0-Watch-Ready': '1' });
    response.flushHeaders();
    while (!signal.aborted) {
      if (failure_) throw failure_;
      const event = pending.shift();
      if (!event) { await new Promise((resolve) => { wake = resolve; if (signal.aborted || pending.length || failure_) notify(); }); continue; }
      const line = JSON.stringify(event);
      if (Buffer.byteLength(line) > MAX_BODY) throw failure();
      await new Promise((resolve, reject) => response.write(`${line}\n`, (error) => error ? reject(error) : resolve()));
    }
  } finally {
    clearTimeout(initializationTimer);
    rejectInitialization = undefined;
    initializationListeners.forEach((remove) => remove());
    signal.removeEventListener('abort', onAbort);
    await subscription.terminate().catch(() => {});
  }
}

export function createServer({ pkiRoot = process.env.SOURCE_OPCUA_PKI || '/run/source-opcua/pki',
  managedPort = Number(process.env.SOURCE_OPCUA_SERVER_PORT || 4841), managedHostname = process.env.SOURCE_OPCUA_SERVER_HOSTNAME,
  managedLeaseMs = 30000, clientIdleMs = 60000 } = {}) {
  let active = 0;
  const watching = new Map();
  const managed = new ManagedOPCUAServer({ pkiRoot, port: managedPort, hostname: managedHostname, leaseMs: managedLeaseMs });
  const sessions = new SourceSessionPool({ idleMs: clientIdleMs, connect: (body, signal, lease) => connectSession(body, signal, pkiRoot, lease) });
  const server = http.createServer(async (request, response) => {
    if (request.method === 'GET' && request.url === '/readyz') { response.writeHead(200).end('ready'); return; }
    if (request.method !== 'POST' || !['/probe', '/discover', '/invoke', '/watch', '/release', '/managed/sync'].includes(request.url)) { response.writeHead(404).end(); return; }
    if (active >= 128) { response.writeHead(503).end(); return; }
    active++;
    const controller = new AbortController();
    const abort = () => controller.abort();
    response.on('close', abort);
    let body;
    let uncertain = false;
    let ownedWatch = false;
    let watchKey;
    controller.signal.addEventListener('abort', () => {
      // Release only this attempt's lease before slow protocol/PKI cleanup.
      // Its late finally must not remove a replacement watcher.
      if (ownedWatch && watching.get(watchKey) === controller) watching.delete(watchKey);
    }, { once: true });
    const timer = request.url === '/watch' ? undefined : setTimeout(abort, 30000);
    try {
      body = await readBody(request);
      if (request.url === '/release') {
        if (!Number.isSafeInteger(body.sourceId) || body.sourceId < 1) throw failure();
        await sessions.releaseSource(body.sourceId);
        response.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }).end(JSON.stringify({ data: { released: true } }));
        return;
      }
      if (request.url === '/managed/sync') {
        const status = await managed.sync(body.sources);
        response.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }).end(JSON.stringify({ data: status }));
        return;
      }
      if (request.url !== '/probe' && body.sourceId < 1) throw failure();
      if (request.url === '/watch') {
        const scope = structuredClone(body.definition);
        delete scope.name; delete scope.description;
        scope.opcua.nodeIds = [...scope.opcua.nodeIds].sort();
        watchKey = `${body.sourceId}:${body.configRevision ?? 0}:${createHash('sha256').update(JSON.stringify(scope)).digest('hex')}`;
        if (watching.has(watchKey)) throw failure('SourceStream.alreadyWatching');
        watching.set(watchKey, controller); ownedWatch = true;
      }
      let result;
      if (body.definition?.role === 'server') {
        validateManagedSource(body);
        if (request.url === '/probe' || request.url === '/discover') await managed.start();
        if (request.url === '/probe') result = { health: 'healthy', listenerReady: true, deviceOnline: null };
        else if (request.url === '/discover') {
          if (body.parent) throw failure('SourceStream.resourceScope');
          result = managed.resources(body);
        } else if (request.url === '/watch') result = await managed.watch(body, response, controller.signal);
        else { uncertain = true; result = managed.invoke(body); }
      } else {
        const definition = validate(body, request.url === '/watch' || request.url === '/invoke');
        result = await sessions.run(body, controller.signal, async (session, signal) => {
          if (request.url === '/probe') {
            const status = await session.read({ nodeId: 'i=2259', attributeId: AttributeIds.Value });
            if (!status.statusCode.isGood()) throw failure();
            return { health: 'healthy' };
          }
          if (request.url === '/discover') {
            // Existing configured Sources keep their declared discovery scope;
            // connection-only Sources browse the actual address space.
            if (!body.parent && definition.opcua.nodeIds?.length) {
              const namespaces = await session.readNamespaceArray();
              return Promise.all(definition.opcua.nodeIds.map((id) => describe(session, id, namespaces)));
            }
            return discover(session, body.parent || '');
          }
          if (request.url === '/watch') return watch(session, definition, response, signal);
          uncertain = true;
          return invoke(session, definition, body);
        }, { watch: request.url === '/watch' });
      }
      if (!response.headersSent) response.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }).end(JSON.stringify({ data: result }));
    } catch (error) {
      const known = typeof error.code === 'string' && error.code.startsWith('SourceStream.');
      const code = known ? error.code : uncertain ? 'SourceStream.resultUnknown' : 'SourceStream.protocolFailed';
      if (!response.headersSent) response.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }).end(JSON.stringify({ error: code }));
      else response.destroy();
    } finally {
      clearTimeout(timer);
      response.removeListener('close', abort);
      if (ownedWatch && watching.get(watchKey) === controller) watching.delete(watchKey);
      if (!response.writableEnded) response.end();
      active--;
    }
  });
  server.closeManaged = () => managed.close();
  server.closeClients = () => sessions.close();
  server.once('close', () => { void sessions.close().catch(() => {}); void managed.close().catch(() => {}); });
  return server;
}

export async function start(socket, options = {}) {
  // Node uses named pipes for Windows IPC; Go clients use go-winio.
  if (process.platform === 'win32' && socket.startsWith('\\\\.\\pipe\\Tier0-')) {
    const server = createServer(options);
    await new Promise((resolve, reject) => { server.once('error', reject); server.listen(socket, resolve); });
    return server;
  }
  if (!isAbsolute(socket)) throw failure();
  await mkdir(dirname(socket), { recursive: true, mode: 0o750 });
  try {
    const info = await lstat(socket);
    if (!info.isSocket()) throw new Error('runtime socket path occupied');
    await new Promise((resolve, reject) => {
      const connection = createConnection(socket);
      connection.setTimeout(1000, () => connection.destroy(new Error('runtime socket probe timed out')));
      connection.once('connect', () => {
        connection.destroy();
        reject(new Error('runtime already running'));
      });
      connection.once('error', (error) => {
        if (error.code === 'ECONNREFUSED' || error.code === 'ENOENT') resolve();
        else reject(error);
      });
    });
    await unlink(socket).catch((error) => { if (error.code !== 'ENOENT') throw error; });
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
  }
  const server = createServer(options);
  await new Promise((resolve, reject) => { server.once('error', reject); server.listen(socket, resolve); });
  await chmod(socket, 0o660);
  return server;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const server = await start(process.env.SOURCE_OPCUA_SOCKET || '/run/source-opcua/runtime.sock');
  const stop = () => { void server.closeClients(); server.close(); setTimeout(() => process.exit(0), 3000).unref(); };
  process.on('SIGTERM', stop); process.on('SIGINT', stop);
}
