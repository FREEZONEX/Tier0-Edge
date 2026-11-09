// Pure client contract tests. No browser script execution, API calls or device actions.
'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const { createRequire } = require('node:module');
const exportTemplates = process.argv.includes('--export-templates');
const test = exportTemplates ? () => {} : require('node:test').test;
const appRequire = createRequire(path.resolve(__dirname, '../package.json'));
const ts = appRequire('typescript');
const file = path.resolve(__dirname, '../src/pages/uns/components/source-stream/program-document.ts');
const compiled = ts.transpileModule(fs.readFileSync(file, 'utf8'), {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
}).outputText;
const fixture = { exports: {} };
vm.runInNewContext(compiled, { module: fixture, exports: fixture.exports }, { filename: file });
const {
  initialProgram,
  readProgramDocument,
  sampleInput,
  appendProgramSnippet,
  sourceInsertionModes,
  sourceInsertionSnippet,
  entryInsertionSnippet,
  topicInsertionSnippet,
  manifestIDs,
} = fixture.exports;
const normalize = (value) => JSON.parse(JSON.stringify(value));
const sourceFixture = (protocol, resources, configuration = {}, id = 8101) => ({
  id,
  name: protocol,
  protocol,
  document: { role: 'client', ...configuration },
  resources: resources.map((resource) => ({ name: resource.id, ...resource })),
});

// Execute the actual generated code against a single-context registration contract.
// This controlled test double is not Goja or a live protocol/device test.
function registerGenerated(script) {
  const entries = [];
  const sources = [];
  const topics = [];
  const calls = [];
  const outputs = [];
  const topicHandles = new Map();
  const sourceHandles = new WeakMap();
  const register = (kind) => (options, handler) => {
    assert.equal(typeof handler, 'function');
    assert.equal(handler.length, 1, 'handlers receive exactly one context');
    assert.ok(!entries.some((entry) => entry.id === options.id), 'duplicate entry rejected');
    if (kind === 'request' && options.source) {
      assert.ok(sourceHandles.has(options.source), 'HTTP entry references a registered handle');
      kind = 'http-request';
    }
    entries.push({ ...options, kind, handler });
  };
  const api = {
    sources: {
      use(config) {
        assert.equal(typeof config.id, 'string');
        const previous = sources.find((source) => source.id === config.id);
        if (previous) previous.resources = [...new Set([...previous.resources, ...config.resources])];
        else sources.push(normalize(config));
        const allowed = [...config.resources];
        const handle = Object.fromEntries(
          ['read', 'write', 'invoke', 'publish'].map((kind) => [
            kind,
            async (ref, input) => {
              assert.ok(allowed.includes(ref), 'operation resource explicitly declared on this handle');
              calls.push(normalize({ kind, sourceId: config.id, ref, input }));
              return { kind, value: 37 };
            },
          ])
        );
        Object.assign(handle, {
          async parameters() {
            calls.push({ kind: 'parameters', sourceId: config.id });
            return { path: '/data' };
          },
          async secrets() {
            calls.push({ kind: 'secrets', sourceId: config.id });
            return { token: 'mock-only' };
          },
          async request(options) {
            assert.ok(options.path.startsWith('/'), 'HTTP request uses a relative path');
            calls.push(normalize({ kind: 'request', sourceId: config.id, input: options }));
            return { status: 200, headers: {}, data: { value: 37 } };
          },
        });
        sourceHandles.set(handle, config.id);
        return handle;
      },
    },
    uns: {
      use(config) {
        assert.equal(typeof config.topicId, 'string');
        if (topicHandles.has(config.topicId)) return topicHandles.get(config.topicId);
        topics.push(normalize(config));
        const handle = {
          async read() {
            return { value: 37 };
          },
          async write(data) {
            outputs.push(normalize({ topicId: config.topicId, data }));
            return { stage: 'buffered', topicId: config.topicId };
          },
        };
        topicHandles.set(config.topicId, handle);
        return handle;
      },
    },
    stream: {
      onRequest: register('request'),
      onMessage: register('source-event'),
      onEvent: register('uns-event'),
      onTimer: register('interval'),
    },
    log: Object.fromEntries(['info', 'warn', 'error', 'debug'].map((level) => [level, () => {}])),
  };
  const module = { exports: {} };
  vm.runInNewContext(script, { module }, { timeout: 1000 });
  assert.equal(typeof module.exports, 'function');
  module.exports(api);
  return {
    entries,
    sources,
    topics,
    calls,
    outputs,
    execute: async (id, input) => {
      const entry = entries.find((item) => item.id === id);
      assert.ok(entry, 'entry registered');
      return normalize(
        await entry.handler(
          Object.freeze({
            input,
            entryId: id,
            dryRun: true,
            trace: 'context-only',
            ...(entry.kind === 'http-request' ? { request: input } : {}),
          })
        )
      );
    },
  };
}

