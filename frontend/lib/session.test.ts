import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  clearRoomToken,
  clearSession,
  getAccountToken,
  getRoomSummary,
  getRoomToken,
  setAccountToken,
  setRoomSummary,
  setRoomToken,
} from "./session";

describe("session token storage", () => {
  beforeEach(() => {
    sessionStorage.clear();
  });

  it("returns null for the account token on empty storage", () => {
    // Arrange & Act
    const token = getAccountToken();

    // Assert
    expect(token).toBeNull();
  });

  it("round-trips an account token through storage", () => {
    // Arrange
    setAccountToken("acc1");

    // Act
    const token = getAccountToken();

    // Assert
    expect(token).toBe("acc1");
  });

  it("round-trips a room token through storage", () => {
    // Arrange
    setRoomToken("room1");

    // Act
    const token = getRoomToken();

    // Assert
    expect(token).toBe("room1");
  });

  it("stores the account token and room token under distinct keys", () => {
    // Arrange
    setAccountToken("acc1");
    setRoomToken("room1");

    // Act & Assert
    expect(getAccountToken()).toBe("acc1");
    expect(getRoomToken()).toBe("room1");
  });

  it("returns null instead of throwing when sessionStorage.getItem throws", () => {
    // Arrange
    const spy = vi
      .spyOn(Storage.prototype, "getItem")
      .mockImplementation(() => {
        throw new Error("SecurityError: access denied");
      });

    // Act
    const token = getAccountToken();

    // Assert
    expect(token).toBeNull();
    spy.mockRestore();
  });

  it("clears only the room token when leaving a room", () => {
    // Arrange
    setAccountToken("acc1");
    setRoomToken("room1");

    // Act
    clearRoomToken();

    // Assert
    expect(getRoomToken()).toBeNull();
    expect(getAccountToken()).toBe("acc1");
  });

  it("clears both tokens on logout", () => {
    // Arrange
    setAccountToken("acc1");
    setRoomToken("room1");

    // Act
    clearSession();

    // Assert
    expect(getAccountToken()).toBeNull();
    expect(getRoomToken()).toBeNull();
  });

  it("does not throw when clearing the room token on already-empty storage", () => {
    // Arrange & Act & Assert
    expect(() => clearRoomToken()).not.toThrow();
  });

  it("returns null for the room summary on empty storage", () => {
    // Arrange & Act
    const summary = getRoomSummary();

    // Assert
    expect(summary).toBeNull();
  });

  it("round-trips a room summary through storage as a number, not a string", () => {
    // Arrange
    setRoomSummary({
      room_id: "r1",
      guest: true,
      session_balance: 200,
      partial_buy_in: true,
    });

    // Act
    const summary = getRoomSummary();

    // Assert
    expect(summary).toEqual({
      room_id: "r1",
      guest: true,
      session_balance: 200,
      partial_buy_in: true,
    });
    expect(summary?.session_balance).toBe(200);
  });

  it("clears the room summary along with the room token", () => {
    // Arrange
    setAccountToken("acc1");
    setRoomToken("room1");
    setRoomSummary({
      room_id: "r1",
      guest: true,
      session_balance: 200,
      partial_buy_in: true,
    });

    // Act
    clearRoomToken();

    // Assert
    expect(getRoomSummary()).toBeNull();
    expect(getAccountToken()).toBe("acc1");
  });

  it("clears the room summary on logout", () => {
    // Arrange
    setRoomSummary({
      room_id: "r1",
      guest: true,
      session_balance: 200,
      partial_buy_in: true,
    });

    // Act
    clearSession();

    // Assert
    expect(getRoomSummary()).toBeNull();
  });

  it("returns null for a corrupt stored room summary instead of throwing", () => {
    // Arrange
    sessionStorage.setItem("callit.room_summary", "not json");

    // Act
    const summary = getRoomSummary();

    // Assert
    expect(summary).toBeNull();
  });
});
