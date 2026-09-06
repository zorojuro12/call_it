import { describe, expect, it } from "vitest";
import { initialRoundState, reduceRound } from "./roundState";
import type { RoundState } from "./roundState";

function lockedRoundWithStake(): RoundState {
  let state = reduceRound(initialRoundState(1000), {
    type: "round_opened",
    data: { round_id: "rd1", question: "Q?", outcomes: ["Home", "Away"], lock_at_ms: 1000 },
  });
  state = { ...state, self_id: "u1" };
  state = reduceRound(state, {
    type: "wager_accepted",
    data: { round_id: "rd1", outcome: 0, amount: 100, balance: 900 },
  });
  state = reduceRound(state, { type: "round_locked", data: { round_id: "rd1" } });
  return state;
}

describe("initialRoundState", () => {
  it("returns the idle baseline for a fresh room", () => {
    // Arrange & Act
    const state = initialRoundState(1000);

    // Assert
    expect(state).toEqual({
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
      balance: 1000,
      balance_at_open: null,
      my_stake: 0,
      results: null,
      dust: 0,
      refunded: false,
      refund_total: null,
      winning_outcome: null,
    });
  });
});

describe("reduceRound: connected", () => {
  it("establishes self identity and host status without touching other fields", () => {
    // Arrange
    const before = initialRoundState(1000);

    // Act
    const after = reduceRound(before, {
      type: "connected",
      data: { user_id: "u1", display_name: "Ann", room_id: "r1", guest: false, host: true, balance: 1000 },
    });

    // Assert
    expect(after).toEqual({ ...before, self_id: "u1", is_host: true, balance: 1000 });
    expect(after).not.toBe(before);
  });

  it("replaces a stale cached balance with the connected event's current one", () => {
    // Arrange: initialRoundState(1000) stands in for a client-cached
    // session_balance from the original join, now stale after a wager.
    const before = initialRoundState(1000);

    // Act
    const after = reduceRound(before, {
      type: "connected",
      data: { user_id: "u1", display_name: "Ann", room_id: "r1", guest: false, host: false, balance: 600 },
    });

    // Assert
    expect(after.balance).toBe(600);
  });
});

describe("reduceRound: round_opened", () => {
  it("starts a round, sizes pools per outcome, and snapshots the balance", () => {
    // Arrange
    const before = { ...initialRoundState(1000), self_id: "u1" };

    // Act
    const after = reduceRound(before, {
      type: "round_opened",
      data: { round_id: "rd1", question: "Next goal?", outcomes: ["Home", "Away"], lock_at_ms: 1735000000000 },
    });

    // Assert
    expect(after).toEqual({
      ...before,
      phase: "open",
      round_id: "rd1",
      question: "Next goal?",
      outcomes: ["Home", "Away"],
      lock_at_ms: 1735000000000,
      pools: [0, 0],
      total: 0,
      multipliers: [],
      bettors: 0,
      balance_at_open: 1000,
      my_stake: 0,
      results: null,
      dust: 0,
      refunded: false,
      refund_total: null,
      winning_outcome: null,
    });
  });

  it("clears a previous round's reveal when a new round opens", () => {
    // Arrange
    const before: RoundState = {
      ...initialRoundState(1000),
      phase: "revealed",
      round_id: "rd1",
      results: [{ user_id: "u1", display_name: "Ann", staked: 100, returned: 200, net: 100 }],
      refunded: true,
      winning_outcome: 0,
    };

    // Act
    const after = reduceRound(before, {
      type: "round_opened",
      data: { round_id: "rd2", question: "Next goal?", outcomes: ["Home", "Away"], lock_at_ms: 1735000000000 },
    });

    // Assert
    expect(after.phase).toBe("open");
    expect(after.results).toBeNull();
    expect(after.refunded).toBe(false);
    expect(after.round_id).toBe("rd2");
    expect(after.winning_outcome).toBeNull();
  });
});

describe("reduceRound: odds_updated", () => {
  it("records the six aggregate fields without touching balance or stake", () => {
    // Arrange
    const before = reduceRound(initialRoundState(1000), {
      type: "round_opened",
      data: { round_id: "rd1", question: "Q?", outcomes: ["Home", "Away"], lock_at_ms: 1000 },
    });

    // Act
    const after = reduceRound(before, {
      type: "odds_updated",
      data: { round_id: "rd1", pools: [300, 100], total: 400, multipliers: [1.333, 4], bettors: 2, players: 5 },
    });

    // Assert
    expect(after).toEqual({
      ...before,
      pools: [300, 100],
      total: 400,
      multipliers: [1.333, 4],
      bettors: 2,
      players: 5,
    });
    expect(after.phase).toBe("open");
  });

  it("drops a stale odds_updated for a round that is no longer current", () => {
    // Arrange
    const before = reduceRound(initialRoundState(1000), {
      type: "round_opened",
      data: { round_id: "rd1", question: "Q?", outcomes: ["Home", "Away"], lock_at_ms: 1000 },
    });

    // Act
    const after = reduceRound(before, {
      type: "odds_updated",
      data: { round_id: "rd0", pools: [1, 1], total: 2, multipliers: [1, 1], bettors: 1, players: 1 },
    });

    // Assert
    expect(after).toBe(before);
  });
});