test('new list Stream has a complete CommonJS program with no Source or Model', () => {
  const doc = normalize(initialProgram());
  assert.equal(Object.hasOwn(doc, 'sdkVersion'), false);
  assert.equal(doc.timeoutMs, 5000);
  assert.equal(typeof doc.script, 'string');
  assert.match(doc.script, /module.exports = function \(api\)/);
  assert.match(doc.script, /api.stream.onRequest/);
  for (const key of ['model', 'sourceId', 'program', 'responseScript']) assert.ok(!(key in doc));
  new vm.Script(doc.script); // Test fixture syntax check, never invoked by the browser editor.
});
test('JSON roundtrip preserves extensions and default description without changing script', () => {
  const source = { ...normalize(initialProgram()), description: undefined, extensions: { own: true } };
  const parsed = normalize(readProgramDocument(JSON.stringify(source)));
  assert.equal(parsed.description, '');
  assert.deepEqual(parsed.extensions, { own: true });
  assert.equal(parsed.script, source.script);
});
test('invalid JSON, legacy contracts and invalid timeouts cannot be saved', () => {
  assert.equal(readProgramDocument('{bad'), undefined);
  for (const patch of [
    { model: 'publish' },
    { sourceId: 0 },
    { program: {} },
    { responseScript: {} },
    { timeoutMs: 0 },
    { timeoutMs: 5001 },
    { timeoutMs: 1.5 },
    { script: {} },
    { description: 1 },
    { sdkVersion: 'other' },
    { sdkVersion: 'tier0-stream/v2' },
    { configVersion: 1 },
    { supportedVersions: [] },
    { supportedSDKVersions: ['old'] },
    { compatEngine: 'old' },
    { release: 7 },
    { unknown: true },
  ]) {
    assert.equal(readProgramDocument(JSON.stringify({ ...initialProgram(), ...patch })), undefined);
  }
  for (const timeoutMs of [1, 1000, 5000])
    assert.equal(readProgramDocument(JSON.stringify({ ...initialProgram(), timeoutMs })).timeoutMs, timeoutMs);
});
test('Source entry uses stable ID and escaped resources, with MQTT subscription instead of unsupported read', () => {
  const source = sourceFixture('opcua', [
    { id: 'opcua:ns=2;s=a"\\x', models: ['publish'], metadata: { nodeClass: 2 } },
  ]);
  const script = initialProgram(source).script;
  assert.match(script, /id: "8101"/);
  assert.match(script, /await source.read/);
  new vm.Script(script);
  const mqtt = initialProgram(sourceFixture('mqtt', [{ id: 'mqtt:factory/+/telemetry', models: ['publish'] }])).script;
  assert.match(mqtt, /api.stream.onMessage/);
  assert.doesNotMatch(mqtt, /source.read/);
  new vm.Script(mqtt);
  const empty = initialProgram({ ...source, resources: [] }).script;
  assert.match(empty, /Discover a supported resource in Source 8101/);
  new vm.Script(empty);
});
test('Topic insertion is a stable string handle and buffered write', () => {
  const script = initialProgram(undefined, 9101).script;
  assert.match(script, /topicId: "9101"/);
  assert.match(script, /await topic.write\(input\)/);
  new vm.Script(script);
});
test('samples key Source and Topic receipts by registered stable ID, not alias or script text', () => {
  const sample = JSON.parse(
    sampleInput({ sources: [{ id: '8101', resources: ['ns=2;s=a', 'b'] }], topics: [{ topicId: '9101' }], entries: [] })
  );
  assert.deepEqual(sample, {
    data: {},
    sourceSamples: { 8101: { 'ns=2;s=a': {}, b: {} } },
    topicSamples: { 9101: {} },
    checkpointSample: null,
  });
});
test('snippet insertion preserves all prior code and advanced fields', () => {
  const doc = { ...normalize(initialProgram()), extra: 7 };
  const next = normalize(appendProgramSnippet(doc, '/* user insertion */'));
  assert.equal(next.script, `${doc.script}\n\n/* user insertion */`);
  assert.equal(next.extra, 7);
  assert.equal(doc.script, initialProgram().script);
});

test('actual default and Topic programs receive business input, not context metadata', async () => {
  const input = { deviceId: 'boiler', value: 12 };
  const blank = registerGenerated(initialProgram().script);
  assert.deepEqual(await blank.execute('request', input), input);
  assert.deepEqual(blank.sources, []);
  const topic = registerGenerated(initialProgram(undefined, 9101).script);
  assert.deepEqual(await topic.execute('write', input), input);
  assert.deepEqual(topic.outputs, [{ topicId: '9101', data: input }]);
  assert.ok(!('entryId' in topic.outputs[0].data));
});

test('actual Source defaults use supported read, message, write and Method operations', async () => {
  const cases = [
    [sourceFixture('opcua', [{ id: 'opcua:temp', models: ['publish'], metadata: { nodeClass: 2 } }]), 'read', {}],
    [
      sourceFixture('mqtt', [{ id: 'mqtt:plant/+/temperature', models: ['publish'] }], { mqtt: { qos: 2 } }),
      'message',
      { temperature: 37 },
    ],
    [
      sourceFixture('opcua', [{ id: 'opcua:reset', models: ['request-reply'], metadata: { nodeClass: 4 } }]),
      'invoke',
      { arguments: [] },
    ],
    [
      sourceFixture('opcua', [{ id: 'opcua:setpoint', models: ['subscribe'], metadata: { nodeClass: 2 } }]),
      'write',
      { value: 3 },
    ],
    [
      sourceFixture('modbus-tcp', [{ id: 'modbus:register', models: ['publish', 'subscribe'] }], {
        modbus: { ranges: [{ id: 'register', table: 'holding-register', quantity: 1 }] },
      }),
      'read',
      {},
    ],
    [sourceFixture('mqtt', [{ id: 'mqtt:plant/control', models: ['subscribe'] }]), 'publish', { value: 3 }],
  ];
  for (const [source, entryId, input] of cases) {
    const registered = registerGenerated(initialProgram(source).script);
    const result = await registered.execute(entryId, input);
    assert.deepEqual(registered.sources, [{ id: String(source.id), resources: [source.resources[0].id] }]);
    if (entryId === 'message') {
      assert.deepEqual(result, input);
      assert.equal(registered.entries[0].kind, 'source-event');
      assert.equal(registered.entries[0].qos, 2);
      assert.deepEqual(normalize(registered.entries[0].resourceRefs), ['mqtt:plant/+/temperature']);
      assert.equal(registered.calls.length, 0, 'message handler does not create unsupported MQTT reads');
    } else {
      assert.deepEqual(result, { kind: entryId, value: 37 });
      assert.equal(registered.calls[0].kind, entryId);
      if (entryId !== 'read') assert.deepEqual(registered.calls[0].input, input);
    }
  }
});

test('protocol-aware insertion rejects unsupported operations and honors resource/config boundaries', () => {
  const mqtt = sourceFixture('mqtt', [{ id: 'mqtt:plant/+/telemetry', models: ['publish'] }]);
  assert.deepEqual(normalize(sourceInsertionModes(mqtt, mqtt.resources[0].id)), ['source-event']);
  assert.equal(sourceInsertionSnippet(mqtt, mqtt.resources[0].id, 'read', 'bad'), undefined);
  assert.deepEqual(normalize(sourceInsertionModes(mqtt, 'missing')), []);
  const wildcard = sourceFixture('mqtt', [{ id: 'mqtt:plant/#', models: ['subscribe'] }]);
  assert.deepEqual(normalize(sourceInsertionModes(wildcard, wildcard.resources[0].id)), []);
  const rpc = sourceFixture('mqtt', [{ id: 'mqtt-request-reply', models: ['request-reply'] }]);
  assert.deepEqual(normalize(sourceInsertionModes(rpc, 'mqtt-request-reply')), []);
  rpc.document.mqtt = { requestTopic: 'request', replyTopic: 'reply', correlationField: 'id' };
  assert.deepEqual(normalize(sourceInsertionModes(rpc, 'mqtt-request-reply')), ['invoke']);
  const method = sourceFixture(
    'opcua',
    [{ id: 'opcua:method', models: ['request-reply'], metadata: { nodeClass: 4 } }],
    { role: 'server' }
  );
  assert.deepEqual(normalize(sourceInsertionModes(method, 'opcua:method')), []);
  for (const [table, quantity] of [
    ['input-register', 1],
    ['holding-register', 124],
    ['coil', 1969],
  ]) {
    const modbus = sourceFixture('modbus-tcp', [{ id: 'modbus:x', models: ['subscribe'] }], {
      modbus: { ranges: [{ id: 'x', table, quantity }] },
    });
    assert.deepEqual(normalize(sourceInsertionModes(modbus, 'modbus:x')), []);
  }
});

