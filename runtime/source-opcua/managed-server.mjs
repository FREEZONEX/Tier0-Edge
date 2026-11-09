import { createHash, timingSafeEqual, randomUUID } from 'node:crypto';
import { join } from 'node:path';
import {
  OPCUAServer, OPCUACertificateManager, DataType, Variant, DataValue,
  StatusCodes, MessageSecurityMode, SecurityPolicy, PermissionFlag,
  AccessRestrictionsFlag, WellKnownRoles, resolveNodeId,
} from 'node-opcua';

const fail = (code = 'SourceStream.protocolFailed') => Object.assign(new Error(code), { code });
const types = new Set(['Double', 'Int32', 'Boolean', 'String']);
const directions = new Set(['ingress', 'egress', 'bidirectional']);
const hash = (text) => createHash('sha256').update(text).digest();
const MAX_VALUE_BYTES = 64 * 1024;
const MAX_SOURCES = 200;

export function validateManagedSource(body) {
  const def = body?.definition;
  const nodes = def?.opcua?.serverNodes;
  if (!Number.isSafeInteger(body?.sourceId) || body.sourceId < 1 || def?.protocol !== 'opcua'
    || def.role !== 'server' || def.endpoint !== 'opc.tcp://managed'
    || typeof def.credentialRef !== 'string' || !/^[a-zA-Z0-9-]{1,80}$/.test(def.credentialRef)
    || body.credential?.kind !== 'basic' || typeof body.credential.username !== 'string'
    || !body.credential.username || Buffer.byteLength(body.credential.username) > 128
    || typeof body.credential.secret !== 'string' || !body.credential.secret || Buffer.byteLength(body.credential.secret) > 4096) {
    throw fail('SourceStream.credentialInvalid');
  }
  const mode = def.opcua.securityMode;
  const policy = def.opcua.securityPolicy;
  if (!((mode === 'None' && policy === 'None') || (mode === 'SignAndEncrypt' && policy === 'Basic256Sha256'))
    || !Array.isArray(nodes) || !nodes.length || nodes.length > 1000
    || (def.opcua.nodeIds != null && (!Array.isArray(def.opcua.nodeIds) || def.opcua.nodeIds.length > 1000))
    || !Number.isInteger(def.opcua.samplingIntervalMs) || def.opcua.samplingIntervalMs < 100
    || def.opcua.samplingIntervalMs > 3600000) throw fail();
  const ids = new Set();
  for (const node of nodes) {
    if (typeof node?.id !== 'string' || !/^[a-zA-Z][a-zA-Z0-9_-]{0,62}$/.test(node.id)
      || ids.has(node.id) || typeof node.name !== 'string' || !node.name.trim()
      || Array.from(node.name).length > 63 || !types.has(node.dataType)
      || !directions.has(node.direction) || !['Variable', 'Method'].includes(node.nodeClass)
      || (node.nodeClass === 'Method' && (node.direction !== 'ingress' || node.dataType !== 'String'))) throw fail();
    ids.add(node.id);
  }
  if (!(def.opcua.nodeIds || []).every((id) => ids.has(id))) throw fail('SourceStream.resourceScope');
  return body;
}

// One OPC UA listener serves multiple leased Sources. Control requests arrive
// only over the protected runtime socket; remote sessions never receive secrets.
export class ManagedOPCUAServer {
  constructor({ pkiRoot, port = 4841, hostname, leaseMs = 30000 } = {}) {
    this.options = { pkiRoot, port, hostname };
    this.leaseMs = leaseMs;
    this.sources = new Map();
    this.starting = undefined;
    this.sweep = setInterval(() => this.expire(), Math.min(1000, leaseMs));
    this.sweep.unref();
  }

  async start() {
    if (this.server) return;
    this.starting ||= this.initialize().catch((error) => { this.starting = undefined; throw error; });
    await this.starting;
  }

