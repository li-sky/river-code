const { chromium } = require("playwright");
const assert = require("node:assert/strict");
const path = require("node:path");
const fs = require("node:fs/promises");
// Run against an isolated local service; never use real user sessions.
const base = process.env.RIVER_TEST_URL || "http://localhost:8092";
const out =
  process.env.RIVER_ARTIFACT_DIR ||
  path.join(require("node:os").tmpdir(), "river-compact-ui-review");
const settings = {
  visibility: "public",
  smallBlind: 10,
  bigBlind: 20,
  buyIn: 2000,
  maxPlayers: 9,
  actionSeconds: 120,
  voiceEnabled: true,
  voiceMode: "free",
  spectatorVoiceEnabled: false,
  chatEnabled: true,
  reactionsEnabled: true,
};
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
async function until(fn) {
  for (let i = 0; i < 100; i++) {
    const v = await fn();
    if (v) return v;
    await wait(75);
  }
  throw new Error("State did not update");
}
(async () => {
  await fs.mkdir(out, { recursive: true });
  const browser = await chromium.launch({ channel: "chrome" });
  const contexts = [];
  const errors = [];
  try {
    async function player(name, width = 390) {
      const c = await browser.newContext({
        viewport: { width, height: 844 },
        hasTouch: true,
      });
      contexts.push(c);
      assert(
        (
          await c.request.post(base + "/api/auth/guest", {
            data: { name, password: "" },
          })
        ).ok(),
      );
      const user = await (await c.request.get(base + "/api/me")).json();
      const page = await c.newPage();
      page.on("pageerror", (e) => errors.push(e.stack));
      return { c, page, user };
    }
    const host = await player("skyli", 390),
      guest = await player("Mia", 390);
    const room = await (
      await host.c.request.post(base + "/api/rooms", {
        data: { name: "周四好友局", settings },
      })
    ).json();
    const expectedWins = new Map();
    let countedHand = 0;
    const read = async () => {
      const state = await (
        await host.c.request.get(base + "/api/rooms/" + room.id)
      ).json();
      if (state.hand?.phase === "complete" && state.hand.number > countedHand) {
        for (const id of new Set(
          state.hand.winners.filter((w) => w.amount > 0).map((w) => w.id),
        )) {
          expectedWins.set(id, (expectedWins.get(id) || 0) + 1);
        }
        countedHand = state.hand.number;
      }
      for (const player of state.players)
        assert.equal(
          player.wins,
          expectedWins.get(player.id) || 0,
          "Authoritative wins must match completed hands",
        );
      return state;
    };
    async function enter(p, seat, buyIn = 2000) {
      await p.page.goto(base + "/?room=" + room.id);
      await p.page.getByText("连接正常", { exact: true }).waitFor();
      await p.page
        .getByRole("button", { name: `坐入 ${seat + 1} 号座位`, exact: true })
        .click();
      await p.page.getByLabel("买入筹码", { exact: true }).fill(String(buyIn));
      await p.page
        .getByRole("button", { name: "确认入座", exact: true })
        .click();
      await until(async () =>
        (await read()).players.some(
          (x) => x.id === p.user.id && x.seat === seat,
        ),
      );
    }
    await enter(host, 0);
    await enter(guest, 1);
    assert.equal(await host.page.locator(".seat-win-count").count(), 2);
    assert.equal(
      await host.page.locator(".seat-name .lucide-crown").count(),
      0,
    );
    assert.equal(await host.page.locator(".seat-host-label").innerText(), "主");
    assert.equal(
      await host.page.locator(".table-seat .lucide-crown").count(),
      2,
    );
    for (const width of [1440, 768, 390, 320]) {
      await host.page.setViewportSize({ width, height: 844 });
      await host.page.evaluate(() => scrollTo(0, 0));
      await wait(180);
      const geo = await host.page.evaluate(() => {
        const h = document
          .querySelector(".room-topbar")
          .getBoundingClientRect();
        const b = document
          .querySelector(".action-dock")
          .getBoundingClientRect();
        return {
          overflow: document.documentElement.scrollWidth > innerWidth,
          headerHeight: h.height,
          dockBottom: b.bottom,
          buttons: [...document.querySelectorAll(".bet-actions button")].map(
            (x) => ({
              text: x.innerText,
              disabled: x.disabled,
              w: x.getBoundingClientRect().width,
              h: x.getBoundingClientRect().height,
            }),
          ),
          headerButtons: [
            ...document.querySelectorAll(".room-topbar button"),
          ].map((x) => {
            const r = x.getBoundingClientRect();
            return {
              name: x.getAttribute("aria-label"),
              left: r.left,
              right: r.right,
              top: r.top,
              bottom: r.bottom,
            };
          }),
        };
      });
      assert(!geo.overflow, JSON.stringify(geo));
      assert.equal(geo.buttons.length, 4);
      assert(geo.buttons.every((x) => x.disabled && x.h >= 48));
      assert(Math.abs(geo.dockBottom - 844) < 1);
      assert(
        geo.headerButtons.every(
          (x) =>
            x.left >= 0 &&
            x.right <= width &&
            x.top >= 0 &&
            x.bottom <= geo.headerHeight + 1,
        ),
        JSON.stringify(geo),
      );
      assert.equal(
        await host.page.locator(".topbar,.under-table,.allin-button").count(),
        0,
      );
      await host.page.evaluate(() =>
        window.scrollTo(0, document.body.scrollHeight),
      );
      assert(
        await host.page.locator(".bet-actions").evaluate((x) => {
          const r = x.getBoundingClientRect();
          return r.top >= 0 && r.bottom <= innerHeight;
        }),
      );
      await host.page.screenshot({
        path: path.join(out, `waiting-${width}.png`),
        fullPage: true,
      });
      console.log(
        `Layout ${width}px: header ${Math.round(geo.headerHeight)}px; four fixed actions; no overflow.`,
      );
    }
    await host.page.setViewportSize({ width: 390, height: 844 });
    await host.page.evaluate(() => scrollTo(0, 0));
    await guest.page.getByRole("button", { name: "暂离", exact: true }).click();
    let paused = await until(async () => {
      const s = await read();
      return s.players.find((x) => x.id === guest.user.id).sittingOut && s;
    });
    assert.equal(
      paused.players.find((x) => x.id === guest.user.id).stack,
      2000,
    );
    await until(() =>
      host.page
        .getByRole("button", { name: "开始牌局", exact: true })
        .isDisabled(),
    );
    await guest.page
      .getByRole("button", { name: "返回牌局", exact: true })
      .click();
    await until(() =>
      host.page
        .getByRole("button", { name: "开始牌局", exact: true })
        .isEnabled(),
    );
    // Drawer keyboard, focus, scroll containment, chat/pin and touch directions.
    await host.page
      .getByRole("button", { name: "打开牌桌菜单", exact: true })
      .click();
    await host.page
      .getByRole("dialog", { name: "牌桌菜单", exact: true })
      .waitFor();
    await wait(250);
    assert.equal(
      Math.round(
        await host.page
          .locator("#table-menu")
          .evaluate((x) => x.getBoundingClientRect().left),
      ),
      0,
    );
    await host.page.screenshot({ path: path.join(out, "left-drawer.png") });
    await host.page.keyboard.press("Escape");
    await wait(220);
    assert.equal(
      await host.page.evaluate(() =>
        document.activeElement.getAttribute("aria-label"),
      ),
      "打开牌桌菜单",
    );
    async function swipe(page, selector, from, to) {
      const session = await page.context().newCDPSession(page);
      const y = selector === ".game-layout" ? 180 : 35;
      await session.send("Input.dispatchTouchEvent", {
        type: "touchStart",
        touchPoints: [{ x: from, y, id: 1 }],
      });
      for (let step = 1; step <= 6; step++) {
        await session.send("Input.dispatchTouchEvent", {
          type: "touchMove",
          touchPoints: [
            {
              x: from + ((to - from) * step) / 6,
              y: y + (5 * step) / 6,
              id: 1,
            },
          ],
        });
        await wait(16);
      }
      await session.send("Input.dispatchTouchEvent", {
        type: "touchEnd",
        touchPoints: [],
      });
      await session.detach();
    }
    // Genuine Chromium input begins in the middle, rather than at the edges.
    await swipe(host.page, ".game-layout", 130, 260);
    await host.page.locator("#table-menu").waitFor();
    await wait(250);
    await swipe(host.page, "#table-menu", 80, 220);
    assert.equal(await host.page.locator("[role=dialog]:visible").count(), 1);
    assert(await host.page.locator("#table-menu").isVisible());
    await swipe(host.page, "#table-menu", 200, 40);
    await host.page.locator("#table-menu").waitFor({ state: "hidden" });
    assert.equal(
      await host.page.locator("#table-chat").count(),
      0,
      "Menu close opened chat",
    );
    await swipe(host.page, ".game-layout", 260, 130);
    await host.page.locator("#table-chat").waitFor();
    await wait(250);
    const chatBounds = await host.page.locator("#table-chat").evaluate((x) => {
      const r = x.getBoundingClientRect();
      return { left: r.left, right: r.right, top: r.top, bottom: r.bottom };
    });
    assert.equal(chatBounds.right, 390);
    assert.equal(chatBounds.bottom, 844);
    await host.page
      .getByLabel("聊天消息", { exact: true })
      .fill("今晚开局，欢迎来到好友桌。");
    await host.page.keyboard.press("Enter");
    await host.page
      .getByRole("button", { name: "置顶此消息", exact: true })
      .click();
    await host.page
      .getByRole("region", { name: "置顶消息", exact: true })
      .waitFor();
    await host.page.screenshot({ path: path.join(out, "right-drawer.png") });
    await swipe(host.page, "#table-chat", 280, 100);
    assert.equal(await host.page.locator("[role=dialog]:visible").count(), 1);
    assert(await host.page.locator("#table-chat").isVisible());
    await swipe(host.page, "#table-chat", 100, 280);
    await host.page.locator("#table-chat").waitFor({ state: "hidden" });
    assert.equal(
      await host.page.locator("#table-menu").count(),
      0,
      "Chat close opened menu",
    );
    await guest.page
      .getByRole("button", { name: "牌桌聊天", exact: true })
      .click();
    await guest.page
      .getByRole("region", { name: "置顶消息", exact: true })
      .waitFor();
    assert.equal(
      await guest.page
        .getByRole("button", { name: "取消置顶消息", exact: true })
        .count(),
      0,
    );
    await guest.page.keyboard.press("Escape");
    await host.page
      .getByRole("button", { name: "玩法简介", exact: true })
      .click();
    await host.page.getByRole("dialog", { name: "无限注德州扑克" }).waitFor();
    await host.page.keyboard.press("Escape");
    const players = [host, guest];
    async function actor() {
      const s = await read();
      return {
        s,
        p: players.find(
          (x) =>
            x.user.id === s.players.find((x) => x.seat === s.hand.turnSeat)?.id,
        ),
      };
    }
    async function act(kind) {
      const { s, p } = await actor();
      assert(p);
      const index = { call: 0, raise: 1, check: 2, fold: 3 }[kind];
      await p.page.locator(".bet-actions button").nth(index).click();
      return await until(async () => {
        const next = await read();
        return next.hand.turnToken !== s.hand.turnToken && next;
      });
    }
    await host.page
      .getByRole("button", { name: "开始牌局", exact: true })
      .click();
    await until(async () => (await read()).hand);
    const old = await read();
    await guest.page.getByRole("button", { name: "暂离", exact: true }).click();
    await until(
      async () =>
        (await read()).players.find((x) => x.id === guest.user.id).sittingOut,
    );
    assert.equal((await read()).hand.turnToken, old.hand.turnToken);
    await guest.page
      .getByRole("button", { name: "返回牌局", exact: true })
      .click();
    const { s, p } = await actor();
    await p.page.locator(".bet-actions button").nth(1).click();
    const dlg = p.page.getByRole("dialog", { name: "加注", exact: true });
    await dlg.waitFor();
    const mine = s.hand.players.find((x) => x.id === p.user.id),
      max = s.players.find((x) => x.id === p.user.id).stack + mine.bet,
      min = s.hand.currentBet + s.hand.minRaise;
    for (const fraction of [0.25, 0.5, 0.75]) {
      await dlg
        .getByRole("button", { name: `${fraction * 100}%`, exact: true })
        .click();
      assert.equal(
        Number(
          await dlg.getByLabel("加注到的总金额", { exact: true }).inputValue(),
        ),
        Math.min(
          max,
          Math.max(min, s.hand.currentBet + Math.round(s.hand.pot * fraction)),
        ),
      );
    }
    await dlg.getByLabel("加注到的总金额", { exact: true }).fill("40.5");
    assert(await dlg.getByRole("button", { name: /确认加注至/ }).isDisabled());
    await dlg
      .getByLabel("加注到的总金额", { exact: true })
      .fill(String(max + 1));
    assert(await dlg.getByRole("button", { name: /确认加注至/ }).isDisabled());
    await dlg.getByLabel("加注到的总金额", { exact: true }).fill(String(min));
    await dlg.getByLabel("加注筹码滑块", { exact: true }).focus();
    await p.page.keyboard.press("ArrowRight");
    assert.equal(
      Number(
        await dlg.getByLabel("加注到的总金额", { exact: true }).inputValue(),
      ),
      min + 1,
    );
    const range = await dlg
      .getByLabel("加注筹码滑块", { exact: true })
      .boundingBox();
    await p.page.mouse.move(
      range.x + range.width / 4,
      range.y + range.height / 2,
    );
    await p.page.mouse.down();
    await p.page.mouse.move(
      range.x + range.width * 0.75,
      range.y + range.height / 2,
      { steps: 6 },
    );
    await p.page.mouse.up();
    assert(
      Number(
        await dlg.getByLabel("加注到的总金额", { exact: true }).inputValue(),
      ) >
        min + 1,
      "Range drag did not adjust amount",
    );
    assert.equal(
      await p.page.locator("#table-menu, #table-chat").count(),
      0,
      "Range drag opened a drawer",
    );
    await dlg
      .getByLabel("加注到的总金额", { exact: true })
      .fill(String(min + 1));
    await p.page.screenshot({ path: path.join(out, "raise-modal.png") });
    await dlg.getByRole("button", { name: /确认加注至/ }).click();
    await until(async () => (await read()).hand.turnToken !== s.hand.turnToken);
    await act("call");
    await act("check");
    await act("check");
    // Stale turn closes the raise modal without applying a new action.
    let a = await actor();
    await a.p.page.locator(".bet-actions button").nth(1).click();
    await a.p.page.getByRole("dialog", { name: "加注", exact: true }).waitFor();
    await a.p.page.evaluate(
      ({ token }) => {
        const ws = new WebSocket(
          `ws://${location.host}/api/rooms/${new URLSearchParams(location.search).get("room")}/ws`,
        );
        ws.onopen = () =>
          ws.send(
            JSON.stringify({
              type: "action",
              action: "check",
              turnToken: token,
            }),
          );
        window.testSocket = ws;
      },
      { token: a.s.hand.turnToken },
    );
    await until(
      async () => (await read()).hand.turnToken !== a.s.hand.turnToken,
    );
    await a.p.page
      .getByRole("dialog", { name: "加注", exact: true })
      .waitFor({ state: "hidden" });
    await a.p.page.reload();
    await a.p.page.getByText("连接正常", { exact: true }).waitFor();
    await act("fold");
    await until(() =>
      host.page
        .getByRole("button", { name: "开始下一手", exact: true })
        .isEnabled(),
    );
    await host.page
      .getByRole("button", { name: "开始下一手", exact: true })
      .click();
    await until(async () => (await read()).hand.number === 2);
    a = await actor();
    await a.p.page.locator(".bet-actions button").nth(1).click();
    const allin = a.p.page.getByRole("dialog", { name: "加注", exact: true });
    await allin.getByRole("button", { name: "All-in", exact: true }).click();
    await allin.getByRole("button", { name: /确认全下/ }).click();
    await until(
      async () => (await read()).hand.turnToken !== a.s.hand.turnToken,
    );
    await act("call");
    async function setStacks(hostAmount, guestAmount) {
      await host.page
        .getByRole("button", { name: "打开牌桌菜单", exact: true })
        .click();
      await host.page
        .getByRole("button", { name: "房间设置", exact: true })
        .click();
      for (const [name, amount] of [
        ["skyli", hostAmount],
        ["Mia", guestAmount],
      ]) {
        await host.page
          .locator(".stack-settings > div")
          .filter({ hasText: name })
          .getByRole("button", { name: "调整", exact: true })
          .click();
        const stack = host.page.getByRole("dialog", {
          name: `调整 ${name} 的筹码`,
          exact: true,
        });
        await stack
          .getByLabel("筹码总额", { exact: true })
          .fill(String(amount));
        await stack
          .getByRole("button", { name: "确认调整", exact: true })
          .click();
        await stack.waitFor({ state: "hidden" });
      }
      await host.page.keyboard.press("Escape");
      await until(async () => {
        const s = await read();
        return (
          s.players.find((x) => x.id === host.user.id).stack === hostAmount &&
          s.players.find((x) => x.id === guest.user.id).stack === guestAmount
        );
      });
    }
    await setStacks(30, 2000);
    await host.page
      .getByRole("button", { name: "开始下一手", exact: true })
      .click();
    await until(async () => (await read()).hand.number === 3);
    a = await actor();
    assert.equal(a.p.user.id, host.user.id);
    await a.p.page.locator(".bet-actions button").nth(1).click();
    const short = a.p.page.getByRole("dialog", { name: "加注", exact: true });
    assert.equal(
      await short
        .getByLabel("加注筹码滑块", { exact: true })
        .getAttribute("min"),
      "30",
    );
    assert.equal(
      await short
        .getByLabel("加注筹码滑块", { exact: true })
        .getAttribute("max"),
      "30",
    );
    await short.getByRole("button", { name: "All-in", exact: true }).click();
    await short
      .getByRole("button", { name: "确认全下 20", exact: true })
      .click();
    await until(
      async () => (await read()).hand.turnToken !== a.s.hand.turnToken,
    );
    await act("call");
    await setStacks(2000, 2000);
    // Nine occupied seats, keeping authentic server sessions.
    for (let i = 2; i < 9; i++) {
      const q = await player("玩家" + (i + 1));
      players.push(q);
      await enter(q, i);
    }
    for (const width of [1440, 768, 390, 320]) {
      await host.page.setViewportSize({ width, height: 844 });
      await wait(180);
      await host.page.evaluate(() => scrollTo(0, 0));
      assert(
        await host.page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      );
      const boxes = await host.page
        .locator(".seat-avatar-button")
        .evaluateAll((nodes) =>
          nodes.map((n) => {
            const b = n.getBoundingClientRect();
            return { x: b.x, y: b.y, w: b.width, h: b.height };
          }),
        );
      assert.equal(boxes.length, 9);
      for (let i = 0; i < boxes.length; i++) {
        const b = boxes[i];
        assert(b.x >= 0 && b.x + b.w <= width, JSON.stringify({ width, b }));
        for (let j = i + 1; j < boxes.length; j++) {
          const c = boxes[j];
          assert(
            Math.min(b.x + b.w, c.x + c.w) - Math.max(b.x, c.x) <= 0 ||
              Math.min(b.y + b.h, c.y + c.h) - Math.max(b.y, c.y) <= 0,
            "Overlapping avatars " + width,
          );
        }
      }
      await host.page.evaluate(() => scrollTo(0, document.body.scrollHeight));
      const own = await host.page
          .locator(".own-seat .seat-stack")
          .boundingBox(),
        dock = await host.page.locator(".action-dock").boundingBox();
      assert(own.y + own.height <= dock.y, "Dock covers bottom seat");
      await host.page.screenshot({
        path: path.join(out, `nine-${width}.png`),
        fullPage: true,
      });
    }
    // Reconnect and explicitly leave/rejoin with the same identity, preserving this room's wins.
    const winningPlayer = players.find(
      (p) => (expectedWins.get(p.user.id) || 0) > 0,
    );
    const expected = expectedWins.get(winningPlayer.user.id);
    await winningPlayer.page.reload();
    await winningPlayer.page.getByText("连接正常", { exact: true }).waitFor();
    await until(
      async () =>
        (
          await winningPlayer.page
            .locator(".own-seat .seat-win-count")
            .innerText()
        ).trim() === String(expected),
    );
    await winningPlayer.page
      .getByRole("button", { name: "退出房间", exact: true })
      .click();
    await until(
      async () =>
        !(await read()).players.some((x) => x.id === winningPlayer.user.id),
    );
    const availableSeat = (await read()).players.some((x) => x.seat === 0)
      ? 1
      : 0;
    await enter(winningPlayer, availableSeat);
    await until(
      async () =>
        (
          await winningPlayer.page
            .locator(".own-seat .seat-win-count")
            .innerText()
        ).trim() === String(expected),
    );
    assert.equal(
      await winningPlayer.page.locator(".own-seat .lucide-crown").count(),
      1,
    );
    await winningPlayer.page.screenshot({
      path: path.join(out, "win-counter-rejoined.png"),
      fullPage: true,
    });
    const otherRoom = await (
      await winningPlayer.c.request.post(base + "/api/rooms", {
        data: { name: "新桌次数归零", settings },
      })
    ).json();
    await winningPlayer.page.goto(base + "/?room=" + otherRoom.id);
    await winningPlayer.page.getByText("连接正常", { exact: true }).waitFor();
    await until(async () =>
      (
        await (
          await winningPlayer.c.request.get(base + "/api/rooms/" + otherRoom.id)
        ).json()
      ).players.some((x) => x.id === winningPlayer.user.id && x.wins === 0),
    );
    assert.equal(errors.length, 0, errors.join("\n"));
    console.log(
      "PASS real room: responsive header/fixed dock, drawers/swipes/focus, chat/pin, pause/resume, raise/presets/bounds/keyboard, call/check/fold/all-in, stale modal, nine seats; authoritative win counts, one crown, reconnect/leave/rejoin retention, new-room zero.",
    );
  } finally {
    await Promise.all(contexts.map((c) => c.close()));
    await browser.close();
  }
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