test('HTTP and OAuth use controlled request handles and Topic references remain opaque', () => {
  assert.deepEqual(normalize(sourceInsertionModes(sourceFixture('http-api', []), '')), ['request']);
  for (const protocol of ['oauth2']) {
    const source = sourceFixture(protocol, [{ id: 'http:GET:/data', models: ['request-reply'] }]);
    assert.deepEqual(normalize(sourceInsertionModes(source, 'http:GET:/data')), ['request']);
    assert.equal(sourceInsertionSnippet(source, 'http:GET:/data', 'read', 'unsupported'), undefined);
    assert.match(sourceInsertionSnippet(source, '', 'request', 'oauth'), /source.request/);
  }
  const uuid = '24bca45a-5e52-46a8-95b5-57d3185d4a2a';
  const ids = normalize(
    manifestIDs({ sources: [{ id: '8101' }], topics: [{ topicId: '372962161192048' }, { topicId: uuid }] })
  );
  assert.deepEqual(ids, { sources: [8101], topics: ['372962161192048', uuid] });
});

test('actual HTTP client and server templates execute one-context controlled requests', async () => {
  const client = registerGenerated(initialProgram(sourceFixture('http-api', [])).script);
  assert.deepEqual(normalize(client.sources), [{ id: '8101', resources: [] }]);
  assert.deepEqual(await client.execute(client.entries[0].id, {}), { status: 200, data: { value: 37 } });
  assert.deepEqual(client.calls, [
    { kind: 'parameters', sourceId: '8101' },
    { kind: 'request', sourceId: '8101', input: { method: 'GET', path: '/data' } },
  ]);
  const server = registerGenerated(initialProgram(sourceFixture('http-api', [], { role: 'server' })).script);
  assert.equal(server.entries[0].kind, 'http-request');
  assert.equal(server.entries[0].method, 'POST');
  assert.equal(server.entries[0].path, '/ingress');
  assert.deepEqual(await server.execute(server.entries[0].id, { body: { value: 9 } }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
    body: { received: { value: 9 } },
  });
  assert.deepEqual(server.calls, [], 'server response does not make an outbound request');
  const samples = JSON.parse(sampleInput({ sources: server.sources, topics: [], entries: server.entries }));
  assert.equal(samples.data.method, 'POST');
  assert.deepEqual(samples.sourceSamples['8101'].secrets, {});
  assert.deepEqual(samples.sourceSamples['8101'].request, { status: 200, headers: {}, data: {} });
});

test('actual insertion blocks support multiple Sources and every trigger with one-context callbacks', async () => {
  const opc = sourceFixture('opcua', [{ id: 'opcua:temp', models: ['publish'], metadata: { nodeClass: 2 } }]);
  const mqtt = sourceFixture(
    'mqtt',
    [{ id: 'mqtt:plant/+/telemetry', models: ['publish'] }],
    { mqtt: { qos: 1 } },
    8102
  );
  let doc = initialProgram();
  for (const snippet of [
    sourceInsertionSnippet(opc, 'opcua:temp', 'read', 'read-opc'),
    sourceInsertionSnippet(mqtt, 'mqtt:plant/+/telemetry', 'source-event', 'mqtt-event'),
    entryInsertionSnippet('request', [], 'manual'),
    entryInsertionSnippet('interval', [], 'timer'),
    entryInsertionSnippet('uns-event', ['9101'], 'topic-event'),
    topicInsertionSnippet(['9102', '9103'], 'writes'),
  ])
    doc = appendProgramSnippet(doc, snippet);
  const registered = registerGenerated(doc.script);
  assert.deepEqual(
    registered.sources.map((source) => source.id),
    ['8101', '8102']
  );
  const input = { value: 7 };
  for (const id of ['request', 'mqtt-event', 'manual', 'timer', 'topic-event', 'writes']) {
    assert.deepEqual(await registered.execute(id, input), input);
  }
  assert.deepEqual(await registered.execute('read-opc', input), { kind: 'read', value: 37 });
  assert.deepEqual(registered.outputs, [
    { topicId: '9102', data: input },
    { topicId: '9103', data: input },
  ]);
  assert.equal(registered.entries.find((entry) => entry.id === 'timer').intervalMs, 5000);
  assert.equal(registered.entries.find((entry) => entry.id === 'mqtt-event').qos, 1);
});

test('actual repeated Source and Topic insertions reuse references without changing existing code', async () => {
  const source = sourceFixture('opcua', [
    { id: 'opcua:temp', models: ['publish'], metadata: { nodeClass: 2 } },
    { id: 'opcua:pressure', models: ['publish'], metadata: { nodeClass: 2 } },
  ]);
  let doc = initialProgram();
  for (const snippet of [
    sourceInsertionSnippet(source, 'opcua:temp', 'read', 'temperature'),
    sourceInsertionSnippet(source, 'opcua:pressure', 'read', 'pressure'),
    entryInsertionSnippet('uns-event', ['9101'], 'topic-event'),
    topicInsertionSnippet(['9101'], 'topic-write'),
  ])
    doc = appendProgramSnippet(doc, snippet);
  const registered = registerGenerated(doc.script);
  assert.deepEqual(registered.sources, [{ id: '8101', resources: ['opcua:temp', 'opcua:pressure'] }]);
  assert.deepEqual(registered.topics, [{ topicId: '9101' }]);
  for (const id of ['temperature', 'pressure'])
    assert.deepEqual(await registered.execute(id, {}), { kind: 'read', value: 37 });
  const input = { value: 7 };
  assert.deepEqual(await registered.execute('topic-event', input), input);
  assert.deepEqual(await registered.execute('topic-write', input), input);
  assert.deepEqual(registered.outputs, [{ topicId: '9101', data: input }]);
  const duplicatedEntry = appendProgramSnippet(initialProgram(), entryInsertionSnippet('request', [], 'request'));
  assert.throws(() => registerGenerated(duplicatedEntry.script), /duplicate entry rejected/);
});

test('reused Source handles retain their own resource boundary; Topic handles are cached', async () => {
  const script = `module.exports = function(api) {
    const first = api.sources.use({ id: '8101', resources: ['opcua:temp'] });
    api.sources.use({ id: '8101', resources: ['opcua:pressure'] });
    const firstTopic = api.uns.use({ topicId: '9101' });
    const nextTopic = api.uns.use({ topicId: '9101' });
    if (firstTopic !== nextTopic) throw Error('Topic handles must be cached');
    api.stream.onRequest({ id: 'forbidden' }, async function(context) {
      return await first.read('opcua:pressure');
    });
  };`;
  const registered = registerGenerated(script);
  assert.deepEqual(registered.sources, [{ id: '8101', resources: ['opcua:temp', 'opcua:pressure'] }]);
  await assert.rejects(() => registered.execute('forbidden', {}), /explicitly declared on this handle/);
});

