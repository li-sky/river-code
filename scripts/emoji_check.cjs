// Browser acceptance against an isolated service; requires Playwright + Chrome.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { chromium } = require("playwright");
const base = process.env.RIVER_TEST_URL || "http://localhost:18081";
const out = process.env.RIVER_ARTIFACT_DIR;
// Check the real component data, including every ZWJ/skin-tone variant, so
// dependency upgrades cannot silently introduce sequences rejected by the API.
const data = require("../frontend/node_modules/emoji-picker-react/dist/data/emojis-zh.json");
let sequenceCount = 0;
for (const entry of Object.values(data.emojis).flat()) {
  for (const unified of [entry.u, ...(entry.v || [])]) {
    const emoji = String.fromCodePoint(...unified.split("-").map((code) => parseInt(code, 16)));
    assert([...emoji].length <= 16 && Buffer.byteLength(emoji) <= 64, `Sequence exceeds API limit: ${unified}`);
    sequenceCount++;
  }
}

async function checkWidth(page) {
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "Page overflow");
  assert(await page.getByRole("dialog").evaluate((el) => el.scrollWidth <= el.clientWidth), "Dialog overflow");
}
async function select(page, query, unified) {
  await page.getByPlaceholder("搜索表情").fill(query);
  await page.locator(`.EmojiPickerReact button[data-unified="${unified}"]`).first().click();
}

