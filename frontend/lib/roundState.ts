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
  winning_outcome: number | null;
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
    winning_outcome: null,
  };
}

// isStale reports whether action targets a round other than the one
// state is currently tracking. A late broadcast from a previous round
// must be dropped rather than corrupting the current one. round_opened
// is exempt by construction — a new round always supersedes.
function isStale(state: RoundState, roundID: string): boolean {
  return state.round_id !== null && roundID !== state.round_id;
}

export function reduceRound(state: RoundState, action: RoundAction): RoundState {
  switch (action.type) {
    case "connected":
      return { ...state, self_id: action.data.user_id, is_host: action.data.host };

    case "round_opened":
      return {
        ...state,
        phase: "open",
        round_id: action.data.round_id,
        question: action.data.question,
        outcomes: action.data.outcomes,
        lock_at_ms: action.data.lock_at_ms,
        pools: new Array(action.data.outcomes.length).fill(0),
        total: 0,
        multipliers: [],
        bettors: 0,
        balance_at_open: state.balance,
        my_stake: 0,
        results: null,
        dust: 0,
        refunded: false,
        refund_total: null,
        winning_outcome: null,
      };

    case "odds_updated":
      if (isStale(state, action.data.round_id)) {
        return state;
      }
      return {
        ...state,
        pools: action.data.pools,
        total: action.data.total,
        multipliers: action.data.multipliers,
        bettors: action.data.bettors,
        players: action.data.players,
      };

    case "round_locked":
      if (isStale(state, action.data.round_id)) {
        return state;
      }
      return { ...state, phase: "locked" };

    case "wager_accepted":
      if (isStale(state, action.data.round_id)) {
        return state;
      }
      return { ...state, balance: action.data.balance, my_stake: state.my_stake + action.data.amount };

    case "round_resolved": {
      if (isStale(state, action.data.round_id)) {
        return state;
      }
      const myRow = action.data.results.find((r) => r.user_id === state.self_id);
      const net = myRow?.net ?? 0;
      return {
        ...state,
        phase: "revealed",
        results: action.data.results,
        dust: action.data.dust,
        refunded: action.data.refunded,
        balance: (state.balance_at_open ?? state.balance) + net,
        winning_outcome: action.data.winning_outcome,
      };
    }

    case "round_refunded":
      if (isStale(state, action.data.round_id)) {
        return state;
      }
      return {
        ...state,
        phase: "revealed",
        balance: state.balance_at_open ?? state.balance,
        refunded: true,
        refund_total: action.data.total,
        results: null,
        winning_outcome: null,
      };
  }
}
