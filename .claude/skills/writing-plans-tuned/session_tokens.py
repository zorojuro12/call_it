#!/usr/bin/env python3
"""Summarize token usage per Claude Code session for this project.

Reads the local JSONL transcripts and reports, per session: total tokens
by category, assistant turn count, wall-clock span, and the opening user
prompt (to identify which session was which phase).

Usage:
    python3 session_tokens.py                 # all sessions, newest first
    python3 session_tokens.py --since 2026-08-24
"""
import argparse
import glob
import json
import os
from datetime import datetime, timezone

TRANSCRIPTS = os.path.expanduser(
    "~/.claude/projects/-home-chikara-projects-call-it/*.jsonl"
)


def parse_ts(raw):
    if not raw:
        return None
    try:
        return datetime.fromisoformat(raw.replace("Z", "+00:00"))
    except ValueError:
        return None


def summarize(path):
    s = {
        "file": os.path.basename(path)[:8],
        "in": 0, "out": 0, "cache_read": 0, "cache_create": 0,
        "turns": 0, "tool_calls": 0,
        "first": None, "last": None, "prompt": "",
    }
    with open(path, errors="replace") as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                d = json.loads(line)
            except json.JSONDecodeError:
                continue

            ts = parse_ts(d.get("timestamp"))
            if ts:
                s["first"] = ts if s["first"] is None else min(s["first"], ts)
                s["last"] = ts if s["last"] is None else max(s["last"], ts)

            msg = d.get("message")
            if not isinstance(msg, dict):
                continue

            # First real user prompt identifies the session.
            if d.get("type") == "user" and not s["prompt"]:
                content = msg.get("content")
                text = ""
                if isinstance(content, str):
                    text = content
                elif isinstance(content, list):
                    for block in content:
                        if isinstance(block, dict) and block.get("type") == "text":
                            text = block.get("text", "")
                            break
                text = " ".join(text.split())
                # Skip harness-injected reminders and command wrappers.
                if text and not text.startswith(("<system-reminder", "<local-command", "<command-")):
                    s["prompt"] = text[:90]

            usage = msg.get("usage")
            if isinstance(usage, dict):
                s["turns"] += 1
                s["in"] += usage.get("input_tokens", 0) or 0
                s["out"] += usage.get("output_tokens", 0) or 0
                s["cache_read"] += usage.get("cache_read_input_tokens", 0) or 0
                s["cache_create"] += usage.get("cache_creation_input_tokens", 0) or 0

            content = msg.get("content")
            if isinstance(content, list):
                s["tool_calls"] += sum(
                    1 for b in content
                    if isinstance(b, dict) and b.get("type") == "tool_use"
                )
    return s


def fmt(n):
    if n >= 1_000_000:
        return f"{n/1_000_000:.1f}M"
    if n >= 1_000:
        return f"{n/1_000:.0f}k"
    return str(n)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--since", help="only sessions starting on/after YYYY-MM-DD")
    args = ap.parse_args()

    cutoff = None
    if args.since:
        cutoff = datetime.fromisoformat(args.since).replace(tzinfo=timezone.utc)

    rows = []
    for path in glob.glob(TRANSCRIPTS):
        s = summarize(path)
        if s["turns"] == 0 or s["first"] is None:
            continue
        if cutoff and s["first"] < cutoff:
            continue
        rows.append(s)

    rows.sort(key=lambda r: r["first"], reverse=True)

    hdr = (f"{'id':<9}{'start':<17}{'mins':>5}{'turns':>7}{'tools':>7}"
           f"{'in':>8}{'out':>8}{'cwrite':>8}{'cread':>8}{'TOTAL':>9}  prompt")
    print(hdr)
    print("-" * len(hdr))
    for r in rows:
        mins = (r["last"] - r["first"]).total_seconds() / 60
        total = r["in"] + r["out"] + r["cache_create"] + r["cache_read"]
        print(
            f"{r['file']:<9}"
            f"{r['first'].astimezone().strftime('%m-%d %H:%M'):<17}"
            f"{mins:>5.0f}{r['turns']:>7}{r['tool_calls']:>7}"
            f"{fmt(r['in']):>8}{fmt(r['out']):>8}"
            f"{fmt(r['cache_create']):>8}{fmt(r['cache_read']):>8}"
            f"{fmt(total):>9}  {r['prompt']}"
        )

    print()
    print("TOTAL = in + out + cache_create + cache_read (raw volume, not")
    print("billed cost — cache reads bill at a large discount).")
    print("Normalize by checkpoints or commits before comparing phases.")


if __name__ == "__main__":
    main()
