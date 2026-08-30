import { render, screen } from "@testing-library/react";
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

  constructor(url: string) {
    this.url = url;
    instances.push(this);
  }

  close() {
    this.closeCalls += 1;
  }

  fireMessage(data: string) {
    this.onmessage?.({ data });
  }
}

let instances: FakeWebSocket[] = [];
let originalWebSocket: typeof globalThis.WebSocket;

function fire(instance: FakeWebSocket, type: string, data: unknown) {
  instance.fireMessage(JSON.stringify({ type, data }));
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
});
