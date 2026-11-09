const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { stripTypeScriptTypes } = require('node:module');

const source = fs.readFileSync(path.join(__dirname, 'graph-layout.ts'), 'utf8');
const modulePromise = import(
  `data:text/javascript;base64,${Buffer.from(stripTypeScriptTypes(source)).toString('base64')}`
);

test('Record identity includes Collection and preserves delimiter distinctions', async () => {
  const { recordNodeKey } = await modulePromise;
  assert.notEqual(recordNodeKey('a', 'b:c'), recordNodeKey('a:b', 'c'));
  assert.notEqual(recordNodeKey('collection-a', 'same'), recordNodeKey('collection-b', 'same'));
});

test('initial scatter is deterministic and focuses the actual selected object', async () => {
  const { initialPositions } = await modulePromise;
  const nodes = ['topic:1', 'source:1', 'stream:1'].map((id) => ({ id, radius: 40 }));
  assert.deepEqual(initialPositions(nodes, 'source:1'), initialPositions(nodes, 'source:1'));
  assert.deepEqual(initialPositions(nodes, 'source:1')['source:1'], { x: 0, y: 0 });
});

test('restoring positions does not move dragged nodes', async () => {
  const { initialPositions } = await modulePromise;
  assert.deepEqual(initialPositions([{ id: 'a', radius: 40, position: { x: 122, y: -44 } }]).a, { x: 122, y: -44 });
});

test('partial Meta reload preserves dragged Record coordinates until the Record query returns', async () => {
  const { mergeLayoutPositions, initialPositions, recordNodeKey } = await modulePromise;
  const record = recordNodeKey('input', 'pump');
  const previous = { 'collection:input': { x: 0, y: 0 }, [record]: { x: -158.292, y: -40.2901 } };
  const partial = mergeLayoutPositions(previous, { 'collection:input': { x: 0, y: 0 } });
  assert.deepEqual(partial[record], previous[record]);
  assert.deepEqual(initialPositions([{ id: record, radius: 24, position: partial[record] }])[record], previous[record]);
  const moved = mergeLayoutPositions(partial, { [record]: { x: 22, y: 33 } });
  assert.deepEqual(moved[record], { x: 22, y: 33 });
  assert.deepEqual(previous[record], { x: -158.292, y: -40.2901 });
  assert.deepEqual(mergeLayoutPositions(undefined, {}), {});
});

test('layout is finite, bounded, does not mutate input, and ignores unloaded references', async () => {
  const { initialPositions, layoutStep } = await modulePromise;
  const nodes = [
    { id: 'a', radius: 40 },
    { id: 'b', radius: 40 },
  ];
  const original = initialPositions(nodes);
  const copy = structuredClone(original);
  const next = layoutStep(nodes, [{ source: 'a', target: 'missing' }], original);
  assert.deepEqual(original, copy);
  for (const id of Object.keys(next)) {
    assert.ok(Number.isFinite(next[id].x) && Number.isFinite(next[id].y));
    assert.ok(Math.abs(next[id].x - original[id].x) <= 20);
    assert.ok(Math.abs(next[id].y - original[id].y) <= 20);
  }
});

test('selected focus remains at the origin through layout iterations', async () => {
  const { initialPositions, layoutStep } = await modulePromise;
  const nodes = Array.from({ length: 30 }, (_, i) => ({ id: `n${i}`, radius: 40 }));
  let points = initialPositions(nodes, 'n3');
  for (let step = 0; step < 120; step += 1) points = layoutStep(nodes, [{ source: 'n3', target: 'n4' }], points, 'n3');
  assert.deepEqual(points.n3, { x: 0, y: 0 });
});

test('100 spheres settle without overlapping balls or synthetic edges', async () => {
  const { initialPositions, layoutStep } = await modulePromise;
  const nodes = Array.from({ length: 100 }, (_, i) => ({ id: `n${i}`, radius: 40 }));
  const edges = [{ source: 'n0', target: 'n1' }];
  let points = initialPositions(nodes);
  for (let step = 0; step < 180; step += 1) points = layoutStep(nodes, edges, points);
  for (let i = 0; i < nodes.length; i += 1)
    for (let j = i + 1; j < nodes.length; j += 1) {
      const a = points[nodes[i].id];
      const b = points[nodes[j].id];
      assert.ok(Math.hypot(a.x - b.x, a.y - b.y) >= 80);
    }
  assert.equal(edges.length, 1);
});

test('numeric trends do not coerce large integer strings, booleans, arrays, or bad numbers', async () => {
  const { numericPaths, numericValue } = await modulePromise;
  const values = {
    id: '9007199254740993',
    decimal: '12.345',
    value: 3.2,
    bad: Infinity,
    active: true,
    nested: { temperature: 25 },
    rows: [1, 2],
  };
  assert.deepEqual(numericPaths(values), ['value', 'nested.temperature']);
  assert.equal(numericValue(values, 'nested.temperature'), 25);
  assert.equal(numericValue(values, 'id'), null);
  assert.equal(numericValue(values, 'missing.deep'), null);
});

test('numeric traversal is depth-bounded', async () => {
  const { numericPaths } = await modulePromise;
  let object = { value: 1 };
  for (let depth = 0; depth < 10; depth += 1) object = { nested: object };
  assert.deepEqual(numericPaths(object), []);
});

test('numeric paths preserve literal dots and backslashes in field names', async () => {
  const { numericPaths, numericValue } = await modulePromise;
  const values = { 'a.b': 1, a: { b: 2 }, 'c\\d': 3 };
  assert.deepEqual(numericPaths(values), ['a\\.b', 'a.b', 'c\\\\d']);
  assert.equal(numericValue(values, 'a\\.b'), 1);
  assert.equal(numericValue(values, 'a.b'), 2);
  assert.equal(numericValue(values, 'c\\\\d'), 3);
});
