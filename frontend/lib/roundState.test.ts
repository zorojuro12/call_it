import { describe, expect, it } from "vitest";
import { initialRoundState, reduceRound } from "./roundState";

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