  async initialize() {
    const owner = this;
    const server = new OPCUAServer({
      port: this.options.port, ...(this.options.hostname ? { hostname: this.options.hostname } : {}),
      resourcePath: '/tier0/source', allowAnonymous: false, unresolvedPermissionPolicy: 'deny',
      securityModes: [MessageSecurityMode.None, MessageSecurityMode.SignAndEncrypt],
      securityPolicies: [SecurityPolicy.None, SecurityPolicy.Basic256Sha256],
      serverCertificateManager: new OPCUACertificateManager({
        rootFolder: join(this.options.pkiRoot, 'managed-server'), automaticallyAcceptUnknownCertificate: false,
      }),
      // node-opcua otherwise creates UserPKI in the read-only service home.
      userCertificateManager: new OPCUACertificateManager({
        rootFolder: join(this.options.pkiRoot, 'managed-users'), automaticallyAcceptUnknownCertificate: false,
      }),
      serverInfo: { applicationUri: 'urn:tier0:source-server', productUri: 'urn:tier0:enterprise' },
      serverCapabilities: { maxSessions: 128, maxSubscriptions: 128, maxMonitoredItems: 2000 },
      userManager: {
        isValidUser(username, password) {
          const source = owner.user(username);
          if (!source || typeof password !== 'string' || !owner.channelAllowed(source, this.channel)) return false;
          return timingSafeEqual(hash(password), hash(source.body.credential.secret));
        },
        getUserRoles(username) {
          const source = owner.user(username);
          return source ? [source.role] : [];
        },
      },
    });
    try {
      await server.initialize();
      const namespace = server.engine.addressSpace.registerNamespace('urn:tier0:source-resources');
      const root = namespace.addObject({ organizedBy: server.engine.addressSpace.rootFolder.objects,
        browseName: 'Sources', nodeId: `ns=${namespace.index};s=Sources`,
        rolePermissions: [{ roleId: resolveNodeId(WellKnownRoles.AuthenticatedUser), permissions: PermissionFlag.Browse }],
      });
      await server.start();
      this.server = server; this.namespace = namespace; this.root = root;
    } catch (error) { await server.shutdown(0).catch(() => {}); throw error; }
  }

  channelAllowed(source, channel) {
    const config = source.body.definition.opcua;
    return !!channel && channel.securityMode === MessageSecurityMode[config.securityMode]
      && channel.securityPolicy === SecurityPolicy[config.securityPolicy];
  }

  user(username) {
    return [...this.sources.values()].find((source) => source.expiresAt > Date.now() && source.username === username);
  }

  async sync(bodies) {
    if (!Array.isArray(bodies) || bodies.length > MAX_SOURCES) throw fail();
    bodies.forEach(validateManagedSource);
    if (new Set(bodies.map((body) => body.sourceId)).size !== bodies.length) throw fail();
    if (bodies.length) await this.start();
    const live = new Set(bodies.map((body) => body.sourceId));
    for (const id of this.sources.keys()) if (!live.has(id)) this.remove(id);
    for (const body of bodies) {
      const current = this.sources.get(body.sourceId);
      // Preserve values/watches when only renewing a lease or changing a name.
      const signature = JSON.stringify({ credential: body.credential, ref: body.definition.credentialRef, opcua: body.definition.opcua });
      if (current?.signature === signature) { current.expiresAt = Date.now() + this.leaseMs; continue; }
      this.remove(body.sourceId);
      try { this.install(body, signature); }
      catch (error) { this.remove(body.sourceId); throw error; }
    }
    return { sources: this.sources.size, endpoint: this.server?.getEndpointUrl() || null, leaseMs: this.leaseMs };
  }

