// Acceptance against an isolated local service. Uses real browser input.
const { chromium } = require("playwright");
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const base = process.env.RIVER_TEST_URL || "http://localhost:8094";
const out = process.env.RIVER_ARTIFACT_DIR || path.join(require("node:os").tmpdir(), "river-chat-review");
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
const settings = { visibility: "public", smallBlind: 10, bigBlind: 20, buyIn: 2000, maxPlayers: 6, actionSeconds: 120, voiceEnabled: true, voiceMode: "free", spectatorVoiceEnabled: false, chatEnabled: true, reactionsEnabled: true };

(async () => {
  await fs.mkdir(out, { recursive: true });
  const browser = await chromium.launch({ channel: "chrome" });
  try {
    const errors = [];
    const hostContext = await browser.newContext({ viewport: { width: 1440, height: 900 }, permissions: ["clipboard-read", "clipboard-write"] });
    const guestContext = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
    for (const [context, name] of [[hostContext, "Sky"], [guestContext, "Mia"]]) {
      assert((await context.request.post(base + "/api/auth/guest", { data: { name, password: "" } })).ok());
    }
    const room = await (await hostContext.request.post(base + "/api/rooms", { data: { name: "周四好友局", settings } })).json();
    const host = await hostContext.newPage(), guest = await guestContext.newPage();
    host.setDefaultTimeout(10000); guest.setDefaultTimeout(10000);
    const input = await guestContext.newCDPSession(guest);
    await guest.addInitScript(() => {
      window.chatTestSockets = [];
      const NativeWebSocket = window.WebSocket;
      window.WebSocket = class extends NativeWebSocket {
        constructor(...args) { super(...args); window.chatTestSockets.push(this); }
      };
    });
    for (const [i, page] of [host, guest].entries()) {
      page.on("pageerror", (error) => errors.push(error.stack));
      await page.goto(base + "/?room=" + room.id);
      await page.getByText("连接正常", { exact: true }).waitFor();
      await page.getByRole("button", { name: `坐入 ${i + 1} 号座位`, exact: true }).click();
      await page.getByRole("button", { name: "确认入座", exact: true }).click();
      await page.getByRole("button", { name: "牌桌聊天", exact: true }).click();
      await page.locator(".empty-chat").waitFor();
    }
    async function send(page, text) {
      await page.getByLabel("聊天消息", { exact: true }).fill(text);
      await page.getByRole("button", { name: "发送消息", exact: true }).click();
      for (const p of [host, guest]) await p.locator(".cs-message__text-content").filter({ hasText: text }).waitFor();
    }
    const message = (page, text) => page.locator(".chat-message").filter({ hasText: text });
    async function rightClick(page, text) {
      await message(page, text).locator(".cs-message__content").scrollIntoViewIfNeeded();
      await wait(200);
      await message(page, text).locator(".cs-message__content").click({ button: "right", position: { x: 20, y: 20 } });
      await page.getByRole("menu", { name: "消息操作", exact: true }).waitFor();
      await wait(100);
    }
    async function touch(text, kind = "hold") {
      await message(guest, text).scrollIntoViewIfNeeded();
      await wait(200);
      const b = await message(guest, text).locator(".cs-message__content").boundingBox();
      const point = { x: b.x + Math.min(25, b.width / 2), y: b.y + Math.min(25, b.height / 2), id: 1 };
      await input.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [point] });
      if (kind === "move") await input.send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints: [{ ...point, y: point.y + 45 }] });
      if (kind === "multi") await input.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [point, { ...point, x: point.x + 40, id: 2 }] });
      if (kind === "cancel") await input.send("Input.dispatchTouchEvent", { type: "touchCancel", touchPoints: [] });
      await wait(kind === "short" ? 90 : 850);
      if (kind !== "cancel") await input.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
      await wait(100);
    }
    await send(host, "晚上好，大家先坐下吧 👋");
    await send(guest, "好呀！今天手气一定不错 🍀");
    await send(host, "祝大家玩得开心");
    await rightClick(host, "好呀！");
    assert.equal(await host.getByRole("menuitem", { name: "撤回消息", exact: true }).count(), 0);
    await host.getByRole("menuitem", { name: "置顶消息", exact: true }).click();
    for (const p of [host, guest]) await p.getByRole("region", { name: "置顶消息", exact: true }).waitFor();
    await rightClick(host, "好呀！");
    await host.getByRole("menuitem", { name: "取消置顶", exact: true }).click();
    await host.getByRole("region", { name: "置顶消息", exact: true }).waitFor({ state: "hidden" });
    await rightClick(host, "祝大家");
    await host.getByRole("menuitem", { name: "复制文字", exact: true }).click();
    assert.equal(await host.evaluate(() => navigator.clipboard.readText()), "祝大家玩得开心");
    const trigger = message(host, "祝大家").locator(".chat-message-trigger");
    await trigger.focus(); await host.keyboard.press("Shift+F10");
    await host.getByRole("menu").waitFor(); await host.keyboard.press("ArrowDown");
    assert.equal(await host.evaluate(() => document.activeElement.getAttribute("role")), "menuitem");
    await host.keyboard.press("Escape");
    await host.getByRole("menu").waitFor({ state: "hidden" });
    await wait(100);
    assert(await trigger.evaluate((el) => el === document.activeElement));
    assert(await host.locator("#table-chat").isVisible(), "Escape closed drawer with menu");
    await rightClick(host, "祝大家");
    await host.getByLabel("聊天消息", { exact: true }).click();
    await host.getByRole("menu").waitFor({ state: "hidden" });
    await wait(100);
    assert(await host.getByLabel("聊天消息", { exact: true }).evaluate((el) => el === document.activeElement), "Outside click stole input focus");
    for (const kind of ["short", "move", "cancel", "multi"]) {
      await touch("好呀！", kind);
      assert.equal(await guest.getByRole("menu").count(), 0, `Unexpected menu after ${kind}`);
      assert(await guest.locator("#table-chat").isVisible());
    }
    console.log("PASS right-click, clipboard, keyboard, outside/Escape, touch cancellation");
    await touch("好呀！");
    await guest.getByRole("menu").waitFor();
    assert.equal(await guest.getByRole("menuitem", { name: "置顶消息", exact: true }).count(), 0);
    await guest.keyboard.press("Escape");
    await send(guest, "这句话撤回试一下");
    await rightClick(host, "这句话"); await host.getByRole("menuitem", { name: "置顶消息", exact: true }).click();
    await guest.getByRole("region", { name: "置顶消息", exact: true }).waitFor();
    assert.equal(await host.locator(".thought-bubble").filter({ hasText: "这句话" }).count(), 1);
    await touch("这句话");
    await guest.getByRole("menuitem", { name: "撤回消息", exact: true }).click();
    for (const p of [host, guest]) {
      await p.locator(".chat-recalled").waitFor();
      assert.equal(await p.locator(".cs-message__text-content").filter({ hasText: "这句话" }).count(), 0);
      assert.equal(await p.getByRole("region", { name: "置顶消息", exact: true }).count(), 0);
      assert.equal(await p.locator(".thought-bubble").filter({ hasText: "这句话" }).count(), 0);
    }
    await guest.reload(); await guest.getByText("连接正常", { exact: true }).waitFor();
    await guest.getByRole("button", { name: "牌桌聊天", exact: true }).click();
    await guest.locator(".chat-recalled").waitFor();
    await guest.locator(".chat-recalled").click({ button: "right" });
    assert.equal(await guest.getByRole("menu").count(), 0);
    console.log("PASS native long press, recall sync, avatar clearing, reload");
    const long = '<img src=x onerror="alert(1)"> ' + "今晚是好友局，轻松聊天慢慢玩。".repeat(14);
    await send(guest, long);
    assert.equal(await host.locator(".cs-message__text-content img").count(), 0);
    await rightClick(host, long); await host.getByRole("menuitem", { name: "置顶消息", exact: true }).click();
    for (const [page, widths] of [[host, [1440]], [guest, [390, 320]]]) {
      for (const width of widths) {
        await page.setViewportSize({ width, height: width === 320 ? 667 : 844 });
        await wait(200);
        const geometry = await page.locator(".chat-panel").evaluate((panel) => {
          const form = panel.querySelector(".chat-form").getBoundingClientRect();
          const pin = panel.querySelector(".pinned-message").getBoundingClientRect();
          const rows = [...panel.querySelectorAll(".chat-message")].map((row) => {
            const r = row.getBoundingClientRect(), b = row.querySelector(".cs-message__content").getBoundingClientRect(), avatar = row.querySelector(".cs-avatar").getBoundingClientRect();
            return { own: row.classList.contains("own-message"), left: b.left >= r.left, right: b.right <= r.right, side: row.classList.contains("own-message") ? avatar.left > b.left : avatar.left < b.left };
          });
          return { overflow: document.documentElement.scrollWidth > innerWidth || panel.scrollWidth > panel.clientWidth, form: form.bottom <= innerHeight && pin.bottom < form.top, rows };
        });
        assert(!geometry.overflow && geometry.form && geometry.rows.every((r) => r.left && r.right && r.side), JSON.stringify(geometry));
        await page.screenshot({ path: path.join(out, `chat-${width}.png`) });
        await rightClick(page, long);
        const menu = await page.getByRole("menu").boundingBox();
        assert(menu.x >= 0 && menu.y >= 0 && menu.x + menu.width <= width && menu.y + menu.height <= (width === 320 ? 667 : 844), JSON.stringify({ width, menu }));
        await page.screenshot({ path: path.join(out, `menu-${width}.png`) });
        await page.keyboard.press("Escape");
      }
    }
    await rightClick(host, long);
    await host.locator(".chat-messages").evaluate((el) => { el.scrollTop = 0; });
    await host.getByRole("menu").waitFor({ state: "hidden" });
    await rightClick(guest, long);
    await guestContext.setOffline(true);
    // Existing WebSockets can survive browser offline emulation: explicitly
    // close the test connection and block retries to verify disconnected UI.
    await guest.evaluate(() => window.chatTestSockets.forEach((socket) => socket.close()));
    await guest.getByText("重新连接中", { exact: true }).waitFor();
    assert.equal(await guest.getByRole("menuitem", { name: "撤回消息", exact: true }).getAttribute("data-disabled"), "");
    await guestContext.setOffline(false);
    assert.equal(errors.length, 0, errors.join("\n"));
    console.log("PASS Chatscope bubbles, safe text, clipboard, Radix right-click/keyboard/native touch, cancellation/multitouch, host pin/unpin, recall sync/reload, scroll/outside/Escape, offline, 1440/390/320 layout.");
  } finally { await browser.close(); }
})().catch((error) => { console.error(error); process.exitCode = 1; });