// Export exactly the production-generated scripts for the real isolated Goja executor.
// No credentials, device operations or fixture files are written by this command.
if (exportTemplates) {
  const opc = sourceFixture('opcua', [
    { id: 'opcua:temp', models: ['publish'], metadata: { nodeClass: 2 } },
    { id: 'opcua:pressure', models: ['publish'], metadata: { nodeClass: 2 } },
  ]);
  const mqtt = sourceFixture(
    'mqtt',
    [{ id: 'mqtt:plant/+/telemetry', models: ['publish'] }],
    { mqtt: { qos: 2 } },
    8102
  );
  let repeated = initialProgram();
  for (const snippet of [
    sourceInsertionSnippet(opc, 'opcua:temp', 'read', 'temperature'),
    sourceInsertionSnippet(opc, 'opcua:pressure', 'read', 'pressure'),
    sourceInsertionSnippet(mqtt, 'mqtt:plant/+/telemetry', 'source-event', 'message'),
    entryInsertionSnippet('request', [], 'manual'),
    entryInsertionSnippet('interval', [], 'timer'),
    entryInsertionSnippet('uns-event', ['9101'], 'topic-event'),
    topicInsertionSnippet(['9101', '9102'], 'topic-write'),
  ])
    repeated = appendProgramSnippet(repeated, snippet);
  const cases = [
    { name: 'zero-source-request', document: initialProgram() },
    { name: 'topic-default', document: initialProgram(undefined, 9101) },
    { name: 'opc-default', document: initialProgram(opc) },
    { name: 'mqtt-default', document: initialProgram(mqtt) },
    {
      name: 'opc-write-default',
      document: initialProgram(
        sourceFixture('opcua', [{ id: 'opcua:setpoint', models: ['subscribe'], metadata: { nodeClass: 2 } }])
      ),
    },
    {
      name: 'opc-method-default',
      document: initialProgram(
        sourceFixture('opcua', [{ id: 'opcua:reset', models: ['request-reply'], metadata: { nodeClass: 4 } }])
      ),
      input: { arguments: [] },
    },
    {
      name: 'mqtt-publish-default',
      document: initialProgram(sourceFixture('mqtt', [{ id: 'mqtt:plant/control', models: ['subscribe'] }])),
    },
    {
      name: 'mqtt-request-reply-default',
      document: initialProgram(
        sourceFixture('mqtt', [{ id: 'mqtt-request-reply', models: ['request-reply'] }], {
          mqtt: { requestTopic: 'request', replyTopic: 'reply', correlationField: 'id' },
        })
      ),
    },
    {
      name: 'modbus-read-default',
      document: initialProgram(
        sourceFixture('modbus-tcp', [{ id: 'modbus:register', models: ['publish'] }], {
          modbus: { ranges: [{ id: 'register', table: 'holding-register', quantity: 1 }] },
        })
      ),
    },
    {
      name: 'modbus-write-default',
      document: initialProgram(
        sourceFixture('modbus-tcp', [{ id: 'modbus:register', models: ['subscribe'] }], {
          modbus: { ranges: [{ id: 'register', table: 'holding-register', quantity: 1 }] },
        })
      ),
      input: { values: [7] },
    },
    { name: 'reused-source-topic-multiple-entries', document: repeated },
    { name: 'http-client-default', document: initialProgram(sourceFixture('http-api', [])) },
    {
      name: 'http-server-default',
      document: initialProgram(sourceFixture('http-api', [], { role: 'server' })),
      input: { body: { value: 7 } },
    },
  ];
  Promise.all(
    cases.map(async ({ name, document, input: suppliedInput }) => {
      const registered = registerGenerated(document.script);
      const executions = [];
      for (const entry of registered.entries) {
        registered.calls.length = 0;
        registered.outputs.length = 0;
        const input = suppliedInput ?? { value: 7 };
        executions.push({
          entryId: entry.id,
          input,
          expectedData: await registered.execute(entry.id, input),
          expectedCalls: normalize(registered.calls),
          expectedOutputs: normalize(registered.outputs),
        });
      }
      return { name, document, expectedSources: registered.sources, expectedTopics: registered.topics, executions };
    })
  )
    .then((cases) =>
      process.stdout.write(
        JSON.stringify(
          { scope: 'generated-template-goja-fixture-not-live-device', cases },
          null,
          2
        ) + '\n'
      )
    )
    .catch(() => {
      process.exitCode = 1;
    });
}

