---
name: journal
description: Write, resume from, or list dated session journal entries in this project's journal/ folder, as part of a spec-driven development workflow (living spec doc + journal history). Use this whenever the user says "journal this", "write a journal entry", "log this session", asks to resume/catch up from a previous session's journal, or asks to see/list past journal entries. Also proactively SUGGEST running this near the end of a substantial session — after a feature lands, a bug is fixed, or a significant decision is made — but always ask for confirmation before writing; never write an entry unprompted. This replaces /save-session, /resume-session, and /sessions for this project — do not use those commands here.
---

# Journal

A project-local, dated log of development sessions, written to `journal/` at the
project root. Each entry is a single markdown file: a dense, scannable summary
block up top, and optional fuller detail below a separator. The journal is the
running history behind this project's living spec doc — decisions and their
reasoning live in the journal (or in linked ADRs under `docs/decisions/`), while
the spec doc stays a clean statement of current design.

This skill has three actions: **writing** a new entry, **resuming** from the
latest (or a specific) one, and **listing** past entries. Infer which the user
wants from their phrasing; if genuinely ambiguous, ask.

## Why entries are structured this way

The summary block exists so a future session (yours or the user's) can
understand what happened in ten seconds without opening the detail section.
Every field in it answers a specific resumption question: what's the state,
what got decided and why, did the spec change, what's next, what's blocking,
what files are involved. If a session was small, the summary block alone is a
complete, valid entry — don't pad it with an empty detail section just to look
thorough. An honest short entry beats a padded long one.

## Writing an entry

### Step 1: Determine the session name

Ask the user for their name (or an identifier) **only once per session** — the
first time `/journal` is invoked to write an entry. Remember it for the rest
of the conversation and reuse it for any later entries in the same session
without asking again.

### Step 2: Determine the topic slug

Derive a short kebab-case slug from what was actually worked on (e.g.
`auth-jwt-flow`, `journal-skill-design`). Keep it to a few words — it's part
of the filename, not a description field.

### Step 3: Create the journal/ folder if it doesn't exist

```bash
mkdir -p journal
```
(relative to the project root — this is a project-local folder, always commit
it to the repo.)

### Step 4: Write the file

Filename: `journal/YYYY-MM-DD_HHMM_<name>_<topic-slug>.md`, using the actual
current date and local time (24h `HHMM`, no colon). The time component exists
specifically so that multiple entries written on the same day still sort
newest-first under `ls -r` — without it, same-day entries would sort
alphabetically by topic instead of by when they were written.

Valid example: `journal/2026-08-15_1430_ansh_journal-skill-design.md`

Use this exact template:

```markdown
# YYYY-MM-DD — <name> — <short human-readable topic>

**Status:** [one line: what state things are in right now]
**Decided:** [the single most important decision this session, if any — point to docs/decisions/NNNN-*.md for an ADR if one exists, rather than re-explaining the reasoning here]
**Spec:** [Updated — one line on what changed and why | No change]
**Next:** [the single most important next action]
**Blocked on:** [what's blocking progress, or "Nothing"]
**Touches:** [files/dirs most relevant to this session, glob-style is fine]

---

## What We Worked On
[Context — what feature/problem, why it matters. Skip if the summary block already says it all.]

## Decisions Made
- **[decision]** — reason: [why, especially if it affects the spec]
[Only include if there were multiple decisions, or the one in "Decided" needs more room than a single line. If the reasoning is already written down in the spec, an ADR, or the plan, link to that section instead of restating it — e.g. "Outbox amendment — see plan §1" — one line, not a re-explanation. Only spell out reasoning here if it lives nowhere else: a mid-session judgment call, something later reverted, or context that didn't make it into a committed doc.]

## What Worked
- [confirmed working, with evidence — a test passed, it ran in the browser, an API returned the expected response]

## What Didn't Work
- [approach tried] — failed because: [exact reason, so a future session doesn't retry it blind]

## Test Coverage
- **Covered:** [what's tested and how]
- **Not covered yet:** [known gaps — say this explicitly so nobody assumes coverage that doesn't exist]

## Open Questions / Blockers
- [unresolved questions, external dependencies, anything the next session needs to address]

## Relevant Commits
- `<short-sha>` — [one-line description] (link the PR instead if one exists)

## Spec Changes
[Only if Spec: Updated above — what changed in the living spec doc and why. Omit entirely if no change.]

## Next Step
[Expand on the summary's "Next" line only if it needs more than one sentence.]
```

Every section below the `---` is optional — include only the ones that have
real content. A short session might just be the summary block with nothing
below the separator at all, and that's a complete entry, not an unfinished one.

**Exception: "What Didn't Work."** If anything was tried and abandoned this
session, always include this section — never drop it for brevity. A future
session blindly retrying a dead end wastes far more than the section costs to
write. If nothing failed, it's fine to omit the section entirely (there's
nothing to warn against retrying); the exception is only "don't cut this for
space when there's real content."

