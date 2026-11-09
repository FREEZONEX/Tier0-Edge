'use strict';

const assert = require('node:assert/strict');
const { createRequire } = require('node:module');
const path = require('node:path');
const { test } = require('node:test');

const appRequire = createRequire(path.resolve(__dirname, '../package.json'));
const ts = appRequire('typescript');
const apiFile = path.resolve(__dirname, '../src/apis/core-api/source-stream.ts');

test('current write APIs accept ProgramDocument and reject both historical document types', () => {
  const fixtureFile = path.join(path.dirname(apiFile), '__stream_write_contract__.ts');
  const adapterFile = path.join(path.dirname(apiFile), '__stream_write_adapter__.d.ts');
  const requestFile = path.join(path.dirname(apiFile), '__stream_write_request__.d.ts');
  const source = [
    `import { sourceStreamApi, type ProgramDocument, type StreamDocument, type ScriptStreamDocument, type Stream } from './source-stream';`,
    'declare const current: ProgramDocument;',
    'declare const legacy: StreamDocument;',
    'declare const script: ScriptStreamDocument;',
    'const history: Stream["document"][] = [current, legacy, script];',
    'void history;',
  ];
  const calls = (document) => [
    `sourceStreamApi.createStream(${document});`,
    `sourceStreamApi.updateStream(1, ${document}, { expectedRevision: 1 });`,
    `sourceStreamApi.validate(${document}, 0);`,
    `sourceStreamApi.test(${document}, { data: {} }, 0, 'input');`,
  ];
  source.push(...calls('current'));
  const invalidLines = new Set();
  for (const document of ['legacy', 'script']) {
    for (const call of calls(document)) {
      invalidLines.add(source.length);
      source.push(call);
    }
  }

  // Compile the production API module. Only transport declarations are isolated;
  // no request or device operation runs and no files are written.
  const virtualFiles = new Map([
    [fixtureFile, source.join('\n')],
    [adapterFile, `export declare const coreApi: {
      get(...args: unknown[]): Promise<unknown>;
      post(...args: unknown[]): Promise<unknown>;
      put(...args: unknown[]): Promise<unknown>;
      delete(...args: unknown[]): Promise<unknown>;
    };`],
    [requestFile, `export interface AxiosWrapperRequestConfig { signal?: AbortSignal; }
      export declare class ApiWrapper {
        constructor(prefix: string);
        post(...args: unknown[]): Promise<unknown>;
      }`],
  ]);
  const options = {
    strict: true,
    noEmit: true,
    skipLibCheck: true,
    types: [],
    target: ts.ScriptTarget.ES2022,
    module: ts.ModuleKind.ESNext,
    moduleResolution: ts.ModuleResolutionKind.Bundler,
  };
  const host = ts.createCompilerHost(options);
  const readFile = host.readFile.bind(host);
  const fileExists = host.fileExists.bind(host);
  host.readFile = (file) => virtualFiles.get(file) ?? readFile(file);
  host.fileExists = (file) => virtualFiles.has(file) || fileExists(file);
  host.resolveModuleNames = (names, containingFile) => names.map((name) => {
    if (containingFile === apiFile && name === './core-adapter') return { resolvedFileName: adapterFile };
    if (containingFile === apiFile && name === '@/utils/request') return { resolvedFileName: requestFile };
    return ts.resolveModuleName(name, containingFile, options, host).resolvedModule;
  });
  const program = ts.createProgram([fixtureFile], options, host);
  const diagnostics = ts.getPreEmitDiagnostics(program);
  assert.equal(diagnostics.length, invalidLines.size,
    diagnostics.map((d) => ts.flattenDiagnosticMessageText(d.messageText, '\n')).join('\n'));
  for (const diagnostic of diagnostics) {
    assert.equal(diagnostic.file?.fileName, fixtureFile);
    assert.equal(diagnostic.code, 2345, 'historical documents must be rejected as write arguments');
    const line = diagnostic.file.getLineAndCharacterOfPosition(diagnostic.start).line;
    assert.ok(invalidLines.delete(line), 'each historical write call must fail independently');
  }
  assert.equal(invalidLines.size, 0);
});
