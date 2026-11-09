// Node 22: node --experimental-strip-types scripts/check-integration-agent-context.mjs
// 纯快照/源码门禁合同回归；不替代浏览器权限撤销和真实模型 E2E。
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { integrationAgentContext as context, topicAgentSelection } from '../src/pages/uns/agent/integration-context.ts';
import { canReadUnsMetadata } from '../src/pages/uns/metadata-permission.ts';

assert.equal(canReadUnsMetadata({ resourceKeys: ['uns.read', 'uns.stream'] }, true), true);
assert.equal(canReadUnsMetadata({ resourceKeys: ['uns.edit', 'uns.stream'] }, true), false);
assert.equal(canReadUnsMetadata({ resourceKeys: ['uns.page'] }, true), false);
assert.equal(canReadUnsMetadata({ resourceKeys: [] }, true), false);
assert.equal(canReadUnsMetadata(undefined, true), false);
assert.equal(canReadUnsMetadata(undefined, undefined), false);
assert.equal(canReadUnsMetadata({ superAdmin: true }, true), true);
assert.equal(canReadUnsMetadata(undefined, false), true);

const source = {
  id: 1,
  name: 'Source',
  protocol: 'mqtt',
  revision: 2,
  desiredState: 'enabled',
  health: 'healthy',
  resources: [],
  document: { role: 'server', endpoint: 'private', credentialRef: 'secret' },
};
assert.equal(context('source', source).id, 1);
assert.ok(!JSON.stringify(context('source', source)).match(/private|secret|endpoint|credential/));
assert.equal(context('source'), undefined);
assert.equal(context('namespace', source), undefined);
assert.equal(context('source', { ...source, resources: null }).resourceCount, 0);
const stream = {
  id: 2,
  name: 'Stream',
  revision: 3,
  status: 'draft',
  activeVersion: 0,
  model: 'publish',
  sourceId: 0,
  document: {
    topicIds: [10],
    program: { sources: [{ sourceId: 1 }, { sourceId: 4 }], trigger: { kind: 'interval' } },
    script: { code: 'private secret' },
  },
};
assert.deepEqual(context('stream', undefined, stream).dependencySourceIds, [1, 4]);
assert.deepEqual(
  context('stream', undefined, { ...stream, document: { topicIds: [], script: {} } }).dependencySourceIds,
  []
);
assert.ok(!JSON.stringify(context('stream', undefined, stream)).match(/private|secret|script/));

const topic = { id: 'topic-a', kind: 'topic', data: { name: 'Temperature', parentId: 'line', fields: [] } };
const parents = {
  line: { id: 'line', kind: 'folder', data: { name: 'Line', parentId: 'factory' } },
  factory: { id: 'factory', kind: 'folder', data: { name: 'Factory' } },
};
assert.deepEqual(topicAgentSelection(topic, [parents.factory, parents.line]), {
  id: 'topic-a',
  namespace: 'Factory/Line/Temperature',
});
assert.deepEqual(topicAgentSelection({ ...topic, data: { name: 'Root topic' } }, []), {
  id: 'topic-a',
  namespace: 'Root topic',
});
assert.throws(() => topicAgentSelection(topic, []), /Unavailable Topic parent/);
assert.throws(() => topicAgentSelection({ ...topic, deletedAt: 1 }, []), /Invalid Topic/);
assert.throws(
  () => topicAgentSelection(topic, [{ ...parents.line, data: { name: 'Loop', parentId: 'line' } }]),
  /Cyclic/
);
assert.throws(
  () => topicAgentSelection(topic, [{ ...parents.line, deletedAt: 1 }, parents.factory]),
  /Unavailable Topic parent/
);
const deepParents = Array.from({ length: 100 }, (_, i) => ({
  id: `level-${i}`,
  kind: 'folder',
  data: { name: `Level ${i}`, parentId: i ? `level-${i - 1}` : undefined },
}));
assert.equal(
  topicAgentSelection({ ...topic, data: { ...topic.data, parentId: 'level-99' } }, deepParents).namespace,
  [...deepParents.map((parent) => parent.data.name), topic.data.name].join('/')
);

const workspace = readFileSync(
  new URL('../src/pages/uns/components/source-stream/IntegrationWorkspace.tsx', import.meta.url),
  'utf8'
);
const panel = readFileSync(new URL('../src/pages/uns/agent/integration-agent-panel.tsx', import.meta.url), 'utf8');
assert.match(
  workspace,
  /licenseModules\?\.unsAgent !== false && hasPermission\(ButtonPermission\['uns\.agentView'\]\)/
);
assert.match(workspace, /ownerId && sourceAllowed \? await sourceStreamApi\.source\(ownerId\) : undefined/);
assert.match(workspace, /agentAllowed=\{agentAllowed\}/);
assert.match(panel, /if \(!agentAllowed\) return <>\{children\}<\/>/);
assert.ok(panel.indexOf('if (!agentAllowed) return') < panel.indexOf('<AiChatSetupGate'));
assert.match(panel, /if \(!agentAllowed\) setOpen\(false\)/);
assert.match(panel, /currentObjectAllowed=\{allowed\}/);
assert.match(
  workspace,
  /permissionKey=\{`\$\{agentPermissionKey\(sourceAllowed, streamAllowed\)\}:\$\{metadataAllowed\}`\}/
);
assert.match(workspace, /const metadataAllowed = canReadUnsMetadata\(user, authEnable\)/);
assert.doesNotMatch(workspace, /uns\.fileDetail/);
assert.match(
  workspace,
  /metadataAllowed &&[\s\S]+?await metadataApi.find\('topic', route.metaId!, false, controller.signal\)/
);
assert.match(workspace, /metadataApi.namespaceAncestors\(topic.data.parentId, controller.signal\)/);
assert.doesNotMatch(workspace, /metadataApi.find\('folder'/);
assert.match(workspace, /controller.abort\(\)/);
const readme = readFileSync(
  new URL('../src/pages/uns/components/source-stream/StreamReadme.tsx', import.meta.url),
  'utf8'
);
assert.match(readme, /const metadataAllowed = canReadUnsMetadata\(user, authEnable\)/);
assert.doesNotMatch(readme, /uns\.fileDetail/);
assert.match(workspace, /initialTopic\?\.id === route.metaId/);
assert.match(workspace, /selectedNode=\{selectedTopic\}/);
assert.match(panel, /selectedNode=\{allowed \? selectedNode : null\}/);
assert.match(panel, /<AgentPermissionBoundary permissionKey=\{permissionKey\}>/);
console.log('Integration Agent: snapshot, Topic context and permission gate contracts passed (runtime/E2E separate)');
