import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { getRoomSummary, getRoomToken, setAccountToken } from "@/lib/session";
import Page from "./page";

const push = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push }),
}));

describe("landing page", () => {
  beforeEach(() => {
    push.mockClear();
    sessionStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  test("renders the app shell", () => {
    render(<Page />);

    expect(
      screen.getByRole("heading", { name: /CallIt/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("textbox", { name: /room code/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /join/i }),
    ).toBeInTheDocument();
  });

  test("a guest joins by code with a display name", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          data: {
            room_id: "r1",
            guest: true,
            session_balance: 1000,
            partial_buy_in: false,
            token: "room-tok",
          },
        }),
        { status: 200 },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    render(<Page />);

    await user.type(screen.getByRole("textbox", { name: /room code/i }), "ABC123");
    await user.type(screen.getByLabelText(/display name/i), "Ann");
    await user.click(screen.getByRole("button", { name: /join/i }));

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toMatch(/\/api\/v1\/rooms\/ABC123\/participants$/);
    expect(JSON.parse(init.body as string)).toEqual({ display_name: "Ann" });
    expect(init.headers["Authorization"]).toBeUndefined();

    expect(getRoomToken()).toBe("room-tok");
    expect(getRoomSummary()).toEqual({
      room_id: "r1",
      guest: true,
      session_balance: 1000,
      partial_buy_in: false,
    });
    expect(push).toHaveBeenCalledWith("/room/ABC123");
  });

  test("an account holder joining sends their bearer token", async () => {
    setAccountToken("acc-tok");
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          data: {
            room_id: "r1",
            guest: false,
            session_balance: 1000,
            partial_buy_in: false,
            token: "room-tok",
          },
        }),
        { status: 200 },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    render(<Page />);

    await user.type(screen.getByRole("textbox", { name: /room code/i }), "ABC123");
    await user.type(screen.getByLabelText(/display name/i), "Ann");
    await user.click(screen.getByRole("button", { name: /join/i }));

    const [, init] = fetchMock.mock.calls[0];
    expect(init.headers["Authorization"]).toBe("Bearer acc-tok");
  });

  test("a room-not-found failure is surfaced and does not navigate", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          error: { code: "room_not_found", message: "room not found" },
        }),
        { status: 404 },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    render(<Page />);
    await user.type(screen.getByRole("textbox", { name: /room code/i }), "ZZZ999");
    await user.type(screen.getByLabelText(/display name/i), "Ann");
    await user.click(screen.getByRole("button", { name: /join/i }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toMatch(/room not found/i);
    expect(push).not.toHaveBeenCalled();
    expect(getRoomToken()).toBeNull();
  });

  test("a partial buy-in still succeeds and navigates", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          data: {
            room_id: "r1",
            guest: true,
            session_balance: 200,
            partial_buy_in: true,
            token: "room-tok",
          },
        }),
        { status: 200 },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    render(<Page />);
    await user.type(screen.getByRole("textbox", { name: /room code/i }), "ABC123");
    await user.type(screen.getByLabelText(/display name/i), "Ann");
    await user.click(screen.getByRole("button", { name: /join/i }));

    expect(push).toHaveBeenCalledWith("/room/ABC123");
    expect(getRoomToken()).toBe("room-tok");
    expect(getRoomSummary()).toEqual({
      room_id: "r1",
      guest: true,
      session_balance: 200,
      partial_buy_in: true,
    });
  });
});
