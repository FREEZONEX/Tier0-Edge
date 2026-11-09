// Run with Node >= 22.20: node frontend/apps/web/scripts/check-notification-app-links.mjs
import assert from 'node:assert/strict';
import { resolveAppNotificationUrl } from '../src/utils/app-url.ts';

globalThis.window = { location: { origin: 'http://platform.internal:8088' } };

const cases = [
  ['http://app.internal/', '/records', 'http://app.internal/records'],
  ['http://app.internal', '/', 'http://app.internal/'],
  ['https://app.example/', '/records?project=1&tab=all#latest', 'https://app.example/records?project=1&tab=all#latest'],
  ['/apps/demo/', '/records', 'http://platform.internal:8088/apps/demo/records'],
  ['/apps/demo', '/records', 'http://platform.internal:8088/apps/demo/records'],
  [
    '/apps/demo/?lang=zh&tab=default',
    '/records?tab=all#latest',
    'http://platform.internal:8088/apps/demo/records?tab=all&lang=zh#latest',
  ],
  [
    'http://app.internal/',
    '/records?q=https%3A%2F%2Fexample.com',
    'http://app.internal/records?q=https%3A%2F%2Fexample.com',
  ],
  ['http://app.internal/?lang=zh#home', null, 'http://app.internal/?lang=zh#home'],
];

for (const [entry, path, expected] of cases) {
  assert.equal(resolveAppNotificationUrl(entry, path), expected, `${entry} + ${path}`);
}

for (const path of [
  '',
  'records',
  '//evil.example',
  '/\\evil.example',
  '/\n/evil.example',
  '/https://evil.example',
  '/../other',
  '/%2e%2e/other',
  '/%2f%2fevil.example',
  '/%5cother',
  '/records%00',
  '/invalid%',
]) {
  assert.equal(resolveAppNotificationUrl('/apps/demo/', path), '', `unsafe path: ${JSON.stringify(path)}`);
}
assert.equal(resolveAppNotificationUrl('javascript:alert(1)', '/records'), '');

// AppDetail adds authentication through URLSearchParams, never after the fragment.
const authenticated = new URL(resolveAppNotificationUrl('http://app.internal/', '/records?_token=untrusted#latest'));
authenticated.searchParams.set('_token', 'session-token');
assert.equal(authenticated.searchParams.get('_token'), 'session-token');
assert.equal(authenticated.hash, '#latest');
assert.equal(authenticated.searchParams.getAll('_token').length, 1);

console.log(`App notification links: ${cases.length} routing cases and unsafe-path checks passed.`);