// 编译编辑器实际展示的类型参考，不用字符串匹配代替函数签名验证。
function sdkDiagnostics(code) {
  const sdk = path.resolve(__dirname, '../src/pages/uns/components/source-stream/stream-sdk.d.ts');
  const example = path.join(path.dirname(sdk), 'sdk-contract-example.ts');
  const options = {
    strict: true,
    noEmit: true,
    skipLibCheck: true,
    types: [],
    target: ts.ScriptTarget.ES2022,
    module: ts.ModuleKind.ESNext,
    moduleResolution: ts.ModuleResolutionKind.Bundler,
  };
  const host = ts.createCompilerHost(options);
  const readFile = host.readFile.bind(host);
  const getSourceFile = host.getSourceFile.bind(host);
  const fileExists = host.fileExists.bind(host);
  const isExample = (name) => path.normalize(name) === path.normalize(example);
  host.readFile = (name) => (isExample(name) ? code : readFile(name));
  host.fileExists = (name) => isExample(name) || fileExists(name);
  host.getSourceFile = (name, languageVersion, ...args) =>
    isExample(name)
      ? ts.createSourceFile(name, code, languageVersion, true)
      : getSourceFile(name, languageVersion, ...args);
  return ts.getPreEmitDiagnostics(ts.createProgram([sdk, example], options, host));
}
test('SDK reference compiles one context argument and every injected Source/UNS/entry method', () => {
  const diagnostics = sdkDiagnostics(`
    import type { ProgramContext, StreamAPI } from './stream-sdk';
    declare const api: StreamAPI;
    const source = api.sources.use({ id: '8101', resources: ['opcua:ns=2;s=a'] });
    const topic = api.uns.use({ topicId: '9101' });
    const handler = async (context: ProgramContext) => {
      const input: unknown = context.input;
      const entryId: string = context.entryId;
      const dryRun: boolean | undefined = context.dryRun;
      const metadata: unknown = context.trace;
      await source.read('opcua:ns=2;s=a', { mode: 'read' });
      await source.write('opcua:ns=2;s=a', input, {});
      await source.invoke('opcua:ns=2;s=a', input, {});
      await source.publish('opcua:ns=2;s=a', input, {});
      const parameters = await source.parameters();
      const secrets = await source.secrets();
      const response = await source.request({ method: 'POST', path: '/data', headers: { 'X-Signature': secrets.signingKey }, query: { key: 'value' }, json: parameters });
      const status: number = response.status;
      const headers: Record<string, string[]> = response.headers;
      await topic.read({});
      const buffered = await topic.write(input);
      const stage: 'buffered' = buffered.stage;
      const topicId: string | undefined = buffered.topicId;
      api.log.info(entryId); api.log.warn(stage); if (topicId) api.log.error(topicId); api.log.debug('test');
      return { input, dryRun, metadata };
    };
    api.stream.every({ id: 'interval', intervalMs: 1000 }, handler);
    api.stream.onSource({ id: 'source', source, resourceRefs: ['opcua:ns=2;s=a'], qos: 2 }, handler);
    api.stream.onTopic({ id: 'topic', topics: [topic] }, handler);
    api.stream.onTimer({ id: 'timer', intervalMs: 1000 }, handler);
    api.stream.onMessage({ id: 'message', source, resourceRefs: ['opcua:ns=2;s=a'], qos: 2 }, handler);
    api.stream.onEvent({ id: 'event', topics: [topic] }, handler);
    api.stream.onRequest({ id: 'request' }, async ({ input, entryId, dryRun }) => ({ input, entryId, dryRun }));
    api.stream.onRequest({ id: 'published', request: { code: 'fixture.read', name: 'Read', contractVersion: '1', inputSchema: {}, outputSchema: {}, sideEffect: 'read', idempotency: 'optional', timeoutMs: 30000 } }, context => context.input);
    api.stream.onRequest({ id: 'webhook', source, method: 'POST', path: '/orders/:id' }, async context => ({ status: 200, body: context.request?.body }));
  `);
  assert.deepEqual(
    diagnostics.map((diagnostic) => ts.flattenDiagnosticMessageText(diagnostic.messageText, '\n')),
    []
  );
});
test('SDK reference rejects the old two-argument callback and undeclared runtime APIs', () => {
  const oldHandler = sdkDiagnostics(`
    import type { StreamAPI } from './stream-sdk';
    declare const api: StreamAPI;
    api.stream.onRequest({ id: 'request' }, (input: unknown, context: Record<string, unknown>) => input);
  `);
  assert.ok(
    oldHandler.some((diagnostic) => [2345, 2769].includes(diagnostic.code)),
    'two-argument handler is incompatible'
  );
  const unsupported = sdkDiagnostics(`
    import type { StreamAPI } from './stream-sdk';
    declare const api: StreamAPI;
    api.fetch('https://example.invalid');
    api.stream.onRequest({ id: 'request', sourceId: '8101' }, context => context.input);
  `);
  assert.ok(
    unsupported.some((diagnostic) => diagnostic.code === 2339),
    'no injected network API'
  );
  assert.ok(
    unsupported.some((diagnostic) => [2353, 2769].includes(diagnostic.code)),
    'no Source owner on a request entry'
  );
});

test('HTTP and crypto type reference allows restricted signatures and rejects ambiguous body/unsupported modules', () => {
  const valid = sdkDiagnostics(`
    import { require, type StreamAPI } from './stream-sdk';
    declare const api: StreamAPI;
    const source = api.sources.use({ id: '8101', resources: [] });
    api.stream.onRequest({ id: 'http' }, async context => {
      const secrets = await source.secrets();
      const signature = require('node:crypto').createHmac('sha256', secrets.signingKey).update(JSON.stringify(context.input)).digest('hex');
      return source.request({ method: 'POST', path: '/', headers: { 'X-Signature': signature }, json: context.input });
    });
  `);
  assert.deepEqual(
    valid.map((d) => ts.flattenDiagnosticMessageText(d.messageText, '\n')),
    []
  );
  const invalid = sdkDiagnostics(`
    import { require, type StreamAPI } from './stream-sdk';
    declare const api: StreamAPI;
    require('node:fs');
    require('node:crypto').createHash('md5');
    api.sources.use({ id: '8101', resources: [] }).request({ method: 'POST', path: '/', body: 'x', json: {} });
  `);
  assert.ok(invalid.filter((d) => d.category === ts.DiagnosticCategory.Error).length >= 3);
});

// Optional real React 18 lifecycle checks with an EXISTING private JSDOM fixture.
// NODE_PATH=<fixture>/node_modules TIER0_STREAM_REACT_FIXTURE=1 node --test scripts/check-stream-program.cjs
// Components use isolated API/UI doubles: these checks are not protocol or deployed-browser E2E.
function loadReactComponent(filename, resolveDependency) {
  const output = ts.transpileModule(fs.readFileSync(filename, 'utf8'), {
    compilerOptions: {
      target: ts.ScriptTarget.ES2022,
      module: ts.ModuleKind.CommonJS,
      jsx: ts.JsxEmit.ReactJSX,
      esModuleInterop: true,
    },
  }).outputText;
  const loaded = { exports: {} };
  vm.runInNewContext(output, { module: loaded, exports: loaded.exports, require: resolveDependency }, { filename });
  return loaded.exports;
}

