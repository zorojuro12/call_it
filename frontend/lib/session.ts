const ACCOUNT_TOKEN_KEY = "callit.account_token";
const ROOM_TOKEN_KEY = "callit.room_token";

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
