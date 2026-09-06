import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, test } from "vitest";
import { clearSession, setRoomSummary, setRoomToken } from "@/lib/session";
import Page from "./page";

class FakeWebSocket {
  url: string;
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closeCalls = 0;
  sent: { type: string; data: unknown }[] = [];

  constructor(url: string) {
    this.url = url;
    instances.push(this);
  }

  close() {
    this.closeCalls += 1;
  }

  send(raw: string) {
    this.sent.push(JSON.parse(raw));
  }

  fireMessage(data: string) {
    this.onmessage?.({ data });
  }
}

let instances: FakeWebSocket[] = [];
let originalWebSocket: typeof globalThis.WebSocket;

function fire(instance: FakeWebSocket, type: string, data: unknown) {
  act(() => {
    instance.fireMessage(JSON.stringify({ type, data }));
  });
}

function renderPage(code = "ABC123") {
  return render(<Page params={Promise.resolve({ code })} />);
}

describe("room page", () => {
  beforeEach(() => {
    instances = [];
    sessionStorage.clear();
    originalWebSocket = globalThis.WebSocket;
    // @ts-expect-error test fake, not a full WebSocket implementation
    globalThis.WebSocket = FakeWebSocket;
  });

  afterEach(() => {
    globalThis.WebSocket = originalWebSocket;
  });

  test("shows own identity, balance, and the partial buy-in notice", async () => {
    setRoomToken("room-tok");
    setRoomSummary({
      room_id: "r1",
      guest: true,
      session_balance: 200,
      partial_buy_in: true,
    });

    renderPage("ABC123");

    expect(instances).toHaveLength(1);
    expect(instances[0].url).toContain("token=room-tok");

    fire(instances[0], "connected", {
      user_id: "u1",
      display_name: "Ann",
      room_id: "r1",
      guest: true,
      balance: 200,
    });

    expect(await screen.findByText(/Ann/)).toBeInTheDocument();
    expect(screen.getByText("200")).toBeInTheDocument();
    expect(
      screen.getByText("Joined with a partial buy-in: 200 tokens"),
    ).toBeInTheDocument();
  });

  test("hides the partial buy-in notice for a full buy-in", async () => {
    setRoomToken("room-tok");
    setRoomSummary({
      room_id: "r1",
      guest: false,
      session_balance: 1000,
      partial_buy_in: false,
    });

    renderPage("ABC123");
    fire(instances[0], "connected", {
      user_id: "u1",
      display_name: "Ann",
      room_id: "r1",
      guest: false,
      balance: 1000,
    });

    await screen.findByText(/Ann/);
    expect(screen.queryByText(/partial buy-in/i)).not.toBeInTheDocument();
  });

  test("without a room token, prompts back to / and opens no socket", () => {
    clearSession();
    renderPage("ABC123");

    expect(
      screen.getByRole("link", { name: /join|back|home/i }),
    ).toBeInTheDocument();
    expect(instances).toHaveLength(0);
  });

  test("the roster tracks joins and leaves", async () => {
    setRoomToken("room-tok");
    setRoomSummary({
      room_id: "r1",
      guest: false,
      session_balance: 1000,
      partial_buy_in: false,
    });

    renderPage("ABC123");
    fire(instances[0], "connected", {
      user_id: "u1",
      display_name: "Ann",
      room_id: "r1",
      guest: false,
      balance: 1000,
    });
    await screen.findByText(/Ann/);

    fire(instances[0], "player_joined", {
      user_id: "u2",
      display_name: "Bo",
      player_count: 2,
    });
    expect(await screen.findByText("Bo")).toBeInTheDocument();
    expect(screen.getByText(/^Ann/)).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();

    // A duplicate join for the same user must not double the entry.
    fire(instances[0], "player_joined", {
      user_id: "u2",
      display_name: "Bo",
      player_count: 2,
    });
    expect(screen.getAllByText("Bo")).toHaveLength(1);

    fire(instances[0], "player_left", {
      user_id: "u2",
      display_name: "Bo",
      player_count: 1,
    });
    expect(screen.queryByText("Bo")).not.toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();

    // Leaving for an unseen user_id must not throw or change the roster.
    expect(() =>
      fire(instances[0], "player_left", {
        user_id: "u3",
        display_name: "Zed",
        player_count: 1,
      }),
    ).not.toThrow();
    expect(screen.getByText(/^Ann/)).toBeInTheDocument();
  });

  test("the room page never renders any wager or stake data", async () => {
    setRoomToken("room-tok");
    setRoomSummary({
      room_id: "r1",
      guest: false,
      session_balance: 1000,
      partial_buy_in: false,
    });

    const { container } = renderPage("ABC123");
    fire(instances[0], "connected", {
      user_id: "u1",
      display_name: "Ann",
      room_id: "r1",
      guest: false,
      balance: 1000,
    });
    await screen.findByText(/Ann/);
    fire(instances[0], "player_joined", {
      user_id: "u2",
      display_name: "Bo",
      player_count: 2,
    });
    await screen.findByText("Bo");

    expect(container.textContent).not.toMatch(/\btokens?\b.*\b(staked|wagered)\b/i);
  });

  test("a player sees the round, the bettors counter, and the wager pad, never the host console", async () => {
    setRoomToken("room-tok");
    setRoomSummary({ room_id: "r1", guest: false, session_balance: 1000, partial_buy_in: false });

    renderPage("ABC123");
    fire(instances[0], "connected", {
      user_id: "u1",
      display_name: "Ann",
      room_id: "r1",
      guest: false,
      host: false,
      balance: 1000,
    });
    await screen.findByText(/Ann/);

    fire(instances[0], "round_opened", {
      round_id: "rd1",
      question: "Next goal?",
      outcomes: ["Home", "Away"],
      lock_at_ms: Date.now() + 30000,
    });
    fire(instances[0], "odds_updated", {
      round_id: "rd1",
      pools: [0, 0],
      total: 0,
      multipliers: [],
      bettors: 1,
      players: 3,
    });

    expect(await screen.findByText("Next goal?")).toBeInTheDocument();
    // "Home"/"Away" render twice — once in the OddsBoard table, once as a
    // WagerPad outcome button — so assert via the button role specifically.
    expect(screen.getByRole("button", { name: "Home" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Away" })).toBeInTheDocument();
    expect(screen.getByText("1/3 players have placed their bets")).toBeInTheDocument();
    expect(screen.getByLabelText("Amount")).toBeInTheDocument();
    expect(screen.queryByText("Which outcome won?")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Question")).not.toBeInTheDocument();
  });

  test("the host sees the open-round form and never the wager pad", async () => {
    setRoomToken("room-tok");
    setRoomSummary({ room_id: "r1", guest: false, session_balance: 1000, partial_buy_in: false });

    renderPage("ABC123");
    fire(instances[0], "connected", {
      user_id: "host1",
      display_name: "Hank",
      room_id: "r1",
      guest: false,
      host: true,
      balance: 1000,
    });

    expect(await screen.findByLabelText("Question")).toBeInTheDocument();
    expect(screen.queryByLabelText("Amount")).not.toBeInTheDocument();
  });

  test("placing a wager sends place_wager and applies the server's confirmed balance", async () => {
    const user = userEvent.setup();
    setRoomToken("room-tok");
    setRoomSummary({ room_id: "r1", guest: false, session_balance: 1000, partial_buy_in: false });

    renderPage("ABC123");
    fire(instances[0], "connected", {
      user_id: "u1",
      display_name: "Ann",
      room_id: "r1",
      guest: false,
      host: false,
      balance: 1000,
    });
    fire(instances[0], "round_opened", {
      round_id: "rd1",
      question: "Next goal?",
      outcomes: ["Home", "Away"],
      lock_at_ms: Date.now() + 30000,
    });
    await screen.findByText("Next goal?");

    await user.click(screen.getByRole("button", { name: "Home" }));
    await user.type(screen.getByLabelText("Amount"), "100");
    await user.click(screen.getByRole("button", { name: "Place bet" }));

    const sent = instances[0].sent.find((m) => m.type === "place_wager");
    expect(sent).toBeDefined();
    const data = sent!.data as { outcome: number; amount: number; idempotency_key: string };
    expect(data.outcome).toBe(0);
    expect(data.amount).toBe(100);
    expect(data.idempotency_key).toBeTruthy();

    fire(instances[0], "wager_accepted", { round_id: "rd1", outcome: 0, amount: 100, balance: 900 });
    expect(await screen.findByText("900")).toBeInTheDocument();
  });

  test("locking disables the pad and resolving reveals the settlement", async () => {
    setRoomToken("room-tok");
    setRoomSummary({ room_id: "r1", guest: false, session_balance: 1000, partial_buy_in: false });

    renderPage("ABC123");
    fire(instances[0], "connected", {
      user_id: "u1",
      display_name: "Ann",
      room_id: "r1",
      guest: false,
      host: false,
      balance: 1000,
    });
    fire(instances[0], "round_opened", {
      round_id: "rd1",
      question: "Next goal?",
      outcomes: ["Home", "Away"],
      lock_at_ms: Date.now() + 30000,
    });
    await screen.findByText("Next goal?");

    fire(instances[0], "round_locked", { round_id: "rd1" });
    expect(await screen.findByText("Betting is closed")).toBeInTheDocument();

    fire(instances[0], "round_resolved", {
      round_id: "rd1",
      winning_outcome: 0,
      results: [
        { user_id: "u1", display_name: "Ann", staked: 100, returned: 250, net: 150 },
        { user_id: "u2", display_name: "Bob", staked: 100, returned: 0, net: -100 },
      ],
      dust: 3,
      refunded: false,
    });

    const settlementTable = (await screen.findByText("Bob")).closest("table");
    expect(settlementTable?.textContent).toContain("Ann (you)");
  });
});
