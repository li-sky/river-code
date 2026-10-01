// Real room/showdown layout acceptance. Use an isolated local service.
const { chromium } = require("playwright");
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const base = process.env.RIVER_TEST_URL || "http://localhost:8092";
const out =
  process.env.RIVER_ARTIFACT_DIR ||
  path.join(require("node:os").tmpdir(), "river-mobile-table-review");
const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
async function until(fn) {
  for (let i = 0; i < 120; i++) {
    const result = await fn();
    if (result) return result;
    await pause(60);
  }
  throw new Error("Room state did not update");
}
async function geometry(page, width) {
  const result = await page.evaluate(() => {
    const rect = (n) => {
      const b = n.getBoundingClientRect();
      return {
        left: b.left,
        right: b.right,
        top: b.top,
        bottom: b.bottom,
        width: b.width,
        height: b.height,
      };
    };
    const visible = (n) => n.getClientRects().length > 0;
    const seats = [...document.querySelectorAll(".table-seat")].map((n) => ({
      own: n.classList.contains("own-seat"),
      bounds: rect(n),
      parts: [
        ...n.querySelectorAll(
          ".seat-avatar-button,.seat-name,.seat-stack,.seat-cards .card,.seat-hand-rank,.seat-bet,.empty-seat,.avatar-emoji,.winner-crown,.dealer-button,.turn-timer,.player-badge",
        ),
      ]
        .filter(visible)
        .map(rect),
    }));
    return {
      overflow: document.documentElement.scrollWidth > innerWidth,
      seats,
      central: [...document.querySelectorAll(".pot,.board,.hand-caption")].map(
        rect,
      ),
      dock: rect(document.querySelector(".action-dock")),
      ownRankVisible: visible(
        document.querySelector(".own-seat .seat-hand-rank") ||
          document.createElement("span"),
      ),
      avatars: [...document.querySelectorAll(".seat-avatar-button")].map(rect),
      boardCards: [...document.querySelectorAll(".board .card")].map(rect),
    };
  });
  const intersects = (a, b) =>
    Math.min(a.right, b.right) - Math.max(a.left, b.left) > 1 &&
    Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > 1;
  assert(!result.overflow, `Overflow at ${width}`);
  const cards = result.boardCards;
  assert(
    Math.abs(cards[0].top - cards[2].top) < 1 &&
      Math.abs(cards[3].top - cards[4].top) < 1 &&
      cards[3].top >= cards[0].bottom + 3,
    "Board should have three cards above two",
  );
  assert(
    Math.abs((cards[3].left + cards[4].right) / 2 - width / 2) < 2,
    "Second board row is not centered",
  );
  assert(!result.ownRankVisible, "Own hand rank duplicated on mobile seat");
  for (const seat of result.seats) {
    for (const part of seat.parts) {
      assert(
        part.left >= 0 && part.right <= width,
        JSON.stringify({ width, part }),
      );
      for (const center of result.central)
        assert(
          !intersects(part, center),
          "Seat overlaps board/pot/result: " +
            JSON.stringify({ width, part, center }),
        );
    }
  }
  for (let i = 0; i < result.seats.length; i++) {
    for (let j = i + 1; j < result.seats.length; j++) {
      for (const a of result.seats[i].parts) {
        for (const b of result.seats[j].parts)
          assert(
            !intersects(a, b),
            "Players overlap: " + JSON.stringify({ width, i, j, a, b }),
          );
      }
    }
  }
  assert(result.avatars.every((b) => b.width >= 48 && b.height >= 48));
  const own = result.seats.find((s) => s.own).bounds;
  assert(
    Math.abs((own.left + own.right) / 2 - width / 2) < 2 &&
      own.top > result.central[1].bottom,
    "Own bottom seat is not centered",
  );
  assert(Math.abs(result.dock.bottom - 844) < 1, "Action dock is not fixed");
  return result;
}
(async () => {
  await fs.mkdir(out, { recursive: true });
  const browser = await chromium.launch({ channel: "chrome" });
  const players = [],
    errors = [];
  try {
    for (let i = 0; i < 9; i++) {
      const context = await browser.newContext({
        viewport: { width: 390, height: 844 },
        hasTouch: true,
      });
      let response;
      for (let attempt = 0; attempt < 90; attempt++) {
        response = await context.request.post(base + "/api/auth/guest", {
          data: {
            name:
              i === 0
                ? "skyli"
                : i === 8
                  ? "名字很长的朋友九号"
                  : "好友" + (i + 1),
            password: "",
          },
        });
        if (response.status() !== 429) break;
        await pause(1000);
      }
      assert(response.ok(), "Guest auth: " + response.status());
      if (i < 2) {
        assert(
          (
            await context.request.patch(base + "/api/me", {
              data: { settings: { avatarEmoji: i === 0 ? "😊" : "😘" } },
            })
          ).ok(),
        );
      }
      const user = await (await context.request.get(base + "/api/me")).json();
      const page = await context.newPage();
      page.on("pageerror", (error) => errors.push(error.stack));
      players.push({ context, user, page });
    }
    const host = players[0];
    for (const count of [2, 3, 4, 5, 7, 6, 8, 9]) {
      const room = await (
        await host.context.request.post(base + "/api/rooms", {
          data: {
            name: "宽松牌桌 " + count,
            settings: {
              visibility: "public",
              smallBlind: 10,
              bigBlind: 20,
              buyIn: 50000,
              maxPlayers: count,
              actionSeconds: 120,
              voiceEnabled: false,
              voiceMode: "free",
              spectatorVoiceEnabled: false,
              chatEnabled: true,
              reactionsEnabled: true,
            },
          },
        })
      ).json();
      const read = async () =>
        (await host.context.request.get(base + "/api/rooms/" + room.id)).json();
      const enter = async (p, seat) => {
        await p.page.goto(base + "/?room=" + room.id);
        await p.page.getByText("连接正常", { exact: true }).waitFor();
        await p.page
          .getByRole("button", { name: `坐入 ${seat + 1} 号座位`, exact: true })
          .click();
        await p.page.getByLabel("买入筹码", { exact: true }).fill("50000");
        await p.page
          .getByRole("button", { name: "确认入座", exact: true })
          .click();
        await until(async () =>
          (await read()).players.some(
            (x) => x.id === p.user.id && x.seat === seat,
          ),
        );
      };
      await enter(host, 0);
      if (count === 8) {
        await enter(players[1], 1);
        await host.page.screenshot({
          path: path.join(out, "eight-two-players.png"),
          fullPage: true,
        });
      }
      for (const width of [320, 390]) {
        await host.page.setViewportSize({ width, height: 844 });
        await pause(150);
        await geometry(host.page, width);
      }
      if (![6, 8, 9].includes(count)) continue;
      for (let i = count === 8 ? 2 : 1; i < count; i++)
        await enter(players[i], i);
      if (count === 8) {
        // Capture the full waiting-table layout as well.
        await host.page.screenshot({
          path: path.join(out, "eight-waiting.png"),
          fullPage: true,
        });
      }
      await host.page
        .getByRole("button", { name: "开始牌局", exact: true })
        .click();
      await until(async () => (await read()).hand?.phase === "preflop");
      await host.page.setViewportSize({ width: 320, height: 844 });
      await pause(1100);
      await geometry(host.page, 320);
      let raised = false;
      for (let turn = 0; turn < 12; turn++) {
        const state = await read();
        if (state.hand.phase === "complete") break;
        const id = state.players.find((x) => x.seat === state.hand.turnSeat).id;
        const actor = players.find((x) => x.user.id === id);
        if (!raised) {
          await actor.page.locator(".bet-actions button").nth(1).click();
          const dialog = actor.page.getByRole("dialog", {
            name: "加注",
            exact: true,
          });
          await dialog
            .getByRole("button", { name: "All-in", exact: true })
            .click();
          await dialog.getByRole("button", { name: /确认全下/ }).click();
          raised = true;
        } else {
          await actor.page.locator(".bet-actions button").nth(0).click();
        }
        await until(
          async () =>
            (await read()).hand.turnToken !== state.hand.turnToken ||
            (await read()).hand.phase === "complete",
        );
        await pause((await read()).hand.phase === "complete" ? 1100 : 100);
        await geometry(host.page, 320);
      }
      assert.equal((await read()).hand.phase, "complete");
      await players[1].page.setViewportSize({ width: 390, height: 844 });
      await geometry(players[1].page, 390);
      await host.page.locator(".board .card-face").first().waitFor();
      await until(
        async () =>
          (await host.page
            .locator(".seat-cards.revealed-cards .card-face")
            .count()) ===
          count * 2,
      );
      for (const width of [320, 390, 600]) {
        await host.page.setViewportSize({ width, height: 844 });
        await host.page.evaluate(() => scrollTo(0, 0));
        await pause(1100);
        await geometry(host.page, width);
        assert.equal(await host.page.locator(".board .card-face").count(), 5);
        await host.page.screenshot({
          path: path.join(out, `showdown-${count}-${width}.png`),
          fullPage: true,
        });
        await host.page.evaluate(() => scrollTo(0, document.body.scrollHeight));
        const stack = await host.page
          .locator(".own-seat .seat-stack")
          .boundingBox();
        const dock = await host.page.locator(".action-dock").boundingBox();
        assert(
          stack.y + stack.height <= dock.y,
          "Dock covers own stack after scrolling",
        );
      }
      // Synthetic long-result text checks bounding; it is not settlement evidence.
      await host.page.setViewportSize({ width: 320, height: 844 });
      await host.page
        .locator(".hand-caption")
        .evaluate(
          (n) =>
            (n.textContent = Array(9)
              .fill("名字很长的获胜玩家 +50,000 · 皇家同花顺")
              .join(" / ")),
        );
      await geometry(host.page, 320);
      console.log(
        `PASS ${count} seats: real all-in/showdown, full seat contents, board/results, 320/390/600px`,
      );
    }
    assert.equal(errors.length, 0, errors.join("\n"));
    console.log(
      "PASS 2–9 seat order, empty-seat entry, touch targets, bounded long settlement, fixed dock.",
    );
  } finally {
    await Promise.all(players.map((p) => p.context.close()));
    await browser.close();
  }
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
