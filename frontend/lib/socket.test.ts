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
