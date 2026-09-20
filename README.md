# Pulse

A context-aware assistant for developers, in Go.

The thesis: **the reminder is not the product, the context is.** Every habit app
fails the same way — it only knows what you typed into it, so it can only nag you
about things you already knew. Pulse never asks what to remind you about. It reads
your actual work state — git, GitHub, edit activity — and speaks only when the
evidence justifies an interruption.

This is v0, a falsification instrument rather than a business. It exists to answer
one question in 14 days: **will a developer still accept interruptions from
software two weeks in?** If the answer is no, nothing else about the idea matters.

---

## Install

```bash
cd ~/pulse-go
make install          # builds, then links into /opt/homebrew/bin
pulse init            # detects your repos and GitHub identity
pulse run --dry --now # see what it would say right now, without sending
```

Requires Go 1.25+ and an authenticated `gh` for the GitHub signals. Everything
else is local. A single static binary — no runtime to install and nothing that a
`brew upgrade` can break underneath it.

---

## What it can do right now

### It watches six things

| Kind | Source | Fires when |
|---|---|---|
| `ci_failed` | GitHub GraphQL | your PR's checks went red |
| `review_debt` | GitHub GraphQL | a teammate has waited past the threshold on *your* review |
| `stale_pr` | GitHub GraphQL | your PR sat unreviewed, with priority decaying as it ages |
| `long_focus` | file mtimes across repos | you've been heads-down past the break interval |
| `uncommitted_work` | `git diff --numstat` | a big diff has no commit behind it |
| `routine` | your config | something you declared is due, within a 15-minute grace window |

All GitHub data comes through one GraphQL round-trip via the `gh` CLI, so Pulse
never handles a token and needs no OAuth app. If GitHub is unreachable the cycle
still completes on local git signals alone.

### It decides what not to say

This is the actual product. Every gate below exists to protect the one number
that determines whether this lives:

- **one nudge per cycle, maximum** — the highest-priority candidate wins; the rest are dropped, never queued
- **hard daily cap** (default 6) and a **minimum gap** between nudges (default 45m)
- **quiet hours**, handling the overnight wrap correctly
- **abandonment cutoff** — a PR older than 14 days is dead, not stale, and earns silence
- **per-kind caps** — a 14-PR backlog yields 2 candidates, not 14
- **priority decay** — the longer a PR sits, the less a ping helps, until the rule stops firing
- **re-nudge cooldown** — once a fact is spoken, 20 hours of silence about it
- **self-review exclusion** — your own PR in your review queue is not a debt
- **per-kind mute** and a global timed kill switch

On a real account with 14 open PRs, the rules produced 21 candidates. These
filters cut that to 2 and delivered 1. That ratio is the product.

### It measures whether it deserves to exist

`pulse metrics` reports the day-14 verdict from the log. `ack` and `dismiss` both
count as engagement — a dismiss means you read it and made a decision. Only
silence counts as failure.

### It stays out of the way

Quiet by default, one notification at a time, and every nudge carries a six-character
id so you can respond in one command.

---

## Commands

```
pulse init                    detect repos and GitHub identity, write config
pulse run [--dry] [--now]     one cycle (--dry sends nothing, --now ignores quiet hours)
pulse start [--interval=10]   run continuously in this terminal
pulse daemon [--interval=10]  install as a launchd agent, survives reboots
pulse daemon --uninstall

pulse status                  what it sees now, and what it's holding back
pulse ack <id>                you acted on it (opens the PR)
pulse dismiss <id>            you read it, it wasn't useful
pulse snooze <id>             later
pulse mute <kind>             kill a whole category
pulse mute --hours=4          silence everything for a while
pulse unmute [kind]

pulse metrics                 the day-14 verdict
pulse log                     every nudge ever sent
pulse config                  print config path and contents
```

`pulse run --dry --now` is the one to reach for while tuning: it shows every
signal, every candidate with its priority, the decision, and exactly why each
losing candidate was suppressed — without sending anything.

---

## The experiment

Run `pulse daemon` and use it normally for 14 days. **Do not tune it to be nicer
mid-run** — that destroys the result you are trying to measure. Then run
`pulse metrics`, which returns one of three verdicts:

- **muted before day 14** → falsified. The nudges weren't worth the interruption. Stop.
- **survived, ≥40% engaged** → the wedge holds. Build the next layer.
- **survived but mostly ignored** → tolerated, not valued. Fix relevance before adding features.

---

## Configuration

