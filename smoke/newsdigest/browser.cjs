const assert = require('node:assert/strict');
const { mkdir } = require('node:fs/promises');
const { join } = require('node:path');
const [playwright, origin, screenshots] = process.argv.slice(2);
const { chromium, expect } = require(join(playwright, 'test'));
const editions = require('./fixture.cjs');

async function main() {
  await mkdir(screenshots, { recursive: true });
  const browser = await chromium.launch({ timeout: 20_000 });
  try {
    for (const { name, viewport, colorScheme } of [
      { name: 'desktop', viewport: { width: 1440, height: 1000 }, colorScheme: 'light' },
      { name: 'mobile', viewport: { width: 390, height: 844 }, colorScheme: 'dark' },
    ]) {
      const context = await browser.newContext({ viewport, colorScheme });
      await context.route('**/*', (route) => {
        const request = route.request();
        return request.method() === 'GET' && new URL(request.url()).origin === origin
          ? route.continue()
          : route.abort();
      });
      const page = await context.newPage();
      page.setDefaultTimeout(10_000);
      const errors = [];
      page.on('pageerror', (error) => errors.push(error.message));

      const latest = await page.goto(origin);
      assert.ok(latest.ok(), `Home returned HTTP ${latest.status()}`);
      await expect(page.getByRole('heading', { name: editions[0].title })).toBeVisible();
      await expect(page.getByText(editions[0].summary, { exact: true })).toBeVisible();
      await page.screenshot({ path: join(screenshots, `home-${name}.png`), fullPage: true });

      const archive = await page.goto(`${origin}/editions`);
      assert.ok(archive.ok(), `Archive returned HTTP ${archive.status()}`);
      for (const edition of editions) {
        await expect(page.locator(`a[href="/editions/${edition.id}"]`).first()).toBeVisible();
      }
      await page.screenshot({ path: join(screenshots, `archive-${name}.png`), fullPage: true });
      await page.locator(`a[href="/editions/${editions[1].id}"]`).first().click();
      await expect(page.getByRole('heading', { name: editions[1].title })).toBeVisible();
      await expect(page.getByText(editions[1].summary, { exact: true })).toBeVisible();
      await page.screenshot({ path: join(screenshots, `detail-${name}.png`), fullPage: true });
      assert.deepEqual(errors, []);
      await context.close();
    }
  } finally {
    await browser.close();
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
