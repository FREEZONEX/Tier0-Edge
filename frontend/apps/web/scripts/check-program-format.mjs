import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { test } from 'node:test';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
const __dirname = path.dirname(fileURLToPath(import.meta.url));
const appRequire = createRequire(path.resolve(__dirname, '../package.json'));
const ts = appRequire('typescript');
const file = path.resolve(__dirname, '../src/pages/uns/components/source-stream/program-format.ts');
const output = ts.transpileModule(fs.readFileSync(file, 'utf8'), {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
}).outputText;
const fixture = { exports: {} };
// Load the formatter module only. Input scripts are parsed as text, never executed.
vm.runInNewContext(output, { module: fixture, exports: fixture.exports, require: appRequire }, { filename: file });
const { formatProgramScript } = fixture.exports;

test('formats a complete multi-route HTTP program without changing literals', () => {
  const code =
    'module.exports=api=>{const server=api.sources.use({id:"123",resources:[]}); const client=api.sources.use({id:"124",resources:[]}); api.stream.onRequest({id:"health",source:server,method:"GET",path:"/health"},()=>({status:200,body:{ok:true}})); api.stream.onRequest({id:"orders",source:server,method:"POST",path:"/orders/:id"},async context=>{const params=await client.parameters();const secrets=await client.secrets();const response=await client.request({path:"/orders",method:"POST",json:{id:context.request.params.id,appId:params.appId},headers:{Authorization:`Bearer ${secrets.token}`}});return{status:response.status,body:response.data};});};';
  const result = formatProgramScript(code);
  assert.equal(result.valid, true);
  assert.ok(result.code.split('\n').length > 20);
  assert.ok(result.code.includes('path: "/orders/:id"'));
  assert.ok(result.code.includes('`Bearer ${secrets.token}`'));
  assert.equal(code.includes('\n'), false, 'the input text is immutable');
});

for (const [name, code] of [
  ['regex and escaped slash', 'const pattern=/orders\\/([a-z]+)\\s*/gi; const valid=pattern.test("orders/abc");'],
  ['template and nested expression', 'const text=`line 1\n${JSON.stringify({value: input.value})}\nline 3`;'],
  ['block and line comments', '/* multiline\n * retained comment\n */\nconst value=1;// end\nconst next=value+1;'],
  ['ASI array expression', 'const value = 1\n;[value].forEach(item=>item);'],
]) {
  test(`preserves parse structure: ${name}`, () => {
    const result = formatProgramScript(code);
    assert.equal(result.valid, true);
    assert.ok(result.code.length > 0);
  });
}

test('rejects a formatter ASI change and keeps return/postfix input verbatim', () => {
  const code = 'function f(){return\n{ok:true}}\nlet a=1,b=2; a\n++b;';
  const result = formatProgramScript(code);
  assert.equal(result.valid, false);
  assert.equal(result.code, code);
});

for (const code of ['module.exports = api => {', 'const secret = "unterminated', 'const x = ;']) {
  test(`invalid input is preserved verbatim: ${JSON.stringify(code)}`, () => {
    const result = formatProgramScript(code);
    assert.equal(result.valid, false);
    assert.equal(result.code, code);
  });
}

test('does not execute input and preserves empty drafts', () => {
  const code = 'globalThis.shouldNeverRun = true; throw new Error("never execute");';
  assert.equal(formatProgramScript(code).valid, true);
  assert.equal(globalThis.shouldNeverRun, undefined);
  assert.equal(formatProgramScript('   ').code, '   ');
});

test('read-only preview cannot persist or invoke scripts', () => {
  const preview = fs.readFileSync(
    path.resolve(__dirname, '../src/pages/uns/components/source-stream/ProgramScriptView.tsx'),
    'utf8'
  );
  assert.match(preview, /readOnly/);
  assert.match(preview, /editable=\{false\}/);
  assert.match(preview, /value: 'original'/);
  assert.doesNotMatch(preview, /sourceStreamApi|eval\(|new Function/);
  assert.doesNotMatch(preview.slice(preview.indexOf('<ProCodemirror')), /onChange/);
});

test('manifest and Source interfaces display method/path and exact Source ownership', () => {
  const manifest = fs.readFileSync(
    path.resolve(__dirname, '../src/pages/uns/components/source-stream/ProgramManifestView.tsx'),
    'utf8'
  );
  const source = fs.readFileSync(
    path.resolve(__dirname, '../src/pages/uns/components/source-stream/SourceDetail.tsx'),
    'utf8'
  );
  assert.match(manifest, /entry\.method/);
  assert.match(manifest, /entry\.path/);
  assert.match(source, /entry\.sourceId === String\(sourceId\)/);
  assert.match(source, /active \?\? stream\.draftManifest/);
});
