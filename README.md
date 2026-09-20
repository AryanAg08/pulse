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

New machine, or setting this up for someone else? [`SETUP.md`](SETUP.md) is the
full walkthrough, including the macOS notification permission that is easy to
miss and the GoLand debug configurations.

```bash
cd ~/pulse-go
make install          # builds, then links into /opt/homebrew/bin
pulse init            # asks about your day, your routines, and how much it may talk

# or start from the documented sample instead:
cp example.yml ~/.pulse/config.yml
pulse run --dry --now # see what it would say right now, without sending
```

Requires Go 1.25+ and an authenticated `gh` for the GitHub signals. Everything
else is local. A single static binary — no runtime to install and nothing that a
`brew upgrade` can break underneath it.

---

## What it can do right now

### It asks first

`pulse init` is a short questionnaire, not a config file you have to learn.
Enter accepts every default, so the fastest correct path through setup is
holding Enter.

```
▌ your day
  When does your day start [09:00] › 08:30
  When do you want to stop being interrupted [22:30] › 21:00
  Which days do you work [weekdays] ›

▌ routines
  Daily stand-up? [Y/n] › y
    what time [10:00] › 09:45
  Gym or exercise? [Y/n] › y
    what time [19:00] › 06:30
    which days [mon,wed,fri] › tue,thu,sat
  Posture and stand-up-from-the-desk reminders? [Y/n] ›

▌ how much should it talk
  Most cycles say nothing. This is the ceiling, not the target.
  quiet / normal / chatty [normal] ›
```

Quiet hours are derived from the working day rather than asked for separately.
Times are forgiving — `9`, `09:00`, `0900`, and `19.30` all parse — and days
accept `weekdays`, `daily`, `weekends`, or `tue,thu,sat`. A posture routine is
automatically marked "only when active", because a posture nudge to an empty
chair is pure noise.

It never asks for an API key. A key typed at a prompt lands in a file on disk;
the questionnaire tells you where it belongs instead, including the
`launchctl setenv` line the background agent needs.

`pulse init --yes` skips the questions entirely, and `--force` re-runs them.

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

### It shows you your whole surface

`pulse metrics` lists every repo it discovered, joined to GitHub: open PRs, red
builds, stale PRs, and reviews you owe, per repo.

```
▌ repositories (15 local · 13 open PRs · 1 review owed)
  repo                      branch              dirty   PRs   red  stale  review
  arya-backend →ARYA-api    main                ·       4     4    4      ·
  logSense-api →LogSense    fix/feedback-slack… ·       4     2    4      1
  mongowrapper              master              ·       1     ·    1      ·
  pulse-go →pulse           master              287     ·     ·    ·      ·
  + 10 clean repos with nothing open

  ○ open PRs in repos not cloned here: Hacktoberfest2022(1), Orcha-api(1)
```

Repos are matched to GitHub by parsing the git remote, never by directory name
— `arya-backend →ARYA-api` above is a clone whose directory differs from its
repository, which basename matching would have mis-attributed. Remotes are then
resolved through GitHub, so a repo that was renamed or transferred between
organisations still matches even though the local `origin` is stale; the
resolution is cached in `~/.pulse/remotes.json` for a week. `×2` marks a
repository with more than one local clone, whose PR counts therefore appear on
more than one row (the header total still counts each PR once). Repos with
nothing open are counted but not printed, and PRs with no local clone are
listed separately so the totals reconcile rather than quietly disappearing.

Stale remotes are reported with the command to fix them. Add `--no-repos` to
skip the git and GitHub scan when you only want the verdict.

Discovery searches `repoScanDepth` directories below each root (default 5),
skipping `node_modules`, `vendor`, `dist`, `build`, `target`, and `Library`.

### It has an interactive dashboard

`pulse browse` opens a full-screen, three-tab view over everything Pulse knows.
Switch with `tab` or the number keys.

```
 1 repositories │ 2 metrics │ 3 config
                 ───────────

  ── the 14-day experiment ─────────────────────────────
  day             ██░░░░░░░░░░░░░░░░░░░░░░  1 of 14
  engaged         ████████████░░░░░░░░░░░░   50%   40% is the bar
  ignored         ████████████░░░░░░░░░░░░   50%

  ── today's noise budget ──────────────────────────────
  nudges          ████████░░░░░░░░░░░░░░░░  2 of 6  ·  min 45m apart
  quiet hours     no   22:30–08:00
```

**`2 metrics`** is the experiment dashboard: the day-14 progress, engagement
against the 40% bar, today's noise budget against the daily cap, a per-kind
breakdown so one bad rule is visible rather than averaged away, and a hoverable
history of every nudge.

