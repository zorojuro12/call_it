const ACCOUNT_TOKEN_KEY = "callit.account_token";
const ROOM_TOKEN_KEY = "callit.room_token";
const ROOM_SUMMARY_KEY = "callit.room_summary";

export type RoomSummary = {
  room_id: string;
  guest: boolean;
  session_balance: number;
  partial_buy_in: boolean;
};

function isRoomSummary(value: unknown): value is RoomSummary {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  const v = value as Record<string, unknown>;
  return (
    typeof v.room_id === "string" &&
    typeof v.guest === "boolean" &&
    typeof v.session_balance === "number" &&
    typeof v.partial_buy_in === "boolean"
  );
}

function readItem(key: string): string | null {
  try {
    return sessionStorage.getItem(key);
  } catch {
    return null;
  }
}

function writeItem(key: string, value: string): void {
  try {
    sessionStorage.setItem(key, value);
  } catch {
    // Quota/security errors are swallowed: a storage write must never crash
    // a page render.
  }
}

function removeItem(key: string): void {
  try {
    sessionStorage.removeItem(key);
  } catch {
    // Swallow: removal must never crash a page render.
  }
}

export function setAccountToken(token: string): void {
  writeItem(ACCOUNT_TOKEN_KEY, token);
}

export function getAccountToken(): string | null {
  return readItem(ACCOUNT_TOKEN_KEY);
}

export function setRoomToken(token: string): void {
  writeItem(ROOM_TOKEN_KEY, token);
}

export function getRoomToken(): string | null {
  return readItem(ROOM_TOKEN_KEY);
}

export function clearRoomToken(): void {
  removeItem(ROOM_TOKEN_KEY);
  removeItem(ROOM_SUMMARY_KEY);
}

export function clearSession(): void {
  removeItem(ACCOUNT_TOKEN_KEY);
  removeItem(ROOM_TOKEN_KEY);
  removeItem(ROOM_SUMMARY_KEY);
}

export function setRoomSummary(s: RoomSummary): void {
  writeItem(ROOM_SUMMARY_KEY, JSON.stringify(s));
}

export function getRoomSummary(): RoomSummary | null {
  const raw = readItem(ROOM_SUMMARY_KEY);
  if (raw === null) {
    return null;
  }
  try {
    const parsed: unknown = JSON.parse(raw);
    return isRoomSummary(parsed) ? parsed : null;
  } catch {
    return null;
  }
}
