// Pure remaining-time math for the lockout countdown, plus the ticking hook
// that drives it in the UI. This is display-only: round_locked from the
// server is what actually closes wagering. lock_at_ms is absolute server
// wall-clock, so a skewed browser clock makes this cosmetic value wrong
// without affecting correctness.
import { useCallback, useRef, useSyncExternalStore } from "react";

const TICK_MS = 100;

export function remainingMs(lockAtMs: number, nowMs: number): number {
  return Math.max(0, lockAtMs - nowMs);
}

// useSyncExternalStore is the React-sanctioned way to read a value that
// changes outside React's knowledge (the wall clock) without violating the
// render-purity rule that a plain `Date.now()` read during render would
// trip. getSnapshot must return a cached, call-stable value — it must
// NOT read Date.now() itself. React calls getSnapshot more than once per
// commit to detect "tearing" (a store that changed mid-render); a
// getSnapshot that recomputes from the live clock returns a different
// answer on each of those calls, which React reads as "still tearing" and
// re-renders forever ("Maximum update depth exceeded"). So the clock is
// read only inside subscribe's interval tick, cached in a ref, and
// getSnapshot just returns that cached value.
export function useCountdown(lockAtMs: number | null): number {
  const cacheRef = useRef(0);

  const subscribe = useCallback(
    (onStoreChange: () => void) => {
      if (lockAtMs === null) {
        cacheRef.current = 0;
        onStoreChange();
        return () => {};
      }

      cacheRef.current = remainingMs(lockAtMs, Date.now());
      onStoreChange();

      const interval = setInterval(() => {
        cacheRef.current = remainingMs(lockAtMs, Date.now());
        onStoreChange();
        if (cacheRef.current === 0) {
          clearInterval(interval);
        }
      }, TICK_MS);

      return () => clearInterval(interval);
    },
    [lockAtMs],
  );

  const getSnapshot = useCallback(() => cacheRef.current, []);

  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}
