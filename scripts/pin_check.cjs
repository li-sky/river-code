// Browser acceptance against an isolated service; requires playwright.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { chromium } = require("playwright");

const base = process.env.RIVER_TEST_URL || "http://localhost:8091";
const out = process.env.RIVER_ARTIFACT_DIR;

async function checkLayout(page) {
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "Page overflow");
  const layout = await page.locator(".chat-panel").evaluate((panel) => {
    const bounds = panel.getBoundingClientRect();
    const pin = panel.querySelector(".pinned-message").getBoundingClientRect();
    const form = panel.querySelector(".chat-form").getBoundingClientRect();
    return {
      fits: panel.scrollWidth <= panel.clientWidth,
      pinVisible: pin.top >= bounds.top && pin.bottom < form.top,
      inputVisible: form.bottom <= bounds.bottom && form.bottom <= innerHeight,
      textScrolls: panel.querySelector(".pinned-message-content").scrollHeight > panel.querySelector(".pinned-message-content").clientHeight,
    };
  });
  assert(layout.fits && layout.pinVisible && layout.inputVisible, JSON.stringify(layout));
  return layout;
}

(async () => {
  const browser = await chromium.launch(process.env.RIVER_BROWSER_PATH
    ? { executablePath: process.env.RIVER_BROWSER_PATH }
    : { channel: "chrome" });
  try {
    const errors = [];
    const contexts = await Promise.all([
      browser.newContext({ viewport: { width: 1440, height: 1000 } }),
      browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true }),
    ]);
    const [host, guest] = await Promise.all(contexts.map((c) => c.newPage()));
    for (const [i, page] of [host, guest].entries()) {
      page.on("pageerror", (e) => errors.push(String(e)));
      await page.goto(base);
      await page.getByLabel("你的昵称").fill(i ? "置顶访客" : "置顶房主");
      if (process.env.JOIN_PASSWORD) await page.getByLabel("全站加入密码").fill(process.env.JOIN_PASSWORD);
      await page.getByRole("button", { name: "以访客身份进入", exact: true }).click();
      await page.getByRole("heading", { name: "今晚，在哪一桌？" }).waitFor();
    }
    await host.getByRole("button", { name: "创建牌桌", exact: true }).click();
    await host.getByLabel("牌桌名称").fill(`消息置顶 ${Date.now()}`);
    await host.getByRole("dialog").getByRole("button", { name: "创建牌桌", exact: true }).click();
    await host.getByRole("button", { name: "坐入 1 号座位", exact: true }).waitFor();
    await guest.goto(host.url());
    await guest.getByRole("button", { name: "坐入 1 号座位", exact: true }).waitFor();
    for (const page of [host, guest]) await page.getByRole("button", { name: "牌桌聊天" }).click();
    const text = '<img src=x onerror="alert(1)"> ' + "长消息需要完整保留。".repeat(25);
    await guest.getByLabel("聊天消息").fill(text);
    await guest.getByRole("button", { name: "发送消息", exact: true }).click();
    await host.locator(".chat-message").filter({ hasText: text }).waitFor();
    assert.equal(await guest.getByRole("button", { name: "置顶此消息", exact: true }).count(), 0);
    await host.getByRole("button", { name: "置顶此消息", exact: true }).click();
    for (const page of [host, guest]) {
      await page.getByRole("region", { name: "置顶消息", exact: true }).waitFor();
      assert.equal(await page.locator(".pinned-message p").innerText(), text);
      assert.equal(await page.locator(".pinned-message img").count(), 0, "Message rendered HTML");
      await checkLayout(page);
    }
    assert.equal(await guest.getByRole("button", { name: "取消置顶消息", exact: true }).count(), 0);
    if (out) {
      await fs.mkdir(out, { recursive: true });
      await host.screenshot({ path: path.join(out, "pinned-desktop.png"), fullPage: true });
      await guest.screenshot({ path: path.join(out, "pinned-mobile.png") });
    }
    for (let i = 0; i < 6; i++) {
      await guest.getByLabel("聊天消息").fill(`后续聊天 ${i}`);
      await guest.getByRole("button", { name: "发送消息", exact: true }).click();
      await host.locator(".chat-message").filter({ hasText: `后续聊天 ${i}` }).waitFor();
    }
    await guest.locator(".chat-messages").evaluate((el) => { el.scrollTop = el.scrollHeight; });
    await checkLayout(guest);
    await host.reload();
    await host.getByRole("button", { name: "牌桌聊天" }).click();
    await host.getByRole("region", { name: "置顶消息", exact: true }).waitFor();
    assert.equal(await host.locator(".pinned-message p").innerText(), text);
    await host.locator(".chat-message").filter({ hasText: "后续聊天 0" }).getByRole("button", { name: "置顶此消息", exact: true }).click();
    for (const page of [host, guest]) await page.locator(".pinned-message p").filter({ hasText: "后续聊天 0" }).waitFor();
    await host.getByRole("button", { name: "取消置顶消息", exact: true }).click();
    for (const page of [host, guest]) await page.getByRole("region", { name: "置顶消息", exact: true }).waitFor({ state: "detached" });
    // Exercise the same controls as a host on a narrow and short phone viewport.
    await host.setViewportSize({ width: 390, height: 667 });
    await host.locator(".chat-message").filter({ hasText: text }).getByRole("button", { name: "置顶此消息", exact: true }).click();
    await host.getByRole("region", { name: "置顶消息", exact: true }).waitFor();
    assert((await checkLayout(host)).textScrolls, "Long pin should scroll inside its bounded area");
    if (out) await host.screenshot({ path: path.join(out, "pinned-host-mobile.png") });
    await host.locator(".chat-message").filter({ hasText: text }).getByRole("button", { name: "取消置顶此消息", exact: true }).click();
    for (const page of [host, guest]) await page.getByRole("region", { name: "置顶消息", exact: true }).waitFor({ state: "detached" });
    assert.deepEqual(errors, [], "Browser errors");
    console.log("PASS: host pin/replace/unpin, guest read-only, two-client broadcast, reload, escaped text, scroll and desktop/mobile layouts");
  } finally {
    await browser.close();
  }
})().catch((err) => { console.error(err); process.exitCode = 1; });
