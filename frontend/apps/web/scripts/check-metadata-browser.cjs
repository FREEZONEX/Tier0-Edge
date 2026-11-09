// 元数据展示边界：权限组合、未知字段与嵌套敏感内容，均使用测试夹具。
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const ts = require('typescript');
const file = path.resolve(__dirname, '../src/pages/uns/components/relationship-canvas/metadata-browser-model.ts');
const fixture = { exports: {} };
vm.runInNewContext(
  ts.transpileModule(fs.readFileSync(file, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText,
  { module: fixture, exports: fixture.exports }
);
const { readableMetadataKinds, metadataRowForDisplay } = fixture.exports;
const normalize = (value) => JSON.parse(JSON.stringify(value));

test('metadata kinds require the exact backend resource permissions', () => {
  for (const [permissions, expected] of [
    [undefined, []],
    [[], []],
    [['uns.page'], []],
    [['uns.read'], ['collection', 'topic', 'directory']],
    [['uns.source'], ['source']],
    [['uns.stream'], ['stream']],
    [['uns.app.read'], ['app']],
    [['uns.source.manage', 'uns.stream.manage'], []],
    [
      ['uns.read', 'uns.source'],
      ['collection', 'topic', 'directory', 'source'],
    ],
  ])
    assert.deepEqual(normalize(readableMetadataKinds(permissions)), expected);
});

for (const kind of ['source', 'stream', 'source_state', 'source_plan', 'source_version', 'app', 'unknown']) {
  test(`${kind} does not expose configuration, code, payloads or unknown metadata fields`, () => {
    const row = {
      id: 'test',
      kind,
      revision: 2,
      updatedAt: 123,
      privateExtension: 'SECRET',
      launch: { url: 'https://example.test/?token=SECRET' },
      data: {
        name: 'Visible name',
        status: 'draft',
        adapter: 'mqtt',
        health: 'healthy',
        credentials: { token: 'SECRET' },
        config: { connection: { password: 'SECRET' } },
        document: { script: 'SECRET' },
        draft: { processor: { code: 'SECRET' } },
        releases: [{ processor: { code: 'SECRET' } }],
        resources: [{ payload: 'SECRET' }],
        extension: { arbitrary: 'SECRET' },
        entry: { url: 'SECRET' },
        // 即使已允许的摘要字段被写入嵌套对象，也不能穿透投影。
        desiredState: { token: 'SECRET' },
        description: { password: 'SECRET' },
      },
    };
    const result = normalize(metadataRowForDisplay(row));
    assert.equal(JSON.stringify(result).includes('SECRET'), false);
    assert.equal(result.id, 'test');
    assert.equal(result.revision, 2);
    if (kind === 'unknown') assert.deepEqual(result.data, {});
    else assert.equal(result.data.name, 'Visible name');
    assert.equal(row.data.document.script, 'SECRET', 'projection must not mutate the API row');
  });
}

test('collection and topic retain their declared field contracts', () => {
  const recordSchema = { type: 'object', properties: { temperature: { type: 'number' } } };
  const collection = metadataRowForDisplay({ kind: 'collection', id: 'c', data: { name: 'Readings', recordSchema } });
  assert.deepEqual(normalize(collection.data.recordSchema), recordSchema);
  const fields = [{ name: 'temperature', collectionId: 'c', fieldPath: 'temperature' }];
  const topic = metadataRowForDisplay({
    kind: 'topic',
    id: 't',
    data: { name: 'Temperature', parentId: null, fields },
  });
  assert.deepEqual(normalize(topic.data.fields), fields);
  assert.equal(topic.data.parentId, null);
});
