import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { getRoomToken, setAccountToken } from "@/lib/session";
import Page from "./page";

const push = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push }),
}));

describe("host page", () => {
  beforeEach(() => {
    push.mockClear();
    sessionStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  test("a host creates a room and gets a shareable code", async () => {
    setAccountToken("acc-tok");
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          data: { room_id: "r1", code: "ABC123", buy_in: 1000, token: "room-tok" },
        }),
        { status: 201 },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    render(<Page />);

    await user.clear(screen.getByLabelText(/buy-in/i));
    await user.type(screen.getByLabelText(/buy-in/i), "1000");
    await user.click(screen.getByRole("button", { name: /create room/i }));

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toMatch(/\/api\/v1\/rooms$/);
    expect(init.headers["Authorization"]).toBe("Bearer acc-tok");
    expect(JSON.parse(init.body as string)).toEqual({ buy_in: 1000 });

    expect(getRoomToken()).toBe("room-tok");
    expect(await screen.findByText("ABC123")).toBeInTheDocument();

    const shareLink = screen.queryByRole("link", { name: /ABC123/i });
    const shareTextbox = screen.queryByRole("textbox", {
      name: /shareable|share|link/i,
    });
    expect(shareLink ?? shareTextbox).not.toBeNull();
    if (shareTextbox) {
      expect(shareTextbox).toHaveAttribute("readonly");
      expect((shareTextbox as HTMLInputElement).value).toMatch(/\/room\/ABC123$/);
    }

    expect(push).not.toHaveBeenCalled();
  });

  test("without an account token, the page prompts to log in instead of a form", () => {
    render(<Page />);

    expect(screen.getByRole("link", { name: /log in/i })).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /create room/i }),
    ).not.toBeInTheDocument();
  });
});
