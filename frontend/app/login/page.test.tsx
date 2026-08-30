import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { getAccountToken } from "@/lib/session";
import Page from "./page";

const push = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push }),
}));

describe("login page", () => {
  beforeEach(() => {
    push.mockClear();
    sessionStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  test("a successful login stores the account token and navigates", async () => {
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
        { status: 200 },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    render(<Page />);

    const emailInput = screen.getByLabelText(/email/i);
    const passwordInput = screen.getByLabelText(/password/i);
    expect(emailInput).toHaveAttribute("type", "email");
    expect(passwordInput).toHaveAttribute("type", "password");

    await user.type(emailInput, "a@b.test");
    await user.type(passwordInput, "supersecretpassword");
    await user.click(screen.getByRole("button", { name: /log in/i }));

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toMatch(/\/api\/v1\/auth\/login$/);
    expect(JSON.parse(init.body as string)).toEqual({
      email: "a@b.test",
      password: "supersecretpassword",
    });

    expect(getAccountToken()).toBe("acc-tok");
    expect(push).toHaveBeenCalledWith("/host");
  });
});