function executionFixture(respond) {
  const React = appRequire('react');
  const { createRoot } = appRequire('react-dom/client');
  const { JSDOM } = require('jsdom');
  assert.match(React.version, /^18\./);
  const dom = new JSDOM('<!doctype html><html><body></body></html>');
  global.window = dom.window;
  global.document = dom.window.document;
  global.IS_REACT_ACT_ENVIRONMENT = true;
  const element = React.createElement;
  const calls = [];
  const defaultModule = (value) => ({ __esModule: true, default: value });
  const target = path.resolve(__dirname, '../src/pages/uns/components/source-stream/ProgramExecutionModal.tsx');
  const output = ts.transpileModule(fs.readFileSync(target, 'utf8'), {
    compilerOptions: {
      target: ts.ScriptTarget.ES2022,
      module: ts.ModuleKind.CommonJS,
      jsx: ts.JsxEmit.ReactJSX,
      esModuleInterop: true,
    },
  }).outputText;
  const component = { exports: {} };
  vm.runInNewContext(
    output,
    {
      module: component,
      exports: component.exports,
      require: function componentRequire(id) {
        if (id === 'react' || id === 'react/jsx-runtime') return appRequire(id);
        if (id === './StreamTestResultView' || id === './StreamEditorStep')
          return loadReactComponent(path.resolve(path.dirname(target), id + '.tsx'), componentRequire);
        if (id === 'antd')
          return {
            App: { useApp: () => ({ modal: {} }) },
            Alert: (props) => element('div', null, props.message),
            Tabs: (props) =>
              element(
                'div',
                null,
                props.items.map((item) => element('section', { key: item.key, 'data-panel': item.key }, item.children))
              ),
            Collapse: (props) =>
              element(
                'div',
                null,
                props.items.map((item) => element('section', { key: item.key, 'data-panel': item.key }, item.children))
              ),
            Descriptions: ({ items }) =>
              element(
                'dl',
                null,
                items.map((item) => element('div', { key: item.key }, item.label, item.children))
              ),
            Tag: (props) => element('span', null, props.children),
            Empty: Object.assign((props) => element('span', null, props.description), {
              PRESENTED_IMAGE_SIMPLE: 'empty',
            }),
            Select: (props) =>
              element(
                'select',
                {
                  'aria-label': props['aria-label'],
                  value: props.value ?? '',
                  onChange: (event) => props.onChange(event.target.value),
                },
                element('option', { value: '' }, props.placeholder),
                ...props.options.map((option) =>
                  element('option', { key: option.value, value: option.value }, option.label)
                )
              ),
          };
        if (id === 'uuid') return { v4: () => 'fixture-request-id' };
        if (id === '@codemirror/lang-json') return { json: () => ({}) };
        if (id === '@/hooks') return { useTranslate: () => (key) => key };
        if (id === '@/apis/core-api/source-stream')
          return {
            sourceStreamApi: {
              test: async (...args) => {
                calls.push({ kind: 'test', args });
                return respond(...args);
              },
              invoke: async (...args) => {
                calls.push({ kind: 'invoke', args });
                return respond(...args);
              },
            },
          };
        if (id === '@/components/pro-modal')
          return defaultModule((props) =>
            props.open
              ? element(
                  'div',
                  null,
                  props.children,
                  element(
                    'button',
                    { 'data-run': true, disabled: props.okButtonProps.disabled, onClick: props.onOk },
                    props.okText
                  )
                )
              : null
          );
        if (id === '@/components/pro-codemirror')
          return defaultModule((props) =>
            element('textarea', { value: props.value, onChange: (event) => props.onChange(event.target.value) })
          );
        if (id === '@/components/help-tooltip') return defaultModule((props) => element('span', null, props.title));
        if (id === '@/components/confirm-modal') return { openConfirmModal: (_modal, config) => config.onOk() };
        if (id === './resource-links')
          return { TopicLink: (props) => element('span', { 'data-topic': props.id }, `Topic ${props.id}`) };
        if (id === './program-document') return { sampleInput };
        if (id === './index.module.scss') return defaultModule({ section: 'section', code: 'code' });
        throw Error(`Unexpected execution component dependency: ${id}`);
      },
    },
    { filename: target }
  );
  const container = global.document.createElement('div');
  const root = createRoot(container);
  return {
    calls,
    container,
    render: (props) => React.act(() => root.render(element(component.exports.default, props))),
    select: async (id) =>
      React.act(async () => {
        const select = container.querySelector('select');
        select.value = id;
        select.dispatchEvent(new dom.window.Event('change', { bubbles: true }));
      }),
    run: async () =>
      React.act(async () => {
        container.querySelector('[data-run]').click();
        await Promise.resolve();
      }),
    act: React.act,
    close: () => {
      React.act(() => root.unmount());
      dom.window.close();
    },
  };
}

