# writing-plans-tuned — baseline, prediction, and verdict procedure

Reference doc for the experiment. **Not loaded with the skill body** — read
it only when setting up or deciding the experiment.

Measured 2026-08-26 from the local Claude Code transcripts at
`~/.claude/projects/-home-chikara-projects-call-it/*.jsonl`, which carry
per-message `input_tokens`, `output_tokens`, `cache_creation_input_tokens`,
and `cache_read_input_tokens`. Reproduce with `session_tokens.py` in this
directory.

## Baselines (execution sessions only, normalized per checkpoint)

| session | plan lines | CPs | tok/CP | turns/CP | tools/CP | commits/CP | plan ln/CP |
|---|---|---|---|---|---|---|---|
| Phase 2 exec (`7f3db1c0`) | 1,599 | 25 | **5.17M** | **22.1** | **12.0** | 1.12 | 64 |
| Phase 3 exec (`3d03b560`) | 2,904 | 38 | 12.67M | 30.4 | 15.9 | 1.29 | 76 |
| Phase 4a exec | 899 | 25 | ? | ? | ? | ? | 36 |
| Phase 4b exec | 1,150 | 31 | ? | ? | ? | ? | 37 |

**Session identification is verified, not inferred.** `7f3db1c0` opens with
*"okay we have a written plan for phase 2 down in the plan folder. can we
start working on executing the plan /executing-plans"*; `3d03b560` with
*"/executing-plans … we can go ahead with the phase 3 execution"*.

**Figures are execution-only.** Neither session was purely execution — both
opened with `/journal` and discussion, and Phase 2's also merged and pushed
Phase 1. Tokens are summed from the `/executing-plans` invocation forward
(`2026-08-24T08:36:19` and `2026-08-25T08:58:52`). The preamble was 1.0M of
130.4M and 1.4M of 482.8M — 0.8% and 0.3%, negligible, but measure the same
way for 4a/4b so the comparison stays like-for-like. Note also that
`7f3db1c0`'s 22-hour wall-clock span is idle time, not work; duration is not
a usable metric here.

`tok` = `input + output + cache_creation + cache_read` (raw volume, not
billed cost — cache reads bill at a large discount, but they still track
quota consumption and are ~99.5% of the total).

**Use Phase 2 as the control.** It has the same checkpoint count as 4a
(25), ran inline in a fresh window on Sonnet, and used the same
spec-driven plan format the tuned skill is a delta over. Phase 3 varies
size, subject, and format simultaneously and is a weak comparator.

## Two findings that reshaped the experiment

1. **`cache_read` is 99.5% of token volume.** Cost is `turns × context
   size per turn`, and both terms are in play — Phase 3 spent 2.4× Phase
   2's tokens per checkpoint from *both* more turns/CP (31 vs 22.8) and a
   larger context. Override B (scoped test output) and the split (cold
   sessions) attack the second term; they are not secondary.

2. **Real execution runs 12–16 tool calls per checkpoint, not 5.** The
   plan's nominal steps are ~35% of actual tool calls; the rest is file
   reads, diagnosis, formatting, and retries. Override A therefore moves
   ~8% of tool calls, not the ~20% first estimated. **The larger lever is
   plan precision** — exact file paths and exact contracts remove
   exploratory reads, which are the majority of the untracked 65%.

## Pre-registered prediction

Recorded **before** 4a executed. If the tuned skill works, against the
Phase 2 control:

| metric | control | predicted | reasoning |
|---|---|---|---|
| checkpoints un-batched | n/a | **0** | Override A holds or it doesn't |
| commits with a failing test | n/a | **0** | the `&&` chain makes it unreachable |
| commits/CP | 1.12 | **≤1.10** | template preserves 1 CP = 1 commit |
| turns/CP | 22.1 | **<20** | one fewer call/CP, plus fewer exploratory reads |
| tok/CP | 5.17M | **<4.0M** | fewer turns × smaller base context (36 vs 64 ln/CP) |

**A result that fails these is a real result.** If tok/CP lands near or
above 5.2M, the overrides did not pay for themselves and the skill should
be deleted, not defended.

## Confounders — state these alongside any verdict

- **Biases toward the tuned skill:** 4a's Tasks 1–6 need no Redis, so they
  avoid Docker round trips and integration flakiness that Phase 2 carried
  throughout. The codebase is also more mature, so fewer surprises.
- **Biases against:** `internal/ws` is concurrency and timing code, where
  flaky tests and `-race` failures cost extra diagnostic turns that
  Phase 2's Lua work largely avoided.
- **4b is the fairer test of the two** — it is Redis-heavy throughout,
  like Phase 2. Prefer the two-phase result over either alone.

## Verdict procedure

1. After 4a merges, run `python3 session_tokens.py --since <exec date>` and
   identify the execution session by its opening prompt.
2. Fill the 4a row. Count un-batched checkpoints from the plan's checkbox
   history and `git log` (a checkpoint that produced a commit whose tests
   were run separately shows as an extra tool call sequence).
3. Verify "commits with a failing test" retroactively:
   `git rebase --exec 'make test' <base>..<head>` on a scratch branch.
4. Repeat for 4b.
5. **If both phases beat the prediction:** fold Overrides A–C into
   `.claude/skills/writing-plans/SKILL.md` and the portable copy at
   `~/projects/claude-skills/writing-plans/`, then delete this skill.
6. **If they don't:** delete the skill and record why in
   `docs/dev-workflow-guide.md` §9 alongside the other declined tooling.