describe("reduceRound: round_locked", () => {
  it("closes wagering without touching pools, totals, balance, or stake", () => {
    // Arrange
    let before = reduceRound(initialRoundState(1000), {
      type: "round_opened",
      data: { round_id: "rd1", question: "Q?", outcomes: ["Home", "Away"], lock_at_ms: 1000 },
    });
    before = reduceRound(before, {
      type: "odds_updated",
      data: { round_id: "rd1", pools: [300, 100], total: 400, multipliers: [1.333, 4], bettors: 2, players: 5 },
    });

    // Act
    const after = reduceRound(before, { type: "round_locked", data: { round_id: "rd1" } });

    // Assert
    expect(after).toEqual({ ...before, phase: "locked" });
  });

  it("drops a stale round_locked for a round that is no longer current", () => {
    // Arrange
    const before = reduceRound(initialRoundState(1000), {
      type: "round_opened",
      data: { round_id: "rd1", question: "Q?", outcomes: ["Home", "Away"], lock_at_ms: 1000 },
    });

    // Act
    const after = reduceRound(before, { type: "round_locked", data: { round_id: "rd0" } });

    // Assert
    expect(after).toBe(before);
  });
});

describe("reduceRound: wager_accepted", () => {
  it("applies the server's balance and accumulates the stake", () => {
    // Arrange
    const opened = reduceRound(initialRoundState(1000), {
      type: "round_opened",
      data: { round_id: "rd1", question: "Q?", outcomes: ["Home", "Away"], lock_at_ms: 1000 },
    });

    // Act
    const afterFirst = reduceRound(opened, {
      type: "wager_accepted",
      data: { round_id: "rd1", outcome: 0, amount: 100, balance: 900 },
    });
    const afterSecond = reduceRound(afterFirst, {
      type: "wager_accepted",
      data: { round_id: "rd1", outcome: 1, amount: 50, balance: 850 },
    });

    // Assert
    expect(afterFirst.balance).toBe(900);
    expect(afterFirst.my_stake).toBe(100);
    expect(afterFirst.balance_at_open).toBe(1000);
    expect(afterFirst.phase).toBe("open");

    expect(afterSecond.balance).toBe(850);
    expect(afterSecond.my_stake).toBe(150);
  });

  it("drops a stale wager_accepted for a round that is no longer current", () => {
    // Arrange
    const opened = reduceRound(initialRoundState(1000), {
      type: "round_opened",
      data: { round_id: "rd1", question: "Q?", outcomes: ["Home", "Away"], lock_at_ms: 1000 },
    });

    // Act
    const after = reduceRound(opened, {
      type: "wager_accepted",
      data: { round_id: "rd0", outcome: 0, amount: 100, balance: 900 },
    });

    // Assert
    expect(after).toBe(opened);
  });
});

describe("reduceRound: round_resolved", () => {
  it("reveals results and settles the winner's balance from their own net", () => {
    // Arrange
    const before = lockedRoundWithStake();
    const results = [
      { user_id: "u1", display_name: "Ann", staked: 100, returned: 250, net: 150 },
      { user_id: "u2", display_name: "Bob", staked: 100, returned: 0, net: -100 },
    ];

    // Act
    const after = reduceRound(before, {
      type: "round_resolved",
      data: { round_id: "rd1", winning_outcome: 0, results, dust: 3, refunded: false },
    });

    // Assert
    expect(after.phase).toBe("revealed");
    expect(after.results).toEqual(results);
    expect(after.dust).toBe(3);
    expect(after.refunded).toBe(false);
    expect(after.balance).toBe(1150);
    expect(after.winning_outcome).toBe(0);
  });

  it("settles a non-participant's balance at balance_at_open plus zero", () => {
    // Arrange
    const before = { ...lockedRoundWithStake(), self_id: "u9" };
    const results = [
      { user_id: "u1", display_name: "Ann", staked: 100, returned: 250, net: 150 },
      { user_id: "u2", display_name: "Bob", staked: 100, returned: 0, net: -100 },
    ];

    // Act
    const after = reduceRound(before, {
      type: "round_resolved",
      data: { round_id: "rd1", winning_outcome: 0, results, dust: 3, refunded: false },
    });

    // Assert
    expect(after.balance).toBe(1000);
  });

  it("settles every stake returned when nobody backed the winner", () => {
    // Arrange
    const before = lockedRoundWithStake();
    const results = [{ user_id: "u1", display_name: "Ann", staked: 100, returned: 100, net: 0 }];

    // Act
    const after = reduceRound(before, {
      type: "round_resolved",
      data: { round_id: "rd1", winning_outcome: 1, results, dust: 0, refunded: true },
    });

    // Assert
    expect(after.refunded).toBe(true);
    expect(after.phase).toBe("revealed");
    expect(after.balance).toBe(1000);
  });

  it("drops a stale round_resolved for a round that is no longer current", () => {
    // Arrange
    const before = lockedRoundWithStake();

    // Act
    const after = reduceRound(before, {
      type: "round_resolved",
      data: { round_id: "rd0", winning_outcome: 0, results: [], dust: 0, refunded: false },
    });

    // Assert
    expect(after).toBe(before);
  });
});

describe("reduceRound: round_refunded", () => {
  it("restores the pre-round balance, with no per-player rows", () => {
    // Arrange
    const before = lockedRoundWithStake();

    // Act
    const after = reduceRound(before, { type: "round_refunded", data: { round_id: "rd1", total: 400 } });

    // Assert
    expect(after.phase).toBe("revealed");
    expect(after.balance).toBe(1000);
    expect(after.refunded).toBe(true);
    expect(after.refund_total).toBe(400);
    expect(after.results).toBeNull();
    expect(after.winning_outcome).toBeNull();
  });

  it("drops a stale round_refunded for a round that is no longer current", () => {
    // Arrange
    const before = lockedRoundWithStake();

    // Act
    const after = reduceRound(before, { type: "round_refunded", data: { round_id: "rd0", total: 400 } });

    // Assert
    expect(after).toBe(before);
  });
});
