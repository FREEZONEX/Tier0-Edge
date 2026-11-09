const assert = require('node:assert/strict');
const { test } = require('node:test');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('typescript');
const limits = { exports: {} };
vm.runInNewContext(
  ts.transpileModule(fs.readFileSync(path.join(__dirname, '../src/utils/limits.ts'), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS },
  }).outputText,
  { module: limits, exports: limits.exports }
);
const model = { exports: {} };
vm.runInNewContext(
  ts.transpileModule(
    fs.readFileSync(path.join(__dirname, '../src/pages/uns/components/metadata/topic-composer-model.ts'), 'utf8'),
    { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }
  ).outputText,
  { module: model, exports: model.exports, require: () => limits.exports }
);
const field = { name: 'temperature', kind: 'sample', collectionId: 'machines', recordId: '', fieldPath: 'temp' };
const { topicDocument, topicNameTooLong } = model.exports;

test('Topic names use the backend Unicode character limit, not UTF-16 code units', () => {
  const limit = limits.exports.MAX_LENGTHS.name;
  for (const character of ['A', '中', '😀']) {
    assert.equal(topicNameTooLong(character.repeat(limit)), false);
    assert.equal(topicNameTooLong(character.repeat(limit + 1)), true);
  }
  assert.equal(topicNameTooLong('客😀' + 'A'.repeat(limit - 2)), false);
  assert.equal(topicNameTooLong('客😀' + 'A'.repeat(limit - 1)), true);
  assert.equal(topicNameTooLong('  ' + '😀'.repeat(limit) + '  '), false);
});
test('entire collection remains a live collection reference, even with a previous field selection', () => {
  const document = topicDocument({
    mode: 'records',
    name: ' Machines ',
    parentId: 'factory',
    collectionId: 'machines',
    fields: [field],
    records: {},
  });
  assert.deepEqual(JSON.parse(JSON.stringify(document)), {
    name: 'Machines',
    parentId: 'factory',
    viewMode: 'records',
    collectionId: 'machines',
  });
});
test('field composition binds chosen records and excludes a previous entire collection selection', () => {
  const document = topicDocument({
    mode: 'fields',
    name: 'Temperature',
    collectionId: 'unused',
    fields: [field],
    records: { machines: { id: 'machine-1' } },
  });
  assert.equal(document.collectionId, undefined);
  assert.equal(document.viewMode, 'fields');
  assert.equal(document.fields[0].recordId, 'machine-1');
  assert.equal(document.fields[0].collectionId, 'machines');
  assert.equal(field.recordId, '');
});
