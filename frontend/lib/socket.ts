import { API_BASE_URL } from "./config";

export type SocketStatus = "connecting" | "open" | "closed";
export type Handler = (data: unknown) => void;

export interface RoomSocket {
  on(type: string, handler: Handler): () => void;
  onStatus(handler: (s: SocketStatus) => void): () => void;
  send(type: string, data: unknown): void;
  close(): void;
}

export function toWebSocketUrl(baseUrl: string, token: string): string {
  const url = new URL(baseUrl);
  const wsProtocol = url.protocol === "https:" ? "wss:" : "ws:";
  // URLSearchParams encodes space as "+" (form-urlencoded); encodeURIComponent
  // encodes it as %20, matching what the server's query-parameter parser
  // expects (internal/ws/handler.go:89 reads r.URL.Query().Get("token")).
  return `${wsProtocol}//${url.host}/api/v1/socket?token=${encodeURIComponent(token)}`;
}

export function openRoomSocket(token: string): RoomSocket {
  const ws = new WebSocket(toWebSocketUrl(API_BASE_URL, token));
  const handlers = new Map<string, Set<Handler>>();

  ws.onmessage = (event: MessageEvent<string>) => {
    let envelope: unknown;
    try {
      envelope = JSON.parse(event.data);
    } catch {
      return;
    }

    if (
      typeof envelope !== "object" ||
      envelope === null ||
      typeof (envelope as { type?: unknown }).type !== "string"
    ) {
      return;
    }

    const { type, data } = envelope as { type: string; data?: unknown };
    const set = handlers.get(type);
    if (!set) {
      return;
    }
    for (const handler of set) {
      try {
        handler(data);
      } catch (err) {
        // eslint-disable-next-line no-console
        console.error(`socket: handler for "${type}" threw`, err);
      }
    }
  };

  return {
    on(type, handler) {
      let set = handlers.get(type);
      if (!set) {
        set = new Set();
        handlers.set(type, set);
      }
      set.add(handler);
      return () => {
        set?.delete(handler);
      };
    },
    onStatus() {
      return () => {};
    },
    send(type, data) {
      ws.send(JSON.stringify({ type, data }));
    },
    close() {
      ws.close();
    },
  };
}
