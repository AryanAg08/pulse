# Pulse — phased plan

Last updated 2026-09-21. Written to be falsifiable: every phase after the
current one is **gated on evidence**, not on a date.

The governing rule of this project: *the reminder is not the product, the
context is.* Everything below is downstream of that, and of one open question
that phase 1 exists to answer.

---

## Where we are

| | |
|---|---|
| Phase | **1 — running the falsification test** |
| Verdict due | **2026-10-04** |
| Code | 4,704 lines Go, 1,491 lines tests, 86 tests |
| Repo | `github.com/AryanAg08/pulse`, 12 commits (10 unpushed) |
| Nudges sent | 2 |

---

## Phase 0 — v0 · **done**

A working instrument, not a product. Built 2026-09-20.

**Shipped:**

- Signal collection: git across 15 repos, one GitHub GraphQL query via `gh`, focus inferred from file mtimes
- Six rules producing candidates; nine arbiter gates reducing them (20 → 1 on real data)
- `pulse-ai` phrasing layer — pluggable provider, Anthropic SDK or any OpenAI-compatible endpoint, always falling back to a deterministic template
- launchd agent, macOS notifications, `ack`/`dismiss`/`mute`
- `pulse browse` — three-tab dashboard: repositories → PRs → detail, metrics, config
- `pulse init` — first-run questionnaire covering the working day, routines, and the noise ceiling
- Self-measurement: `pulse metrics` reports the day-14 verdict in words

**Deliberately not built,** because they can't be judged before phase 1 answers:
calendar signal, any non-macOS delivery, multi-user anything, a server.

---

## Phase 1 — the falsification test · **in flight, ends 2026-10-04**

**The question:** not "is this useful" and not "do people like it". Only —
**will a developer still accept interruptions from software after two weeks?**
Every notification product dies at exactly that question, and no amount of
feature work moves it.

**Method:** run `pulse daemon`, use the machine normally, answer nudges with
`ack` or `dismiss`, change nothing. Then `pulse metrics`.

**Discipline, and this is the whole phase:** do not tune the thresholds
mid-run. How annoying it is *is* the measurement. Adjusting it partway replaces
the result with a number about your patience for configuration.

**Three outcomes, already coded into `metrics.go`:**

| Result | Reading | Next |
|---|---|---|
| Muted before day 14 | **Falsified.** Not worth the interruption. | Phase 2A |
| Survived, ≥40% engaged | **The wedge holds.** | Phase 2B |
| Survived, <40% engaged | Tolerated, not valued. | Phase 2C |

**Known weakness of this run, to state honestly:** n=1, and the subject is the
author. It cannot prove the idea works. It can only kill it — which is the more
valuable direction, and is why it is worth two weeks rather than six months.

---

## Phase 2 — branches on the verdict

### 2A · Falsified → stop, and say so

The honest path, and the one most likely to be ignored.

If you muted it, the correct move is to write down why in two paragraphs and
stop. The instinct will be "the rules just need tuning" — resist it. The gates
are already aggressive: one nudge per cycle, six a day, forty-five minutes
apart, three-year-old PRs silenced entirely. If *that* was too much, the
problem is the channel, not the calibration.

Salvage worth keeping: `pulse browse` and `pulse metrics` are a decent
read-only dashboard over your GitHub surface with no notification in sight.
That is a smaller, honest thing.

### 2B · Wedge holds → widen the evidence, not the features

**The temptation:** start building. **The correct move:** find out whether n=1
generalises, because everything after depends on it.

1. **Get 10–15 developers running it** — the concierge test skipped in favour of
   building. Now cheaper, because the thing exists.
2. **Ship the calendar signal** — the single highest-value gap. *"You have no
   meetings after 6:40"* was the sharpest line in the original pitch and is the
   one signal that turns a routine reminder into something no other app can
   say. Needs an OAuth scope decision, which is why it was deferred.
