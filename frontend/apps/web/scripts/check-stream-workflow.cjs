// Pure compiler contracts. Test fixtures never connect to devices or write databases.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const ts = require('typescript');
const file = path.resolve(__dirname, '../src/pages/uns/components/source-stream/program-workflow.ts');
function loadTypescript(file, dependencies = {}) {
  const fixture = { exports: {} };
  vm.runInNewContext(
    ts.transpileModule(fs.readFileSync(file, 'utf8'), {
      compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
    }).outputText,
    {
      module: fixture,
      exports: fixture.exports,
      require: (name) => {
        assert.ok(Object.hasOwn(dependencies, name), `Unexpected dependency: ${name}`);
        return dependencies[name];
      },
    }
  );
  return fixture;
}
const moduleFixture = loadTypescript(file);
const readmeHelpers = loadTypescript(path.join(path.dirname(file), 'stream-readme.ts'), {
  './program-workflow': moduleFixture.exports,
}).exports;
const connectionHelpers = loadTypescript(path.join(path.dirname(file), 'workflow-connections.ts'), {
  './program-workflow': moduleFixture.exports,
  './program-document': loadTypescript(path.join(path.dirname(file), 'program-document.ts')).exports,
}).exports;
const workflowData = loadTypescript(path.join(path.dirname(file), 'workflow-data.ts'), {
  './program-workflow': moduleFixture.exports,
});
const { inferRecordSchema, identityFields, recordLists } = loadTypescript(
  path.join(path.dirname(file), 'workflow-records.ts'),
  { './workflow-data': workflowData.exports }
).exports;
const {
  newWorkflow,
  newWorkflowAction,
  withWorkflow,
  readWorkflow,
  compileWorkflow,
  workflowIssues,
  workflowThrough,
  outputPaths,
  workflowSource,
  WORKFLOW_LIMITS,
  validWorkflowInterval,
  requestAllowsBody,
  editedProgramDocument,
  executableWorkflowDocument,
  withoutWorkflow,
} = moduleFixture.exports;
const normalize = (value) => JSON.parse(JSON.stringify(value));

// 直接渲染生产 README，隔离主题、路由和重量级 UI 依赖；不伪造业务接口。
const React = require('react');
const { renderToStaticMarkup } = require('react-dom/server');
const element = ({ children }) => React.createElement('div', null, children);
const Readme = loadTypescript(path.join(path.dirname(file), 'StreamReadme.tsx'), {
  react: React,
  'react/jsx-runtime': require('react/jsx-runtime'),
  antd: { Alert: element, Segmented: element, Spin: element },
  '@carbon/icons-react': { Chat: element, Document: element, Plug: element },
  '@codemirror/lang-javascript': {},
  'react-markdown': { default: element },
  'remark-gfm': { default: () => {} },
  '@/apis/core-api/source-stream': {},
  '@/apis/core-api/source-stream-scope': loadTypescript(
    path.resolve(__dirname, '../src/apis/core-api/source-stream-scope.ts'),
    { react: React, './source-stream': {}, './uns-metadata': { metadataApi: {} } }
  ).exports,
  '@/apis/core-api/uns-metadata': {},
  '@/apis/core-api/uns': {},
  '@/common-types/button-permission': { ButtonPermission: {} },
  '@/utils/auth': { hasPermission: () => false },
  '@/components/com-button': { default: element },
  '@/components/pro-codemirror': { default: element },
  '@/components/help-tooltip': { default: element },
  '@/hooks': { useTranslate: () => (key) => key },
  '@/stores/theme-store': { useThemeStore: (select) => select({ theme: 'light' }) },
  '@/stores/base': { useBaseStore: (select) => select({}) },
  '../../metadata-permission': loadTypescript(path.join(path.dirname(file), '../../metadata-permission.ts')).exports,
  '../../agent/integration-agent-control': { useIntegrationAgent: () => undefined },
  './IntegrationTabs': { useIntegrationRoute: () => ({ go: () => {} }) },
  './stream-readme': readmeHelpers,
  './program-document': loadTypescript(path.join(path.dirname(file), 'program-document.ts')).exports,
  './workflow-connections': connectionHelpers,
  './stream-codemirror-theme': {},
  './ProgramScriptView': { default: element },
  './index.module.scss': { default: {} },
  './stream-readme.module.scss': { default: {} },
}).exports.default;

test('README renders valid minimal drafts with omitted description and missing/null manifest arrays', () => {
  const document = { name: 'Minimal', script: 'module.exports = function(api) {};' };
  for (const manifest of [undefined, {}, { sources: null, topics: null, entries: null }]) {
    const html = renderToStaticMarkup(React.createElement(Readme, { document, manifest, canManage: false }));
    assert.ok(html.includes('Minimal'));
    assert.ok(html.includes('StreamReadme.noDescription'));
    assert.ok(html.includes('StreamReadme.noManifest'));
    assert.ok(!html.includes('NaN'));
  }
});

test('README renders registered Sources without resources and does not mutate the stored manifest', () => {
  const manifest = { sources: [{ id: '23', resources: null }], topics: [], entries: [] };
  const before = JSON.stringify(manifest);
  const html = renderToStaticMarkup(
    React.createElement(Readme, {
      document: { name: 'Source draft', description: null, script: '' },
      manifest,
      canManage: false,
    })
  );
  assert.ok(html.includes('23'));
  assert.ok(html.includes('StreamReadme.noResources'));
  assert.equal(JSON.stringify(manifest), before);
});

test('README never describes stale workflow metadata as the saved Agent script', () => {
  const workflow = newWorkflow();
  workflow.actions.push({ ...newWorkflowAction('script', 'process', 'trigger'), code: 'return input;' });
  const document = withWorkflow(
    { name: 'README', description: '', script: '', timeoutMs: 5000 },
    workflow
  );
  assert.ok(readmeHelpers.documentedWorkflow(document));
  const changed = { ...document, script: 'module.exports = function(api) { api.log.info("agent"); };' };
  const snapshot = JSON.stringify(changed);
  assert.equal(readmeHelpers.documentedWorkflow(changed), undefined);
  assert.equal(JSON.stringify(changed), snapshot);
});

