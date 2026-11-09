// 从 frontend 执行：node apps/web/scripts/check-uns-tree-initialization.mjs
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { extname } from 'node:path';
import { runInNewContext } from 'node:vm';
import ts from 'typescript';

const require = createRequire(import.meta.url);
const modules = new Map();
let failRequest = false;
let nodes = [];
const api = {
  getTreeData: async () => {
    if (failRequest) throw new Error('injected root request failure');
    return structuredClone(nodes);
  },
  getUnsLazyTree: async ({ pageNo, pageSize }) => ({
    data: await api.getTreeData(),
    pageNo,
    pageSize,
    total: nodes.length,
  }),
};

// 使用真实 store、树工具函数及 Zustand/Immer，仅隔离接口和无关 UI 依赖。
function loadModule(url) {
  if (modules.has(url.href)) return modules.get(url.href).exports;
  const compiled = { exports: {} };
  modules.set(url.href, compiled);
  const { outputText } = ts.transpileModule(readFileSync(url, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2022 },
  });
  runInNewContext(outputText, {
    module: compiled,
    exports: compiled.exports,
    Map,
    Set,
    AbortController,
    setTimeout,
    clearTimeout,
    console: { log() {}, error() {} },
    require: (id) => {
      if (id === '@/apis/core-api/uns') return api;
      if (id === '@/hooks') return { useTranslate: () => (key) => key };
      if (id === '@/utils/request.ts') return { CustomAxiosConfigEnum: { BusinessResponse: '_businessResponse' } };
      if (id.startsWith('@/') || id.startsWith('.')) {
        const dependency = id.startsWith('@/') ? new URL(`../src/${id.slice(2)}`, import.meta.url) : new URL(id, url);
        if (!extname(dependency.pathname)) dependency.pathname += '.ts';
        return loadModule(dependency);
      }
      return require(id);
    },
  });
  return compiled.exports;
}

const storage = new Map();
const previousStorage = Object.getOwnPropertyDescriptor(globalThis, 'localStorage');
Object.defineProperty(globalThis, 'localStorage', {
  configurable: true,
  value: {
    getItem: (key) => storage.get(key) ?? null,
    setItem: (key, value) => storage.set(key, value),
    removeItem: (key) => storage.delete(key),
  },
});

async function load(store, options) {
  const completed = new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('UNS load did not finish')), 5000);
    const unsubscribe = store.subscribe(
      (state) => state.loading,
      (loading) => {
        if (!loading) {
          clearTimeout(timer);
          unsubscribe();
          resolve();
        }
      }
    );
  });
  store.getState().loadData(options);
  await completed;
}

try {
  const { createTreeStore } = loadModule(new URL('../src/pages/uns/store/treeStore.tsx', import.meta.url));
  for (const lazyTree of [true, false]) {
    storage.clear();
    const store = createTreeStore({ lazyTree });
    let readyCount = 0;
    store.subscribe(
      (state) => state.rootInitialized,
      (ready) => {
        if (ready) readyCount++;
      }
    );

    failRequest = true;
    await load(store, { reset: true });
    assert.equal(store.getState().rootInitialized, false, '初次失败不能放行 URL 定位');

    failRequest = false;
    nodes = [];
    store.getState().setSearchValue('missing');
    await load(store, { reset: true, queryType: 'search' });
    assert.equal(store.getState().rootInitialized, false, '搜索响应不能代替正常根节点初始化');

    store.getState().setSearchValue('');
    await load(store, { reset: true });
    assert.equal(store.getState().rootInitialized, true, '失败后刷新成功应恢复定位，空树也算加载成功');
    assert.equal(readyCount, 1);

    nodes = [{ id: 'factory', key: 'factory', path: 'factory', pathName: 'factory', hasChildren: false }];
    await load(store, { reset: true });
    assert.equal(store.getState().treeData[0].id, 'factory');
    assert.equal(readyCount, 1, '后续刷新不能重复触发初始化定位');

    failRequest = true;
    await load(store, { reset: true });
    assert.equal(store.getState().rootInitialized, true, '已初始化的页面不能因后续失败再次锁死');

    const saved = JSON.parse([...storage.values()][0]);
    assert.equal('rootInitialized' in saved.state, false, '初始化状态不能跨页面持久化');
    store.getState().reset({ lazyTree });
    assert.equal(store.getState().rootInitialized, false, '重置 store 后应重新等待根节点加载');
  }
} finally {
  if (previousStorage) Object.defineProperty(globalThis, 'localStorage', previousStorage);
  else delete globalThis.localStorage;
}

console.log(
  'UNS root initialization: lazy/full tree failure recovery, empty success, search isolation and reset passed.'
);