Move the cursor with `↑↓` and the pane below shows the full record for that
nudge — untruncated text, which kind fired, whether it was worded by `pulse-ai`
or by a fixed template, how long you took to answer, and what it opens. `o`
opens it.

```
  ── history ───────────────────────────────────────────
  ▸ 20 Sep 14:19  mongowrapper#4 has sat for 204h with…  ○ ignored
    20 Sep 13:31  LogSense-sdk-node#2 has sat for 201h…  ● acked

  ── selected ──────────────────────────────────────────
  text          mongowrapper#4 has sat for 204h with no review. Worth a ping.
  kind          stale_pr
  phrased by    pulse-ai   worded by the model
  response      no response   counts as ignored
  opens         https://github.com/AryanAg08/mongowrapper/pull/4
```

**`3 config`** shows what is actually in effect, not what the file says — the
resolved phrasing provider and why it fell back if it did, discovery roots and
what they found, every threshold with a one-line explanation of what it
protects, routines, and mutes. `e` opens the config in your editor. The
credential is never rendered, only whether one resolved.

**`1 repositories`** is the browser — for the moment you want the whole picture
rather than one nudge.

```
▌ repositories  18 repositories

    repository                    branch                PRs   review  dirty
  ▸ api                           main                  2 1✗  1       40
    remote-only                   —                     1     ·       ·
```

Three levels, `→` to descend and `←` to come back:

1. **Repositories** — every clone plus any repo with open PRs that is not cloned here
2. **Pull requests** — number, checks, title, `+additions -deletions`, files changed, age
3. **Detail** — author, branch, diffstat, checks, and the full description with its file list

Anything worded by the model is tagged `pulse-ai`; the model id itself is never
displayed, since a dashboard is the sort of thing that gets screen-shared.
`PULSE_SHOW_MODEL=1` reveals it when debugging a provider.

The description and file list are the expensive half of the data, so they are
fetched only when you open a pull request, not for every row in the list. `o`
opens the PR in a browser, `r` refetches it, `q` quits.

It is read-only by design: Pulse decides what deserves an interruption, and this
is the view for everything that does not.

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
pulse init [--yes]            set up, asking about your day and routines
pulse run [--dry] [--now]     one cycle (--dry sends nothing, --now ignores quiet hours)
          [--phrase]          on a dry run, also show the pulse-ai wording
pulse start [--interval=10]   run continuously in this terminal
pulse daemon [--interval=10]  install as a launchd agent, survives reboots
pulse daemon --uninstall

pulse status                  what it sees now, and what it's holding back
pulse browse                  dashboard: repositories · metrics · config
pulse ack <id>                you acted on it (opens the PR)
pulse dismiss <id>            you read it, it wasn't useful
pulse snooze <id>             later
pulse mute <kind>             kill a whole category
pulse mute --hours=4          silence everything for a while
pulse unmute [kind]

pulse metrics [--no-repos]    the day-14 verdict, plus a per-repo breakdown
pulse log                     every nudge ever sent
pulse config                  print config path and contents
```

`pulse run --dry --now --phrase` also words the winning nudge through
`pulse-ai` and prints it without delivering or logging — the only way to see
what the model would actually say without spending one of the day's nudges.

`pulse run --dry --now` is the one to reach for while tuning: it shows every
signal, every candidate with its priority, the decision, and exactly why each
losing candidate was suppressed — without sending anything.

---

## Plan

[`docs/PLAN.md`](docs/PLAN.md) lays out the phases, each gated on evidence
rather than a date, plus the standing constraints and the things deliberately
not being built. [`docs/FLOW.md`](docs/FLOW.md) traces a nudge end to end.

## The experiment

Run `pulse daemon` and use it normally for 14 days. **Do not tune it to be nicer
mid-run** — that destroys the result you are trying to measure. Then run
`pulse metrics`, which returns one of three verdicts:

- **muted before day 14** → falsified. The nudges weren't worth the interruption. Stop.
- **survived, ≥40% engaged** → the wedge holds. Build the next layer.
- **survived but mostly ignored** → tolerated, not valued. Fix relevance before adding features.

---

## Configuration

[`example.yml`](example.yml) is a fully commented reference covering every
option — copy it to `~/.pulse/config.yml` and edit. A test loads it on every CI
run, so it cannot drift out of sync with the code.

`~/.pulse/config.yaml` or `~/.pulse/config.yml` — both are read, via viper. Any
field you omit falls back to a default, so an older config keeps working as
options are added, and any field can be overridden from the environment.

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
  provider: api            # "anthropic" or "api"
  apiUrl: https://your-gateway.example/v1
  modelName: gpt-luna
  # apiKey: prefer PULSE_API_KEY in the environment instead
```