  install(body, signature) {
    const namespace = this.namespace;
    const prefix = `source/${body.sourceId}/${body.definition.credentialRef}/`;
    const nodeId = (id) => `ns=${namespace.index};s=${prefix}${id}`;
    const role = resolveNodeId(nodeId('$role'));
    const permissions = (flags) => [{ roleId: role, permissions: flags }];
    const restriction = body.definition.opcua.securityMode === 'None' ? 0
      : AccessRestrictionsFlag.SigningRequired | AccessRestrictionsFlag.EncryptionRequired | AccessRestrictionsFlag.ApplyRestrictionsToBrowse;
    const folder = namespace.addObject({ organizedBy: this.root, browseName: body.definition.name || `Source ${body.sourceId}`,
      nodeId: nodeId('$root'), rolePermissions: permissions(PermissionFlag.Browse | PermissionFlag.Call), accessRestrictions: restriction });
    const source = { body, signature, folder, role, nodes: new Map(), expiresAt: Date.now() + this.leaseMs,
      username: `source:${body.sourceId}:${body.definition.credentialRef}:${body.credential.username}`, watchers: new Set() };
    this.sources.set(body.sourceId, source);
    for (const config of body.definition.opcua.serverNodes) {
      const entry = { config, value: undefined, node: undefined };
      source.nodes.set(config.id, entry);
      if (config.nodeClass === 'Method') {
        const method = namespace.addMethod(folder, { browseName: config.name, nodeId: nodeId(config.id),
          rolePermissions: permissions(PermissionFlag.Browse | PermissionFlag.Call), accessRestrictions: restriction,
          inputArguments: [{ name: 'payload', dataType: DataType.String }],
          outputArguments: [{ name: 'receipt', dataType: DataType.String }],
        });
        for (const property of method.getProperties()) { property.setRolePermissions(permissions(PermissionFlag.Browse | PermissionFlag.Read)); property.setAccessRestrictions(restriction); }
        method.bindMethod((args, _, callback) => {
          if (source.expiresAt <= Date.now() || this.sources.get(body.sourceId) !== source) return callback(null, { statusCode: StatusCodes.BadUserAccessDenied });
          let data;
          try { if (Buffer.byteLength(args[0]?.value || '') > MAX_VALUE_BYTES) throw fail(); data = JSON.parse(args[0].value); }
          catch { return callback(null, { statusCode: StatusCodes.BadInvalidArgument }); }
          if (!this.emit(source, config.id, data)) return callback(null, { statusCode: StatusCodes.BadResourceUnavailable });
          callback(null, { statusCode: StatusCodes.Good, outputArguments: [new Variant({ dataType: DataType.String,
            value: JSON.stringify({ receipt: 'server-accepted', deviceExecuted: false }) })] });
        });
        entry.node = method;
        continue;
      }
      const flags = PermissionFlag.Browse | PermissionFlag.Read | (config.direction !== 'egress' ? PermissionFlag.Write : 0);
      entry.node = namespace.addVariable({ componentOf: folder, browseName: config.name, nodeId: nodeId(config.id), dataType: config.dataType,
        minimumSamplingInterval: body.definition.opcua.samplingIntervalMs, accessLevel: config.direction === 'egress' ? 'CurrentRead' : 'CurrentRead | CurrentWrite',
        rolePermissions: permissions(flags), accessRestrictions: restriction,
        value: {
          timestamped_get: () => entry.value || new DataValue({ statusCode: StatusCodes.BadWaitingForInitialData }),
          timestamped_set: (value, callback) => {
            if (source.expiresAt <= Date.now() || this.sources.get(body.sourceId) !== source) return callback(null, StatusCodes.BadUserAccessDenied);
            if (!this.validValue(config.dataType, value.value.value)) return callback(null, StatusCodes.BadTypeMismatch);
            if (!this.emit(source, config.id, { value: value.value.value, sourceTimestamp: value.sourceTimestamp || new Date(), statusCode: 'Good' })) {
              return callback(null, StatusCodes.BadResourceUnavailable);
            }
            entry.value = new DataValue({ value: value.value, sourceTimestamp: value.sourceTimestamp || new Date(), statusCode: StatusCodes.Good });
            callback(null, StatusCodes.Good);
          },
        },
      });
    }
  }

  validValue(type, value) {
    if (type === 'String') return typeof value === 'string' && Buffer.byteLength(value) <= MAX_VALUE_BYTES;
    if (type === 'Boolean') return typeof value === 'boolean';
    if (type === 'Int32') return Number.isInteger(value) && value >= -2147483648 && value <= 2147483647;
    return typeof value === 'number' && Number.isFinite(value);
  }

  emit(source, key, data) {
    const watchers = [...source.watchers].filter((watcher) => watcher.ids.has(key));
    if (watchers.some((watcher) => watcher.pending.length >= 1000)) return false;
    const event = { resourceRef: `opcua:${key}`, data, receivedAt: Date.now(), eventId: randomUUID() };
    for (const watcher of watchers) {
      watcher.pending.push(event);
      watcher.wake?.(); watcher.wake = undefined;
    }
    return true;
  }

  checked(body) {
    validateManagedSource(body);
    const source = this.sources.get(body.sourceId);
    if (!source || source.expiresAt <= Date.now() || source.body.definition.credentialRef !== body.definition.credentialRef
      || source.body.credential.username !== body.credential.username
      || !timingSafeEqual(hash(source.body.credential.secret), hash(body.credential.secret))) throw fail('SourceStream.credentialInvalid');
    return source;
  }

