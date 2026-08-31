// Pure client-side counterpart of internal/domain — no I/O, no React.
// Holds every rule that could leak wager data or miscount money, driven
// entirely by the socket events Phase 4b already broadcasts. Wagers stay
// anonymous until round_resolved/round_refunded; the only in-round signal
// is the aggregate bettors count carried on OddsUpdatedEvent.
import type {
  ConnectedEvent,
  OddsUpdatedEvent,
  ResultRow,
  RoundLockedEvent,
  RoundOpenedEvent,
  RoundRefundedEvent,
  RoundResolvedEvent,
  WagerAcceptedEvent,
} from "./protocol";

export type Phase = "idle" | "open" | "locked" | "revealed";

export type RoundState = {
  phase: Phase;
  self_id: string | null;
  is_host: boolean;
  round_id: string | null;
  question: string | null;
  outcomes: string[];
  lock_at_ms: number | null;
  pools: number[];
  total: number;
  multipliers: number[];
  bettors: number;
  players: number;
  balance: number;
  balance_at_open: number | null;
  my_stake: number;
  results: ResultRow[] | null;
  dust: number;
  refunded: boolean;
  refund_total: number | null;
};

export type RoundAction =
  | { type: "connected"; data: ConnectedEvent }
  | { type: "round_opened"; data: RoundOpenedEvent }
  | { type: "odds_updated"; data: OddsUpdatedEvent }
  | { type: "round_locked"; data: RoundLockedEvent }
  | { type: "wager_accepted"; data: WagerAcceptedEvent }
  | { type: "round_resolved"; data: RoundResolvedEvent }
  | { type: "round_refunded"; data: RoundRefundedEvent };

export function initialRoundState(balance: number): RoundState {
  return {
    phase: "idle",
    self_id: null,
    is_host: false,
    round_id: null,
    question: null,
    outcomes: [],
    lock_at_ms: null,
    pools: [],
    total: 0,
    multipliers: [],
    bettors: 0,
    players: 0,
    balance,
    balance_at_open: null,
    my_stake: 0,
    results: null,
    dust: 0,
    refunded: false,
    refund_total: null,
  };
}

export function reduceRound(state: RoundState, action: RoundAction): RoundState {
  switch (action.type) {
    case "connected":
      return { ...state, self_id: action.data.user_id, is_host: action.data.host };

    default:
      return state;
  }
}
