import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

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

  fireOpen() {
    this.onopen?.();
  }

  fireMessage(data: string) {
    this.onmessage?.({ data });
  }

  fireClose() {
    this.onclose?.();
  }
}

let instances: FakeWebSocket[] = [];
let originalWebSocket: typeof globalThis.WebSocket;

beforeEach(() => {
  instances = [];
  originalWebSocket = globalThis.WebSocket;
  // @ts-expect-error test fake, not a full WebSocket implementation
  globalThis.WebSocket = FakeWebSocket;
});

afterEach(() => {
  globalThis.WebSocket = originalWebSocket;
  vi.useRealTimers();
});

describe("toWebSocketUrl", () => {
  test.each([
    [
      "http://localhost:8080",
      "t1",
      "ws://localhost:8080/api/v1/socket?token=t1",
    ],
    ["https://api.test", "t1", "wss://api.test/api/v1/socket?token=t1"],
  ])("%s + %s -> %s", async (base, token, expected) => {
    const { toWebSocketUrl } = await import("./socket");
    expect(toWebSocketUrl(base, token)).toBe(expected);
  });

  test("percent-encodes the token", async () => {
    const { toWebSocketUrl } = await import("./socket");
    const url = toWebSocketUrl("http://localhost:8080", "a b+c/d");
    expect(url).toContain("token=a%20b%2Bc%2Fd");
  });
});

describe("openRoomSocket", () => {
  test("constructs exactly one WebSocket at the derived URL", async () => {
    const { openRoomSocket, toWebSocketUrl } = await import("./socket");
    const { API_BASE_URL } = await import("./config");

    const socket = openRoomSocket("t1");
    socket.close();

    expect(instances).toHaveLength(1);
    expect(instances[0].url).toBe(toWebSocketUrl(API_BASE_URL, "t1"));
  });
});

describe("envelope dispatch", () => {
  test("routes an envelope's inner data to handlers registered for its type", async () => {
    const { openRoomSocket } = await import("./socket");
    const socket = openRoomSocket("t1");
    const raw = instances[0];

    const connectedHandler = vi.fn();
    socket.on("connected", connectedHandler);

    raw.fireMessage(
      JSON.stringify({
        type: "connected",
        data: { user_id: "u1", display_name: "Ann", room_id: "r1", guest: true },
      }),
    );

    expect(connectedHandler).toHaveBeenCalledTimes(1);
    expect(connectedHandler).toHaveBeenCalledWith({
      user_id: "u1",
      display_name: "Ann",
      room_id: "r1",
      guest: true,
    });
  });

  test("only the matching type's handler runs", async () => {
    const { openRoomSocket } = await import("./socket");
    const socket = openRoomSocket("t1");
    const raw = instances[0];

    const connectedHandler = vi.fn();
    const playerJoinedHandler = vi.fn();
    socket.on("connected", connectedHandler);
    socket.on("player_joined", playerJoinedHandler);

    raw.fireMessage(JSON.stringify({ type: "player_joined", data: {} }));

    expect(playerJoinedHandler).toHaveBeenCalledTimes(1);
    expect(connectedHandler).not.toHaveBeenCalled();
  });

  test("an envelope with no registered handler does not throw", async () => {
    const { openRoomSocket } = await import("./socket");
    const socket = openRoomSocket("t1");
    const raw = instances[0];

    expect(() =>
      raw.fireMessage(JSON.stringify({ type: "round_opened", data: {} })),
    ).not.toThrow();
  });

  test("invalid JSON does not throw and runs no handler", async () => {
    const { openRoomSocket } = await import("./socket");
    const socket = openRoomSocket("t1");
    const raw = instances[0];

    const handler = vi.fn();
    socket.on("connected", handler);

    expect(() => raw.fireMessage("not json")).not.toThrow();
    expect(handler).not.toHaveBeenCalled();
  });

  test("an envelope with no type field does not throw and runs no handler", async () => {
    const { openRoomSocket } = await import("./socket");
    const socket = openRoomSocket("t1");
    const raw = instances[0];

    const handler = vi.fn();
    socket.on("connected", handler);

    expect(() =>
      raw.fireMessage(JSON.stringify({ data: { foo: "bar" } })),
    ).not.toThrow();
    expect(handler).not.toHaveBeenCalled();
  });

  test("two handlers registered for the same type both run", async () => {
    const { openRoomSocket } = await import("./socket");
    const socket = openRoomSocket("t1");
    const raw = instances[0];

    const first = vi.fn();
    const second = vi.fn();
    socket.on("connected", first);
    socket.on("connected", second);

    raw.fireMessage(JSON.stringify({ type: "connected", data: {} }));

    expect(first).toHaveBeenCalledTimes(1);
    expect(second).toHaveBeenCalledTimes(1);
  });

  test("the unsubscribe function returned by on() stops that handler", async () => {
    const { openRoomSocket } = await import("./socket");
    const socket = openRoomSocket("t1");
    const raw = instances[0];

    const handler = vi.fn();
    const unsubscribe = socket.on("connected", handler);
    unsubscribe();

    raw.fireMessage(JSON.stringify({ type: "connected", data: {} }));

    expect(handler).not.toHaveBeenCalled();
  });
});