`pulse init` accepts these up front:

```bash
pulse init --provider=api --api-url=https://your-gateway.example/v1 --model=gpt-luna
```

---

## Phrasing (AI)

**The model only phrases — it never decides.** What to send and whether to send
it stays in auditable Go, so the noise ceiling cannot drift with a prompt change.
Any failure — timeout, API error, refusal, an over-long or multi-line answer —
falls back to the deterministic template, because a nudge that arrives late or
malformed is worse than one that arrives plain. With nothing configured, Pulse
runs fully offline on templates and nothing else degrades.

Two providers, selected by `phrasing.provider`:

### `anthropic` — Claude via the official SDK

```yaml
phrasing:
  useLLM: true
  provider: anthropic
  modelName: claude-opus-5
```

Adaptive thinking at low effort, since rewording one sentence needs no depth.

### `api` — any OpenAI-compatible endpoint

For OpenAI, Groq, DeepSeek, OpenRouter, a private gateway, or a local runtime.
The model name passes through verbatim, so a new model is a config edit rather
than a release.

```yaml
phrasing:
  useLLM: true
  provider: api
  apiUrl: https://your-gateway.example/v1
  modelName: gpt-luna
```

`apiUrl` works with or without the path — both `https://host/v1` and
`https://host/v1/chat/completions` resolve correctly. When no key is present the
`Authorization` header is omitted entirely, which local runtimes require.

### Credentials

Resolution is environment-first, so a secret never has to be written to disk:

| Order | Source |
|---|---|
| 1 | `PULSE_API_KEY` (works for either provider) |
| 2 | `ANTHROPIC_API_KEY` / `ANTHROPIC_AUTH_TOKEN`, or `OPENAI_API_KEY` for `api` |
| 3 | `phrasing.apiKey` in the config file |

The config file is supported for convenience but is the least preferred option.
When it does contain a key, Pulse writes it `0600` rather than `0644`.

Any config value can also be overridden by environment, via viper:
`PULSE_PHRASING_MODELNAME`, `PULSE_PHRASING_APIURL`, `PULSE_PHRASING_PROVIDER`,
`PULSE_USER_GITHUBLOGIN`.

### Seeing whether it is actually on

```
$ pulse status --check
  phrasing      ● pulse-ai — live round-trip ok
```

`--check` makes a real round-trip. Plain `pulse status` only proves the config
parsed, which cannot distinguish a working credential from a missing one, or a
valid model id from a typo. Unrecognised config fields are reported rather than
ignored, so a misspelled key does not look applied while doing nothing.

[`docs/FLOW.md`](docs/FLOW.md) traces a nudge end to end, from a file changing
on disk to a banner appearing, including exactly where the model does and does
not get a say.

or, when something is wrong:

```
  phrasing      templates — provider "api" requires phrasing.apiUrl
```

A misconfigured provider otherwise degrades to templates silently and you would
never learn the AI half was dead.

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
  phrase.go              prompt construction, output guards, template fallback
  canon.go               resolves renamed/transferred remotes, cached on disk
  ui/
    ui.go                terminal styling; degrades to plain ASCII off-TTY
  tui/
    model.go             dashboard state and the repo/PR join
    update.go            key handling, tab switching, scrolling
    view.go              the repository browser's three levels
    tabs.go              tab bar, metrics dashboard, config view
    run.go               entry point
  onboard/
    ask.go               prompt primitives, forgiving time and day parsing
    run.go               the questionnaire and its summary
  llm/
    provider.go          Provider interface and Config
    factory.go           provider selection, env-first credential resolution
    anthropic.go         Claude via the official SDK
    openai_compat.go     any OpenAI-compatible /chat/completions endpoint
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

98 tests covering the arbiter's gates, the abandonment cutoff, priority decay,
routine grace windows and weekday rules, focus-streak continuity across sampling
cadence, the daily cap, and the day-14 verdict logic — plus config loading
(`.yml` and `.yaml`, env overrides, legacy key compatibility) and the AI layer
against a fake OpenAI-compatible server. They construct state in-memory or in a
`t.TempDir()` with `HOME` redirected, so running them can't corrupt an experiment
in flight.

Terminal output is styled but never at the expense of the log: colour switches
off when stdout is not a TTY, when `NO_COLOR` is set, or when `TERM=dumb`, and
the launchd plist sets `NO_COLOR` explicitly so `agent.log` stays plain and
greppable. A test asserts no escape code can leak into unstyled output.

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
