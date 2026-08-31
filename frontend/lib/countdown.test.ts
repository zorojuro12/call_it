import { act, render, screen } from "@testing-library/react";
import { createElement } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { remainingMs, useCountdown } from "./countdown";

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

function Probe({ lockAtMs }: { lockAtMs: number | null }) {
  const remaining = useCountdown(lockAtMs);
  return createElement("div", { "data-testid": "remaining" }, remaining);
}

describe("useCountdown", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("ticks down and stops at zero", () => {
    render(createElement(Probe, { lockAtMs: 1000 }));
    expect(screen.getByTestId("remaining").textContent).toBe("1000");

    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(screen.getByTestId("remaining").textContent).toBe("500");

    act(() => {
      vi.advanceTimersByTime(700);
    });
    expect(screen.getByTestId("remaining").textContent).toBe("0");

    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(screen.getByTestId("remaining").textContent).toBe("0");
  });

  it("reports zero and registers no interval when lockAtMs is null", () => {
    render(createElement(Probe, { lockAtMs: null }));
    expect(screen.getByTestId("remaining").textContent).toBe("0");

    act(() => {
      vi.advanceTimersByTime(5000);
    });
    expect(screen.getByTestId("remaining").textContent).toBe("0");
  });
});
