const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const ts = require('typescript');

const root = path.resolve(__dirname, '../src');
const cache = new Map();
const browser = { location: new URL('http://localhost/uns') };
function load(relativePath) {
  const file = path.resolve(root, relativePath);
  if (cache.has(file)) return cache.get(file);
  const fixture = { exports: {} };
  cache.set(file, fixture.exports);
  vm.runInNewContext(
    ts.transpileModule(fs.readFileSync(file, 'utf8').replaceAll('import.meta.env', '({})'), {
      compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
    }).outputText,
    {
      module: fixture,
      exports: fixture.exports,
      window: browser,
      URL,
      URLSearchParams,
      require: (id) => {
        // Pure navigation tests must never send API requests.
        if (id === '@/utils/request') return { ApiWrapper: class {} };
        const target = id.startsWith('@/') ? path.join(root, id.slice(2)) : path.resolve(path.dirname(file), id);
        return load(`${target}.ts`);
      },
    }
  );
  return fixture.exports;
}

const { isRemovedFeaturePath } = load('utils/removed-feature-pages.ts');
const { toLegacyResources, coreResourcesForResourceKeys } = load('apis/core-api/core-adapter.ts');
const { getUserDefaultHomePath } = load('utils/launchpad-site.ts');

test('retired editor URLs are recognized including nested routes, queries and case', () => {
  for (const url of [
    '/flow',
    '/flow/folder/1',
    '/collection-flow/flow-editor?id=1',
    '/EventFlow/Editor',
    '/notebook/editor/1',
    '/mqtt-auth?tab=mqttAccess',
    '/marimo',
  ]) {
    assert.equal(isRemovedFeaturePath(url), true, url);
  }
  for (const url of [
    '/uns?view=stream',
    '/edge-connection',
    '/edge-connection/nodes/1',
    '/project/notebook',
    '/launchpad',
    '/vision',
    '/flow-report',
    'https://example.test/notebook',
  ]) {
    assert.equal(isRemovedFeaturePath(url), false, url);
  }
});

test('old MQTT menu registration still opens Edge; Flow and Notebook menus are removed', () => {
  const resources = [
    { id: 1, resourceKey: 'mqtt.auth.manage', routePath: '/mqtt-auth', type: 2 },
    { id: 2, resourceKey: 'flow.collection.page', routePath: '/collection-flow', type: 2 },
    { id: 3, resourceKey: 'flow.event.page', routePath: '/EventFlow', type: 2 },
    { id: 4, resourceKey: 'notebook.view', routePath: '/notebook', type: 2 },
    { id: 5, resourceKey: 'vision.camera.page', routePath: '/vision', type: 2 },
  ];
  for (const includeHiddenMenus of [false, true]) {
    const menu = toLegacyResources(resources, { includeActionButtons: false, includeHiddenMenus });
    assert.equal(menu.find((item) => item.resourceKey === 'mqtt.auth.manage').url, '/edge-connection');
    assert.ok(menu.some((item) => item.resourceKey === 'vision.camera.page'));
    assert.equal(
      menu.some((item) => ['flow.collection.page', 'flow.event.page', 'notebook.view'].includes(item.resourceKey)),
      false
    );
  }
});

test('resource-key-only users retain UNS, Edge and Launchpad without retired pages', () => {
  const menu = toLegacyResources(
    coreResourcesForResourceKeys([
      'uns.page',
      'mqtt.auth.manage',
      'launchpad.view',
      'flow.collection.page',
      'notebook.view',
    ])
  );
  const urls = menu.map((item) => item.url).filter(Boolean);
  assert.ok(urls.includes('/uns'));
  assert.ok(urls.includes('/edge-connection'));
  assert.ok(urls.includes('/launchpad'));
  assert.equal(urls.some(isRemovedFeaturePath), false);
});

test('retired default home falls back while Operator Launchpad keeps standalone context', () => {
  assert.equal(getUserDefaultHomePath({ homePage: '/notebook' }), '/uns');
  assert.equal(getUserDefaultHomePath({ homePage: '/flow' }, ''), '');
  assert.equal(getUserDefaultHomePath({ homePage: '/mqtt-auth' }), '/uns');
  assert.equal(getUserDefaultHomePath({ homePage: '/edge-connection' }), '/edge-connection');
  assert.equal(
    getUserDefaultHomePath({ homePage: '/launchpad', roleCode: 'operator' }),
    '/launchpad?launchpadStandalone=true'
  );
  assert.equal(
    getUserDefaultHomePath({ homePage: '/launchpad', roleCode: 'operator', superAdmin: true }),
    '/launchpad'
  );
});
