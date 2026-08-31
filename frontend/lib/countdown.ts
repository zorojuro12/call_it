// Pure remaining-time math for the lockout countdown, plus the ticking hook
// that drives it in the UI. This is display-only: round_locked from the
// server is what actually closes wagering. lock_at_ms is absolute server
// wall-clock, so a skewed browser clock makes this cosmetic value wrong
// without affecting correctness.
import { useEffect, useState } from "react";

const TICK_MS = 100;

export function remainingMs(lockAtMs: number, nowMs: number): number {
  return Math.max(0, lockAtMs - nowMs);
}

export function useCountdown(lockAtMs: number | null): number {
  const [remaining, setRemaining] = useState(() =>
    lockAtMs === null ? 0 : remainingMs(lockAtMs, Date.now()),
  );

  useEffect(() => {
    if (lockAtMs === null) {
      setRemaining(0);
      return;
    }

    setRemaining(remainingMs(lockAtMs, Date.now()));

    const interval = setInterval(() => {
      const next = remainingMs(lockAtMs, Date.now());
      setRemaining(next);
      if (next === 0) {
        clearInterval(interval);
      }
    }, TICK_MS);

    return () => clearInterval(interval);
  }, [lockAtMs]);

  return remaining;
}
