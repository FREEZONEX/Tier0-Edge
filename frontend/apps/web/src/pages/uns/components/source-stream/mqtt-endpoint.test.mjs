import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readMqttEndpoint, updateMqttEndpoint } from './mqtt-endpoint.ts';

test('MQTT fields preserve TLS, explicit ports, IPv6 and trailing slash when untouched', () => {
  for (const endpoint of ['mqtt://broker', 'mqtts://broker', 'mqtts://broker:1883/', 'mqtt://[::1]:1884']) {
    assert.equal(updateMqttEndpoint(endpoint, {}), endpoint);
  }
  assert.equal(readMqttEndpoint('mqtt://broker').port, 1883);
  assert.equal(readMqttEndpoint('mqtts://broker').port, 8883);
  assert.equal(readMqttEndpoint('mqtt://[::1]:1884').host, '::1');
});

test('editing one MQTT field retains the other connection values', () => {
  assert.equal(updateMqttEndpoint('mqtts://broker:8884/', { host: 'other' }), 'mqtts://other:8884/');
  assert.equal(updateMqttEndpoint('mqtts://broker:8884/', { port: 9883 }), 'mqtts://broker:9883/');
  assert.equal(updateMqttEndpoint('mqtt://broker:1884', { scheme: 'mqtts' }), 'mqtts://broker:1884');
  assert.equal(updateMqttEndpoint('mqtt://broker', { scheme: 'mqtts' }), 'mqtts://broker');
  assert.equal(updateMqttEndpoint('mqtts://broker:8884', { host: '2001:db8::2' }), 'mqtts://[2001:db8::2]:8884');
  assert.equal(updateMqttEndpoint('mqtt://broker:1883', { host: '' }), 'mqtt://:1883');
  assert.equal(updateMqttEndpoint('mqtt://:1883', { host: 'new' }), 'mqtt://new:1883');
});

test('unsupported endpoint shapes remain intact for the raw URL editor', () => {
  for (const endpoint of ['ws://broker/mqtt', 'mqtt://user:secret@broker', 'mqtt://broker/path', 'invalid']) {
    assert.equal(readMqttEndpoint(endpoint), undefined);
    assert.equal(updateMqttEndpoint(endpoint, { host: 'other' }), endpoint);
  }
});
