import { describe, expect, it } from "vitest";
import { initialRoundState, reduceRound } from "./roundState";
import type { RoundState } from "./roundState";

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
      data: { user_id: "u1", display_name: "Ann", room_id: "r1", guest: false, host: true },
    });

    // Assert
    expect(after).toEqual({ ...before, self_id: "u1", is_host: true });
    expect(after).not.toBe(before);
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
