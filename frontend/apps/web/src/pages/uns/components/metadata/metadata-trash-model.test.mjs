import { test } from 'node:test';
import assert from 'node:assert/strict';
import { trashRoots, trashBatches, purgeJobState } from './metadata-trash-model.ts';
const meta = (id, kind = 'collection', data = {}) => ({ id, kind, deletedAt: 1, data: { name: id, ...data } });

test('recycle bin keeps directory roots and independently archived descendants separate', () => {
  const items = [
    meta('root', 'folder', { directoryTrashId: 'root' }),
    meta('child', 'collection', { parentId: 'root', directoryTrashId: 'root' }),
    meta('earlier', 'collection', { parentId: 'root' }),
  ];
  assert.deepEqual(
    trashRoots(items).map((item) => item.id),
    ['root', 'earlier']
  );
  assert.deepEqual(
    trashBatches(items).flatMap((batch) => batch.items.map((item) => item.id)),
    ['root', 'earlier']
  );
  assert.deepEqual(
    trashBatches(items, items, true).flatMap((batch) => batch.items.map((item) => item.id)),
    ['root']
  );
});

test('empty-bin batches stay within 200 explicit IDs and exclude remote or already purging objects', () => {
  const items = Array.from({ length: 401 }, (_, i) => meta('c' + i));
  items.push({ ...meta('remote'), mountId: 'node' }, meta('purging', 'collection', { purgeJobId: 'job' }));
  assert.deepEqual(
    trashBatches(items).map((batch) => batch.items.length),
    [200, 200, 1]
  );
});

test('restoring selected nested independent directories orders their parents first', () => {
  const items = [
    meta('leaf', 'folder', { parentId: 'nested' }),
    meta('root', 'folder'),
    meta('nested', 'folder', { parentId: 'root' }),
  ];
  assert.deepEqual(
    trashBatches(items)[0].items.map((item) => item.id),
    ['root', 'nested', 'leaf']
  );
  assert.deepEqual(
    trashBatches(items, items, true)[0].items.map((item) => item.id),
    ['root']
  );
});

test('purge acceptance and partial outcomes never appear as success', () => {
  for (const status of ['queued', 'running']) assert.equal(purgeJobState({ status }), 'pending');
  assert.equal(purgeJobState({ status: 'succeeded', result: { status: 'committed' } }), 'succeeded');
  for (const job of [
    { status: 'succeeded' },
    { status: 'succeeded', result: { status: 'partiallyCommitted' } },
    { status: 'failed', result: { status: 'committed' } },
  ])
    assert.equal(purgeJobState(job), 'failed');
});
