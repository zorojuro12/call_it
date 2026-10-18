// Game-logic check: drives the real UI with one host and two guests and
// asserts the game's rules against hand-computed numbers, through to the
// PostgreSQL ledger.
//
// Opt-in, because it needs more than the default e2e setup: the outbox
// relay and ledger worker running against the dev stack, and the local
// `call-it-postgres-1` container. Run it with:
//   make up-full
//   (cd backend && POSTGRES_DSN=... go run ./cmd/migrate)
//   make relay & POSTGRES_DSN=... make ledger-worker &
//   cd frontend && CALLIT_LEDGER_E2E=1 npx playwright test e2e/logic-check.spec.ts
import { test, expect, type Page } from "@playwright/test";
import { execSync } from "node:child_process";

const psql = (sql: string): string =>
  execSync(`docker exec call-it-postgres-1 psql -U callit -tAc "${sql}"`).toString().trim();

const balanceOf = (page: Page) => page.locator("p.text-2xl");

/** Records every WebSocket frame a page receives, in order. */
function recordFrames(page: Page): string[] {
  const frames: string[] = [];
  page.on("websocket", (ws) => ws.on("framereceived", (f) => frames.push(String(f.payload))));
  return frames;
}

async function joinAsGuest(page: Page, code: string, name: string) {
  await page.goto("/");
  await page.getByRole("textbox", { name: /room code/i }).fill(code);
  await page.getByLabel(/display name/i).fill(name);
  await page.getByRole("button", { name: /join/i }).click();
  await page.waitForURL(new RegExp(`/room/${code}$`));
}

async function openRound(host: Page, question: string, lockSeconds: number) {
  await host.getByLabel("Question").fill(question);
  await host.getByLabel("Outcome 1").fill("Red");
  await host.getByLabel("Outcome 2").fill("Blue");
  await host.getByLabel("Lock (seconds)").fill(String(lockSeconds));
  await host.getByRole("button", { name: "Open round" }).click();
}

async function bet(page: Page, outcome: string, amount: number) {
  await page.getByRole("group", { name: "Outcomes" }).getByRole("button", { name: outcome }).click();
  await page.getByLabel("Amount").fill(String(amount));
  await page.getByRole("button", { name: "Place bet" }).click();
}

test.skip(!process.env.CALLIT_LEDGER_E2E, "opt-in: set CALLIT_LEDGER_E2E=1 with relay and ledger-worker running");

