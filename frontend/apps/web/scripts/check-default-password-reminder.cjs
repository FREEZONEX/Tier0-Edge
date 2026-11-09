// 真实表单登录与密码提醒回归；不改密码、用户权限或注入浏览器认证状态。
// 需要测试账号仍被标记为使用初始密码；不通过重置该标记制造测试前提。
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const { chromium } = require(process.env.TIER0_PLAYWRIGHT_MODULE || 'playwright');

async function main() {
  const base = new URL(process.env.TIER0_SMOKE_BASE_URL || '');
  const target = process.env.TIER0_SMOKE_TARGET_PATH || '/home';
  const username = process.env.TIER0_SMOKE_USERNAME;
  const password = process.env.TIER0_SMOKE_PASSWORD;
  const heading = process.env.TIER0_SMOKE_EXPECTED_HEADING;
  assert(
    base.protocol === 'https:' || ['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname),
    'Use HTTPS or loopback'
  );
  assert(
    !base.username && !base.password && base.pathname === '/' && !base.search && !base.hash,
    'Provide an origin only'
  );
  assert(target.startsWith('/') && !target.startsWith('//'), 'Provide a same-origin target path');
  const destination = new URL(target, base);
  assert.equal(destination.origin, base.origin);
  const expectEditor = destination.searchParams.get('edit') === 'stream';
  assert(username && password, 'Provide the initial-password test account through environment variables');
  const artifacts = process.env.TIER0_SMOKE_ARTIFACTS;
  if (artifacts) await fs.mkdir(artifacts, { recursive: true, mode: 0o700 });
  const report = {
    startedAt: new Date().toISOString(),
    passed: false,
    base: base.origin,
    target,
    authStateInjected: false,
    passwordChanged: false,
    viewports: [],
  };
  const browser = await chromium.launch({
    headless: true,
    executablePath: process.env.TIER0_BROWSER_EXECUTABLE,
    args: process.env.TIER0_BROWSER_NO_SANDBOX === '1' ? ['--no-sandbox'] : [],
  });
  try {
    // 每轮使用全新非持久化上下文，不继承前一轮的 Token、提醒状态或缓存。
    for (const [width, height] of [
      [1440, 900],
      [1280, 800],
    ]) {
      const context = await browser.newContext({ viewport: { width, height } });
      const page = await context.newPage();
      let pageErrors = 0;
      page.on('pageerror', () => {
        pageErrors += 1;
      });
      const result = { width, height, passed: false, checks: [] };
      report.viewports.push(result);
      try {
        await page.goto(destination.href, { waitUntil: 'domcontentloaded' });
        await page.locator('input[type="text"]').fill(username);
        await page.locator('input[type="password"]').fill(password);
        await page.locator('button[type="submit"]').click();
        await page.waitForURL((url) => url.origin === base.origin && url.pathname === destination.pathname);
        if (heading) await page.getByRole('heading', { name: heading, exact: true }).waitFor();
        result.checks.push('normal form login reaches target');
        const later = page.getByRole('button', { name: /^(稍\s*后|Later)$/i });
        await later.waitFor({ state: 'visible' });
        if (expectEditor) {
          const background = page.locator('.ant-modal-wrap[inert][aria-hidden="true"]').first();
          await background.waitFor({ state: 'attached' });
          assert.equal(await background.isVisible(), false, 'Background editor must not intercept the reminder');
          const reminder = page.getByRole('dialog').filter({ has: later });
          await reminder.locator('input').first().click();
          for (let index = 0; index < 8; index += 1) {
            await page.keyboard.press('Tab');
            assert(
              await reminder.evaluate((element) => element.contains(element.ownerDocument.activeElement)),
              'Keyboard focus must remain in the foreground reminder'
            );
          }
          result.checks.push('deep-linked editor is inert and keyboard focus stays in the reminder');
        }
        const before = await page.evaluate(() => performance.eventCounts?.get('click'));
        await later.click();
        await later.waitFor({ state: 'hidden', timeout: 5000 });
        const after = await page.evaluate(() => performance.eventCounts?.get('click'));
        if (typeof before === 'number' && typeof after === 'number') {
          assert(after > before, 'Native click must reach the page');
        }
        result.checks.push('ordinary native click dismisses reminder');
        if (expectEditor) {
          await page.locator('.pro-modal').first().waitFor({ state: 'visible' });
          assert.equal(await page.locator('.ant-modal-wrap[inert]').count(), 0);
          result.checks.push('existing editor resumes without being closed');
        }
        await page.reload({ waitUntil: 'domcontentloaded' });
        if (heading) await page.getByRole('heading', { name: heading, exact: true }).waitFor();
        else if (expectEditor) await page.locator('.pro-modal').first().waitFor({ state: 'visible' });
        else await page.locator('[role="tabpanel"]').first().waitFor();
        assert.equal(await later.isVisible(), false, 'Reminder must remain dismissed in the same session');
        result.checks.push('refresh retains ordinary session dismissal');
        const pageWidth = await page.locator('html').evaluate((element) => element.scrollWidth);
        assert(pageWidth <= width, 'Page must not overflow horizontally');
        assert.equal(pageErrors, 0, 'No uncaught page errors');
        result.checks.push('viewport and uncaught-error checks');
        if (artifacts) await page.screenshot({ path: path.join(artifacts, `reminder-${width}.png`) });
        result.passed = true;
      } finally {
        await context.close();
      }
    }
    report.passed = true;
  } finally {
    await browser.close();
    report.finishedAt = new Date().toISOString();
    if (artifacts) {
      await fs.writeFile(path.join(artifacts, 'reminder-result.json'), JSON.stringify(report, null, 2), {
        flag: 'wx',
        mode: 0o600,
      });
    }
    console.log(JSON.stringify(report));
  }
}

main().catch(() => {
  // 不把登录失败响应、表单数据或账号凭据打印到共享日志。
  console.error('Initial-password reminder smoke failed; inspect the bounded test report');
  process.exitCode = 1;
});
