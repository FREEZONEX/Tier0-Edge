import { test } from 'node:test';
import assert from 'node:assert/strict';
import { namespaceRelations } from './namespace-relations.ts';
const row = (kind, id, parentKey) => ({
  key: kind + ':' + id,
  id,
  name: id,
  kind,
  group: kind === 'directory' || kind === 'topic' ? 'namespace' : kind,
  parentKey,
});
const relation = (from, kind, to) => ({ id: from + kind + to, from, to, kind });
test('directory relationships include directly nested Collections and their published Stream and Source', () => {
  const rows = [
    row('directory', 'root'),
    row('collection', 'data', 'directory:root'),
    row('stream', 'sync'),
    row('source', 'sensor'),
    row('app', 'dashboard'),
    row('collection', 'unrelated'),
  ];
  const relations = [
    relation('stream:sync', 'write', 'collection:data'),
    relation('source:sensor', 'input', 'stream:sync'),
    relation('app:dashboard', 'grant', 'collection:data'),
    relation('app:dashboard', 'grant', 'collection:unrelated'),
  ];
  const linked = namespaceRelations(rows[0], rows, relations);
  assert.deepEqual([...linked.keys()].sort(), ['app:dashboard', 'collection:data', 'source:sensor', 'stream:sync']);
});
test('standalone references still resolve to their collections without pulling unrelated App data', () => {
  const rows = [row('topic', 'view'), row('collection', 'data'), row('collection', 'other'), row('app', 'dashboard')];
  const relations = [
    relation('topic:view', 'fields', 'collection:data'),
    relation('app:dashboard', 'grant', 'collection:data'),
    relation('app:dashboard', 'grant', 'collection:other'),
  ];
  assert.deepEqual([...namespaceRelations(rows[0], rows, relations).keys()].sort(), [
    'app:dashboard',
    'collection:data',
  ]);
});