test("game rules hold end to end: odds, anonymity, payouts, refund, ledger", async ({ browser }) => {
  test.setTimeout(120_000);
  const [hostCtx, annCtx, boCtx] = await Promise.all([browser.newContext(), browser.newContext(), browser.newContext()]);
  const [host, ann, bo] = await Promise.all([hostCtx.newPage(), annCtx.newPage(), boCtx.newPage()]);
  const annFrames = recordFrames(ann);

  // --- Setup: host registers and creates a 1000 buy-in room; two guests join.
  await host.goto("/register");
  await host.getByLabel(/email/i).fill(`logic-${crypto.randomUUID()}@e2e.test`);
  await host.getByLabel(/password/i).fill("supersecretpassword123");
  await host.getByLabel(/display name/i).fill("HostHal");
  await host.getByRole("button", { name: /register/i }).click();
  await host.waitForURL(/\/host$/);
  await host.getByLabel(/buy-in/i).fill("1000");
  await host.getByRole("button", { name: /create room/i }).click();
  const code = (await host.locator("p.text-4xl").textContent())!.trim();
  await host.getByRole("link", { name: /go to room/i }).click();
  await joinAsGuest(ann, code, "GuestAnn");
  await joinAsGuest(bo, code, "GuestBo");
  await expect(balanceOf(ann)).toHaveText("1000");
  await expect(balanceOf(bo)).toHaveText("1000");

  // --- Round 1: Ann 100 on Red, Bo 300 on Blue. Blue wins.
  await openRound(host, "Round one?", 6);
  await expect(ann.getByText("Round one?")).toBeVisible();

  // Rule: the host cannot wager — no wager pad is rendered for them.
  await expect(host.getByRole("button", { name: "Place bet" })).toHaveCount(0);

  await bet(ann, "Red", 100);
  await expect(balanceOf(ann)).toHaveText("900");
  await expect(host.getByText("1/2 players have placed their bets")).toBeVisible();
  await bet(bo, "Blue", 300);
  await expect(balanceOf(bo)).toHaveText("700");

  // Rule: live odds are pool totals only — pools 100/300, total 400,
  // multipliers 400/100 = 4.00× and 400/300 = 1.33×; counter counts players.
  for (const page of [host, ann, bo]) {
    await expect(page.getByText("Total pool: 400")).toBeVisible();
    await expect(page.locator("tr", { hasText: "Red" }).getByText("4.00×")).toBeVisible();
    await expect(page.locator("tr", { hasText: "Blue" }).getByText("1.33×")).toBeVisible();
    await expect(page.getByText("2/2 players have placed their bets")).toBeVisible();
  }

  // Rule: wagers stay anonymous until the round is terminal — no page shows
  // a per-player row before the reveal.
  for (const page of [host, ann]) {
    await expect(page.locator("tr", { hasText: "GuestBo" })).toHaveCount(0);
  }

  // Rule: lockout closes betting on the server's clock.
  await expect(ann.getByText("Betting is closed")).toBeVisible({ timeout: 15_000 });
  await expect(ann.getByRole("button", { name: "Place bet" })).toBeDisabled();

  // Same rule, at the wire: nothing Ann's socket received before the reveal
  // carries anyone's stake.
  const framesBeforeReveal = annFrames.length;
  expect(annFrames.slice(0, framesBeforeReveal).filter((f) => f.includes("staked") || f.includes("GuestBo\",\"amount"))).toEqual([]);

  // Settlement: Blue wins. Bo returns floor(300 × 400 / 300) = 400 → 1100.
  // Ann loses her 100 → 900. Dust 0.
  await host.getByRole("button", { name: "Blue" }).click();
  for (const page of [host, ann, bo]) {
    const boRow = page.locator("tr", { hasText: "GuestBo" });
    await expect(boRow.locator("td").nth(1)).toHaveText("300");
    await expect(boRow.locator("td").nth(2)).toHaveText("400");
    const annRow = page.locator("tr", { hasText: "GuestAnn" });
    await expect(annRow.locator("td").nth(1)).toHaveText("100");
    await expect(annRow.locator("td").nth(2)).toHaveText("0");
    await expect(page.getByText("Dust: 0")).toBeVisible();
  }
  await expect(balanceOf(bo)).toHaveText("1100");
  await expect(balanceOf(ann)).toHaveText("900");

  // --- Round 2: only Ann bets, 50 on Red; Blue wins → nobody backed the
  // winner, so every stake is refunded and Ann ends back at 900.
  await openRound(host, "Round two?", 4);
  await expect(ann.getByText("Round two?")).toBeVisible();
  await bet(ann, "Red", 50);
  await expect(balanceOf(ann)).toHaveText("850");
  await expect(ann.getByText("Betting is closed")).toBeVisible({ timeout: 15_000 });
  await host.getByRole("button", { name: "Blue" }).click();
  await expect(ann.getByText("Nobody backed the winning outcome — every stake was returned")).toBeVisible();
  await expect(balanceOf(ann)).toHaveText("900");
  await expect(balanceOf(bo)).toHaveText("1100");

  // --- Ledger: the outbox → Kafka → PostgreSQL path recorded all of it.
  // Reconciliation identity for a live session (CLAUDE.md):
  //   redis_wallet − opening_stake == ledger_balance
  //   Ann: 900 − 1000 = −100;  Bo: 1100 − 1000 = +100.
  const room = psql("select room_id from transactions where room_id is not null order by occurred_at desc limit 1");
  const ledgerNet = (): string =>
    psql(
      `select string_agg(net::text, ',' order by net) from (select sum(case when e.direction='credit' then e.amount else -e.amount end) as net ` +
        // user_wallet accounts are per user, not per room; the room is on the transaction.
        `from ledger_entries e join accounts a on a.id = e.account_id join transactions tx on tx.id = e.transaction_id ` +
        `where a.kind = 'user_wallet' and tx.room_id = '${room}' group by a.id) t`,
    );
  await expect.poll(ledgerNet, { timeout: 30_000 }).toBe("-100,100");

  // Every transaction in this room balances (Σ debits == Σ credits).
  const unbalanced = psql(
    `select count(*) from (select t.id from transactions t join ledger_entries e on e.transaction_id = t.id where t.room_id = '${room}' ` +
      `group by t.id having sum(case when e.direction='debit' then e.amount else 0 end) <> sum(case when e.direction='credit' then e.amount else 0 end)) x`,
  );
  expect(unbalanced).toBe("0");

  await Promise.all([hostCtx.close(), annCtx.close(), boCtx.close()]);
});
