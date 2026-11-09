const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { stripTypeScriptTypes } = require('node:module');
const source = fs.readFileSync(path.join(__dirname, 'navigation.ts'), 'utf8');
const navigationPromise = import(
  `data:text/javascript;base64,${Buffer.from(stripTypeScriptTypes(source)).toString('base64')}`
);

test('return from a Meta restores the original overview page, filters and layout key inputs', async () => {
  const { enterOverviewObject, overviewParent } = await navigationPromise;
  const parent = 'page=3&search=Plant0&kinds=topic%2Csource&archived=true';
  const entered = enterOverviewObject(parent, { kind: 'source', id: 's1', page: undefined }, null);
  assert.equal(new URLSearchParams(entered.search).get('page'), null);
  const back = overviewParent(entered.search, entered.state);
  assert.equal(back.search, parent);
  assert.deepEqual(back.state.unsOverviewParents, []);
});

test('Sample filtering preserves exact parent Meta page, Record page and Collection selection', async () => {
  const { enterOverviewObject, preserveOverviewState, changeOverviewSearch, overviewParent } = await navigationPromise;
  const parent = 'kind=topic&id=t1&page=2&recordPage=3&collectionId=c2&search=Line';
  const entered = enterOverviewObject(parent, { recordId: 'r1' }, { unsOverviewParents: ['page=3'] });
  const sampleSearch = changeOverviewSearch(entered.search, {
    time: 'custom',
    from: '100',
    to: '200',
    samplePage: '4',
  });
  const state = preserveOverviewState(entered.state);
  const back = overviewParent(sampleSearch, state);
  assert.equal(back.search, parent);
  assert.deepEqual(back.state.unsOverviewParents, ['page=3']);
});

test('overview breadcrumb restores the original root query and clears only ancestor frames', async () => {
  const { overviewParent } = await navigationPromise;
  const state = {
    unsOverviewParents: [
      'page=3&kinds=topic',
      'kind=topic&id=t1&collectionId=c1',
      'kind=collection&id=c1&recordPage=2',
    ],
  };
  const back = overviewParent('kind=collection&id=c1&collectionId=c1&recordId=r1&time=all', state, 'overview');
  assert.equal(back.search, 'page=3&kinds=topic');
  assert.deepEqual(back.state.unsOverviewParents, []);
});

test('focus breadcrumb restores matching Meta instead of unrelated ancestor', async () => {
  const { overviewParent } = await navigationPromise;
  const state = {
    unsOverviewParents: ['page=3', 'kind=source&id=s1', 'kind=topic&id=t1&collectionId=c2&recordPage=4'],
  };
  const back = overviewParent('kind=topic&id=t1&collectionId=c2&recordId=r1&time=all', state, 'focus');
  assert.equal(back.search, 'kind=topic&id=t1&collectionId=c2&recordPage=4');
  assert.deepEqual(back.state.unsOverviewParents, ['page=3', 'kind=source&id=s1']);
});

test('deep links derive local parents without using browser history or leaving the overview path', async () => {
  const { overviewParent } = await navigationPromise;
  const meta = overviewParent('kind=source&id=s1&page=2&search=Pump&kinds=source', null);
  assert.equal(meta.search, 'search=Pump&kinds=source');
  const record = overviewParent('kind=topic&id=t1&collectionId=c1&recordId=r1&recordPage=2&time=all', null);
  assert.equal(record.search, 'kind=topic&id=t1&collectionId=c1&recordPage=2');
  assert.deepEqual(record.state.unsOverviewParents, []);
});

test('navigation preserves unrelated Router state and does not mutate it', async () => {
  const { enterOverviewObject, preserveOverviewState } = await navigationPromise;
  const state = { customTab: 'UNS', unsOverviewParents: ['page=2'] };
  const entered = enterOverviewObject('page=3', { kind: 'topic', id: 't1' }, state);
  assert.equal(entered.state.customTab, 'UNS');
  assert.deepEqual(state.unsOverviewParents, ['page=2']);
  assert.deepEqual(preserveOverviewState(undefined).unsOverviewParents, []);
  assert.deepEqual(preserveOverviewState({ unsOverviewParents: [null, 123, 'page=1'] }).unsOverviewParents, ['page=1']);
});

test('ancestor state is bounded and stored values cannot become external navigation paths', async () => {
  const { preserveOverviewState, overviewParent } = await navigationPromise;
  const state = preserveOverviewState({ unsOverviewParents: Array.from({ length: 100 }, (_, i) => `page=${i}`) });
  assert.equal(state.unsOverviewParents.length, 32);
  const safe = overviewParent('kind=source&id=s1', { unsOverviewParents: ['https://example.test/elsewhere'] });
  assert.equal(new URLSearchParams(safe.search).get('https://example.test/elsewhere'), '');
  assert.ok(!safe.search.startsWith('https://'));
});

test('graph deep links and parent navigation stay in the Data topology graph view', async () => {
  const { relationshipGraphLocation, enterOverviewObject, overviewParent } = await navigationPromise;
  const original = 'kind=collection&id=c1&collectionId=c1&recordId=r1&samplePage=2&time=all';
  const redirected = relationshipGraphLocation(original);
  assert.equal(redirected.pathname, '/uns/topology');
  assert.equal(redirected.search, `${original}&view=graph`);
  const entered = enterOverviewObject('search=Pump&page=3', { kind: 'source', id: 's1', page: undefined }, null);
  const back = overviewParent(entered.search, entered.state);
  const destination = relationshipGraphLocation(back.search);
  assert.equal(destination.pathname, '/uns/topology');
  assert.equal(destination.search, 'search=Pump&page=3&view=graph');
  assert.equal(new URLSearchParams(relationshipGraphLocation('view=record').search).get('view'), 'graph');
});
