# Phase 8c — Google Stitch mockup prompts

Inputs for 8c's design-direction pass (parent plan §9, row 8c). Mockups are a
**visual reference only**: no Stitch code is pasted into `frontend/`.

## Where things go

| What | Where |
|------|-------|
| Screenshots (PNG), one per screen | `docs/design/8c-mockups/NN-<screen>.png` |
| Stitch code exports (HTML/Tailwind) | `docs/design/8c-mockups/code/NN-<screen>.html` |
| Your inspiration screenshots | `docs/design/8c-mockups/inspiration/` |
| Answers to the questions at the bottom | `docs/design/8c-mockups/notes.md` |

From Windows, the folder is
`\\wsl$\Ubuntu\home\chikara\projects\call_it\docs\design\8c-mockups\`
(the distro name may differ; it's the name `wsl -l` shows).

## Order

1. Prompt 0 (style direction) on the room screen, **three times**, with a
   different style line each time. Pick one.
2. Prompts 1–5 in the **same Stitch project** as the winner, so they inherit
   its style. Start each with "Same visual style as the previous screen."
3. Optionally, prompt 6 (desktop) for the room screen.

Choose **App (mobile)** layout where Stitch asks; watch parties are played on
phones.

## Product facts every prompt relies on

These keep the mockups honest. A screen that breaks one goes back, not into 8c:
- Tokens are **virtual**, never real money. No currency symbols, no "deposit".
- **Nobody can see who bet what until the round ends.** During a round, the
  only per-player signal is an aggregate count: "2/5 players have placed their
  bets".
- **The host can't bet** in their own room; the host's screen never has a
  wager pad.
- A round has **2–4 outcomes**, a countdown to **lockout**, then the host
  picks the winner. Payouts are pari-mutuel: the multiplier is the total pool
  divided by that outcome's pool.

---

## Prompt 0 — style direction (room screen, run 3 times)

```
Design a mobile screen for "CallIt", a real-time prediction game for watch
parties. Friends watching a game together bet virtual tokens on quick
questions the host asks, like "Will the next play be a touchdown?".

This is the live round screen a player sees:
- Top bar: room code "K7QF2M", the player's token balance "1,250 tokens",
  and a connection dot (green, "Live").
- The question in large type: "Will the next play be a touchdown?"
- A countdown: "Lockout in 12s", visually urgent as it nears zero.
- An odds board with 2 outcomes ("Yes" and "No"). Each shows its pool total
  (Yes: 400 tokens, No: 1,100 tokens) and its payout multiplier (Yes: 3.75x,
  No: 1.36x). Highlight which is the underdog.
- "Total pool: 1,500 tokens".
- "3/5 players have placed their bets", with a small progress indicator. Do
  NOT show names or avatars next to bets: who bet what stays hidden until the
  round ends.
- A wager pad: tap an outcome to select it, a stake input with quick-pick
  chips (10, 50, 100, Max), and a big "Place bet" button.
- A small roster strip of the 6 people in the room (avatars and names only,
  no bet info), one marked "Host".

Style: [STYLE LINE]
Virtual tokens only: no dollar signs, no real-money language.
```

Run it three times, replacing `[STYLE LINE]` with one of these each time:
- **A, sports-bar night:** `Dark theme, deep navy and charcoal, one neon accent (electric green) for primary actions and the countdown, bold condensed sports-broadcast typography, scoreboard feel.`
- **B, party game:** `Bright and playful like Kahoot or Jackbox, rounded shapes, saturated purple and yellow, chunky friendly type, celebratory but readable.`
- **C, sleek fintech:** `Minimal and premium like a modern trading app, near-black background, white type, a single brand colour, thin dividers, precise numeric typography with tabular figures.`

Or write your own, based on your inspiration screenshots.

## Prompt 1 — landing / join

```
Same visual style as the previous screen.
The CallIt landing screen. A short tagline: "Call it before it happens." Two
paths: (1) Join a room: a 6-character room code input plus a "Display name"
input and a "Join" button. Guests can join without an account. (2) "Host a
room", which needs an account, with "Log in" and "Create account" links.
Keep it a single, focused screen.
```

## Prompt 2 — log in / create account

```
Same visual style as the previous screen.
A "Log in" screen with Email and Password fields, a "Log in" button, an
inline error state reading "Invalid email or password", and a link to
"Create account". Also show the matching "Create account" variant with Email,
Password and Display name fields.
```

## Prompt 3 — host: create room, then share it

```
Same visual style as the previous screen.
Two states of the host flow.
State 1, "Create a room": one input, "Buy-in (tokens per player)", default
1000, and a "Create room" button.
State 2, "Room created": the room code "K7QF2M" shown huge and spaced out
for reading aloud across a living room, a shareable link with a copy button,
and a list of players joining live (names only).
```

## Prompt 4 — host console (open a round, then resolve it)

```
Same visual style as the previous screen.
The host's screen during a game. The host cannot bet, so there is no wager
pad. Three states:
State 1, "Open a round": a Question input, 2 to 4 Outcome inputs with
"Add outcome" and "Remove outcome" buttons, a "Lock after (seconds)" input,
and an "Open round" button.
State 2, round open: the live odds board (pool totals and multipliers per
outcome, as players see it), the countdown, "3/5 players have placed their
bets", and a note "Round is open — waiting for lockout".
State 3, locked: "Which outcome won?" with one large button per outcome.
```

## Prompt 5 — settlement reveal

```
Same visual style as the previous screen.
The reveal after the host picks the winner. This is the payoff moment and
should feel celebratory.
- The winning outcome, big: "Yes".
- Now, and only now, each player's result: name, tokens staked, tokens
  returned, and net change (e.g. "Priya — staked 100, returned 375, +275",
  "Sam — staked 200, returned 0, −200"). Mark the viewer's own row "(you)".
  Do not show which outcome each player picked; only these numbers.
- The player's own new balance, prominently.
Also show two alternative states:
(a) "Nobody backed the winning outcome — every stake was returned".
(b) "The round went unresolved — all 1,500 tokens were refunded".
```

## Prompt 6 (optional) — desktop room

```
Same visual style. The live round screen from earlier, laid out for a
desktop or TV browser: odds board and question large in the centre, wager
pad on the right, roster on the left. Same content and the same rule: no
per-player bet info until the round ends.
```

---

## Questions to answer in `notes.md`

1. Which style direction won (A, B, C or your own), and what about it?
2. Dark, light, or both?
3. Anything from the inspiration apps you want kept exactly?
4. Anything in the mockups you dislike, even if you picked that direction?
