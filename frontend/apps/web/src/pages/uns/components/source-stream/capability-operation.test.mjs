import { test } from 'node:test';
import assert from 'node:assert/strict';
import { operationReady, readinessReasonKey } from './capability-operation.ts';
test('UI uses operation grants and actual proof expiry, never aggregate read-ready', () => {
  const operation = {
    operation: 'execute',
    state: 'ready',
    releaseEnabled: true,
    allowed: true,
    enabled: true,
    availability: 'available',
    requiresResourceCheck: true,
    checkedAt: '2026-10-28T00:00:00Z',
    validUntil: '2026-10-28T00:00:02Z',
  };
  assert.equal(operationReady(operation, Date.parse('2026-10-28T00:00:01Z')), true);
  assert.equal(operationReady(operation, Date.parse(operation.validUntil)), false);
  for (const patch of [
    { state: 'unavailable' },
    { enabled: false },
    { allowed: false },
    { releaseEnabled: false },
    { validUntil: 'bad' },
  ])
    assert.equal(operationReady({ ...operation, ...patch }, 0), false);
  assert.equal(operationReady(undefined), false);
  const read = { ...operation, operation: 'read', validUntil: undefined };
  assert.equal(operationReady(read), true);
  const disable = { ...read, operation: 'disable' };
  assert.equal(operationReady(disable), true);
});
test('unknown/private readiness reason falls back to a localized safe message', () => {
  assert.equal(readinessReasonKey('postgres://private-host/secret'), 'SourceStream.readiness.dependencies');
  assert.equal(readinessReasonKey('READINESS_EXPIRED'), 'SourceStream.readiness.expired');
});