test('README groups message entries without presenting a continuously running script', () => {
  assert.equal(readmeHelpers.entryMode('source-event'), 'message');
  assert.equal(readmeHelpers.entryMode('uns-event'), 'message');
  assert.equal(readmeHelpers.entryMode('interval'), 'interval');
  assert.equal(readmeHelpers.entryMode('request'), 'request');
  assert.equal(readmeHelpers.entryMode('http-request'), 'http');
});

test('README distinguishes incoming HTTP routes from manually invoked entries', () => {
  const document = { name: 'HTTP route', script: '' };
  for (const [kind, mode] of [
    ['http-request', 'http'],
    ['request', 'request'],
  ]) {
    const manifest = {
      sources: [],
      topics: [],
      entries: [{ id: 'create-order', kind, sourceId: '23', method: 'POST', path: '/orders/:id' }],
    };
    const before = JSON.stringify(manifest);
    const html = renderToStaticMarkup(React.createElement(Readme, { document, manifest, canManage: false }));
    assert.ok(html.includes(`StreamReadme.mode.${mode}`));
    assert.ok(html.includes(`StreamReadme.run.${mode}`));
    assert.ok(!html.includes(`StreamReadme.mode.${mode === 'http' ? 'request' : 'http'}`));
    if (kind === 'http-request') assert.ok(html.includes('POST /orders/:id'));
    assert.equal(JSON.stringify(manifest), before);
  }
});

test('README HTTP and manual trigger descriptions exist independently in both business locales', () => {
  for (const locale of ['zh_CN', 'en_US']) {
    const messages = JSON.parse(
      fs.readFileSync(path.resolve(__dirname, '../../../../backend/etc/i18n', `${locale}.json`), 'utf8')
    );
    for (const prefix of ['mode', 'run']) {
      const http = messages[`StreamReadme.${prefix}.http`];
      const manual = messages[`StreamReadme.${prefix}.request`];
      assert.equal(typeof http, 'string');
      assert.ok(http.trim());
      assert.equal(typeof manual, 'string');
      assert.notEqual(http, manual);
    }
  }
});

test('schema inference preserves unknown/null/mixed values without inventing required fields', () => {
  const schema = normalize(
    inferRecordSchema([
      { id: 1, name: 'A', note: null, amount: 2, extra: { enabled: true }, tags: ['a'] },
      { id: 2, note: 'present', amount: 'unknown', extra: { code: 'x' }, tags: [] },
    ])
  );
  assert.equal(schema.additionalProperties, true);
  assert.equal(schema.required, undefined);
  assert.equal(schema.properties.id.type, 'number');
  assert.equal(schema.properties.name.type, 'string');
  assert.equal(schema.properties.note, undefined);
  assert.equal(schema.properties.amount, undefined);
  assert.equal(schema.properties.extra.required, undefined);
  assert.deepEqual(schema.properties.tags, { type: 'array', items: { type: 'string' } });
});

test('record identity candidates exclude missing, unsafe and string-normalized duplicate IDs', () => {
  assert.deepEqual(
    normalize(
      identityFields([
        { Id: 0, name: 'A' },
        { Id: 1, name: 'A' },
      ])
    ),
    ['Id']
  );
  assert.deepEqual(normalize(identityFields([{ id: 1 }, { id: '1' }])), []);
  assert.deepEqual(normalize(identityFields([{ id: Number.MAX_SAFE_INTEGER + 1 }, { id: 2 }])), []);
  assert.deepEqual(normalize(identityFields([{ id: '' }, {}])), []);
});

test('record list discovery selects object arrays and rejects unsafe or ambiguous paths', () => {
  const lists = normalize(recordLists({ data: { records: [{ id: 'a' }] }, numbers: [1], 'a.b': [{ id: 'b' }] }));
  assert.deepEqual(lists, [{ path: 'data.records', records: [{ id: 'a' }] }]);
  assert.deepEqual(normalize(recordLists({ data: [null, { id: 'a' }] })), []);
});
const field = (step, path = '') => ({ kind: 'field', step, path });
const json = (value) => ({ kind: 'json', value: JSON.stringify(value) });
const doc = { name: 'Contract test', description: '', timeoutMs: 1000, script: '' };
function fixture(workflow, sourceValue = { value: 12 }) {
  const entries = [],
    sources = [],
    targets = [],
    calls = [],
    writes = [];
  const register = (kind) => (config, handler) => entries.push({ kind, config, handler });
  const api = {
    stream: { onRequest: register('request'), onTimer: register('interval'), onMessage: register('source-event') },
    sources: {
      use: (config) => {
        sources.push(config);
        return {
          read: async (...input) => {
            calls.push(['read', ...input]);
            return sourceValue;
          },
          request: async (input) => {
            calls.push(['request', input]);
            return typeof sourceValue === 'function'
              ? sourceValue(input)
              : 'status' in sourceValue
                ? sourceValue
                : { status: 200, data: sourceValue };
          },
        };
      },
    },
    uns: {
      use: (config) => {
        targets.push(config);
        return {
          write: async (value) => {
            writes.push(value);
            return { stage: 'buffered' };
          },
        };
      },
    },
  };
  const module = { exports: undefined };
  vm.runInNewContext(compileWorkflow(workflow), { module });
  module.exports(api);
  return {
    entries,
    sources,
    targets,
    calls,
    writes,
    run: (input, dryRun = true) => entries[0].handler({ input, dryRun }),
  };
}

