const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

test('existing Agent panel participates in the full-height overview flex workspace', () => {
  const component = fs.readFileSync(path.join(__dirname, 'index.tsx'), 'utf8');
  assert.match(component, /<div className=\{styles\.workspace\}>\s*<IntegrationAgentPanel/);
});

test('Record group controls stay outside the graph viewport rather than covering spheres', () => {
  const component = fs.readFileSync(path.join(__dirname, 'index.tsx'), 'utf8');
  const css = fs.readFileSync(path.join(__dirname, 'index.module.scss'), 'utf8');
  assert.ok(component.indexOf("styles['record-controls']") < component.indexOf("styles['graph-workspace']"));
  const rules = css.match(/\.record-controls\s*\{([\s\S]*?)\n\}/)[1];
  assert.doesNotMatch(rules, /position:\s*absolute/);
  assert.match(rules, /flex-shrink:\s*0/);
});

test('noninteractive sphere tooltips cannot intercept neighboring graph clicks', () => {
  const component = fs.readFileSync(path.join(__dirname, 'components/GraphCanvas.tsx'), 'utf8');
  assert.match(component, /<Tooltip\s+styles=\{\{ root: \{ pointerEvents: 'none' \} \}\}/);
});

test('graph teardown preserves the last measured viewport instead of caching an uninitialized helper', () => {
  const component = fs.readFileSync(path.join(__dirname, 'components/GraphCanvas.tsx'), 'utf8');
  assert.match(component, /sessionLayouts\.get\(cacheKey\)\?\.viewport\s*\|\|/);
  assert.match(component, /viewportReady\.current && api\.viewportInitialized \? api\.getViewport\(\) : undefined/);
  assert.match(component, /if \(Object\.keys\(positions\.current\)\.length\)/);
});

test('initial viewport movement cannot replace the cached fit or restored viewport', () => {
  const component = fs.readFileSync(path.join(__dirname, 'components/GraphCanvas.tsx'), 'utf8');
  assert.match(component, /if \(!viewportReady\.current\) return/);
  assert.match(component, /onMoveEnd=\{\(_, viewport\) => savePositions\(viewport\)\}/);
  assert.match(component, /remember\(cacheKey, positions\.current, viewport \?\? api\.getViewport\(\)\)/);
});

test('read-only legend cannot intercept spheres while its pagination remains interactive', () => {
  const css = fs.readFileSync(path.join(__dirname, 'index.module.scss'), 'utf8');
  const legend = css.slice(css.indexOf('.legend {'), css.indexOf('.legend-kinds {'));
  assert.match(legend, /pointer-events:\s*none/);
  assert.match(legend, /:global\(\.ant-pagination\)\s*\{\s*pointer-events:\s*auto/);
});

test('viewport restoration awaits React Flow and caches the requested viewport', () => {
  const component = fs.readFileSync(path.join(__dirname, 'components/GraphCanvas.tsx'), 'utf8');
  assert.match(component, /await api\.setViewport\(targetViewport\)/);
  assert.match(component, /await api\.fitView\(\{ padding: 0\.22, duration: 0 \}\)/);
  assert.match(component, /if \(cancelled \|\| stopLayout\.current\) return/);
  assert.match(component, /remember\(cacheKey, positions\.current, targetViewport \?\? api\.getViewport\(\)\)/);
});

test('drag completion caches the callback final position rather than the previous store frame', () => {
  const component = fs.readFileSync(path.join(__dirname, 'components/GraphCanvas.tsx'), 'utf8');
  assert.match(component, /onNodeDragStop=\{\(_, node\) =>/);
  assert.match(component, /positions\.current = \{ \.\.\.positions\.current, \[node\.id\]: node\.position \}/);
});