(async () => {
  const browser = await chromium.launch({ channel: "chrome" });
  try {
    const errors = [], loaded = [];
    const contexts = await Promise.all([
      browser.newContext({ viewport: { width: 1440, height: 1000 } }),
      browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true }),
    ]);
    const [host, friend] = await Promise.all(contexts.map((c) => c.newPage()));
    let friendSocket, offline = false;
    await friend.routeWebSocket(/\/api\/rooms\/.*\/ws/, (ws) => {
      if (offline) { ws.close(); return; }
      friendSocket = ws;
      ws.connectToServer();
    });
    for (const [i, page] of [host, friend].entries()) {
      page.on("pageerror", (e) => errors.push(String(e)));
      page.on("request", (r) => { if (r.url().includes("/assets/EmojiPicker-")) loaded.push(i); });
      await page.goto(base);
      await page.getByLabel("你的昵称").fill(i ? "Emoji Friend" : "Emoji Host");
      if (process.env.JOIN_PASSWORD) await page.getByLabel("全站加入密码").fill(process.env.JOIN_PASSWORD);
      await page.getByRole("button", { name: "以访客身份进入", exact: true }).click();
      await page.getByRole("heading", { name: "今晚，在哪一桌？" }).waitFor();
    }
    await host.getByRole("button", { name: "创建牌桌", exact: true }).click();
    await host.getByLabel("牌桌名称").fill("Emoji acceptance");
    await host.getByRole("dialog").getByRole("button", { name: "创建牌桌", exact: true }).click();
    await host.getByRole("button", { name: "坐入 1 号座位", exact: true }).click();
    await host.getByRole("button", { name: "确认入座", exact: true }).click();
    await host.getByRole("button", { name: "设置头像表情", exact: true }).waitFor();
    await friend.goto(host.url());
    await friend.getByRole("button", { name: "坐入 2 号座位", exact: true }).click();
    await friend.getByRole("button", { name: "确认入座", exact: true }).click();
    await friend.getByRole("button", { name: "设置头像表情", exact: true }).waitFor();
    assert.equal(loaded.length, 0, "Picker downloaded before opening");
    await host.getByRole("button", { name: "设置头像表情", exact: true }).click();
    await host.getByPlaceholder("搜索表情").waitFor();
    assert(loaded.includes(0), "Picker not loaded on demand");
    await checkWidth(host);
    await host.getByRole("tab", { name: "旗帜", exact: true }).click();
    await host.locator('.EmojiPickerReact button[data-unified="1f1e8-1f1f3"]').waitFor();
    await host.keyboard.press("Escape");
    assert.equal(await host.getByRole("dialog").count(), 0);
    assert(await host.getByRole("button", { name: "设置头像表情", exact: true }).evaluate((e) => e === document.activeElement), "Focus not restored");

    await friend.getByRole("button", { name: "设置头像表情", exact: true }).click();
    await friend.getByPlaceholder("搜索表情").waitFor();
    await checkWidth(friend);
    await friend.locator(".epr-tone.epr-active").click();
    await friend.getByRole("button", { name: "Skin tone MEDIUM", exact: true }).click();
    await friend.getByPlaceholder("搜索表情").fill("亲吻");
    const unified = "1f469-1f3fb-200d-2764-fe0f-200d-1f48b-200d-1f468-1f3fd";
    const emoji = String.fromCodePoint(...unified.split("-").map((s) => parseInt(s, 16)));
    await friend.locator(`.EmojiPickerReact button[data-unified="${unified}"]`).waitFor();
    if (out) {
      await fs.mkdir(out, { recursive: true });
      await friend.screenshot({ path: path.join(out, "emoji-mobile.png") });
    }
    await select(friend, "亲吻", unified);
    for (const page of [friend, host]) await page.locator(".avatar-emoji").getByText(emoji, { exact: true }).waitFor();
    await friend.reload();
    await friend.locator(".avatar-emoji").getByText(emoji, { exact: true }).waitFor();
    const saved = await contexts[1].request.get(`${base}/api/me`);
    assert.equal((await saved.json()).settings.avatarEmoji, emoji);

    await host.locator(".table-seat").filter({ hasText: "Emoji Friend" }).locator(".seat-avatar-button").click();
    await host.getByPlaceholder("搜索表情").waitFor();
    await host.getByPlaceholder("搜索表情").fill("海豚");
    await host.locator('.EmojiPickerReact button[data-unified="1f42c"]').waitFor();
    if (out) await host.screenshot({ path: path.join(out, "emoji-desktop.png") });
    await select(host, "海豚", "1f42c");
    await friend.locator(".flying-emoji").getByText("🐬", { exact: true }).waitFor();

    await friend.getByRole("button", { name: "设置头像表情", exact: true }).click();
    await friend.getByRole("button", { name: "清除头像表情", exact: true }).click();
    await friend.waitForFunction(() => !document.querySelector(".avatar-emoji"));
    const cleared = await contexts[1].request.get(`${base}/api/me`);
    assert.equal((await cleared.json()).settings.avatarEmoji, "");

    // Keep the modal open while room permission changes on the other client.
    await friend.getByRole("button", { name: "设置头像表情", exact: true }).click();
    await friend.getByPlaceholder("搜索表情").waitFor();
    await host.getByRole("button", { name: "房间设置", exact: true }).click();
    await host.getByRole("switch", { name: "Emoji 互动", exact: true }).click();
    await host.getByRole("button", { name: "保存房间设置", exact: true }).click();
    await friend.getByText("这张牌桌已关闭 Emoji 互动。", { exact: true }).waitFor();
    assert(await friend.getByRole("button", { name: "清除头像表情" }).isDisabled());
    assert.equal(await friend.locator(".EmojiPickerReact").count(), 0);
    await host.getByRole("button", { name: "房间设置", exact: true }).click();
    await host.getByRole("switch", { name: "Emoji 互动", exact: true }).click();
    await host.getByRole("button", { name: "保存房间设置", exact: true }).click();
    await friend.getByPlaceholder("搜索表情").waitFor();
    assert(friendSocket, "Friend WS not intercepted");
    offline = true;
    friendSocket.close();
    await friend.getByText("连接恢复后即可发送表情。", { exact: true }).waitFor();
    assert(await friend.getByRole("button", { name: "清除头像表情" }).isDisabled());
    assert.equal(await friend.locator(".EmojiPickerReact").count(), 0);
    assert.deepEqual(errors, []);
    console.log(`PASS: ${sequenceCount} sequences within API limits; lazy loading, Chinese search, flags, skin tones, desktop/mobile layout, focus, two-client reaction, complex avatar persistence/clear, permission and disconnect guards`);
  } finally { await browser.close(); }
})().catch((e) => { console.error(e); process.exit(1); });
