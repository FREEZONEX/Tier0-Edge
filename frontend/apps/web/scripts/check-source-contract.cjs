// Pure editor parser and routing contracts; not live protocol/browser verification.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { resolve } = require('node:path');
const vm = require('node:vm');
const ts = require('typescript');
const file = resolve(__dirname, '../src/pages/uns/components/source-stream/SourceConfigEditor.tsx');
const source = ts.createSourceFile(file, readFileSync(file, 'utf8'), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
const declaration = source.statements.find(
  (node) => ts.isFunctionDeclaration(node) && node.name?.text === 'readSourceDocument'
);
const fixture = { exports: {} };
const sourceTypesFixture = { exports: {} };
vm.runInNewContext(
  ts.transpileModule(
    readFileSync(resolve(__dirname, '../src/pages/uns/components/source-stream/source-types.ts'), 'utf8'),
    {
      compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
    }
  ).outputText,
  { module: sourceTypesFixture, exports: sourceTypesFixture.exports }
);
fixture.validSourceType = sourceTypesFixture.exports.validSourceType;
vm.runInNewContext(
  ts.transpileModule(declaration.getText(source), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText,
  fixture
);
const parse = (document) => fixture.exports.readSourceDocument(JSON.stringify(document));

test(
  'React18 Source resources are visible to readers while discovery remains in the authorized editor',
  { skip: process.env.TIER0_STREAM_REACT_FIXTURE !== '1' },
  async () => {
    const appRequire = require('node:module').createRequire(resolve(__dirname, '../package.json'));
    const React = appRequire('react');
    const { JSDOM } = require('jsdom');
    const dom = new JSDOM('<html><body><div id="root"></div></body></html>');
    const previous = {
      window: global.window,
      document: global.document,
      IS_REACT_ACT_ENVIRONMENT: global.IS_REACT_ACT_ENVIRONMENT,
    };
    global.window = dom.window;
    global.document = dom.window.document;
    global.IS_REACT_ACT_ENVIRONMENT = true;
    const { createRoot } = appRequire('react-dom/client');
    const element = React.createElement;
    const permissions = new Set(['uns.sources']);
    const hasPermission = (key) => permissions.has(key);
    let tab;
    let changed = 0;
    const calls = [];
    const source = {
      id: 123,
      revision: 4,
      protocol: 'opcua',
      name: 'Read-only device',
      desiredState: 'disabled',
      health: 'unknown',
      document: { name: 'Read-only device', protocol: 'opcua', endpoint: 'opc.tcp://fixture:4840', opcua: {} },
      resources: [{ id: 'opcua:ns=2;s=temperature', name: 'Temperature', models: ['publish'] }],
    };
    const loaded = new Map();
    const wrap = ({ children }) => element('div', null, children);
    const load = (name) => {
      if (loaded.has(name)) return loaded.get(name);
      const component = { exports: {} };
      vm.runInNewContext(
        ts.transpileModule(
          readFileSync(resolve(__dirname, '../src/pages/uns/components/source-stream/' + name), 'utf8'),
          {
            compilerOptions: {
              module: ts.ModuleKind.CommonJS,
              target: ts.ScriptTarget.ES2022,
              jsx: ts.JsxEmit.ReactJSX,
            },
          }
        ).outputText,
        {
          module: component,
          exports: component.exports,
          window: dom.window,
          require(id) {
            if (id === 'react' || id === 'react/jsx-runtime') return appRequire(id);
            if (id === 'antd')
              return {
                App: { useApp: () => ({ modal: {}, message: {} }) },
                Space: wrap,
                Tag: wrap,
                Alert: ({ message }) => element('p', null, message),
                Input: (props) => element('input', props),
                Descriptions: ({ items }) =>
                  element(
                    'dl',
                    null,
                    items.map((item) => element('div', { key: item.key }, item.children))
                  ),
                Dropdown: ({ children, menu }) =>
                  element(
                    'div',
                    { 'data-manage': true },
                    children,
                    menu.items.map((item) => item?.label)
                  ),
                Collapse: ({ items }) => {
                  const [open, setOpen] = React.useState(false);
                  return element(
                    'section',
                    null,
                    items.map((item) =>
                      element(
                        'div',
                        { key: item.key },
                        element('button', { onClick: () => setOpen(!open) }, item.label),
                        open ? item.children : null
                      )
                    )
                  );
                },
              };
            if (id === '@carbon/icons-react') return new Proxy({}, { get: () => () => null });
            if (id === '@/common-types/button-permission')
              return { ButtonPermission: new Proxy({}, { get: (_, key) => key }) };
            if (id === '@/utils/auth') return { hasPermission };
            if (id === '@/hooks') return { useTranslate: () => (key) => key };
            if (id === '@/components/com-button')
              return {
                default: ({ auth, children, onClick, disabled }) =>
                  !auth || hasPermission(auth) ? element('button', { onClick, disabled }, children) : null,
              };
            if (id === '@/components/com-drawer')
              return {
                default: ({ open, children, footer }) => (open ? element('aside', null, children, footer) : null),
              };
            if (id === '@/components/pro-table')
              return {
                default: ({ dataSource }) =>
                  element(
                    'div',
                    { 'data-resources': true },
                    dataSource.map((item) => element('p', { key: item.id }, item.name, ' ', item.id))
                  ),
              };
            if (id === '@/components/help-tooltip') return { default: () => null };
            if (id === '@/components/confirm-modal') return { openConfirmModal() {} };
            if (id === '@/apis/core-api/source-stream')
              return {
                sourceStreamApi: {
                  discover: async (...args) => {
                    calls.push(args);
                  },
                  source: async () => source,
                },
              };
            if (id === './IntegrationTabs')
              return { useIntegrationDirty() {}, useIntegrationRoute: () => ({ tab, go() {} }) };
            if (id === './SourceConfigEditor')
              return { default: () => null, readSourceDocument: (raw) => JSON.parse(raw) };
            if (id === './source-credentials') return load('source-credentials.ts');
            if (id === './source-types') return load('source-types.ts');
            if (id === './SourceOAuth') return { authorizeSource() {} };
            if (id === './SourceCredentialsFields' || id === './TopicLinks') return { default: () => null };
            if (id === './resource-links') return { TopicLink: () => null };
            if (id.endsWith('.scss')) return { default: {} };
            throw new Error('Unexpected Source detail fixture dependency: ' + id);
          },
        }
      );
      loaded.set(name, component.exports);
      return component.exports;
    };
    const container = dom.window.document.getElementById('root');
    const root = createRoot(container);
    const render = () =>
      React.act(() => root.render(element(load('SourceDetail.tsx').default, { source, onChanged: () => changed++ })));
    const findButton = (scope, label) =>
      [...scope.querySelectorAll('button')].find((button) => button.textContent === label);
    try {
      render();
      assert.equal(container.querySelector('[data-manage]'), null);
      assert.equal(container.querySelector('[data-resources]'), null, 'resources start collapsed');
      await React.act(async () => findButton(container, 'SourceStream.nativeResources').click());
      assert.match(container.textContent, /Temperature opcua:ns=2;s=temperature/);
      assert.equal(findButton(container, 'SourceStream.discover'), undefined);
      source.opcuaServer = { endpoint: 'opc.tcp://managed:4840' };
      render();
      assert.match(container.textContent, /opc.tcp:\/\/managed:4840/);
      permissions.add('uns.sourceCredentials');
      tab = 'connection';
      render();
      let drawer = container.querySelector('aside');
      await React.act(async () => findButton(drawer, 'SourceStream.nativeResources').click());
      assert.match(drawer.textContent, /Temperature/);
      assert.equal(findButton(drawer, 'SourceStream.discover'), undefined);
      assert.equal(drawer.querySelector('input'), null);
      permissions.add('uns.sourceManage');
      render();
      drawer = container.querySelector('aside');
      assert.ok(drawer.querySelector('input[aria-label="SourceStream.parentNode"]'));
      await React.act(async () => findButton(drawer, 'SourceStream.discover').click());
      assert.deepEqual(calls, [[123, 4, '']]);
      assert.equal(changed, 1);
      assert.equal(
        [...container.querySelectorAll('button')].filter((button) => button.textContent === 'SourceStream.discover')
          .length,
        1
      );
    } finally {
      React.act(() => root.unmount());
      dom.window.close();
      Object.assign(global, previous);
    }
  }
);

test(
  'React18 HTTP picker selects Client and Server through one protocol with safe role-specific defaults',
  { skip: process.env.TIER0_STREAM_REACT_FIXTURE !== '1' },
  async () => {
    const appRequire = require('node:module').createRequire(resolve(__dirname, '../package.json'));
    const React = appRequire('react');
    const { JSDOM } = require('jsdom');
    const dom = new JSDOM('<html><body><div id="root"></div></body></html>');
    const previous = {
      window: global.window,
      document: global.document,
      IS_REACT_ACT_ENVIRONMENT: global.IS_REACT_ACT_ENVIRONMENT,
    };
    global.window = dom.window;
    global.document = dom.window.document;
    global.IS_REACT_ACT_ENVIRONMENT = true;
    const { createRoot } = appRequire('react-dom/client');
    const element = React.createElement;
    const createFile = resolve(__dirname, '../src/pages/uns/components/source-stream/SourceCreateDialog.tsx');
    const createSource = ts.createSourceFile(
      createFile,
      readFileSync(createFile, 'utf8'),
      ts.ScriptTarget.Latest,
      true,
      ts.ScriptKind.TSX
    );
    const initial = createSource.statements.find(
      (node) => ts.isFunctionDeclaration(node) && node.name?.text === 'initialSourceDocument'
    );
    const defaults = { exports: {}, uuid: () => 'fixture' };
    vm.runInNewContext(
      ts.transpileModule(initial.getText(createSource), {
        compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
      }).outputText,
      defaults
    );
    const catalogFile = resolve(__dirname, '../src/pages/uns/components/source-stream/SourceProtocolCatalog.tsx');
    const component = { exports: {} };
    vm.runInNewContext(
      ts.transpileModule(readFileSync(catalogFile, 'utf8'), {
        compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
      }).outputText,
      {
        module: component,
        exports: component.exports,
        require(id) {
          if (id === 'react') return React;
          if (id === 'react/jsx-runtime') return appRequire(id);
          if (id === 'antd')
            return {
              Empty: Object.assign(() => null, { PRESENTED_IMAGE_SIMPLE: 'empty' }),
              Input: () => null,
              Tabs: () => null,
            };
          if (id === '@carbon/icons-react') return new Proxy({}, { get: () => () => null });
          if (id === '@/hooks') return { useTranslate: () => (key) => key };
          if (id.endsWith('.scss')) return { default: {} };
          throw new Error('unexpected catalog fixture dependency');
        },
      }
    );
    const definition = {
      id: 'http-api',
      roles: ['client', 'server'],
      document: {
        name: '',
        description: '',
        protocol: 'http-api',
        endpoint: 'https://example.test',
        runtimeLocation: 'local',
        http: { authentication: 'none', parameters: { extension: true } },
      },
    };
    const container = dom.window.document.getElementById('root');
    const root = createRoot(container);
    let selected;
    const render = (protocols) =>
      React.act(() =>
        root.render(
          element(component.exports.default, {
            protocols,
            onSelect: (protocol, role) => {
              selected = defaults.exports.initialSourceDocument(protocol, role);
            },
          })
        )
      );
    const button = (id) =>
      Array.from(container.querySelectorAll('button')).find(
        (item) => item.querySelector('strong')?.textContent === 'SourceStream.catalogName.' + id
      );
    try {
      render([definition]);
      assert.equal(button('http'), undefined);
      assert.equal(button('http-client').disabled, false);
      assert.equal(button('http-server').disabled, false);
      React.act(() => button('http-server').click());
      assert.equal(selected.protocol, 'http-api');
      assert.equal(selected.role, 'server');
      assert.equal(selected.endpoint, 'http://managed');
      assert.equal(selected.http.authentication, 'bearer');
      assert.equal(selected.http.anonymous, false);
      assert.equal(selected.http.port, 0);
      assert.equal(selected.http.parameters.extension, true);
      React.act(() => button('http-client').click());
      assert.equal(selected.protocol, 'http-api');
      assert.equal(selected.role, 'client');
      assert.equal(selected.endpoint, definition.document.endpoint);
      assert.equal(selected.http.authentication, 'none');
      assert.equal(selected.http.port, undefined);
      render([{ ...definition, roles: ['client'] }]);
      assert.equal(button('http-server'), undefined);
      assert.equal(button('http-client').disabled, false);
      assert.equal(definition.document.http.authentication, 'none');
    } finally {
      React.act(() => root.unmount());
      dom.window.close();
      Object.assign(global, previous);
    }
  }
);

test(
  'React18 public-parameter editor retains invalid JSON and uses the same JSON/KV value',
  { skip: process.env.TIER0_STREAM_REACT_FIXTURE !== '1' },
  async () => {
    const appRequire = require('node:module').createRequire(resolve(__dirname, '../package.json'));
    const React = appRequire('react');
    const { JSDOM } = require('jsdom');
    const dom = new JSDOM('<html><body><div id="root"></div></body></html>');
    const previous = {
      window: global.window,
      document: global.document,
      IS_REACT_ACT_ENVIRONMENT: global.IS_REACT_ACT_ENVIRONMENT,
    };
    global.window = dom.window;
    global.document = dom.window.document;
    global.IS_REACT_ACT_ENVIRONMENT = true;
    const { createRoot } = appRequire('react-dom/client');
    const component = { exports: {} };
    const element = React.createElement;
    const parameterFile = resolve(__dirname, '../src/pages/uns/components/source-stream/HTTPParameters.tsx');
    const box = ({ children }) => element('div', null, children);
    const antd = {
      Alert: ({ message }) => element('p', null, message),
      Space: box,
      Button: ({ children, ...props }) => element('button', props, children),
      Input: (props) => element('input', props),
      Segmented: ({ options, value, onChange }) =>
        element(
          'select',
          { value, onChange: (event) => onChange(event.target.value) },
          options.map((option) =>
            element('option', { key: option.value, value: option.value, disabled: option.disabled }, option.label)
          )
        ),
    };
    vm.runInNewContext(
      ts.transpileModule(readFileSync(parameterFile, 'utf8'), {
        compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
      }).outputText,
      {
        module: component,
        exports: component.exports,
        require(id) {
          if (id === 'react') return React;
          if (id === 'react/jsx-runtime') return appRequire(id);
          if (id === 'antd') return antd;
          if (id === '@codemirror/lang-json') return { json: () => [] };
          if (id === '@/hooks') return { useTranslate: () => (key) => key };
          if (id === '@/components/help-tooltip') return { default: () => null };
          if (id === '@/components/pro-codemirror')
            return {
              default: ({ value, onChange, readOnly }) =>
                element('textarea', { value, readOnly, onChange: (event) => onChange(event.target.value) }),
            };
          if (id.endsWith('.scss')) return { default: {} };
          throw new Error('unexpected fixture dependency');
        },
      }
    );
    const container = dom.window.document.getElementById('root');
    const root = createRoot(container);
    let value = { nested: { count: 7 }, extension: true };
    let valid = true;
    const validity = (next) => {
      valid = next;
    };
    const render = () =>
      React.act(() =>
        root.render(
          element(component.exports.default, {
            value,
            onChange: (next) => {
              value = next;
              render();
            },
            onValidityChange: validity,
            readOnly: false,
          })
        )
      );
    const change = async (field, next) => {
      const nativeSetter = Object.getOwnPropertyDescriptor(field.constructor.prototype, 'value').set;
      await React.act(async () => {
        nativeSetter.call(field, next);
        field.dispatchEvent(new dom.window.Event('input', { bubbles: true }));
        field.dispatchEvent(new dom.window.Event('change', { bubbles: true }));
      });
    };
    try {
      render();
      await change(container.querySelector('textarea'), '{ broken');
      assert.equal(valid, false);
      assert.equal(container.querySelector('textarea').value, '{ broken');
      assert.equal(container.querySelector('option[value="kv"]').disabled, true);
      assert.deepEqual(JSON.parse(JSON.stringify(value)), { nested: { count: 7 }, extension: true });
      await change(container.querySelector('textarea'), '{"nested":{"count":9},"extension":true}');
      assert.equal(valid, true);
      await change(container.querySelector('select'), 'kv');
      assert.deepEqual(JSON.parse(JSON.stringify(value)), { nested: { count: 9 }, extension: true });
    } finally {
      React.act(() => root.unmount());
      dom.window.close();
      Object.assign(global, previous);
    }
  }
);
const base = {
  name: 'Own source',
  description: '',
  endpoint: 'mqtt://fixture:1883',
  runtimeLocation: 'local',
  protocol: 'mqtt',
  mqtt: { clientId: 'own-client' },
};

test(
  'React18 custom credentials and HTTP JSON stay aligned while existing Server ports are immutable',
  { skip: process.env.TIER0_STREAM_REACT_FIXTURE !== '1' },
  async () => {
    const appRequire = require('node:module').createRequire(resolve(__dirname, '../package.json'));
    const React = appRequire('react');
    const { JSDOM } = require('jsdom');
    const dom = new JSDOM('<html><body><div id="root"></div></body></html>');
    const previous = {
      window: global.window,
      document: global.document,
      IS_REACT_ACT_ENVIRONMENT: global.IS_REACT_ACT_ENVIRONMENT,
    };
    global.window = dom.window;
    global.document = dom.window.document;
    global.IS_REACT_ACT_ENVIRONMENT = true;
    const { createRoot } = appRequire('react-dom/client');
    const element = React.createElement;
    const input = (props) => element('input', props);
    input.Password = (props) => element('input', { ...props, type: 'password' });
    input.TextArea = (props) => element('textarea', props);
    const select = ({ options = [], value, onChange, ...props }) =>
      element(
        'select',
        { ...props, value, onChange: (event) => onChange(event.target.value) },
        options.map((option) =>
          element('option', { key: option.value, value: option.value, disabled: option.disabled }, option.label)
        )
      );
    const antd = {
      Input: input,
      InputNumber: ({ onChange, ...props }) => {
        delete props.precision;
        return element('input', {
          ...props,
          type: 'number',
          onChange: (event) => onChange(Number(event.target.value)),
        });
      },
      Select: select,
      Segmented: select,
      Alert: ({ message }) => element('p', null, message),
      Tag: ({ children }) => element('span', null, children),
      Space: ({ children }) => element('div', null, children),
      Button: ({ children, ...props }) => element('button', props, children),
      Checkbox: ({ children, ...props }) =>
        element('label', null, element('input', { ...props, type: 'checkbox' }), children),
      Collapse: ({ items, defaultActiveKey = [] }) => {
        const [active, setActive] = React.useState(defaultActiveKey);
        return element(
          'div',
          null,
          items.map((item) =>
            element(
              'section',
              { key: item.key },
              element('button', { onClick: () => setActive(active.includes(item.key) ? [] : [item.key]) }, item.label),
              active.includes(item.key) ? item.children : null
            )
          )
        );
      },
    };
    const loaded = new Map();
    const load = (name) => {
      if (loaded.has(name)) return loaded.get(name);
      const component = { exports: {} };
      const componentFile = resolve(__dirname, '../src/pages/uns/components/source-stream/' + name);
      vm.runInNewContext(
        ts.transpileModule(readFileSync(componentFile, 'utf8'), {
          compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
        }).outputText,
        {
          module: component,
          exports: component.exports,
          TextEncoder,
          require(id) {
            if (id === 'react') return React;
            if (id === 'react/jsx-runtime') return appRequire(id);
            if (id === 'antd') return antd;
            if (id === '@codemirror/lang-json') return { json: () => [] };
            if (id === '@/hooks') return { useTranslate: () => (key) => key };
            if (id === '@/utils/limits') return { MAX_LENGTHS: { name: 128 } };
            if (id === '@/components/help-tooltip') return { default: ({ title }) => element('small', null, title) };
            if (id === '@/components/pro-codemirror')
              return {
                default: ({ value, onChange, readOnly }) =>
                  element('textarea', { value, readOnly, onChange: (event) => onChange(event.target.value) }),
              };
            if (id === './source-credentials') return load('source-credentials.ts');
            if (id === './source-types') return load('source-types.ts');
            if (
              ['./HTTPParameters', './OPCUAServerNodes', './VendorConfigFields', './SourceRegisterRanges'].includes(id)
            )
              return { default: () => null };
            if (id.endsWith('.scss')) return { default: {} };
            throw new Error('unexpected fixture dependency: ' + id);
          },
        }
      );
      loaded.set(name, component.exports);
      return component.exports;
    };
    const container = dom.window.document.getElementById('root');
    const root = createRoot(container);
    const change = async (field, next) => {
      const setter = Object.getOwnPropertyDescriptor(field.constructor.prototype, 'value').set;
      await React.act(async () => {
        setter.call(field, next);
        field.dispatchEvent(new dom.window.Event('input', { bubbles: true }));
        field.dispatchEvent(new dom.window.Event('change', { bubbles: true }));
      });
    };
    try {
      const helper = load('source-credentials.ts');
      let draft = { kind: 'none', username: '', secret: '', namedSecrets: [] };
      const credentials = load('SourceCredentialsFields.tsx').default;
      const renderCredentials = () =>
        React.act(() =>
          root.render(
            element(credentials, {
              value: draft,
              onChange: (next) => {
                draft = next;
                renderCredentials();
              },
              canManage: true,
              allowBearer: true,
              allowNamedSecrets: true,
              existing: true,
              existingKeys: ['savedSigningKey'],
            })
          )
        );
      renderCredentials();
      assert.ok(container.querySelector('option[value="custom"]'));
      await change(container.querySelector('select'), 'custom');
      assert.equal(draft.kind, 'custom');
      assert.match(container.textContent, /SourceStream.authCustomHint/);
      assert.match(container.textContent, /savedSigningKey/);
      assert.match(container.textContent, /SourceStream.replaceSecretsHint/);
      assert.equal(container.querySelectorAll('input').length, 0);
      const add = Array.from(container.querySelectorAll('button')).find(
        (button) => button.textContent === 'SourceStream.addNamedSecret'
      );
      await React.act(async () => add.click());
      assert.equal(container.querySelector('input[type="password"]').value, '');
      await change(container.querySelector('input[aria-label="SourceStream.secretName"]'), 'signingKey');
      await change(container.querySelector('input[type="password"]'), 'fixture-only-key');
      assert.deepEqual(
        JSON.parse(
          JSON.stringify(helper.credentialPayload({ protocol: 'http-api', http: { authentication: 'custom' } }, draft))
        ),
        { kind: 'custom', username: '', secret: '', values: { signingKey: 'fixture-only-key' } }
      );
      const editor = load('SourceConfigEditor.tsx').default;
      for (const authentication of ['custom', 'http']) {
        assert.equal(helper.credentialKindForDocument({ protocol: 'http-api', http: { authentication } }), 'custom');
        let value = JSON.stringify({
          ...base,
          protocol: 'http-api',
          endpoint: 'https://fixture.test',
          mqtt: undefined,
          http: { authentication, extension: 'retain' },
        });
        const renderEditor = () =>
          React.act(() =>
            root.render(
              element(editor, {
                key: authentication,
                value,
                onChange: (next) => {
                  value = next;
                  renderEditor();
                },
              })
            )
          );
        renderEditor();
        await change(container.querySelector('input'), 'Edited custom source');
        assert.equal(JSON.parse(value).http.authentication, authentication);
        assert.equal(JSON.parse(value).http.extension, 'retain');
        await change(container.querySelector('select'), 'json');
        assert.equal(container.querySelector('textarea').value, value);
        await change(container.querySelector('select'), 'form');
        assert.equal(container.querySelector('input').value, 'Edited custom source');
      }
      const server = JSON.stringify({
        ...base,
        protocol: 'http-api',
        role: 'server',
        endpoint: 'http://managed',
        mqtt: undefined,
        http: { port: 0 },
      });
      React.act(() =>
        root.render(element(editor, { key: 'new-server', value: server, onChange() {}, creating: true }))
      );
      assert.equal(container.querySelector('input[type="number"]').disabled, false);
      React.act(() =>
        root.render(element(editor, { key: 'existing-server', value: server, onChange() {}, sourceId: 123 }))
      );
      assert.equal(container.querySelector('input[type="number"]').disabled, true);
      assert.match(container.textContent, /SourceStream.httpPortImmutable/);
    } finally {
      React.act(() => root.unmount());
      dom.window.close();
      Object.assign(global, previous);
    }
  }
);

test('Modbus per-resource Unit IDs and read-only flags survive form/JSON roundtrip', () => {
  const document = {
    name: 'Multi-unit gateway',
    description: '',
    endpoint: 'tcp://gateway:502',
    runtimeLocation: 'local',
    protocol: 'modbus-tcp',
    modbus: {
      unitId: 1,
      ranges: [
        {
          id: 'reader',
          table: 'input-register',
          address: 0,
          quantity: 2,
          unitId: 18,
          writable: false,
          extension: 'retained',
        },
      ],
    },
  };
  assert.equal(JSON.stringify(parse(document)), JSON.stringify(document));
  assert.equal(
    parse({ ...document, modbus: { ...document.modbus, ranges: [{ ...document.modbus.ranges[0], unitId: 248 }] } }),
    undefined
  );
  assert.equal(
    parse({
      ...document,
      modbus: { ...document.modbus, ranges: [{ ...document.modbus.ranges[0], writable: 'false' }] },
    }),
    undefined
  );
});

test('connection-only industrial Sources retain optional legacy scope and unknown fields', () => {
  const legacy = {
    ...base,
    mqtt: { ...base.mqtt, qos: 1, topicFilters: ['own/+/data'], extension: { retain: true } },
    extension: 'retain',
  };
  assert.ok(parse(base));
  assert.equal(parse(legacy).mqtt.extension.retain, true);
  assert.equal(parse(legacy).extension, 'retain');
  assert.ok(
    parse({ ...base, protocol: 'opcua', mqtt: undefined, opcua: { securityMode: 'None', securityPolicy: 'None' } })
  );
  assert.ok(parse({ ...base, protocol: 'modbus-tcp', mqtt: undefined, modbus: { unitId: 1 } }));
});

test('managed MQTT/OPC UA Server contracts coexist without accepting an external server endpoint', () => {
  const mqtt = {
    ...base,
    role: 'server',
    endpoint: 'mqtt://managed',
    mqtt: { ...base.mqtt, serverSubscribeTopics: ['down'] },
  };
  assert.ok(parse(mqtt));
  assert.equal(parse({ ...mqtt, endpoint: 'mqtt://external:1883' }), undefined);
  const opcua = {
    ...base,
    protocol: 'opcua',
    role: 'server',
    endpoint: 'opc.tcp://managed',
    mqtt: undefined,
    opcua: {
      securityMode: 'None',
      securityPolicy: 'None',
      serverNodes: [
        {
          id: 'temperature',
          name: 'Temperature',
          nodeClass: 'Variable',
          dataType: 'Double',
          direction: 'bidirectional',
        },
      ],
    },
  };
  assert.ok(parse(opcua));
  assert.equal(
    parse({ ...opcua, opcua: { ...opcua.opcua, serverNodes: [{ id: 'bad', name: 'Bad', nodeClass: 'Unknown' }] } }),
    undefined
  );
});

test('managed HTTP defaults to Bearer and public parameters preserve JSON types and extensions', () => {
  const http = {
    ...base,
    protocol: 'http-api',
    role: 'server',
    endpoint: 'http://managed',
    mqtt: undefined,
    http: { parameters: { nested: { value: 7 }, enabled: true }, port: 0, extension: 'retain' },
  };
  assert.ok(parse(http));
  assert.equal(parse(http).http.extension, 'retain');
  assert.equal(parse({ ...http, endpoint: 'https://external.test' }), undefined);
  for (const port of [1, 1023, 65536, 31000.5])
    assert.equal(parse({ ...http, http: { ...http.http, port } }), undefined);
  for (const port of [0, 1024, 32000, 65535]) assert.ok(parse({ ...http, http: { ...http.http, port } }));
  assert.equal(parse({ ...http, http: { ...http.http, parameters: [] } }), undefined);
  assert.equal(parse({ ...http, http: { ...http.http, anonymous: true } }), undefined);
  assert.ok(parse({ ...http, http: { ...http.http, anonymous: true, authentication: 'none' } }));
  for (const authentication of ['custom', 'http']) {
    const client = {
      ...http,
      role: 'client',
      endpoint: 'https://fixture.test',
      http: { authentication, parameters: { nested: { value: 7 } } },
    };
    assert.ok(parse(client));
    assert.equal(parse(JSON.parse(JSON.stringify(client))).http.authentication, authentication);
    assert.equal(parse({ ...http, http: { ...http.http, authentication } }), undefined);
  }
});

test('named credentials never invent saved values and replace only explicitly supplied maps', () => {
  const helperPath = resolve(__dirname, '../src/pages/uns/components/source-stream/source-credentials.ts');
  const helper = { exports: {}, TextEncoder };
  vm.runInNewContext(
    ts.transpileModule(readFileSync(helperPath, 'utf8'), {
      compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
    }).outputText,
    helper
  );
  const { credentialPayload, namedSecretValues, credentialDraftEntered, credentialKindForDocument } = helper.exports;
  const doc = { protocol: 'http-api', http: { authentication: 'none' } };
  assert.equal(credentialKindForDocument({ protocol: 'http-api', role: 'server', http: {} }), 'bearer');
  assert.equal(
    credentialKindForDocument({ protocol: 'http-api', credentialRef: 'saved-names-only', http: {} }),
    'none'
  );
  const blank = { kind: 'none', username: '', secret: '', namedSecrets: [] };
  for (const authentication of ['custom', 'http'])
    assert.equal(credentialKindForDocument({ protocol: 'http-api', http: { authentication } }), 'custom');
  assert.equal(credentialPayload(doc, blank), undefined);
  assert.equal(credentialPayload(doc, { ...blank, kind: 'custom' }), undefined);
  assert.equal(credentialDraftEntered(blank), false);
  const payload = credentialPayload(doc, { ...blank, namedSecrets: [{ name: 'signingKey', value: 'mock-key' }] });
  assert.deepEqual(JSON.parse(JSON.stringify(payload)), {
    kind: 'custom',
    username: '',
    secret: '',
    values: { signingKey: 'mock-key' },
  });
  assert.equal(
    credentialPayload(doc, { ...blank, kind: 'custom', namedSecrets: [{ name: 'signingKey', value: 'mock-key' }] })
      .kind,
    'custom'
  );
  assert.equal(namedSecretValues({ ...blank, namedSecrets: [{ name: 'x', value: '' }] }), undefined);
  assert.equal(
    namedSecretValues({
      ...blank,
      namedSecrets: [
        { name: 'x', value: 'a' },
        { name: 'x', value: 'b' },
      ],
    }),
    undefined
  );
  const server = { protocol: 'http-api', role: 'server', http: { authentication: 'bearer' } };
  assert.equal(credentialPayload(server, blank), undefined);
  assert.deepEqual(
    JSON.parse(JSON.stringify(credentialPayload(server, { ...blank, kind: 'bearer', secret: 'mock-token' }))),
    { kind: 'custom', username: '', secret: '', values: { token: 'mock-token' } }
  );
  assert.equal(
    credentialPayload(doc, {
      ...blank,
      kind: 'basic',
      username: 'own',
      secret: 'mock',
      namedSecrets: [{ name: 'signature', value: 'own' }],
    }).kind,
    'basic'
  );
});

test('HTTP and OAuth connection contracts remain independently editable', () => {
  const http = {
    ...base,
    protocol: 'http-api',
    endpoint: 'https://api.example.test',
    mqtt: undefined,
    http: { authentication: 'bearer', extra: true },
  };
  assert.equal(parse(http).http.extra, true);
  const oauth = {
    ...http,
    protocol: 'oauth2',
    http: {
      oauth: {
        grantType: 'client_credentials',
        authorizationUrl: '',
        tokenUrl: 'https://api.example.test/token',
        clientId: 'public-id',
        clientAuthentication: 'client_secret_basic',
        scopes: '',
      },
    },
  };
  assert.ok(parse(oauth));
  assert.equal(parse({ ...http, role: 'server' }), undefined);
});

test('invalid JSON, mixed protocols and invalid optional scopes remain unsavable', () => {
  assert.equal(fixture.exports.readSourceDocument('{bad'), undefined);
  assert.equal(parse({ ...base, opcua: {} }), undefined);
  assert.equal(parse({ ...base, role: 'unknown' }), undefined);
  assert.equal(parse({ ...base, mqtt: { clientId: 'own', qos: 3 } }), undefined);
  assert.equal(parse({ ...base, mqtt: { clientId: 'own', topicFilters: [42] } }), undefined);
});

test('numeric historic Topic and UUID Metadata deep links use separate workspaces', () => {
  const index = readFileSync(resolve(__dirname, '../src/pages/uns/index.tsx'), 'utf8');
  assert.match(index, /params\.has\('namespaceId'\) \|\| params\.has\('namespaceKey'\)/);
  assert.match(index, /view === 'namespace' && !legacyTarget/);
  const links = readFileSync(
    resolve(__dirname, '../src/pages/uns/components/source-stream/resource-links.tsx'),
    'utf8'
  );
  assert.match(links, /Number\.isSafeInteger\(Number\(id\)\)/);
  assert.match(links, /if \(numericId === undefined\) return/);
  assert.match(links, /namespaceId: numericId/);
});