test('record lists preserve stable numeric/string identities and validate all rows before writing', async () => {
  const workflow = newWorkflow();
  workflow.actions = [
    {
      ...newWorkflowAction('write', 'save', 'trigger'),
      target: 'collection:11111111-1111-4111-8111-111111111111',
      each: { records: field('trigger'), identityPath: 'id', fields: ['name'] },
    },
  ];
  const run = fixture(workflow);
  const result = await run.run([
    { id: 0, name: 'Zero', other: 1 },
    { id: 'B', name: 'Beta' },
  ]);
  assert.deepEqual(normalize(run.writes), [
    { recordId: '0', record: { name: 'Zero' }, mode: 'upsert' },
    { recordId: 'B', record: { name: 'Beta' }, mode: 'upsert' },
  ]);
  assert.equal(result.result.count, 2);
  for (const rows of [
    [{ id: 'A' }, { id: 'A' }],
    [{ id: 1 }, { id: '1' }],
    [{ id: 'A' }, { name: 'missing' }],
    [{ id: [] }],
  ]) {
    const bad = fixture(workflow);
    await assert.rejects(bad.run(rows), /identity/);
    assert.equal(bad.writes.length, 0);
  }
  assert.equal((await fixture(workflow).run([])).result.count, 0);
});

test('cursor pagination collects every page and fails repeated, missing or excessive cursors', async () => {
  const workflow = newWorkflow();
  workflow.actions = [
    {
      ...newWorkflowAction('source', 'read', 'trigger'),
      source: { id: '1', name: 'API', resource: '' },
      path: '/customers',
      pagination: {
        mode: 'cursor',
        itemsPath: 'items',
        nextPath: 'next',
        parameter: 'cursor',
        sizeParameter: 'limit',
        maxPages: 3,
        pageSize: 1,
        startPage: 1,
      },
    },
    {
      ...newWorkflowAction('write', 'save', 'read'),
      target: 'collection:11111111-1111-4111-8111-111111111111',
      each: { records: field('read', 'data.items'), identityPath: 'id' },
    },
  ];
  const run = fixture(workflow, (req) => ({
    status: 200,
    data: req.query.cursor ? { items: [{ id: 'B' }], next: null } : { items: [{ id: 'A' }], next: 'second' },
  }));
  await run.run({});
  assert.deepEqual(
    normalize(run.writes).map((row) => row.recordId),
    ['A', 'B']
  );
  assert.equal(run.calls[1][1].query.cursor, 'second');
  for (const next of ['again', undefined]) {
    const bad = fixture(workflow, () => ({ status: 200, data: { items: [{ id: 'A' }], next } }));
    await assert.rejects(bad.run({}), /cursor/);
    assert.equal(bad.writes.length, 0);
  }
  let page = 0;
  const capped = fixture(workflow, () => ({
    status: 200,
    data: { items: [{ id: String(++page) }], next: String(page) },
  }));
  await assert.rejects(capped.run({}), /page limit/);
  assert.equal(capped.writes.length, 0);
});

test('page-number pagination sends initial page and size and stops at an empty page', async () => {
  const workflow = newWorkflow();
  workflow.actions = [
    {
      ...newWorkflowAction('source', 'read', 'trigger'),
      source: { id: '1', name: 'API', resource: '' },
      pagination: {
        mode: 'page',
        itemsPath: '',
        nextPath: '',
        parameter: 'page',
        sizeParameter: 'limit',
        maxPages: 4,
        pageSize: 1,
        startPage: 0,
      },
    },
  ];
  const run = fixture(workflow, (req) => ({
    status: 200,
    data: Number(req.query.page) < 2 ? [{ id: req.query.page }] : [],
  }));
  const result = await run.run({});
  assert.deepEqual(
    normalize(run.calls).map((call) => call[1].query),
    [
      { limit: '1', page: '0' },
      { limit: '1', page: '1' },
      { limit: '1', page: '2' },
    ]
  );
  assert.equal(result.result.data.length, 2);
  const failed = fixture(workflow, { status: 401, data: {} });
  await assert.rejects(failed.run({}), /401/);
});

test('output field mapping keeps the explicitly saved replacement contract', async () => {
  const workflow = newWorkflow();
  workflow.actions = [
    {
      id: 'write',
      kind: 'write',
      target: 'collection:11111111-1111-4111-8111-111111111111',
      recordId: field('trigger', 'id'),
      record: field('trigger', 'absent'),
      fields: [
        { key: 'name', value: field('trigger', 'name') },
        { key: 'active', value: json(true) },
      ],
    },
  ];
  assert.deepEqual(normalize(workflowIssues(workflow)), []);
  const runtime = fixture(workflow);
  await runtime.run({ id: 'customer-1', name: 'Customer', ignored: 'not mapped' });
  assert.deepEqual(normalize(runtime.writes), [{ recordId: 'customer-1', record: { name: 'Customer', active: true } }]);
  assert.deepEqual(normalize(readWorkflow(withWorkflow(doc, workflow))), normalize(workflow));
});

test('write modes survive serialization and preserve numeric identity, null and explicit removal', async () => {
  const workflow = newWorkflow();
  workflow.actions = [
    {
      ...newWorkflowAction('write', 'save', 'trigger'),
      target: 'collection:11111111-1111-4111-8111-111111111111',
      recordId: field('trigger', 'id'),
      record: field('trigger', 'data'),
    },
  ];
  assert.equal(workflow.actions[0].mode, 'upsert');
  for (const mode of ['create', 'upsert', 'update', 'replace']) {
    workflow.actions[0].mode = mode;
    const runtime = fixture(workflow);
    await runtime.run({ id: 0, data: { name: 'Beta', nullable: null } });
    assert.deepEqual(normalize(runtime.writes[0]), { mode, recordId: '0', record: { name: 'Beta', nullable: null } });
    assert.equal(readWorkflow(JSON.parse(JSON.stringify(withWorkflow(doc, workflow)))).actions[0].mode, mode);
  }
  workflow.actions[0].mode = 'update';
  workflow.actions[0].unset = ['old'];
  const runtime = fixture(workflow);
  await runtime.run({ id: 'A', data: { name: 'Beta' } });
  assert.deepEqual(normalize(runtime.writes[0].unset), ['old']);
  await assert.rejects(fixture(workflow).run({ id: 'A', data: { old: null } }), /same field/);
  workflow.actions[0].mode = 'replace';
  assert.ok(workflowIssues(workflow).some((issue) => issue.code === 'fields'));
});

