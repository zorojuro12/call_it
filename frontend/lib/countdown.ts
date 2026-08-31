// Pure remaining-time math for the lockout countdown, plus the ticking hook
// that drives it in the UI. This is display-only: round_locked from the
// server is what actually closes wagering. lock_at_ms is absolute server
// wall-clock, so a skewed browser clock makes this cosmetic value wrong
// without affecting correctness.
import { useCallback, useSyncExternalStore } from "react";

const TICK_MS = 100;

export function remainingMs(lockAtMs: number, nowMs: number): number {
  return Math.max(0, lockAtMs - nowMs);
}

// useSyncExternalStore is the React-sanctioned way to read a value that
// changes outside React's knowledge (the wall clock) without violating the
// render-purity rule that a plain `Date.now()` read during render would
// trip. subscribe/getSnapshot both run outside the render body.
export function useCountdown(lockAtMs: number | null): number {
  const subscribe = useCallback(
    (onStoreChange: () => void) => {
      if (lockAtMs === null) {
        return () => {};
      }

      const interval = setInterval(() => {
        onStoreChange();
        if (remainingMs(lockAtMs, Date.now()) === 0) {
          clearInterval(interval);
        }
      }, TICK_MS);

      return () => clearInterval(interval);
    },
    [lockAtMs],
  );

  const getSnapshot = useCallback(
    () => (lockAtMs === null ? 0 : remainingMs(lockAtMs, Date.now())),
    [lockAtMs],
  );

  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}
