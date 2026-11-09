// 可执行 React 18 组件生命周期回归；不等同于登录后的真实模型/浏览器 E2E。
// 使用已有测试 fixture 的 jsdom，不向企业版增加测试依赖：
// node scripts/check-agent-permission-boundary.mjs <已有 frontend 测试 workspace>
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import Module from 'node:module';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const hostRequire = createRequire(new URL('../package.json', import.meta.url));
const fixture = process.argv[2];
if (!fixture) throw new Error('Provide an existing test workspace with jsdom; no install is performed.');
const fixtureRequire = createRequire(resolve(fixture, 'package.json'));
const { JSDOM } = fixtureRequire('jsdom');
const React = hostRequire('react');
const { createRoot } = hostRequire('react-dom/client');
const { transformSync } = hostRequire('esbuild');
const { act, createElement: element, Fragment, useEffect, useRef, useState } = React;
assert.match(React.version, /^18\./);

const componentFile = fileURLToPath(new URL('../src/pages/uns/agent/agent-permission-boundary.tsx', import.meta.url));
const compiled = transformSync(readFileSync(componentFile, 'utf8'), {
  loader: 'tsx',
  format: 'cjs',
  jsx: 'automatic',
}).code;
const componentModule = new Module(componentFile);
componentModule.filename = componentFile;
componentModule.paths = Module._nodeModulePaths(resolve(componentFile, '..'));
componentModule._compile(compiled, componentFile);
const { AgentPermissionBoundary, agentPermissionKey } = componentModule.exports;

const dom = new JSDOM('<!doctype html><html><body></body></html>');
globalThis.window = dom.window;
globalThis.document = dom.window.document;
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

function fixtureCase() {
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  const requests = [];
  let editorMounts = 0;
  let conversationMounts = 0;
  let chat;
  let editor;
  function Editor() {
    const [draft, setDraft] = useState('unsaved-script');
    editor = { draft, setDraft };
    useEffect(() => {
      editorMounts++;
    }, []);
    return element('div', null, draft);
  }
  // 最小可中断会话 fixture：真实 React state 队列 + 真实 AbortController。
  // 正式 ChatPanel 的卸载取消/延迟准备门禁另由上游 use-send-lifetime.test.tsx 验证。
  function Conversation({ objectKind, sourceRead, streamRead }) {
    const [queue, setQueue] = useState([]);
    const active = useRef(false);
    const inFlight = useRef(new Set());
    useEffect(() => {
      conversationMounts++;
      active.current = true;
      return () => {
        active.current = false;
        for (const controller of inFlight.current) controller.abort();
      };
    }, []);
    chat = {
      queue,
      enqueue() {
        const allowed = objectKind === 'source' ? sourceRead : streamRead;
        setQueue((previous) => [...previous, allowed ? { kind: objectKind } : null]);
      },
      prepareQueued() {
        const snapshot = queue[0];
        const controller = new AbortController();
        inFlight.current.add(controller);
        let completePreparation;
        const pending = new Promise((done) => {
          completePreparation = done;
        }).then(() => {
          if (active.current && !controller.signal.aborted) requests.push(snapshot);
        });
        return { completePreparation, pending, signal: controller.signal };
      },
    };
    return element('div', null, objectKind);
  }
  function render(objectKind, sourceRead = true, streamRead = true) {
    act(() =>
      root.render(
        element(
          Fragment,
          null,
          element(Editor),
          element(
            AgentPermissionBoundary,
            { permissionKey: agentPermissionKey(sourceRead, streamRead) },
            element(Conversation, { objectKind, sourceRead, streamRead })
          )
        )
      )
    );
  }
  return {
    render,
    requests,
    get chat() {
      return chat;
    },
    get editor() {
      return editor;
    },
    get mounts() {
      return { editor: editorMounts, conversation: conversationMounts };
    },
    close() {
      act(() => root.unmount());
      container.remove();
    },
  };
}

for (const revokedKind of ['source', 'stream']) {
  const currentKind = revokedKind === 'source' ? 'stream' : 'source';
  const test = fixtureCase();
  test.render(revokedKind);
  act(() => test.chat.enqueue());
  const queued = test.chat.prepareQueued();
  act(() => test.editor.setDraft('edited-unsaved-script'));
  test.render(currentKind);
  assert.equal(test.mounts.conversation, 1, '仅切对象不能丢弃发送时队列');
  test.render(currentKind, revokedKind !== 'source', revokedKind !== 'stream');
  assert.equal(queued.signal.aborted, true, `${revokedKind} 撤权必须取消在途请求`);
  assert.deepEqual(test.chat.queue, [], '权限变化丢弃旧会话队列');
  queued.completePreparation();
  await queued.pending;
  assert.deepEqual(test.requests, [], '旧异步准备完成后不得发出被撤权上下文');
  assert.deepEqual(test.mounts, { editor: 1, conversation: 2 });
  assert.equal(test.editor.draft, 'edited-unsaved-script', '不重建编辑器或丢弃草稿');
  act(() => test.chat.enqueue());
  const newRequest = test.chat.prepareQueued();
  newRequest.completePreparation();
  await newRequest.pending;
  assert.deepEqual(test.requests, [{ kind: currentKind }], '仍有权限的当前对象可继续发送');
  test.close();
}

const stable = fixtureCase();
stable.render('source');
act(() => stable.chat.enqueue());
stable.render('stream');
stable.render('stream');
assert.equal(stable.mounts.conversation, 1, '未变权限不重建会话');
assert.deepEqual(stable.chat.queue, [{ kind: 'source' }], '保留排队时快照，不借用新对象');
const pending = stable.chat.prepareQueued();
pending.completePreparation();
await pending.pending;
assert.deepEqual(stable.requests, [{ kind: 'source' }]);
stable.close();
dom.window.close();
console.log('Agent permission boundary: 3 real React 18 scenarios passed (queue, cancellation, editor preservation).');
