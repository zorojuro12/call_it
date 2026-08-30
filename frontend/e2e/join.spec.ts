import { test, expect } from "@playwright/test";

test("two browsers in one room see each other", async ({ browser }) => {
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
  await expect(hostPage.getByText("HostAnn")).toBeVisible();

  await guestPage.goto("/");
  await guestPage.getByRole("textbox", { name: /room code/i }).fill(code);
  await guestPage.getByLabel(/display name/i).fill("GuestBo");
  await guestPage.getByRole("button", { name: /join/i }).click();
  await guestPage.waitForURL(new RegExp(`/room/${code}$`));

  await expect(guestPage.getByText("HostAnn")).toBeVisible();
  await expect(guestPage.getByText("GuestBo")).toBeVisible();
  await expect(guestPage.getByText("2", { exact: true })).toBeVisible();

  await expect(hostPage.getByText("GuestBo")).toBeVisible();

  await guestContext.close();

  await expect(hostPage.getByText("GuestBo")).not.toBeVisible();
  await expect(hostPage.getByText("1", { exact: true })).toBeVisible();

  await hostContext.close();
});
