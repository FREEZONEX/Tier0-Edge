// Pure UI contract checks. These fixtures do not prove live vendor connectivity.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import vm from 'node:vm';

const appRoot = resolve(fileURLToPath(new URL('..', import.meta.url)));
const require = createRequire(resolve(appRoot, 'package.json'));
const ts = require('typescript');
const directory = resolve(appRoot, 'src/pages/uns/components/source-stream');
function load(file) {
  const fixture = { exports: {} };
  vm.runInNewContext(
    ts.transpileModule(readFileSync(resolve(directory, file), 'utf8'), {
      compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
    }).outputText,
    { module: fixture, exports: fixture.exports }
  );
  return fixture.exports;
}
const types = load('source-types.ts');
const programs = load('program-document.ts');
const vendorPrograms = load('vendor-program.ts');
const source = (sourceType, readOnly = true) => ({
  id: 8101,
  protocol: 'http-api',
  document: { sourceType, protocol: 'http-api', role: 'client' },
  resources: [{ id: `vendor:${sourceType}:operation`, metadata: { sourceType, readOnly } }],
});

test('eleven known Source profiles are client-only and cannot replace industrial protocols', () => {
  assert.equal(types.vendorSourceTypes.length, 11);
  for (const sourceType of types.vendorSourceTypes) {
    assert.equal(types.validSourceType({ sourceType, protocol: 'http-api', role: 'client' }), true);
    assert.equal(types.validSourceType({ sourceType, protocol: 'mqtt' }), false);
    assert.equal(types.validSourceType({ sourceType, protocol: 'http-api', role: 'server' }), false);
  }
  assert.equal(types.validSourceType({ protocol: 'mqtt' }), true);
  assert.equal(types.validSourceType({ sourceType: 'invented', protocol: 'http-api' }), false);
});

test('vendor operations insert only declared read or manual invoke and preserve generic HTTP', () => {
  const item = source('sap-odata');
  assert.deepEqual(Array.from(programs.sourceInsertionModes(item, item.resources[0].id)), ['read']);
  assert.equal(programs.sourceInsertionSnippet(item, item.resources[0].id, 'request', 'test'), undefined);
  const writable = source('sap-odata', false);
  assert.deepEqual(Array.from(programs.sourceInsertionModes(writable, writable.resources[0].id)), ['invoke']);
  assert.match(
    programs.sourceInsertionSnippet(writable, writable.resources[0].id, 'invoke', 'manual-write'),
    /source\.invoke/
  );
  assert.doesNotMatch(programs.sourceProgram(writable), /source\.invoke/);
  assert.doesNotMatch(programs.sourceProgram(writable), /api\.stream\.every/);
  assert.equal(programs.sourceInsertionModes(item, 'vendor:teamcenter:operation').length, 0);
  assert.deepEqual(
    Array.from(programs.sourceInsertionModes({ protocol: 'http-api', document: {}, resources: [] }, '')),
    ['request']
  );
});
test('default Plex sample prefers the bounded workcenters operation instead of jobs or writes', () => {
  const item = source('plex-mes');
  item.resources = ['hold', 'jobs', 'workcenters'].map((operation) => ({
    id: `vendor:plex-mes:${operation}`,
    metadata: { sourceType: 'plex-mes', readOnly: operation !== 'hold' },
  }));
  assert.match(programs.sourceProgram(item), /vendor:plex-mes:workcenters/);
  assert.doesNotMatch(programs.sourceProgram(item), /vendor:plex-mes:jobs|source\.invoke/);
});

test('profile parameter discovery uses descriptor metadata, not client-invented credentials', () => {
  assert.deepEqual(Array.from(types.vendorParameterKeys({ parameterKeys: ['tenantId', 'pcn'] })), ['tenantId', 'pcn']);
  assert.deepEqual(
    Array.from(
      types.vendorParameterKeys({
        configSchema: { properties: { http: { properties: { parameters: { properties: { acctId: {} } } } } } },
      })
    ),
    ['acctId']
  );
});
test('declared credential alternatives preserve the visible draft and require at least one selected secret', () => {
  const draft = {
    kind: 'custom',
    namedSecrets: [
      { name: 'password', value: 'fixture-password' },
      { name: 'appSecret', value: '' },
    ],
  };
  const profile = { credentialAnyOf: ['password', 'appSecret'] };
  const submitted = types.vendorCredentialDraft(profile, draft);
  assert.equal(submitted.namedSecrets.length, 1);
  assert.equal(submitted.namedSecrets[0].name, 'password');
  assert.equal(draft.namedSecrets.length, 2);
  assert.equal(types.vendorCredentialDraft(undefined, draft).namedSecrets.length, 2);
  assert.equal(
    types.vendorCredentialDraft(profile, {
      namedSecrets: [
        { name: 'password', value: '' },
        { name: 'appSecret', value: '' },
      ],
    }).namedSecrets.length,
    2
  );
});

test('complete native read writes the selected Topic before staging the page cursor', async () => {
  const item = source('sap-odata');
  const body = vendorPrograms.vendorCollectionBody(item, item.resources[0].id, 'opaque-topic-id', 'collect');
  assert.ok(body.split('\n').length > 15);
  assert.match(body, /"8101"/);
  assert.match(body, /"opaque-topic-id"/);
  const fixture = { exports: () => {} };
  vm.runInNewContext(programs.registrationSnippet(body), { module: fixture });
  let handler;
  const calls = [];
  fixture.exports({
    sources: {
      use: () => ({
        read: async (ref, options) => {
          calls.push(['read', ref, options.next]);
          return { status: 200, data: { id: '9007199254740993' }, pagination: { next: 'safe-next-page' } };
        },
      }),
    },
    uns: { use: () => ({ write: async (data) => calls.push(['write', data.data.id]) }) },
    stream: {
      onRequest: (_, callback) => {
        handler = callback;
      },
      checkpoint: {
        get: async () => ({ next: 'previous-safe-page' }),
        set: async (value) => calls.push(['checkpoint', value.next]),
      },
    },
  });
  await handler({ input: {} });
  assert.deepEqual(calls, [
    ['read', 'vendor:sap-odata:operation', 'previous-safe-page'],
    ['write', '9007199254740993'],
    ['checkpoint', 'safe-next-page'],
  ]);
});

test('unknown or write resources cannot produce an automatically writing collection example', () => {
  const item = source('pi-web-api', false);
  assert.equal(vendorPrograms.vendorCollectionBody(item, item.resources[0].id, 'topic', 'collect'), undefined);
  assert.equal(vendorPrograms.vendorCollectionBody(item, 'unknown', 'topic', 'collect'), undefined);
});