function editorFixture(sources) {
  const React = appRequire('react');
  const { createRoot } = appRequire('react-dom/client');
  const { JSDOM } = require('jsdom');
  assert.match(React.version, /^18\./);
  const dom = new JSDOM('<!doctype html><html><body></body></html>');
  global.window = dom.window;
  global.document = dom.window.document;
  global.IS_REACT_ACT_ENVIRONMENT = true;
  const element = React.createElement;
  const defaultModule = (value) => ({ __esModule: true, default: value });
  const select = (props) =>
    element(
      'select',
      {
        'aria-label': props['aria-label'],
        disabled: props.disabled,
        value: props.value ?? '',
        onChange: (event) => {
          const option = (props.options ?? []).find((item) => String(item.value) === event.target.value);
          props.onChange(option?.value);
        },
      },
      element('option', { value: '' }, props.placeholder),
      ...(props.options ?? []).map((option) =>
        element('option', { key: option.value, value: option.value }, option.label)
      )
    );
  const input = (props) =>
    element('input', { value: props.value ?? '', readOnly: props.readOnly, onChange: props.onChange });
  input.TextArea = (props) =>
    element('textarea', { value: props.value ?? '', readOnly: props.readOnly, onChange: props.onChange });
  const target = path.resolve(__dirname, '../src/pages/uns/components/source-stream/ProgramConfigEditor.tsx');
  const output = ts.transpileModule(fs.readFileSync(target, 'utf8'), {
    compilerOptions: {
      target: ts.ScriptTarget.ES2022,
      module: ts.ModuleKind.CommonJS,
      jsx: ts.JsxEmit.ReactJSX,
      esModuleInterop: true,
    },
  }).outputText;
  const component = { exports: {} };
  let uuid = 0;
  vm.runInNewContext(
    output,
    {
      module: component,
      exports: component.exports,
      require: function componentRequire(id) {
        if (id === 'react' || id === 'react/jsx-runtime') return appRequire(id);
        if (id === 'uuid') return { v4: () => `fixture-${++uuid}` };
        if (id === './StreamTestResultView' || id === './StreamEditorStep')
          return loadReactComponent(path.resolve(path.dirname(target), id + '.tsx'), componentRequire);
        if (id === 'antd')
          return {
            App: { useApp: () => ({ message: { warning() {} } }) },
            Select: select,
            Alert: (props) => element('div', { 'data-alert': props.type }, props.message),
            Input: input,
            InputNumber: (props) =>
              element('input', {
                type: 'number',
                value: props.value,
                disabled: props.disabled,
                onChange: (event) => props.onChange(Number(event.target.value)),
              }),
            Collapse: (props) =>
              element(
                'div',
                null,
                props.items.map((item) => element('section', { key: item.key, 'data-panel': item.key }, item.children))
              ),
            Tabs: (props) =>
              element(
                'div',
                null,
                props.items.map((item) => element('section', { key: item.key, 'data-panel': item.key }, item.children))
              ),
            Pagination: () => null,
            Segmented: () => null,
            Space: (props) => element('div', null, props.children),
          };
        if (id === '@codemirror/lang-json') return { json: () => ({}) };
        if (id === '@codemirror/lang-javascript') return { javascript: () => ({}) };
        if (id === '@codemirror/view') return { EditorView: { lineWrapping: {} } };
        if (id === '@/common-types/button-permission') return { ButtonPermission: { 'uns.sources': 'source-read' } };
        if (id === '@/utils/auth') return { hasPermission: () => true };
        if (id === '@/apis/core-api/source-stream') return { sourceStreamApi: {} };
        if (id === '@/hooks')
          return {
            useTranslate: () => (key) => key,
            usePagination: () => ({
              data: sources,
              loading: false,
              pagination: { page: 1, pageSize: 20, total: sources.length, onChange() {} },
              setSearchParams() {},
              refreshRequest() {},
            }),
          };
        if (id === '@/utils/limits') return { MAX_LENGTHS: { name: 128, description: 1024 } };
        if (id === '@/components/com-button')
          return defaultModule((props) =>
            element('button', { disabled: props.disabled, onClick: props.onClick }, props.children)
          );
        if (id === '@/components/help-tooltip') return defaultModule((props) => element('span', null, props.title));
        if (id === '@/components/pro-codemirror')
          return defaultModule((props) =>
            element('textarea', {
              value: props.value,
              readOnly: props.readOnly,
              onChange: (event) => props.onChange(event.target.value),
            })
          );
        if (id === '../use-create-modal/components/SearchSelect')
          return defaultModule((props) =>
            element(
              'select',
              {
                'aria-label': 'fixture-topics',
                multiple: true,
                value: props.value,
                disabled: props.disabled,
                onChange: (event) => props.onChange([...event.target.selectedOptions].map((option) => option.value)),
              },
              ...['9101', '9102'].map((topicId) => element('option', { key: topicId, value: topicId }, topicId))
            )
          );
        if (id === './SourcePicker')
          return defaultModule((props) =>
            element(
              'select',
              {
                'aria-label': 'SourceStream.insertSource',
                value: '',
                onChange: (event) => props.onSelect(sources.find((source) => String(source.id) === event.target.value)),
              },
              element('option', { value: '' }),
              sources.map((source) =>
                element(
                  'option',
                  {
                    key: source.id,
                    value: source.id,
                    disabled: !!props.disabledReason?.(source),
                  },
                  source.name
                )
              )
            )
          );
        if (id === './VendorProgramExample') return defaultModule(() => null);
        if (id === './program-document') return fixture.exports;
        if (id === './ProgramManifestView') return defaultModule(() => null);
        if (id === './program-format') return { formatProgramScript: (code) => ({ code, valid: true }) };
        if (id === './stream-sdk.d.ts?raw')
          return defaultModule(fs.readFileSync(path.join(path.dirname(target), 'stream-sdk.d.ts'), 'utf8'));
        if (id === './index.module.scss')
          return defaultModule({ section: 'section', field: 'field', 'mapping-workbench': 'mapping-workbench' });
        throw Error(`Unexpected editor component dependency: ${id}`);
      },
    },
    { filename: target }
  );
  const container = global.document.createElement('div');
  const root = createRoot(container);
  let doc = normalize(initialProgram());
  let props = {};
  const render = () =>
    root.render(
      element(component.exports.default, {
        ...props,
        value: JSON.stringify(doc),
        onChange: (raw) => {
          doc = JSON.parse(raw);
          render();
        },
      })
    );
  React.act(render);
  return {
    container,
    document: () => doc,
    render: (next) =>
      React.act(() => {
        props = next;
        render();
      }),
    select: async (label, value) => {
      if (label === 'SourceStream.insertSource') {
        await React.act(async () => {
          const open = [...container.querySelectorAll('button')].find(
            (button) =>
              button.textContent === 'SourceStream.chooseSource' ||
              sources.some((source) => source.name === button.textContent)
          );
          assert.ok(open, 'Source picker entry');
          open.click();
        });
      }
      await React.act(async () => {
        const target = container.querySelector(`select[aria-label="${label}"]`);
        assert.ok(target, label);
        if (Array.isArray(value)) {
          for (const option of target.options) option.selected = value.includes(option.value);
        } else target.value = String(value);
        target.dispatchEvent(new dom.window.Event('change', { bubbles: true }));
      });
    },
    click: async (label) =>
      React.act(async () => {
        const button = [...container.querySelectorAll('button')].find((item) => item.textContent === label);
        assert.ok(button, label);
        assert.equal(button.disabled, false, `${label} enabled`);
        button.click();
      }),
    close() {
      React.act(() => root.unmount());
      dom.window.close();
    },
  };
}
const reactChecks = process.env.TIER0_STREAM_REACT_FIXTURE === '1';
test(
  'React18 real editor insertions execute business input using the single-context contract',
  { skip: !reactChecks },
  async () => {
    const mqtt = sourceFixture('mqtt', [{ id: 'mqtt:plant/+/telemetry', models: ['publish'] }], { mqtt: { qos: 2 } });
    const view = editorFixture([mqtt]);
    try {
      await view.select('SourceStream.insertSource', mqtt.id);
      await view.select('SourceStream.nativeResource', 'mqtt:plant/+/telemetry');
      const modes = view.container.querySelector('select[aria-label="SourceStream.programInsertion"]');
      assert.deepEqual(
        [...modes.options].filter((option) => option.value).map((option) => option.value),
        ['source-event']
      );
      await view.click('SourceStream.insertSource');
      await view.select('SourceStream.trigger', 'interval');
      await view.click('SourceStream.insertEntry');
      await view.select('fixture-topics', ['9101', '9102']);
      await view.click('SourceStream.insertOutputs');
      const registered = registerGenerated(view.document().script);
      const input = { value: 4 };
      for (const entry of registered.entries) assert.deepEqual(await registered.execute(entry.id, input), input);
      assert.equal(registered.entries.find((entry) => entry.kind === 'source-event').qos, 2);
      assert.equal(registered.calls.length, 0);
      assert.deepEqual(registered.outputs, [
        { topicId: '9101', data: input },
        { topicId: '9102', data: input },
      ]);
      assert.match(view.container.textContent, /SourceStream.sdkBoundary/);
    } finally {
      view.close();
    }
  }
);
test(
  'React18 editor can reuse a validated Source with another resource and the same Topic for event/write',
  { skip: !reactChecks },
  async () => {
    const source = sourceFixture('opcua', [
      { id: 'opcua:temp', models: ['publish'], metadata: { nodeClass: 2 } },
      { id: 'opcua:pressure', models: ['publish'], metadata: { nodeClass: 2 } },
    ]);
    const view = editorFixture([source]);
    try {
      await view.select('SourceStream.insertSource', source.id);
      await view.select('SourceStream.nativeResource', 'opcua:temp');
      await view.click('SourceStream.insertSource');
      view.render({
        manifest: { sources: [{ id: String(source.id), resources: ['opcua:temp'] }], topics: [], entries: [] },
      });
      await view.select('SourceStream.nativeResource', 'opcua:pressure');
      await view.click('SourceStream.insertSource');
      await view.select('fixture-topics', ['9101']);
      await view.select('SourceStream.trigger', 'uns-event');
      await view.click('SourceStream.insertEntry');
      await view.click('SourceStream.insertOutputs');
      const registered = registerGenerated(view.document().script);
      assert.deepEqual(registered.sources, [{ id: String(source.id), resources: ['opcua:temp', 'opcua:pressure'] }]);
      assert.deepEqual(registered.topics, [{ topicId: '9101' }]);
      const input = { value: 5 };
      for (const entry of registered.entries) await registered.execute(entry.id, input);
      assert.deepEqual(
        registered.calls.map((call) => call.ref),
        ['opcua:temp', 'opcua:pressure']
      );
      assert.deepEqual(registered.outputs, [{ topicId: '9101', data: input }]);
      assert.equal(new Set(registered.entries.map((entry) => entry.id)).size, registered.entries.length);
    } finally {
      view.close();
    }
  }
);
test(
  'React18 HTTP insertion needs no discovered resource and preserves independent client/server entries',
  { skip: !reactChecks },
  async () => {
    const client = sourceFixture('http-api', [], {}, 8111);
    const server = sourceFixture('http-api', [], { role: 'server' }, 8112);
    const view = editorFixture([client, server]);
    try {
      await view.select('SourceStream.insertSource', client.id);
      assert.equal(view.container.querySelector('select[aria-label="SourceStream.nativeResource"]'), null);
      await view.click('SourceStream.insertSource');
      await view.select('SourceStream.insertSource', server.id);
      await view.click('SourceStream.insertSource');
      const registered = registerGenerated(view.document().script);
      assert.deepEqual(registered.sources, [
        { id: '8111', resources: [] },
        { id: '8112', resources: [] },
      ]);
      const httpEntry = registered.entries.find((entry) => entry.kind === 'http-request');
      assert.ok(httpEntry);
      assert.deepEqual(await registered.execute(httpEntry.id, { body: { value: 4 } }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
        body: { received: { value: 4 } },
      });
      assert.equal(new Set(registered.entries.map((entry) => entry.id)).size, registered.entries.length);
    } finally {
      view.close();
    }
  }
);
test(
  'React18 editor refuses unsupported Source insertion and keeps read-only controls disabled',
  { skip: !reactChecks },
  async () => {
    const unsupported = sourceFixture('mqtt', [{ id: 'mqtt:plant/#', models: ['subscribe'] }]);
    const view = editorFixture([unsupported]);
    try {
      await view.select('SourceStream.insertSource', unsupported.id);
      await view.select('SourceStream.nativeResource', 'mqtt:plant/#');
      assert.match(view.container.textContent, /SourceStream.noInsertion/);
      const button = [...view.container.querySelectorAll('button')].find(
        (item) => item.textContent === 'SourceStream.insertSource'
      );
      assert.equal(button.disabled, true);
      assert.equal(view.document().script, initialProgram().script);
      view.render({ readOnly: true });
      assert.equal(view.container.querySelector('select[aria-label="SourceStream.insertSource"]'), null);
      for (const button of view.container.querySelectorAll('button')) assert.equal(button.disabled, true);
    } finally {
      view.close();
    }
  }
);
test(
  'React18 multiple entries require selection and preserve repeated Topic outputs',
  { skip: !reactChecks },
  async () => {
    const document = normalize(initialProgram());
    const manifest = {
      sources: [],
      topics: [{ topicId: '9101' }],
      entries: [
        { id: 'first', kind: 'interval' },
        { id: 'second', kind: 'request' },
      ],
    };
    const view = executionFixture(async () => ({
      result: { data: { converted: true }, logs: [], durationMs: 2 },
      targets: [
        { topicId: '9101', data: { value: 1 } },
        { topicId: '9101', data: { value: 2 } },
      ],
    }));
    try {
      view.render({ mode: 'test', document, manifest, onClose() {} });
      assert.equal(view.container.querySelector('[data-run]').disabled, true);
      await view.select('second');
      await view.run();
      assert.equal(view.calls[0].args[4], 'second');
      assert.deepEqual(normalize(view.calls[0].args[1]), {
        data: {},
        sourceSamples: {},
        topicSamples: { 9101: {} },
        checkpointSample: null,
      });
      assert.equal(view.container.querySelectorAll('[data-topic]').length, 2);
      assert.match(view.container.textContent, /converted/);
    } finally {
      view.close();
    }
  }
);
test(
  'React18 close/object change rejects late results; invoke lists request entries only',
  { skip: !reactChecks },
  async () => {
    let resolve;
    const held = new Promise((done) => {
      resolve = done;
    });
    const document = normalize(initialProgram());
    const manifest = {
      sources: [],
      topics: [],
      entries: [
        { id: 'timer', kind: 'interval' },
        { id: 'request', kind: 'request' },
      ],
    };
    const view = executionFixture(() => held);
    try {
      view.render({ mode: 'test', document, manifest, onClose() {} });
      await view.select('timer');
      await view.run();
      view.render({ mode: undefined, document, manifest, onClose() {} });
      await view.act(async () =>
        resolve({ result: { data: 'late-sensitive-sample', logs: [], durationMs: 1 }, targets: null })
      );
      view.render({ mode: 'invoke', document, manifest, stream: { id: 2, activeVersion: 7 }, onClose() {} });
      assert.equal(view.container.querySelector('select'), null, 'single request entry needs no selector');
      assert.doesNotMatch(view.container.textContent, /late-sensitive-sample/);
      assert.equal(view.container.querySelector('[data-run]').disabled, false);
      await view.run();
      assert.equal(view.calls.at(-1).kind, 'invoke');
      assert.equal(view.calls.at(-1).args[4], 'request');
    } finally {
      view.close();
    }
  }
);

