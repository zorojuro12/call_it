// Types mirroring Go wire structs — no logic.
// Every numeric field is an integer token count or a count of players,
// never a fraction. Odds/wager types arrive in Phase 6b.

export type AccountResponse = {
  id: string;
  email: string;
  display_name: string;
  balance: number;
};

export type AuthResponse = {
  account: AccountResponse;
  token: string;
};

export type CreateRoomResponse = {
  room_id: string;
  code: string;
  buy_in: number;
  token: string;
};

export type JoinRoomResponse = {
  room_id: string;
  guest: boolean;
  session_balance: number;
  partial_buy_in: boolean;
  token: string;
};

export type RefillResponse = {
  credited: number;
  balance: number;
  remaining: number;
  reset_at: string;
};

export type ConnectedEvent = {
  user_id: string;
  display_name: string;
  room_id: string;
  guest: boolean;
};

export type PresenceEvent = {
  user_id: string;
  display_name: string;
  player_count: number;
};

export type SocketErrorEvent = {
  code: string;
  message: string;
};

export type Envelope = {
  type: string;
  data?: unknown;
};
