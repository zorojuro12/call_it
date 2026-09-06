import { test, expect } from "@playwright/test";

test("two browsers play a full round end to end", async ({ browser }) => {
  const hostContext = await browser.newContext();
  const guestContext = await browser.newContext();
  const hostPage = await hostContext.newPage();
  const guestPage = await guestContext.newPage();

  const email = `host-${Date.now()}@e2e.test`;

  await hostPage.goto("/register");
  await hostPage.getByLabel(/email/i).fill(email);
  await hostPage.getByLabel(/password/i).fill("supersecretpassword123");
  await hostPage.getByLabel(/display name/i).fill("HostAnn");
  await hostPage.getByRole("button", { name: /register/i }).click();
  await hostPage.waitForURL(/\/host$/);

  await hostPage.getByLabel(/buy-in/i).fill("1000");
  await hostPage.getByRole("button", { name: /create room/i }).click();

  const codeLocator = hostPage.locator("p.text-4xl");
  await expect(codeLocator).toHaveText(/^[A-Z0-9]+$/);
  const code = (await codeLocator.textContent())!.trim();

  await hostPage.getByRole("link", { name: /go to room/i }).click();
  await hostPage.waitForURL(new RegExp(`/room/${code}$`));

  await guestPage.goto("/");
  await guestPage.getByRole("textbox", { name: /room code/i }).fill(code);
  await guestPage.getByLabel(/display name/i).fill("GuestBo");
  await guestPage.getByRole("button", { name: /join/i }).click();
  await guestPage.waitForURL(new RegExp(`/room/${code}$`));

  // Host opens a round: Next goal?, Home/Away, a 3-second lock — the
  // server's minimum, so this test does not idle.
  await hostPage.getByLabel("Question").fill("Next goal?");
  await hostPage.getByLabel("Outcome 1").fill("Home");
  await hostPage.getByLabel("Outcome 2").fill("Away");
  await hostPage.getByLabel("Lock (seconds)").fill("3");
  await hostPage.getByRole("button", { name: "Open round" }).click();

  await expect(guestPage.getByText("Next goal?")).toBeVisible();

  // Guest stakes 100 on Home.
  await guestPage.getByRole("button", { name: "Home" }).click();
  await guestPage.getByLabel("Amount").fill("100");
  await guestPage.getByRole("button", { name: "Place bet" }).click();

  await expect(guestPage.getByText("900", { exact: true })).toBeVisible();
  await expect(hostPage.getByText("1/1 players have placed their bets")).toBeVisible();

  // Wait for the 3-second lockout, then confirm wagering is closed.
  await expect(guestPage.getByText("Betting is closed")).toBeVisible({ timeout: 10_000 });

  // Host resolves Home — the sole backer's pool equals their stake, so the
  // multiplier is 1 and net is 0 (100 staked, 100 returned).
  await hostPage.getByRole("button", { name: "Home" }).click();

  for (const page of [hostPage, guestPage]) {
    const row = page.locator("tr", { hasText: "GuestBo" });
    await expect(row).toBeVisible();
    await expect(row.getByText("100", { exact: true }).first()).toBeVisible();
  }

  await guestContext.close();
  await hostContext.close();
});