`~/.pulse/config.yaml`. Any field you omit falls back to a default, so an older
config keeps working as options are added.

```yaml
user:
  githubLogin: AryanAg08
repoRoots:
  - /Users/you/code
quietHours: { start: "22:30", end: "08:00" }
maxNudgesPerDay: 6
minMinutesBetweenNudges: 45
focus:
  breakAfterMinutes: 90
thresholds:
  stalePrHours: 24
  reviewDebtHours: 12
  uncommittedLines: 400
  abandonedAfterDays: 14
  maxPerKind: 2
routines:
  - name: Stand-up
    at: "10:00"
    days: [1, 2, 3, 4, 5]
    note: what you shipped yesterday
  - name: Gym
    at: "19:00"
    days: [1, 3, 5]
  - name: Posture
    at: "15:00"
    requireActive: true   # skip if you're away from the keyboard
mutedKinds: []
phrasing:
  useLLM: true
  model: claude-opus-5
```

---

## Phrasing

Nudge wording goes through Claude (`claude-opus-5`, adaptive thinking at low
effort) when `ANTHROPIC_API_KEY` is set. **The model only phrases — it never
decides.** What to send and whether to send it stays in auditable Go, so the
noise ceiling cannot drift with a prompt change.

Any failure — timeout, API error, refusal, an over-long or multi-line answer —
falls back to the deterministic template, because a nudge that arrives late or
malformed is worse than one that arrives plain. With no key set, Pulse runs
fully offline on templates and nothing else degrades.

---

## Layout

```
main.go                  CLI: parsing, output, command dispatch
internal/pulse/
  types.go               domain types; JSON tags pinned for log compatibility
  config.go              load/save, defaults, quiet-hours arithmetic
  store.go               state.json and the nudges.jsonl append log
  git.go                 repo discovery, dirty stats, edit recency
  github.go              one GraphQL query for both PR lists, via `gh`
  focus.go               focus streaks inferred across cycles
  collect.go             assembles a Signals snapshot
  rules.go               signals -> candidates (what could be said)
  arbiter.go             candidates -> at most one nudge (what gets said)
  phrase.go              optional Claude rewording, template fallback
  notify.go              macOS notification delivery
  agent.go               launchd install/uninstall
  metrics.go             the day-14 verdict
  cycle.go               observe -> decide -> speak
  pulse_test.go          16 behavioural tests
```

`rules.go` and `arbiter.go` are deliberately separate: generating what *could* be
said is a different concern from deciding what *gets* said, and keeping the noise
ceiling in one auditable place is the whole design.

---

## Tests

```bash
make check     # fmt, vet, test
```

16 tests covering the arbiter's gates, the abandonment cutoff, priority decay,
routine grace windows and weekday rules, focus-streak continuity across sampling
cadence, the daily cap, and the day-14 verdict logic. They construct state
in-memory and never touch `~/.pulse`, so running them can't corrupt an experiment
in flight.

---

## Porting notes

This is a port of an earlier TypeScript implementation (kept at `~/pulse` for
reference). Two things were held fixed deliberately:

- **The on-disk formats are identical.** `~/.pulse/config.yaml`, `state.json`,
  and `nudges.jsonl` use the same keys, so swapping implementations mid-experiment
  preserves the clock, the history, and the dedupe state.
- **The decisions are identical.** Both versions were run against the same live
  account at the same moment and produced byte-identical signals, candidate
  priorities, and verdicts.

The Go version also removes a real failure mode: the Node plist had to pin an
absolute interpreter path, and a `brew upgrade node` would have silently killed
the agent mid-experiment. A static binary has no such dependency.

---

## Known limits

- **macOS only for delivery** (`osascript`) and background install (`launchd`). The engine is portable; only `notify.go` and `agent.go` are not.
- **Notification permission is not verifiable from code.** `osascript` exits 0 whether or not the banner actually renders. macOS attributes these to **Script Editor**; if that app lacks notification permission, nudges are logged but never shown. Check System Settings → Notifications.
- **Focus tracking needs a cycle interval under 10 minutes.** Streaks are inferred from consecutive samples, so sampling slower than the 20-minute break gap means focus never accumulates. `pulse start` warns; PR and routine nudges are unaffected.
- **Focus is inferred from file mtimes**, so reading code, reviewing, or thinking reads as idle.
- **No calendar signal.** "You have no meetings after 6:40" was the sharpest line in the original pitch and is not built. It is the highest-value missing input.
