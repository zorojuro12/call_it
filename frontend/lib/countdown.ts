// Pure remaining-time math for the lockout countdown, plus the ticking hook
// that drives it in the UI. This is display-only: round_locked from the
// server is what actually closes wagering. lock_at_ms is absolute server
// wall-clock, so a skewed browser clock makes this cosmetic value wrong
// without affecting correctness.

export function remainingMs(lockAtMs: number, nowMs: number): number {
  return Math.max(0, lockAtMs - nowMs);
}
