// Browser acceptance against an isolated service; requires the playwright package.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { chromium } = require("playwright");

const base = process.env.RIVER_TEST_URL || "http://localhost:8080";
const out = process.env.RIVER_ARTIFACT_DIR;

async function checkWidth(page) {
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "Horizontal overflow");
  const dialog = page.getByRole("dialog");
  if (await dialog.isVisible()) {
    assert(await dialog.evaluate((el) => el.scrollWidth <= el.clientWidth), "Dialog overflow");
  }
}

(async () => {
  const browser = await chromium.launch(process.env.RIVER_BROWSER_PATH
    ? { executablePath: process.env.RIVER_BROWSER_PATH }
    : { channel: "chrome" });
  try {
    const errors = [];
    const stamp = Date.now();
    const contexts = await Promise.all([
      browser.newContext({ viewport: { width: 1440, height: 1000 } }),
      browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true }),
    ]);
    const pages = await Promise.all(contexts.map((c) => c.newPage()));
    for (const [i, page] of pages.entries()) {
      page.on("pageerror", (e) => errors.push(String(e)));
      await page.goto(base);
      await page.getByLabel("你的昵称").fill(`Visibility ${stamp} ${i}`);
      if (process.env.JOIN_PASSWORD) {
        await page.getByLabel("全站加入密码").fill(process.env.JOIN_PASSWORD);
      }
      await page.getByRole("button", { name: "以访客身份进入", exact: true }).click();
      await page.getByRole("heading", { name: "今晚，在哪一桌？" }).waitFor();
      await checkWidth(page);
    }
    const [host, friend] = pages;
    const name = `私人验收 ${stamp}`;
    // Verify the mobile creation selector before creating on desktop.
    await friend.getByRole("button", { name: "创建牌桌", exact: true }).click();
    await friend.getByLabel("房间可见性").selectOption("private");
    await friend.getByText("不在大厅显示，持邀请链接的用户登录后可加入", { exact: true }).waitFor();
    await checkWidth(friend);
    if (out) {
      await fs.mkdir(out, { recursive: true });
      await friend.getByRole("dialog").evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)));
      await friend.screenshot({ path: path.join(out, "private-create-mobile.png") });
    }
    await friend.keyboard.press("Escape");
    await host.getByRole("button", { name: "创建牌桌", exact: true }).click();
    assert.equal(await host.getByLabel("房间可见性").inputValue(), "public");
    await host.getByLabel("牌桌名称").fill(name);
    await host.getByLabel("房间可见性").selectOption("private");
    await checkWidth(host);
    await host.getByRole("dialog").getByRole("button", { name: "创建牌桌", exact: true }).click();
    await host.getByRole("button", { name: "坐入 1 号座位", exact: true }).waitFor();
    await host.locator(".room-subtitle").getByText("私人房间", { exact: true }).waitFor();
    const roomUrl = host.url();
    const roomId = new URL(roomUrl).searchParams.get("room");
    const list = await contexts[1].request.get(`${base}/api/rooms`);
    assert.equal(list.status(), 200);
    assert(!(await list.json()).some((r) => r.id === roomId), "Private room listed");
    await friend.reload();
    await friend.getByRole("heading", { name: "今晚，在哪一桌？" }).waitFor();
    assert.equal(await friend.getByRole("heading", { name, exact: true }).count(), 0);
    // Open the link without a session: login must retain the invitation URL.
    const freshContext = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const fresh = await freshContext.newPage();
    fresh.on("pageerror", (e) => errors.push(String(e)));
    await fresh.goto(roomUrl);
    await fresh.getByLabel("你的昵称").fill(`Invited ${stamp}`);
    if (process.env.JOIN_PASSWORD) await fresh.getByLabel("全站加入密码").fill(process.env.JOIN_PASSWORD);
    await fresh.getByRole("button", { name: "以访客身份进入", exact: true }).click();
    await fresh.locator(".room-subtitle").getByText("私人房间", { exact: true }).waitFor();
    await checkWidth(fresh);
    await friend.goto(roomUrl);
    await friend.locator(".room-subtitle").getByText("私人房间", { exact: true }).waitFor();
    assert.equal(await friend.getByRole("button", { name: "房间设置", exact: true }).count(), 0);
    await checkWidth(friend);
    if (out) await friend.screenshot({ path: path.join(out, "private-table-mobile.png"), fullPage: true });
    await host.getByRole("button", { name: "房间设置", exact: true }).click();
    assert.equal(await host.getByLabel("房间可见性").inputValue(), "private");
    await host.getByLabel("房间可见性").selectOption("public");
    await host.getByRole("button", { name: "保存房间设置", exact: true }).click();
    for (const page of [host, friend]) {
      await page.locator(".room-subtitle").getByText("公开房间", { exact: true }).waitFor();
      await checkWidth(page);
    }
    await friend.getByRole("button", { name: "返回大厅", exact: true }).click();
    await friend.getByRole("heading", { name, exact: true }).waitFor();
    await host.getByRole("button", { name: "房间设置", exact: true }).click();
    await host.getByLabel("房间可见性").selectOption("private");
    await host.getByRole("button", { name: "保存房间设置", exact: true }).click();
    await friend.getByRole("heading", { name, exact: true }).waitFor({ state: "detached", timeout: 10000 });
    await host.reload();
    await host.locator(".room-subtitle").getByText("私人房间", { exact: true }).waitFor();
    // Mobile host settings use the same selector.
    await host.setViewportSize({ width: 390, height: 844 });
    await host.getByRole("button", { name: "房间设置", exact: true }).click();
    assert.equal(await host.getByLabel("房间可见性").inputValue(), "private");
    await checkWidth(host);
    if (out) {
      await host.getByRole("dialog").evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)));
      await host.screenshot({ path: path.join(out, "private-settings-mobile.png") });
    }
    assert.deepEqual(errors, [], "Browser errors");
    console.log("PASS: private creation, lobby filtering, login from invitation, link join, host switching, broadcast, polling, reload, desktop/mobile layout");
  } finally {
    await browser.close();
  }
})().catch((err) => { console.error(err); process.exitCode = 1; });