test('pagination retains explicit query parameters and headers across every request', async () => {
  const workflow = newWorkflow();
  workflow.actions = [
    {
      ...newWorkflowAction('source', 'read', 'trigger'),
      source: { id: '1', name: 'API', resource: '' },
      path: '/customers',
      query: [{ key: 'region', value: 'north' }],
      headers: [{ key: 'Accept', value: 'application/json' }],
      pagination: {
        mode: 'page',
        itemsPath: 'items',
        nextPath: '',
        parameter: 'page',
        sizeParameter: 'limit',
        maxPages: 3,
        pageSize: 1,
        startPage: 1,
      },
    },
  ];
  const runtime = fixture(workflow, (request) => ({
    status: 200,
    data: { items: Number(request.query.page) === 1 ? [{ id: 'A' }] : [] },
  }));
  await runtime.run({});
  assert.equal(runtime.calls.length, 2);
  for (const [, request] of runtime.calls) {
    assert.equal(request.query.region, 'north');
    assert.equal(request.headers.Accept, 'application/json');
  }
  workflow.actions[0].headers.push({ key: 'accept', value: 'other' });
  assert.ok(workflowIssues(workflow).some((issue) => issue.code === 'requestOptions'));
});

test('separate output steps retain their own collections and record identities', async () => {
  const workflow = newWorkflow();
  const output = (id, target, recordId, key) => ({
    id,
    kind: 'write',
    target: `collection:${target}`,
    recordId: json(recordId),
    record: json({}),
    fields: [{ key, value: field('trigger', key) }],
  });
  workflow.actions = [
    output('customers', '11111111-1111-4111-8111-111111111111', 'c-1', 'name'),
    output('machines', '22222222-2222-4222-8222-222222222222', 'm-1', 'temperature'),
  ];
  const runtime = fixture(workflow);
  await runtime.run({ name: 'Customer', temperature: 24 });
  assert.equal(runtime.targets.length, 2);
  assert.deepEqual(normalize(runtime.writes), [
    { recordId: 'c-1', record: { name: 'Customer' } },
    { recordId: 'm-1', record: { temperature: 24 } },
  ]);
});

test('mapped output rejects unsafe keys, duplicate keys and deleted-step references', () => {
  const workflow = newWorkflow();
  const write = {
    id: 'write',
    kind: 'write',
    target: 'collection:11111111-1111-4111-8111-111111111111',
    recordId: json('id'),
    record: json({}),
    fields: [],
  };
  workflow.actions = [write];
  for (const fields of [
    [],
    [{ key: '__proto__', value: json({}) }],
    [
      { key: 'name', value: json(1) },
      { key: 'name', value: json(2) },
    ],
  ]) {
    write.fields = fields;
    assert(workflowIssues(workflow).some((issue) => issue.code === 'fields'));
  }
  write.fields = [{ key: 'name', value: field('deleted-step') }];
  assert(workflowIssues(workflow).some((issue) => issue.code === 'reference'));
});

