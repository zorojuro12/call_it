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
  const statusHandlers = new Set<(s: SocketStatus) => void>();
  let status: SocketStatus = "connecting";
  let closed = false;

  function setStatus(next: SocketStatus) {
    status = next;
    for (const handler of statusHandlers) {
      handler(status);
    }
  }

  ws.onopen = () => setStatus("open");

  // There is no reconnect timer, no backoff, and no retry — deliberate
  // (Decisions §3, spec §4's known limitation): reconnecting would re-fire
  // the backend's EndSession cycle and silently reset the player to the
  // room buy-in, discarding their in-room result. 6a surfaces a closed
  // status and stops.
  ws.onclose = () => setStatus("closed");

  ws.onmessage = (event: MessageEvent<string>) => {
    if (closed) {
      return;
    }

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
    onStatus(handler) {
      statusHandlers.add(handler);
      handler(status);
      return () => {
        statusHandlers.delete(handler);
      };
    },
    send(type, data) {
      ws.send(JSON.stringify({ type, data }));
    },
    close() {
      if (closed) {
        return;
      }
      closed = true;
      ws.close();
    },
  };
}
