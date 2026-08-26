# 2026-08-26 — ansh — Phase 4 split into 4a/4b, and a token-budget planning experiment

**Status:** Phase 4 is split into two phases, both planned and committed to `dev` — `docs/plans/2026-08-26-phase-4a-ws-transport.md` (899 lines, 25 checkpoints) and `docs/plans/2026-08-26-phase-4b-round-lifecycle.md` (1,150 lines, 31 checkpoints). Neither has been executed; no Go code was written this session. A new experimental skill, `writing-plans-tuned`, was written and used for both.
**Decided:** The binding constraint on this project's workflow is **token quota per 5-hour window**, not wall-clock or context. Phase 3's plan-plus-implementation exhausted a full boosted window. Cumulative burn ≈ `round trips × context size per round trip`, and the *plan format* controls both — so the fix belongs in `writing-plans`, not in a subagent orchestration layer.
**Spec:** No change this session. Both plans carry amendments that will edit the spec and parent plan **at execution close-out**, not before (4a Amendment C1; 4b Amendments D1–D4).
**Next:** Execute Phase 4a in a **fresh window** pointed at `docs/plans/2026-08-26-phase-4a-ws-transport.md`. Before starting 4b later, run its "⚠ Revision required before execution" section — it consumes three `internal/ws` interfaces that 4a has not built yet.
**Blocked on:** Nothing.
**Touches:** `.claude/skills/writing-plans-tuned/SKILL.md` (new), `docs/plans/2026-08-26-phase-4a-ws-transport.md` (new), `docs/plans/2026-08-26-phase-4b-round-lifecycle.md` (new)

---

## What We Worked On

Started as "read the journal and tell me the next steps", turned into a workflow correction plus two phase plans.

The pivot was the user naming the actual constraint: **Phase 3 consumed an entire 5-hour token window** (plan + implementation), at boosted limits. Every earlier framing in the conversation — plan length, wall-clock, context exhaustion — was a proxy for that and partly wrong.

## Decisions Made

- **Split Phase 4 into 4a (transport) and 4b (round lifecycle).** Scoping the parent plan's single Phase 4 row produced ~13 tasks / ~38 checkpoints — the shape §9's own Phase-sizing note warns about. Recorded as 4a Amendment C1.
- **A second justification for the split, beyond plan length: 4a is not money code.** A transport bug drops a connection; a 4b bug loses tokens and breaks the 0.00% double-spend claim. Kept in one phase, both must run at money-code rigor. This is the argument that actually decided it — plan length alone would have been weaker.
- **Wrote `writing-plans-tuned` as a *delta* skill, not a fork.** 137 lines that override three things in `writing-plans` and explicitly list what they do **not** change. A full 300-line copy would have made the A/B unreadable and the merge-back manual.
- **Declined subagent-driven development**, even though `docs/dev-workflow-guide.md:215`'s revisit trigger ("a phase's implementation genuinely exhausts context before completing") has arguably now fired. It addresses *context* exhaustion; it makes *quota* worse — 2 to 12 subagent dispatches per task, each re-reading `CLAUDE.md` cold. The guide's own cheaper substitute (fresh session per phase) is the right tool, applied at a finer grain than phases were previously sized for.
- **Round control travels over the WebSocket, not REST** (4b Amendment D1) — latency target, identity already verified on the socket, and every such action produces a broadcast. Room create/join stay REST; they happen before a socket exists.

## What Worked

- **The tuned checkpoint template held across both plans with zero exceptions.** 4a: 25 checkpoints / 50 steps. 4b: 31 / 62. Exactly 2 steps per checkpoint, no checkpoint needed the old 5-step form at authoring time. Whether it survives *execution* is the open question.
- **Plan density improved substantially** without dropping the precision bar (every checkpoint still names exact inputs → exact outputs or error sentinels):

| | Phase 2 | Phase 3 | 4a | 4b |
|---|---|---|---|---|
| Plan lines | 1,599 | 2,904 | 899 | 1,150 |
| Checkpoints | 25 | 38 | 25 | 31 |
| Lines/checkpoint | ~64 | ~76 | **~36** | **~37** |