test('one trigger; ordered mapping produces values and dry-run traces', async () => {
  const workflow = newWorkflow();
  workflow.actions.push({
    id: 'map',
    kind: 'map',
    fields: [
      { key: 'customer', value: field('trigger', 'company.name') },
      { key: 'active', value: json(true) },
    ],
  });
  const runtime = fixture(workflow);
  assert.equal(runtime.entries.length, 1);
  assert.deepEqual(normalize(await runtime.run({ company: { name: 'Acme' } }, false)), {
    customer: 'Acme',
    active: true,
  });
  const dry = await runtime.run({ company: { name: 'Acme' } });
  assert.equal(dry.steps.length, 2);
  assert.equal(dry.steps[1].output.customer, 'Acme');
});
test('a failed filter stops all later writes; missing fields fail with step context', async () => {
  const workflow = newWorkflow();
  workflow.actions = [
    { id: 'filter', kind: 'filter', left: field('trigger', 'amount'), operator: 'gt', right: json(10) },
    {
      id: 'write',
      kind: 'write',
      target: 'collection:00000000-0000-0000-0000-000000000001',
      recordId: { kind: 'text', value: 'r1' },
      record: field('trigger'),
    },
  ];
  const runtime = fixture(workflow);
  assert.equal((await runtime.run({ amount: 3 })).stoppedAt, 'filter');
  assert.equal(runtime.writes.length, 0);
  await assert.rejects(runtime.run({}), /Step filter: Missing field/);
  await assert.rejects(runtime.run({ amount: '20' }), /requires numbers/);
  await runtime.run({ amount: 20 });
  assert.equal(runtime.writes.length, 1);
});
test('existence filter permits a missing leaf without throwing', async () => {
  const workflow = newWorkflow();
  workflow.actions.push({
    id: 'filter',
    kind: 'filter',
    left: field('trigger', 'missing.value'),
    operator: 'exists',
    right: json(null),
  });
  assert.equal((await fixture(workflow).run({})).stoppedAt, 'filter');
});
test('resource reads share a registered source with explicit resource grants', async () => {
  const workflow = newWorkflow();
  workflow.actions = ['temp', 'pressure'].map((resource) => ({
    id: resource,
    kind: 'source',
    source: { id: '42', name: 'Device', resource },
    operation: 'read',
    method: 'GET',
    path: '/',
  }));
  const runtime = fixture(workflow);
  assert.deepEqual(normalize(runtime.sources), [{ id: '42', resources: ['temp', 'pressure'] }]);
  await runtime.run({});
  assert.deepEqual(normalize(runtime.calls), [
    ['read', 'temp', null],
    ['read', 'pressure', null],
  ]);
});
test('HTTP body is structured data; malicious text cannot escape generated code', async () => {
  const workflow = newWorkflow();
  const text = '"}); globalThis.compromised=true; //';
  workflow.actions.push({
    id: 'http',
    kind: 'source',
    source: { id: '42', name: 'API', resource: '' },
    operation: 'request',
    method: 'POST',
    path: '/items',
    input: { kind: 'text', value: text },
  });
  const runtime = fixture(workflow);
  await runtime.run({});
  assert.equal(runtime.calls[0][1].json, text);
});
test('records and time series target the existing UNS SDK write contract', async () => {
  const workflow = newWorkflow();
  workflow.actions.push({
    id: 'write',
    kind: 'write',
    target: 'topic:00000000-0000-0000-0000-000000000002',
    recordId: field('trigger', 'id'),
    record: field('trigger', 'record'),
    sample: field('trigger', 'sample'),
    time: json(1000),
  });
  const runtime = fixture(workflow);
  await runtime.run({ id: 'r1', record: { name: 'Acme' }, sample: { temperature: 22 } });
  assert.deepEqual(normalize(runtime.targets), [{ topicId: '00000000-0000-0000-0000-000000000002', access: 'write' }]);
  assert.deepEqual(normalize(runtime.writes), [
    { recordId: 'r1', record: { name: 'Acme' }, sample: { temperature: 22 }, time: 1000 },
  ]);
  await assert.rejects(runtime.run({ id: {}, record: {} }), /identity/);
});
test('broken references, reordered steps, unsafe keys and duplicate ids block validation', () => {
  const workflow = newWorkflow();
  const first = { id: 'first', kind: 'map', fields: [{ key: 'value', value: field('trigger') }] };
  const second = { id: 'second', kind: 'map', fields: [{ key: 'value', value: field('first') }] };
  workflow.actions = [first, second];
  assert.equal(workflowIssues(workflow).length, 0);
  workflow.actions = [second, first];
  assert.ok(workflowIssues(workflow).some((issue) => issue.code === 'reference'));
  workflow.actions = [first, { ...first }];
  assert.ok(workflowIssues(workflow).some((issue) => issue.code === 'id'));
  first.fields[0] = { key: '__proto__', value: field('trigger', 'constructor.name') };
  assert.ok(workflowIssues(workflow).some((issue) => issue.code === 'fields'));
  assert.ok(workflowIssues(workflow).some((issue) => issue.code === 'path'));
});
test('workflow definition determines mode; generated artifact changes do not hide steps', () => {
  const workflow = newWorkflow();
  const saved = withWorkflow({ ...doc, extensions: { unrelated: { keep: true } } }, workflow);
  assert.deepEqual(normalize(readWorkflow(JSON.parse(JSON.stringify(saved)))), normalize(workflow));
  const reorder = (value) =>
    Array.isArray(value)
      ? value.map(reorder)
      : value && typeof value === 'object'
        ? Object.fromEntries(
            Object.keys(value)
              .sort()
              .reverse()
              .map((key) => [key, reorder(value[key])])
          )
        : value;
  assert.deepEqual(normalize(readWorkflow(reorder(saved))), normalize(workflow), 'JSONB key order is immaterial');
  assert.equal(saved.extensions.unrelated.keep, true);
  const stored = { ...saved, script: '// Previously generated artifact' };
  assert.deepEqual(normalize(readWorkflow(stored)), normalize(workflow));
  assert.equal(executableWorkflowDocument(stored).script, compileWorkflow(workflow));
  assert.equal(stored.script, '// Previously generated artifact', 'viewing must not mutate the saved document');
  assert.equal(readWorkflow({ ...saved, extensions: { tier0Workflow: { version: 99 } } }), undefined);
  assert.equal(readWorkflow({ ...saved, extensions: { tier0Workflow: null } }), undefined);
});
test('explicit script edits detach workflow metadata while ordinary edits retain it', () => {
  const saved = withWorkflow({ ...doc, extensions: { unrelated: { keep: true } } }, newWorkflow());
  const script = 'module.exports = function(api) { /* custom program */ };';
  const edited = editedProgramDocument(saved, { ...saved, script });
  assert.equal(readWorkflow(edited), undefined);
  assert.equal(edited.script, script);
  assert.equal(edited.extensions.unrelated.keep, true);
  assert.equal(executableWorkflowDocument(edited), edited);
  assert.equal(readWorkflow(withoutWorkflow(saved)), undefined);
  const renamed = editedProgramDocument(saved, { ...saved, name: 'Renamed' });
  assert.ok(readWorkflow(renamed));
  const workflow = newWorkflow();
  workflow.actions = [newWorkflowAction('script', 'calculate', 'trigger')];
  const changed = editedProgramDocument(saved, withWorkflow(saved, workflow));
  assert.deepEqual(normalize(readWorkflow(changed)), normalize(workflow));
});
test('test prefix cannot run later actions, schedule and source subscriptions retain real registrations', () => {
  const workflow = newWorkflow();
  workflow.actions = [newWorkflowAction('filter', 'one', 'trigger'), newWorkflowAction('filter', 'two', 'one')];
  assert.equal(workflowThrough(workflow, 'one').actions.length, 1);
  assert.equal(workflowThrough(workflow, 'trigger').actions.length, 0);
  workflow.trigger.kind = 'interval';
  assert.equal(fixture(workflow).entries[0].config.intervalMs, 60000);
  workflow.trigger = {
    ...workflow.trigger,
    kind: 'source-event',
    source: { id: '42', name: 'MQTT', resource: 'mqtt:factory/#' },
  };
  const runtime = fixture(workflow);
  assert.equal(runtime.entries[0].kind, 'source-event');
  assert.deepEqual(normalize(runtime.entries[0].config.resourceRefs), ['mqtt:factory/#']);
});
test('suggested field paths exclude prototype and ambiguous dot keys', () => {
  const paths = outputPaths(JSON.parse('{"safe":{"value":1},"__proto__":{"bad":true},"a.b":2}'));
  assert.deepEqual(normalize(paths), ['safe', 'safe.value']);
});
const dataFixture = { exports: {} };
vm.runInNewContext(
  ts.transpileModule(
    fs.readFileSync(path.resolve(__dirname, '../src/pages/uns/components/source-stream/workflow-data.ts'), 'utf8'),
    {
      compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
    }
  ).outputText,
  { module: dataFixture, exports: dataFixture.exports, require: () => moduleFixture.exports }
);
const { workflowStages, outputAt, displayValue } = dataFixture.exports;

