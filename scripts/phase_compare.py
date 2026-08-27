#!/usr/bin/env python3
"""Measure a phase's token cost from Claude Code session logs.

Referenced by journal/ entries since 2026-08-26 but never actually committed —
the methodology kept being re-derived from prose. This is it, in code.

Cost model (validated against Phase 4b: 277M predicted vs 281M measured):

    cost ~= sum over turns of (context size at that turn)

Every API turn resends the whole conversation. Cache reads are discounted but
still count against quota, and they are ~99.5% of raw volume — so the two
levers are turn count and context size per turn.

Two rules this script exists to enforce, both learned the hard way:

1. BOUND BOTH ENDS. A session is not a phase. Phase 4a was first reported at
   2.53M/CP using a start marker only; the session continued past the phase
   into a security review, so re-running later returned 74.5M instead of 63.2M
   for "the same" phase. Bound from the /executing-plans invocation to the
   phase's own journal commit.
2. INCLUDE SUBAGENTS. <session>/subagents/*.jsonl carry real cost (3.4M for
   Phase 5a's security-reviewer alone) and were being dropped entirely.

Usage:
    phase_compare.py --session <uuid> --start <iso-ts> --end <iso-ts> --cps N
    phase_compare.py --session <uuid> --list-prompts    # find your bounds
"""
import argparse, glob, json, os, sys

BASE = os.path.expanduser("~/.claude/projects/-home-chikara-projects-call-it")
FIELDS = ("input_tokens", "cache_creation_input_tokens",
          "cache_read_input_tokens", "output_tokens")

# Measured baselines, corrected (see journal/2026-08-26_0250_*tuned-plan-experiment-verdict.md)
BASELINES = {"Phase 2": 4.61, "Phase 3": 10.35, "Phase 4a": 2.49,
             "Phase 4b": 9.08, "Phase 5a": 5.77}


def tally(path, lo=None, hi=None, sidechain=None):
    """Sum usage over a log. Uses top-level usage only — never `iterations`,
    which would double-count."""
    agg = dict.fromkeys(FIELDS, 0)
    ctx, turns = [], 0
    for line in open(path):
        try:
            d = json.loads(line)
        except ValueError:
            continue
        ts = d.get("timestamp")
        if lo and (not ts or ts < lo or ts > hi):
            continue
        if sidechain is not None and bool(d.get("isSidechain", False)) != sidechain:
            continue
        u = (d.get("message") or {}).get("usage")
        if not isinstance(u, dict):
            continue
        turns += 1
        for f in FIELDS:
            agg[f] += u.get(f, 0) or 0
        ctx.append(sum(u.get(f, 0) or 0 for f in FIELDS if f != "output_tokens"))
    return agg, turns, ctx


def list_prompts(session):
    """Print user prompts and skill invocations so bounds can be picked."""
    for i, line in enumerate(open(f"{BASE}/{session}.jsonl"), 1):
        try:
            d = json.loads(line)
        except ValueError:
            continue
        if d.get("isSidechain"):
            continue
        m = d.get("message") or {}
        c = m.get("content")
        if d.get("type") == "user" and isinstance(c, str):
            print(i, d.get("timestamp"), "|", c[:120].replace("\n", " "))
        elif isinstance(c, list):
            for b in c:
                if b.get("type") == "tool_use" and b.get("name") in ("Skill", "Bash"):
                    inp = json.dumps(b.get("input", {}))[:120].replace("\n", " ")
                    if "executing-plans" in inp or "git commit" in inp:
                        print(i, d.get("timestamp"), "|", b["name"], inp)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--session", required=True)
    ap.add_argument("--start"); ap.add_argument("--end")
    ap.add_argument("--cps", type=int); ap.add_argument("--label", default="this phase")
    ap.add_argument("--list-prompts", action="store_true")
    a = ap.parse_args()

    if a.list_prompts:
        list_prompts(a.session); return
    if not (a.start and a.end and a.cps):
        sys.exit("--start, --end and --cps are required (use --list-prompts to find bounds)")

    log = f"{BASE}/{a.session}.jsonl"
    main_a, mt, mctx = tally(log, a.start, a.end, sidechain=False)
    full_a, ft, _ = tally(log, sidechain=False)

    subs = dict.fromkeys(FIELDS, 0); sub_t = 0; sub_files = []
    for f in sorted(glob.glob(f"{BASE}/{a.session}/subagents/*.jsonl")):
        s, t, _ = tally(f)
        sub_files.append((os.path.basename(f), sum(s.values()), t))
        for k in FIELDS:
            subs[k] += s[k]
        sub_t += t

    tot = lambda d: sum(d.values())
    grand = tot(main_a) + tot(subs)
    avg_ctx = sum(mctx) // len(mctx) if mctx else 0

    print(f"window     : {a.start} -> {a.end}")
    print(f"main turns : {mt:>6}   {tot(main_a):>14,} tok   avg ctx {avg_ctx:,}")
    print(f"subagents  : {sub_t:>6}   {tot(subs):>14,} tok")
    print(f"TOTAL      : {mt+sub_t:>6}   {grand:>14,} tok")
    print(f"\ncheckpoints: {a.cps}")
    print(f"tok/CP     : {grand/a.cps/1e6:.2f}M")
    print(f"turns/CP   : {mt/a.cps:.1f}")
    print(f"cache_read : {100*main_a['cache_read_input_tokens']/tot(main_a):.1f}% of main volume")

    dropped = tot(full_a) - tot(main_a)
    print(f"\nbounding removed {dropped/1e6:.1f}M "
          f"({100*dropped/tot(full_a):.0f}% of the session) — unbounded would read "
          f"{(tot(full_a)+tot(subs))/a.cps/1e6:.2f}M/CP")

    for n, v, t in sub_files:
        if t:
            print(f"\nsubagent {n}: {v:,} tok / {t} turns (avg ctx {v//t:,})")
            print(f"  inline counterfactual: {t*avg_ctx:,} tok -> {t*avg_ctx/v:.1f}x saving")

    print("\nmeasured baselines (tok/CP):")
    for k, v in BASELINES.items():
        print(f"  {k:9} {v:>5.2f}M" + ("   <- control for infrastructure phases" if k == "Phase 2" else ""))


if __name__ == "__main__":
    main()