### Step 5: Confirm with the user

Show the written file's path and full contents, and ask if it looks right or
needs edits before considering the entry final.

## Resuming from the journal

When the user wants to pick up from a previous session:

1. Find the target file: default to the most recently modified file in
   `journal/` (which `ls -r journal/` surfaces first, given the
   date+time-prefixed filenames). If the user names a date or topic, match on
   that instead.
2. Read only the summary block (everything above the first `---`) by
   default — not the full file. The summary block exists precisely so
   resumption doesn't cost reading the detail section every time; reading the
   whole file on every resume defeats that design. Fields like **Open
   Questions / Blockers** and **Next Step**, mentioned below, live in the
   summary block itself, not past the separator.
3. Only read past the `---` into the detail section if the summary block
   references something ambiguous (e.g. "see Decisions Made" for a
   non-obvious call), or the user explicitly asks for more detail on
   something. Don't read it "just in case."
4. Check staleness: if the entry's date is more than 7 days old, flag it —
   "This entry is from N days ago — the codebase may have moved on since."
   Still proceed with the briefing; this is a warning, not a blocker.
5. Check the **Touches** field: for each file/dir it names, verify it still
   exists. If something's missing, flag it in the briefing — "WARNING:
   `path` was referenced in this entry but no longer exists." Don't silently
   drop it.
6. Brief the user: summarize the summary block in your own words, then state
   the **Blocked on** and **Next** lines explicitly, even if they're
   "Nothing" / not defined — an empty result is still a result worth stating.
7. Do not start acting on the "Next" step automatically — surface it and let
   the user confirm the direction, the same as you would before starting any
   new task.

## Listing past entries

When the user wants to see what journal entries exist, rather than resume from
one specific entry:

1. Run `ls -r journal/` to get entries newest-first.
2. For each entry (or the most recent N if there are many — ask how many if
   it's not obvious), read just the summary block (everything above the first
   `---`) rather than the full file.
3. Present a compact list: date, name, topic, and the **Status** and
   **Decided** lines from each summary block — enough for the user to
   recognize which entry they want without opening it. Don't dump full file
   contents for a list request; that's what resuming a specific entry is for.

## Proactively suggesting an entry

Near the end of a session where something substantial happened — a feature
landed, a bug got fixed, a non-trivial decision got made — suggest running
this skill to log it, rather than waiting to be asked. Phrase it as a
suggestion, not an action: something like "Want me to write a journal entry
for this session before we wrap up?" Never write an entry without the user
confirming first — the auto-suggestion is about visibility, not autonomy.

## Relationship to /save-session, /resume-session, and /sessions

This project uses the journal instead of ECC's `/save-session` /
`/resume-session` / `/sessions` commands. Those write to and manage a global,
non-project-scoped `~/.claude/session-data/` folder and aren't part of this
project's spec-driven workflow. If the user or a skill suggests any of those
in this project, prefer the matching journal action instead (`/sessions list`
→ listing past entries, `/sessions load` → resuming a specific entry) — don't
run both, they'd fork into two histories that can drift out of sync with each
other.

## Relationship to the global `journal-global` skill

This project-local skill takes precedence over `~/.claude/skills/journal-global/`
whenever both would apply — it's tailored to this project (e.g. ADRs live at
`docs/decisions/` here, not the global default's `docs/adr/`). No action
needed; this is just documenting why this copy exists rather than relying on
the global one.