test('manual trigger skips configuration; incomplete Source blocks setup and test', () => {
  const flow = newWorkflow();
  assert.equal(workflowStages(flow, 'trigger').configure, false);
  assert.equal(workflowStages(flow, 'trigger').issues.length, 0);
  flow.actions = [newWorkflowAction('source', 'read', 'trigger')];
  assert.equal(workflowStages(flow, 'read').setupIssues[0].code, 'source');
  flow.actions[0].source = { id: '42', name: 'API', resource: '' };
  assert.equal(workflowStages(flow, 'read').issues.length, 0);
});
test('invalid upstream blocks downstream test; incomplete downstream does not block trigger', () => {
  const flow = newWorkflow();
  flow.trigger.kind = 'interval';
  flow.trigger.intervalMs = 0;
  flow.actions = [newWorkflowAction('map', 'map', 'trigger')];
  flow.actions[0].fields[0].key = 'value';
  assert.equal(workflowStages(flow, 'map').issues[0].code, 'interval');
  flow.trigger.intervalMs = 1000;
  flow.actions[0].fields[0].key = '';
  assert.equal(workflowStages(flow, 'trigger').issues.length, 0);
  assert.equal(workflowStages(flow, 'map').issues[0].code, 'fields');
});
test('field previews preserve scalar types and cannot traverse inherited or reserved keys', () => {
  const data = { rows: [{ value: 0, enabled: false, blank: '', nullable: null }] };
  assert.equal(outputAt(data, 'rows.0.value'), 0);
  assert.equal(displayValue(outputAt(data, 'rows.0.enabled')), 'false');
  assert.equal(displayValue(outputAt(data, 'rows.0.blank')), '""');
  assert.equal(displayValue(outputAt(data, 'rows.0.nullable')), 'null');
  assert.equal(outputAt(data, 'rows.0.missing'), undefined);
  assert.equal(outputAt(data, 'constructor.name'), undefined);
  assert.equal(outputAt(Object.create({ inherited: 1 }), 'inherited'), undefined);
});

test('Source call limit accepts 64 calls and rejects the 65th before execution', async () => {
  const workflow = newWorkflow();
  workflow.actions = Array.from({ length: WORKFLOW_LIMITS.sourceCalls }, (_, index) => ({
    ...newWorkflowAction('source', `read-${index}`, 'trigger'),
    source: { id: '42', name: 'API', resource: '' },
  }));
  assert.equal(workflowIssues(workflow).length, 0);
  const runtime = fixture(workflow);
  await runtime.run({});
  assert.equal(runtime.calls.length, 64);
  workflow.actions.push({ ...workflow.actions[0], id: 'overflow' });
  assert.deepEqual(normalize(workflowIssues(workflow)), [{ step: 'overflow', code: 'callLimit' }]);
  assert.equal(workflowStages(workflow, 'overflow').issues[0].code, 'callLimit');
  assert.throws(() => fixture(workflow).run({}), /Incomplete workflow/);
});

test('schedule boundaries match the host ceiling and reject non-integer/out-of-range input', () => {
  const backend = fs.readFileSync(
    path.resolve(__dirname, '../../../../backend/internal/streamexec/program.go'),
    'utf8'
  );
  assert.match(backend, new RegExp(`const MaxProgramCalls = ${WORKFLOW_LIMITS.sourceCalls}\\b`));
  assert.match(backend, new RegExp(`entry.IntervalMs <= ${WORKFLOW_LIMITS.maxIntervalMs}\\b`));
  const workflow = newWorkflow();
  workflow.trigger.kind = 'interval';
  for (const value of [1000, WORKFLOW_LIMITS.maxIntervalMs]) {
    workflow.trigger.intervalMs = value;
    assert.equal(validWorkflowInterval(value), true);
    assert.equal(workflowIssues(workflow, false).length, 0);
    assert.equal(fixture(workflow).entries[0].config.intervalMs, value);
  }
  for (const value of [0, 999, 1000.5, WORKFLOW_LIMITS.maxIntervalMs + 1, NaN, Infinity]) {
    workflow.trigger.intervalMs = value;
    assert.equal(validWorkflowInterval(value), false);
    assert.equal(workflowIssues(workflow, false)[0].code, 'interval');
  }
});

