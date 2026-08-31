import { describe, expect, it } from "vitest";
import { remainingMs } from "./countdown";

describe("remainingMs", () => {
  it.each([
    [1000, 400, 600],
    [1000, 1000, 0],
    [1000, 1500, 0],
    [1000, 0, 1000],
  ])("remainingMs(%i, %i) -> %i", (lockAtMs, nowMs, expected) => {
    expect(remainingMs(lockAtMs, nowMs)).toBe(expected);
  });
});
