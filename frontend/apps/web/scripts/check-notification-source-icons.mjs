// 从 frontend 执行：node apps/web/scripts/check-notification-source-icons.mjs
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { runInNewContext } from 'node:vm';
import ts from 'typescript';
import { BellDot, Box } from 'lucide-react';
import { renderToStaticMarkup } from 'react-dom/server';

const source = readFileSync(
  new URL('../src/components/notification-center/notification-source-icon.tsx', import.meta.url),
  'utf8'
);
const { outputText } = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2022 },
});
const compiled = { exports: {} };
runInNewContext(outputText, { module: compiled, exports: compiled.exports, require: createRequire(import.meta.url) });
const { resolveNotificationSourceIcon, renderNotificationSourceIcon } = compiled.exports;

for (const sourceType of ['app', 'platform']) {
  for (const sourceName of ['UNS', 'Event Flow', 'License', 'Project', 'App']) {
    const item = { sourceType, sourceName, sourceId: 'platform-uns', targetPath: '/collection-flow' };
    assert.equal(resolveNotificationSourceIcon(item), sourceType === 'app' ? Box : BellDot);
    const markup = renderToStaticMarkup(renderNotificationSourceIcon(item));
    assert.match(markup, sourceType === 'app' ? /lucide-box/ : /lucide-bell-dot/);
    assert.match(markup, /stroke-width="2"/);
    assert.match(markup, /width="14"/);
  }
}
assert.equal(resolveNotificationSourceIcon({}), BellDot);
const iconUrl = '/api/core/launchpad/apps/201/icon';
const image = renderToStaticMarkup(renderNotificationSourceIcon({ sourceType: 'app', sourceIconUrl: iconUrl }));
assert.match(image, /<img/);
assert.ok(image.includes(`src="${iconUrl}"`));
const system = renderToStaticMarkup(
  renderNotificationSourceIcon({ sourceType: 'platform', sourceIconUrl: iconUrl }, 12)
);
assert.match(system, /lucide-bell-dot/);
assert.match(system, /width="12"/);
assert.doesNotMatch(system, /<img/);
console.log('Notification source icons: Cloud glyph mapping, app images and compact sizes passed.');
