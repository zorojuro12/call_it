---
name: journal
description: Write or resume from dated session journal entries in this project's journal/ folder, as part of a spec-driven development workflow (living spec doc + journal history). Use this whenever the user says "journal this", "write a journal entry", "log this session", or asks to resume/catch up from a previous session's journal. Also proactively SUGGEST running this near the end of a substantial session — after a feature lands, a bug is fixed, or a significant decision is made — but always ask for confirmation before writing; never write an entry unprompted. This replaces /save-session and /resume-session for this project — do not use those commands here.
---

# Journal

A project-local, dated log of development sessions, written to `journal/` at the
project root. Each entry is a single markdown file: a dense, scannable summary
block up top, and optional fuller detail below a separator. The journal is the
running history behind this project's living spec doc — decisions and their
reasoning live in the journal (or in linked ADRs under `docs/decisions/`), while
the spec doc stays a clean statement of current design.

This skill has two actions: **writing** a new entry and **resuming** from the
latest (or a specific) one. Infer which the user wants from their phrasing; if
genuinely ambiguous, ask.

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
[Only include if there were multiple decisions, or the one in "Decided" needs more room than a single line.]

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

### Step 5: Confirm with the user

Show the written file's path and full contents, and ask if it looks right or
needs edits before considering the entry final.

## Resuming from the journal

When the user wants to pick up from a previous session:

1. Find the target file: default to the most recently modified file in
   `journal/` (which `ls -r journal/` surfaces first, given the
   date+time-prefixed filenames). If the user names a date or topic, match on
   that instead.
2. Read the full file.
3. Brief the user: summarize the summary block in your own words, then flag
   anything from **Open Questions / Blockers** and **Next Step** as the things
   most likely to shape what happens next.
4. Do not start acting on the "Next Step" automatically — surface it and let
   the user confirm the direction, the same as you would before starting any
   new task.

## Proactively suggesting an entry

Near the end of a session where something substantial happened — a feature
landed, a bug got fixed, a non-trivial decision got made — suggest running
this skill to log it, rather than waiting to be asked. Phrase it as a
suggestion, not an action: something like "Want me to write a journal entry
for this session before we wrap up?" Never write an entry without the user
confirming first — the auto-suggestion is about visibility, not autonomy.

## Relationship to /save-session and /resume-session

This project uses the journal instead of ECC's `/save-session` /
`/resume-session` commands. Those write to a global, non-project-scoped
`~/.claude/session-data/` folder and aren't part of this project's
spec-driven workflow. If the user or a skill suggests `/save-session` in this
project, prefer `/journal` instead — don't run both, they'd fork into two
histories that can drift out of sync with each other.