  resources(body) {
    validateManagedSource(body);
    const source = this.sources.get(body.sourceId);
    return body.definition.opcua.serverNodes.map((node) => ({ id: `opcua:${node.id}`, name: node.name,
      models: node.nodeClass === 'Method' ? ['publish'] : node.direction === 'ingress' ? ['publish', 'request-reply']
        : node.direction === 'egress' ? ['subscribe', 'request-reply'] : ['publish', 'subscribe', 'request-reply'],
      metadata: { nodeClass: node.nodeClass === 'Method' ? 4 : 2, dataType: node.dataType, direction: node.direction,
        serverNodeId: source?.nodes.get(node.id)?.node.nodeId.toString()
          || (this.namespace ? `ns=${this.namespace.index};s=source/${body.sourceId}/${body.definition.credentialRef}/${node.id}` : null),
        namespaceUri: 'urn:tier0:source-resources', declared: true, registered: !!source, deviceOnline: null },
    }));
  }

  invoke(body) {
    const source = this.checked(body);
    const key = body.resourceRef?.replace(/^opcua:/, '');
    if (!body.resourceRef?.startsWith('opcua:')) throw fail('SourceStream.resourceScope');
    const entry = source.nodes.get(key);
    if (!entry || entry.config.nodeClass !== 'Variable') throw fail('SourceStream.resourceScope');
    if (body.input?.operation === 'read') {
      if (!entry.value) throw fail('SourceStream.noSample');
      return { value: entry.value.value.value, sourceTimestamp: entry.value.sourceTimestamp, statusCode: 'Good', receipt: 'server-value-read', deviceExecuted: false };
    }
    if (body.input?.operation !== 'write' || entry.config.direction === 'ingress'
      || !this.validValue(entry.config.dataType, body.input.value)) throw fail('SourceStream.resourceScope');
    entry.value = new DataValue({ value: new Variant({ dataType: DataType[entry.config.dataType], value: body.input.value }),
      sourceTimestamp: new Date(), statusCode: StatusCodes.Good });
    entry.node.setValueFromSource(entry.value.value, StatusCodes.Good, entry.value.sourceTimestamp);
    // Updating the managed value lets remote clients read/monitor it. It does
    // not prove a physical device applied it, nor loop back into an ingress Stream.
    return { receipt: 'server-value-updated', statusCode: 'Good', deviceExecuted: false };
  }

  async watch(body, response, signal) {
    const source = this.checked(body);
    if (source.watchers.size >= 128) throw fail();
    const ids = new Set(body.definition.opcua.nodeIds || []);
    if (!ids.size || [...ids].some((id) => !source.nodes.has(id) || source.nodes.get(id).config.direction === 'egress')) throw fail('SourceStream.resourceScope');
    const watcher = { ids, pending: [], wake: undefined };
    source.watchers.add(watcher);
    const wake = () => { watcher.wake?.(); watcher.wake = undefined; };
    signal.addEventListener('abort', wake, { once: true });
    try {
      if (signal.aborted) throw fail();
      for (const id of ids) {
        const entry = source.nodes.get(id);
        if (entry.value) watcher.pending.push({ resourceRef: `opcua:${id}`, data: { value: entry.value.value.value, sourceTimestamp: entry.value.sourceTimestamp, statusCode: 'Good' }, receivedAt: Date.now(), eventId: randomUUID() });
      }
      // The isolated watcher is installed before advertising readiness. This
      // proves acceptance of writes, not that a remote device is online.
      response.writeHead(200, { 'Content-Type': 'application/x-ndjson', 'Cache-Control': 'no-store', 'X-Tier0-Watch-Ready': '1' });
      response.flushHeaders();
      while (!signal.aborted && this.sources.get(body.sourceId) === source && source.expiresAt > Date.now()) {
        const event = watcher.pending.shift();
        if (!event) { await new Promise((resolve) => { watcher.wake = resolve; if (signal.aborted || watcher.pending.length) wake(); }); continue; }
        await new Promise((resolve, reject) => response.write(`${JSON.stringify(event)}\n`, (error) => error ? reject(error) : resolve()));
      }
    } finally { signal.removeEventListener('abort', wake); source.watchers.delete(watcher); }
  }

  remove(id) {
    const source = this.sources.get(id);
    if (!source) return;
    this.sources.delete(id);
    for (const watcher of source.watchers) watcher.wake?.();
    this.server.engine.addressSpace.deleteNode(source.folder);
  }

  expire() { for (const [id, source] of this.sources) if (source.expiresAt <= Date.now()) this.remove(id); }

  close() {
    this.closing ||= (async () => {
      clearInterval(this.sweep);
      if (this.starting) await this.starting.catch(() => {});
      for (const id of this.sources.keys()) this.remove(id);
      if (this.server) await this.server.shutdown(0);
    })();
    return this.closing;
  }
}