test(
  'React18 test receipts show sampled payload and metadata instead of manual input, including falsy payloads',
  { skip: !reactChecks },
  async () => {
    const document = normalize(initialProgram());
    const manifest = { sources: [], topics: [], entries: [{ id: 'request', kind: 'request' }] };
    for (const payload of [{ temperature: 21 }, null, false, 0, '']) {
      const sampled = { resourceRef: 'mqtt:plant/one/temperature', data: payload, receivedAt: 1791600000000 };
      const view = executionFixture(async () => ({
        input: sampled,
        result: { data: payload, logs: [], durationMs: 1 },
        targets: null,
      }));
      try {
        view.render({ mode: 'test', document, manifest, onClose() {} });
        await view.run();
        const panel = view.container.querySelector('[data-panel="input"]');
        assert.deepEqual(JSON.parse(panel.querySelector('pre').textContent), payload);
        assert.match(panel.textContent, /mqtt:plant\/one\/temperature/);
        assert.ok(panel.textContent.includes(new Date(sampled.receivedAt).toLocaleString()));
      } finally {
        view.close();
      }
    }
    const view = executionFixture(async () => ({ result: { data: 42, logs: [], durationMs: 1 }, targets: null }));
    try {
      view.render({ mode: 'test', document, manifest, onClose() {} });
      await view.run();
      const panel = view.container.querySelector('[data-panel="input"]');
      assert.deepEqual(JSON.parse(panel.querySelector('pre').textContent), normalize(view.calls[0].args[1]));
      assert.equal(panel.querySelector('dl'), null);
    } finally {
      view.close();
    }
  }
);
