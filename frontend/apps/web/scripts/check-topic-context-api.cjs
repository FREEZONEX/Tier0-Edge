// 只测试生产 API 适配器的请求合同，夹具不调用外部服务。
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const ts = require('typescript');
const source = fs.readFileSync(path.join(__dirname, '../src/apis/core-api/uns-metadata.ts'), 'utf8');
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;
function adapter(query, dependencies = {}) {
  const fixture = { exports: {} };
  vm.runInNewContext(compiled, {
    module: fixture,
    exports: fixture.exports,
    require: (name) => {
      assert.equal(name, './uns-v2');
      return { unsV2Api: { query, metadataAction: query }, ...dependencies };
    },
  });
  return fixture.exports.metadataApi;
}

test('nested Topic ancestry uses one namespace request, independent of depth', async () => {
  const calls = [];
  const controller = new AbortController();
  const ancestors = Array.from({ length: 100 }, (_, i) => ({
    id: `level-${i}`,
    kind: 'directory',
    data: { name: `Level ${i}`, parentId: i ? `level-${i - 1}` : null },
  }));
  const api = adapter(async (operation, queries, signal) => {
    calls.push({ operation, queries: JSON.parse(JSON.stringify(queries)), signal });
    return { results: [{ list: [], ancestors }] };
  });
  const result = await api.namespaceAncestors('level-99', controller.signal);
  assert.deepEqual(calls, [
    {
      operation: 'namespace/query',
      queries: [{ queryId: 'query', parentId: 'level-99', includeAncestors: true, size: 1 }],
      signal: controller.signal,
    },
  ]);
  assert.equal(result.length, 100);
  assert.ok(result.every((parent) => parent.kind === 'folder'));
  assert.equal(ancestors[0].kind, 'directory', 'adapter must not mutate the response');
});

test('Topic lookup forwards cancellation and retains exact kind and ID filters', async () => {
  const controller = new AbortController();
  const api = adapter(async (operation, query, signal) => {
    assert.equal(operation, 'findOne');
    assert.deepEqual(JSON.parse(JSON.stringify(query)), {
      kind: 'topic', filter: { _id: 'chosen-topic' }, state: 'active',
    });
    assert.equal(signal, controller.signal);
    return { value: null };
  });
  assert.equal(await api.find('topic', 'chosen-topic', false, controller.signal), undefined);
});

test('namespace query failure is propagated instead of fabricating a partial path', async () => {
  const failure = new Error('forbidden');
  const api = adapter(async () => {
    throw failure;
  });
  await assert.rejects(api.namespaceAncestors('parent'), (error) => error === failure);
});

test('Collection form preserves hidden configuration while explicitly clearing sample schema and description', async () => {
  const current = {
    id: 'orders', kind: 'collection', revision: 4,
    data: { schemaVersion: 1, name: 'Old', description: 'Old description', recordSchema: { type: 'object' },
      sampleSchema: { type: 'object' }, indexes: [{ name: 'order-number' }], semantics: { domain: 'orders' } },
  };
  const calls = [];
  const api = adapter(async () => ({ value: current }), {
    saveConfiguration: async (...args) => calls.push(args),
  });
  await api.save({ id: 'orders', kind: 'collection', expectedRevision: 4, document: {
    name: 'Renamed', recordSchema: { type: 'object', additionalProperties: false },
  } });
  const replacement = JSON.parse(JSON.stringify(calls[0][4]));
  assert.deepEqual(replacement.indexes, current.data.indexes);
  assert.deepEqual(replacement.semantics, current.data.semantics);
  assert.equal(replacement.name, 'Renamed');
  assert.equal(replacement.sampleSchema, undefined);
  assert.equal(replacement.description, undefined);
  assert.equal(calls[0][2], 4);
  assert.ok(current.data.sampleSchema, 'reading the original must not mutate it');
});
