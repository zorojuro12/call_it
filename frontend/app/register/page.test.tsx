import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { getAccountToken } from "@/lib/session";
import Page from "./page";

const push = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push }),
}));

describe("register page", () => {
  beforeEach(() => {
    push.mockClear();
    sessionStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  test("a successful registration stores the account token and navigates", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          data: {
            account: {
              id: "u1",
              email: "a@b.test",
              display_name: "Ann",
              balance: 5000,
            },
            token: "acc-tok",
          },
        }),
        { status: 201 },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    render(<Page />);

    await user.type(screen.getByLabelText(/email/i), "a@b.test");
    await user.type(screen.getByLabelText(/password/i), "supersecretpassword");
    await user.type(screen.getByLabelText(/display name/i), "Ann");
    await user.click(screen.getByRole("button", { name: /register/i }));

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toMatch(/\/api\/v1\/auth\/register$/);
    expect(JSON.parse(init.body as string)).toEqual({
      email: "a@b.test",
      password: "supersecretpassword",
      display_name: "Ann",
    });

    expect(getAccountToken()).toBe("acc-tok");
    expect(push).toHaveBeenCalledWith("/host");
  });

  test("a server validation failure is surfaced and stores no token", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          error: {
            code: "validation_error",
            message: "password must be at least 12 characters",
          },
        }),
        { status: 400 },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    render(<Page />);

    await user.type(screen.getByLabelText(/email/i), "a@b.test");
    await user.type(screen.getByLabelText(/password/i), "short");
    await user.type(screen.getByLabelText(/display name/i), "Ann");
    await user.click(screen.getByRole("button", { name: /register/i }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toMatch(/at least 12 characters/i);
    expect(getAccountToken()).toBeNull();
    expect(push).not.toHaveBeenCalled();
  });
});