test('GET and HEAD cannot send JSON bodies; body-capable methods preserve them', async () => {
  const workflow = newWorkflow();
  const action = { ...newWorkflowAction('source', 'http', 'trigger'), source: { id: '42', name: 'API', resource: '' } };
  workflow.actions = [action];
  for (const method of ['GET', 'HEAD']) {
    action.method = method;
    assert.equal(requestAllowsBody(method), false);
    assert.equal(workflowIssues(workflow).length, 0);
    const runtime = fixture(workflow);
    await runtime.run({});
    assert.equal(Object.hasOwn(runtime.calls[0][1], 'json'), false);
    action.input = json({ value: 3 });
    assert.equal(workflowIssues(workflow)[0].code, 'body');
    assert.throws(() => fixture(workflow).run({}), /Incomplete workflow/);
    delete action.input;
  }
  for (const method of ['POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS']) {
    action.method = method;
    action.input = json({ value: 3 });
    assert.equal(requestAllowsBody(method), true);
    assert.equal(workflowIssues(workflow).length, 0);
    const runtime = fixture(workflow);
    await runtime.run({});
    assert.deepEqual(normalize(runtime.calls[0][1].json), { value: 3 });
  }
});

test('JSON filters compare structure, preserve scalar types and respect array order', async () => {
  const workflow = newWorkflow();
  const action = { id: 'filter', kind: 'filter', left: field('trigger'), operator: 'eq', right: json(null) };
  workflow.actions = [action];
  const cases = [
    [{ a: 1, b: [2, { enabled: false }] }, { b: [2, { enabled: false }], a: 1 }, true],
    [[1, 2], [2, 1], false],
    [{ a: 1 }, { a: 1, b: null }, false],
    [[], {}, false],
    [null, {}, false],
    [1, '1', false],
    [false, 0, false],
    [null, null, true],
    [0, 0, true],
    [JSON.parse('{"__proto__": {"value": 1}}'), JSON.parse('{"__proto__": {"value": 1}}'), true],
  ];
  for (const [left, right, equal] of cases) {
    action.right = json(right);
    for (const operator of ['eq', 'ne']) {
      action.operator = operator;
      const result = await fixture(workflow).run(left);
      assert.equal(!result.stoppedAt, operator === 'eq' ? equal : !equal);
    }
  }
});

function loadHelper(name, modules = {}) {
  const result = { exports: {} };
  vm.runInNewContext(
    ts.transpileModule(fs.readFileSync(path.resolve(path.dirname(file), name + '.ts'), 'utf8'), {
      compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
    }).outputText,
    {
      module: result,
      exports: result.exports,
      require: (id) => {
        assert.ok(modules[id], id);
        return modules[id];
      },
    }
  );
  return result.exports;
}
const { initialWorkflowProgram } = loadHelper('workflow-initial', {
  './program-document': loadHelper('program-document'),
  './program-workflow': moduleFixture.exports,
});

test('selected MQTT QoS survives initialization, resource selection and document round-trip', () => {
  for (const qos of [0, 1, 2]) {
    const source = {
      id: 42,
      name: 'MQTT',
      protocol: 'mqtt',
      document: { mqtt: { qos } },
      resources: [{ id: 'mqtt:factory/#', models: ['publish'] }],
    };
    const document = initialWorkflowProgram(source, 'mqtt:factory/#');
    const workflow = readWorkflow(JSON.parse(JSON.stringify(document)));
    assert.equal(workflow.trigger.source.qos, qos);
    assert.equal(fixture(workflow).entries[0].config.qos, qos);
    workflow.trigger.source = workflowSource(source, 'mqtt:factory/other');
    assert.equal(fixture(workflow).entries[0].config.qos, qos);
    assert.deepEqual(normalize(fixture(workflow).entries[0].config.resourceRefs), ['mqtt:factory/other']);
  }
  const workflow = newWorkflow();
  workflow.trigger.kind = 'source-event';
  workflow.trigger.source = { id: '42', name: 'MQTT', resource: 'mqtt:factory/#', qos: 3 };
  assert.equal(workflowStages(workflow, 'trigger').setupIssues[0].code, 'qos');
  assert.equal(workflowSource({ id: 43, name: 'HTTP', protocol: 'http-api', document: {} }).qos, undefined);
});

test('current UUID Topic targets remain prefilled even with a Source context', () => {
  const id = '00000000-0000-0000-0000-000000000002';
  for (const kind of ['topic', 'collection']) {
    const target = `${kind}:${id}`;
    for (const source of [undefined, { id: 42, name: 'API', protocol: 'http-api', document: {}, resources: [] }]) {
      const workflow = readWorkflow(initialWorkflowProgram(source, target));
      assert.equal(workflowIssues(workflow).length, 0);
      assert.equal(workflow.actions.at(-1).target, target);
      assert.equal(workflow.actions.at(-1).record.step, source ? 'read-source' : 'trigger');
      assert.deepEqual(normalize(fixture(workflow).targets), [{ [`${kind}Id`]: id, access: 'write' }]);
    }
  }
  const workflow = newWorkflow();
  workflow.actions = [{ ...newWorkflowAction('write', 'write', 'trigger'), target: 'topic:42' }];
  assert.ok(workflowIssues(workflow).some((issue) => issue.code === 'target'));
});

test('JavaScript step receives selected input, awaits output and feeds later steps', async () => {
  const workflow = newWorkflow();
  workflow.actions = [
    {
      ...newWorkflowAction('script', 'calculate', 'trigger'),
      input: field('trigger', 'order'),
      code: 'input.price = await Promise.resolve(input.price * 2); return { total: input.price * input.quantity };',
    },
    { id: 'map', kind: 'map', fields: [{ key: 'amount', value: field('calculate', 'total') }] },
  ];
  const input = { order: { price: 12, quantity: 3 } };
  const result = await fixture(workflow).run(input);
  assert.deepEqual(normalize(result.result), { amount: 72 });
  assert.deepEqual(normalize(result.steps[1].input), { price: 12, quantity: 3 });
  assert.deepEqual(input, { order: { price: 12, quantity: 3 } });
  assert.deepEqual(normalize(result.steps[0].output), input);
  assert.deepEqual(
    normalize(readWorkflow(JSON.parse(JSON.stringify(withWorkflow(doc, workflow))))),
    normalize(workflow)
  );
  assert.deepEqual(normalize(await fixture(workflow).run(input, false)), { amount: 72 });
});

