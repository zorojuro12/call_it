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

  return {
    on() {
      return () => {};
    },
    onStatus() {
      return () => {};
    },
    send() {},
    close() {
      ws.close();
    },
  };
}
