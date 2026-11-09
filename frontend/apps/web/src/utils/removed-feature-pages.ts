// Retired components must also remain hidden in saved menus and recent entries.
const removedPageRoots = new Set([
  'project',
  'anchor',
  'vision',
  'agent-svc',
  'flow',
  'flows',
  'collection-flow',
  'source-flow',
  'event-flow',
  'eventflow',
  'nodered',
  'node-red',
  'notebook',
  'marimo',
  'mqtt-auth',
]);

export const isRemovedFeaturePath = (path?: string) => {
  const value = String(path || '').trim();
  if (!value.startsWith('/') || value.startsWith('//')) return false;
  const root = value.split(/[?#]/, 1)[0].split('/')[1]?.toLowerCase();
  return removedPageRoots.has(root);
};

const removedPageResources = new Set([
  'project.view',
  'anchor.model',
  'anchor.scene',
  'vision.camera.page',
  'flow.collection.page',
  'flow.event.page',
  'flow.runtime.view',
  'flow.runtime.edit',
  'notebook.view',
]);

export const isRemovedFeatureEntry = (item: { resourceKey?: string; url?: string; code?: string }) =>
  removedPageResources.has(String(item.resourceKey || '')) ||
  isRemovedFeaturePath(item.url) ||
  isRemovedFeaturePath(`/${item.code || ''}`);