- **Combined, the split produced more checkpoints but fewer lines** than one Phase 4 would have: 56 checkpoints / 2,049 lines vs. an estimated 38 / ~2,900. Decomposition got *more* honest once each half had room, while the template cut the per-checkpoint cost roughly in half.
- Verified `gorilla/websocket` v1.5.3 declares `go 1.12` — safe against the Go 1.22.10 pin, unlike go-redis and `x/crypto`. Recorded in 4a's Global Constraints so the executor does not re-derive it.
- Measured the test-command lever concretely: warm single-package `go test -race` is ~1.5–2s; a cold multi-package build is ~16s. Both plans mandate package-scoped commands inside checkpoints and the full suite only at task boundaries.

## What Didn't Work

- **Three rounds of `AskUserQuestion` were all rejected before the real constraint surfaced.** Each was well-formed but optimized the wrong variable — first plan length, then wall-clock, then session sequencing. The user's actual concern (token quota) only appeared when they stated it directly. Lesson for a future session: when a workflow question keeps getting bounced, stop reformulating options and ask what resource is actually scarce.
- **I over-claimed "3–5× fewer tokens" from turn batching and had to retract it mid-session.** The error: counting the 5 plan steps as 5 API round trips. A round trip is per *tool call*, and `Write`→`Bash` are dependent, so they cannot share one. Honest figure is 5 → 4 calls per checkpoint (~20%), and the larger saving comes from **context size per turn** — the split, the shorter plan, and scoped test output. Same conclusion, different dominant term. Do not re-derive the optimistic version.

## Test Coverage

- **Covered:** nothing new — no code was written this session. Existing coverage is unchanged from Phase 3 close-out.
- **Not covered yet:** all of `internal/ws`, `internal/round`, `internal/wager`. Both plans set an 80% floor and route coverage checks through `-coverpkg=./...` rather than the per-package figure, per `CLAUDE.md`'s note on that measurement artifact.

## Open Questions / Blockers

- **The experiment is unresolved until 4a executes.** The deciding metric is in both plans' close-out tasks: *how many checkpoints had to be un-batched into separate verify and commit steps mid-execution.* If that number is routinely non-zero, `writing-plans-tuned` gets deleted rather than merged into `writing-plans`.
- **4b was drafted before 4a ran** — against the sequencing I had recommended, at the user's direction. Its three consumed `internal/ws` interfaces (`MessageHandler`, `Room.Broadcast`/`Client.Send`, `Hub.Join`) exist only on paper. The plan opens with a "⚠ Revision required before execution" block naming them; that block is the mitigation and must not be skipped.
- **4b adds four schema amendments** (D2 `room:{roomID}:round`, D3 `room:{roomID}:opening`, D4 `question`/`outcomes` on the round hash, plus a `CreateRound` signature change). D4 modifies an existing tested function — Task 1 CP1 updates its callers in the same checkpoint, but it is the most likely place for 4b's execution to run long.
- **Reconnect loses a session.** 4b Task 8 CP2 fires `EndSession` on socket disconnect, so a dropped player restarts at the room buy-in. Deferred to Phase 7 hardening; recorded in the plan and to be written into the spec at close-out.

## Relevant Commits

- `502702e` — docs: journal Phase 3 merge and phase-sizing planning-notes session (the previous session's entry, uncommitted until now)
- `b9b45e7` — chore: add writing-plans-tuned, an experimental token-budget variant
- `5559cd4` — docs: add Phase 4a WebSocket transport plan
- `a8e55a2` — docs: add Phase 4b round lifecycle plan

## Next Step

Fresh window, `executing-plans`, pointed at `docs/plans/2026-08-26-phase-4a-ws-transport.md`. Do **not** carry this session's history into it — the cold start is the larger half of the budget saving, and both plans are written to be executable with no conversation history (`CLAUDE.md` auto-loads; each plan carries its own Global Constraints and environment gotchas). Expect `executing-plans` Step 1 to raise questions before starting; that is the skill working.
