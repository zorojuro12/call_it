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
  host: boolean;
  balance: number;
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

export type RoundOpenedEvent = {
  round_id: string;
  question: string;
  outcomes: string[];
  lock_at_ms: number;
};

export type OddsUpdatedEvent = {
  round_id: string;
  pools: number[];
  total: number;
  multipliers: number[];
  bettors: number;
  players: number;
};

export type RoundLockedEvent = {
  round_id: string;
};

export type ResultRow = {
  user_id: string;
  display_name: string;
  staked: number;
  returned: number;
  net: number;
};

export type RoundResolvedEvent = {
  round_id: string;
  winning_outcome: number;
  results: ResultRow[];
  dust: number;
  refunded: boolean;
};

export type RoundRefundedEvent = {
  round_id: string;
  total: number;
};

export type WagerAcceptedEvent = {
  round_id: string;
  outcome: number;
  amount: number;
  balance: number;
};

export type Envelope = {
  type: string;
  data?: unknown;
};