3. **Fix the remaining setup cliff** — the Script Editor notification
   permission. It silently makes Pulse look broken, and it bit this install.
   (The first-run questionnaire, added 2026-09-21, closed the other one:
   configuration is no longer a file you have to learn before the tool is
   useful.)
4. **Decide the delivery channel question.** macOS notifications are the only
   channel and are invisible when unpermitted. A second channel — terminal
   status line, menu bar, or a `pulse today` digest — is insurance against the
   channel being the thing that fails.

**Gate out of 2B:** ≥6 of 15 still unmuted at day 14. Below that, n=1 was you,
not a market.

### 2C · Tolerated, not valued → fix relevance, once

Not muted but ignored means the nudges are *true and unhelpful*. One targeted
pass, time-boxed:

- Use the per-kind table. If one kind carries all the ignores, cut that rule
  rather than retuning everything.
- The likely culprit is `stale_pr` — it is the highest-volume kind and the least
  actionable, since a stale PR is usually waiting on somebody else.
- Re-run phase 1 once. If the second run also lands here, treat it as 2A.

---

## Phase 3 — distribution · **gated on 2B**

Only if the wedge held across more than one person.

- Homebrew tap; `brew install pulse` (the symlink dance is a real barrier)
- Linux delivery (`notify-send`) — the engine is already portable, only `notify.go` and `agent.go` are not
- Where developers actually are: Product Hunt, HN, the VS Code marketplace
- **Not** a landing page, not a waitlist, not a pricing page. Distribution before retention is how the category's corpses were made.

---

## Phase 4 — the original vision · **gated on 3**

The idea this started as: *a personal assistant for everything — routines, gym,
streaks, posture, for developers and everyone.*

That assessment has not changed. It remains a saturated category with brutal
retention, near-zero willingness to pay in India, and direct platform risk as
the large assistants absorb reminders and memory. It is not a phase 1 idea.

It becomes reachable only from here, and only in one direction: the habit layer
rides on context that is already earning attention. *"Gym at 7 — you have no
meetings after 6:40"* works because Pulse already knows your calendar and you
already trust its interruptions. The same sentence from a fresh habit app is
noise.

Order matters and is not negotiable: **context first, trust second, habits
third.** Reversing it produces the app we already decided not to build.

---

## Standing constraints

Carried from the first conversation; revisit only with evidence, not with
enthusiasm.

- **India is not the launch market.** Willingness to pay for organisation is near zero; Indians pay for outcomes. Build from India, sell to dollar markets.
- **Never let the model decide what to send.** `pulse-ai` words nudges; the ceiling lives in Go. If a prompt edit could raise the noise cap, the cap is not real.
- **Silence is the default and a feature.** Most cycles send nothing. A quiet day is the system working.
- **The ratio is the product.** 20 candidates → 1 delivered. Rules are easy; restraint is the moat.

---

## Open questions

Unresolved, and listed so they are not quietly forgotten:

1. **Does the notification banner actually render on this machine?** Never confirmed. `osascript` exits 0 whether or not it does. If not, phase 1's data is meaningless — the nudges were logged and never seen.
2. **Calendar OAuth** — Google, Apple, or a local `.ics` read? The cheapest useful version is probably reading the local Calendar store, no OAuth at all.
3. **PR titles reach a third party** when `pulse-ai` words a nudge. Acceptable now; needs a local-model option before anyone else's private repos are involved.
4. **What replaces the metric after day 14?** Retention answers "is it tolerable", not "is it valuable". Phase 2B needs a second measure and does not have one yet.

---

## What is explicitly not planned

Recording these prevents relitigating them:

| Not doing | Why |
|---|---|
| A web dashboard | The data is local; a server adds an account system to a thing with no account |
| A mobile app | The context is your development machine |
| Team/manager features | "Who has stale PRs" becomes surveillance the moment a manager sees it |
| Configurable rules DSL | The gates are the product; making them user-editable makes the noise ceiling user-editable |
| More signal sources before phase 1 concludes | Every one makes the experiment less interpretable |