test('JavaScript errors and missing returns stop downstream writes with step identity', async () => {
  for (const [code, error] of [
    ['throw new Error("invalid amount");', /Step calculate: invalid amount/],
    ['const amount = input;', /Step calculate: Script must return a JSON value/],
    ['const value = {}; value.self = value; return value;', /Step calculate:/],
  ]) {
    const workflow = newWorkflow();
    workflow.actions = [
      { ...newWorkflowAction('script', 'calculate', 'trigger'), code },
      {
        ...newWorkflowAction('write', 'write', 'calculate'),
        target: 'collection:00000000-0000-0000-0000-000000000002',
        recordId: { kind: 'text', value: 'test' },
      },
    ];
    const runtime = fixture(workflow);
    await assert.rejects(runtime.run({}), error);
    assert.equal(runtime.writes.length, 0);
  }
});

test('JavaScript supports JSON scalars, rejects missing references and respects step-test boundaries', async () => {
  const workflow = newWorkflow();
  workflow.actions = [newWorkflowAction('script', 'calculate', 'trigger')];
  for (const input of [null, false, 0, '', [1, 2], { x: 1 }]) {
    assert.deepEqual(normalize((await fixture(workflow).run(input)).result), input);
  }
  workflow.actions[0].input = field('later');
  assert.ok(workflowIssues(workflow).some((issue) => issue.code === 'reference'));
  workflow.actions[0].input = field('trigger');
  workflow.actions[0].code = '   ';
  assert.ok(workflowIssues(workflow).some((issue) => issue.code === 'script'));
  workflow.actions[0].code = 'return input;';
  workflow.actions.push({ ...newWorkflowAction('script', 'later', 'calculate'), code: 'return { broken: ;' });
  assert.throws(() => fixture(workflow), /SyntaxError/);
  assert.equal((await fixture(workflowThrough(workflow, 'calculate')).run(7)).result, 7);
});

test('Source references declare scoped handles without adding requests, entries or outputs', async () => {
  const workflow = newWorkflow();
  workflow.connections = [
    { alias: 'crm', id: '41', name: 'CRM', resource: '', mode: 'request' },
    { alias: 'temperature', id: '42', name: 'PLC', resource: 'opcua:temperature', mode: 'read' },
    { alias: 'pressure', id: '42', name: 'PLC', resource: 'opcua:pressure', mode: 'read' },
  ];
  workflow.actions = [{ ...newWorkflowAction('script', 'process', 'trigger'), code: 'return input;' }];
  const compiled = fixture(workflow);
  assert.equal(compiled.entries.length, 1);
  assert.equal(compiled.calls.length, 0);
  assert.equal(compiled.targets.length, 0);
  assert.deepEqual(normalize(compiled.sources), [
    { id: '41', resources: [] },
    { id: '42', resources: ['opcua:temperature'] },
    { id: '42', resources: ['opcua:pressure'] },
  ]);
  assert.deepEqual(normalize((await compiled.run({ value: 7 })).result), { value: 7 });
  assert.equal(compiled.calls.length, 0);
  const stored = JSON.parse(JSON.stringify(withWorkflow(doc, workflow)));
  assert.deepEqual(normalize(readWorkflow(stored).connections), workflow.connections);
});

test('a script can call a declared Source without a read step or database output', async () => {
  const workflow = newWorkflow();
  workflow.connections = [{ alias: 'crm', id: '41', name: 'CRM', resource: '', mode: 'request' }];
  workflow.actions = [
    {
      ...newWorkflowAction('script', 'process', 'trigger'),
      code: "const response = await connections.crm.request({method: 'GET', path: '/customers'}); return response.data;",
    },
  ];
  const compiled = fixture(workflow, { status: 200, data: [{ id: 'C001' }] });
  assert.deepEqual(normalize((await compiled.run({})).result), [{ id: 'C001' }]);
  assert.equal(compiled.calls.length, 1);
  assert.equal(compiled.writes.length, 0);
  const calls = connectionHelpers.workflowTestCalls(workflow);
  assert.equal(calls[0].optional, true);
  assert.equal(calls[0].safeFetch, false);
  assert.equal(calls[0].key, '41/request');
});

test('subscriptions feed script input directly without a read call', async () => {
  const workflow = newWorkflow();
  workflow.trigger = {
    id: 'trigger',
    kind: 'source-event',
    intervalMs: 60000,
    source: { id: '42', name: 'MQTT', resource: 'mqtt:factory/temperature', qos: 1 },
  };
  workflow.actions = [
    { ...newWorkflowAction('script', 'process', 'trigger'), code: 'return {temperature: input.value};' },
  ];
  const compiled = fixture(workflow);
  assert.equal(compiled.entries[0].kind, 'source-event');
  assert.deepEqual(normalize((await compiled.run({ value: 24 })).result), { temperature: 24 });
  assert.equal(compiled.calls.length, 0);
  assert.equal(compiled.writes.length, 0);
});

test('Source reference aliases and resource scopes are checked before compilation', () => {
  for (const alias of ['__proto__', 'constructor', 'prototype', 'a-b', '1crm', '']) {
    const workflow = newWorkflow();
    workflow.connections = [{ alias, id: '41', name: 'CRM', resource: '', mode: 'request' }];
    assert.ok(workflowIssues(workflow, false).some((issue) => issue.code === 'connectionAlias'));
  }
  const workflow = newWorkflow();
  workflow.connections = [
    { alias: 'crm', id: '41', name: 'CRM', resource: '', mode: 'request' },
    { alias: 'crm', id: '42', name: 'PLC', resource: '', mode: 'read' },
  ];
  const issues = workflowIssues(workflow, false);
  assert.ok(issues.some((issue) => issue.code === 'connectionAlias'));
  assert.ok(issues.some((issue) => issue.code === 'resource'));
});
