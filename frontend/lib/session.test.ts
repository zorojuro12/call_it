import { beforeEach, describe, expect, it, vi } from "vitest";
import { getAccountToken, getRoomToken, setAccountToken, setRoomToken } from "./session";

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
});
