# How Pulse works, end to end

Your configuration, traced through every stage from "a file changed on disk" to
"a banner appeared", including exactly where the AI does and does not get a say.

Your current setup:

```yaml
phrasing:
  useLLM: true
  provider: api
  apiUrl: "https://openrouter.ai/api/v1"
  modelName: "openai/gpt-5.6-luna"
  apiKey: "sk-or-v1-…"
```

---

## The one-line version

Every 10 minutes a launchd agent wakes Pulse. It reads your work state, turns it
into a list of things it *could* say, throws away almost all of them, and — only
if something survives — asks Luna to word the one that's left.

**Luna never decides what to send, or whether to send anything.** By the time it
is called, that has already been decided in Go.

---

## The cycle

```
launchd (every 10 min)
   │
   ▼
┌─ 1. COLLECT ──────────────────────────────────────────────┐
│  git: 15 repos — branch, dirty lines, file mtimes         │
│  gh:  13 open PRs, 1 review owed, CI status               │
│  focus: minutes of unbroken editing                       │
└───────────────────────────────────────────────────────────┘
   │  Signals
   ▼
┌─ 2. RULES  (rules.go) ────────────────────────────────────┐
│  Six rules → everything that COULD be said                │
│  → 20 candidates, each with a priority                    │
└───────────────────────────────────────────────────────────┘
   │  []Candidate
   ▼
┌─ 3. ARBITER  (arbiter.go) ────────────────────────────────┐
│  Nine gates → what GETS said                              │
│  → 1 candidate, or nothing at all                         │
└───────────────────────────────────────────────────────────┘
   │  at most one
   ▼
┌─ 4. PHRASE  (llm/) ───────────────────────────────────────┐
│  ── this is the only place Luna is involved ──            │
│  POST openrouter.ai/api/v1/chat/completions               │
│  → one sentence, or the plain template on any failure     │
└───────────────────────────────────────────────────────────┘
   │
   ▼
┌─ 5. DELIVER ──────────────────────────────────────────────┐
│  macOS banner · append to nudges.jsonl · record dedupe key│
└───────────────────────────────────────────────────────────┘
```

Most cycles stop at stage 3 and nothing is sent. That is the intended outcome,
not a failure — and it means Luna is usually not called at all.

---

## Stage 1 — Collect

No network beyond one GitHub call, and no state of its own.

| Source | How | What it yields |
|---|---|---|
| Local repos | walks `repoRoots` to `repoScanDepth` (5) | branch, uncommitted lines, file mtimes, origin URL |
| GitHub | one GraphQL query via the `gh` CLI | your open PRs, reviews owed, CI rollup, `+/-` diffstat |
| Focus | compares file mtimes across consecutive cycles | minutes of unbroken editing |

Two details that matter:

- **Auth is `gh`'s, not Pulse's.** No token is stored and no OAuth app exists. If
  `gh` is logged out, the cycle still completes on local git signals alone.
- **Remotes are canonicalised.** Your `Logsense-tech` clones point at an org that
  was renamed to `Logsense-cloud`. Pulse resolves the redirect before matching,
  cached in `~/.pulse/remotes.json` for a week, so a stale remote doesn't make a
  cloned repo look absent.

## Stage 2 — Rules: what *could* be said

Six rules, each producing candidates with a priority:

| Kind | Fires when | Priority |
|---|---|---|
| `ci_failed` | your PR's checks are red | 90, decaying with age |
| `routine` | a routine you declared is due, within 15 min | 85 |
| `review_debt` | a teammate has waited > 12h on your review | 80 |
| `stale_pr` | your PR sat > 24h unreviewed | 70, decaying |
| `long_focus` | > 90 min unbroken editing | 60 |
| `uncommitted_work` | > 400 uncommitted lines, no recent commit | 50 |

On your machine this stage produces **20 candidates**. Every one is true. That
is exactly the problem the next stage exists to solve.

## Stage 3 — Arbiter: what *gets* said

Nine gates, in order. The first that trips ends the cycle silently.

1. **Globally muted** — `pulse mute --hours=4`
2. **Quiet hours** — 22:30–08:00
3. **Daily cap** — 6
4. **Minimum gap** — 45 minutes since the last nudge
5. **Per-kind mute**
6. **Re-nudge cooldown** — 20h of silence per fact already spoken
7. **Abandonment cutoff** — a PR older than 14 days is dead, not stale
8. **Per-kind cap** — at most 2 candidates of one kind
9. **Highest priority wins** — the rest are dropped, never queued

20 → 1. That ratio is the product; the rules are the easy half.

## Stage 4 — Phrase: where Luna comes in

Reached only when stage 3 produced a winner.

**Request** — `POST https://openrouter.ai/api/v1/chat/completions`, `Bearer` auth:

```json
{
  "model": "openai/gpt-5.6-luna",
  "max_tokens": 512,
  "messages": [
    { "role": "system", "content": "You phrase notifications… One sentence, under 140 characters. Lead with the concrete fact. Never invent detail that is not in the facts you were given. No emoji…" },
    { "role": "user", "content": "Nudge kind: stale_pr\nFacts: {\"repo\":\"AryanAg08/mongowrapper\",\"number\":4,\"staleHours\":209}\nPlain version (rephrase this, keep every number): mongowrapper#4 has sat for 209h with no review. Worth a ping.\nRecent nudges to avoid echoing:\n- …" }
  ]
}
```

The last part matters: recent nudges are included so wording varies. Repetition
is what gets an app muted.

**Response**, verified live on your setup:

```
AryanAg08/mongowrapper#4 has gone 209h without a review; consider pinging the reviewers.
```

**Guards.** The reply is rejected and the plain template used instead if it is
empty, longer than 200 characters, or multi-line. Surrounding quotes are
stripped. The call is capped at 8 seconds, and any error — timeout, bad key,
bad model id, refusal — falls back silently. A late or malformed nudge is worse
than a boring one.

**Why the model is kept on a leash.** Two properties follow from confining it to
wording:

- The noise ceiling cannot drift. A prompt edit cannot raise your 6-a-day cap,
  because the cap is Go, not a prompt.
- A dead provider costs you nothing but prettier wording. Pulse keeps working
  offline, on templates.

**Cost.** One call per nudge, capped at 6 nudges a day, ~500 tokens in and ~40
out. Most cycles never reach this stage.

## Stage 5 — Deliver

- macOS banner via `osascript` (attributed to Script Editor)
- appended to `~/.pulse/nudges.jsonl`
- dedupe key recorded, starting the 20-hour cooldown for that fact

Your reply is the measurement: `pulse ack <id>` opens the PR, `pulse dismiss`
records that you read it and it wasn't useful. Both count as engagement; only
silence counts as failure.

---

## Credential resolution

Checked in order, first match wins:

| | Source |
|---|---|
| 1 | `PULSE_API_KEY` environment variable |
| 2 | `OPENAI_API_KEY` (for `provider: api`) |
| 3 | `phrasing.apiKey` in the config file |

You are using option 3. It works, and Pulse sets the file `0600` when it
contains a key — but the environment is safer, because a config file gets
copied, screen-shared, and pasted into chats. To move it:

```bash
echo 'export PULSE_API_KEY=sk-or-v1-…' >> ~/.zshrc
# then delete the apiKey line from ~/.pulse/config.yaml
```

The launchd agent does **not** inherit your shell environment. If you move the
key to `~/.zshrc`, add it to the plist too, or keep it in the config file — the
background agent is the thing that actually sends nudges:

```bash
launchctl setenv PULSE_API_KEY sk-or-v1-…   # until reboot
```

---

## Verifying it

```bash
pulse status --check
#   phrasing      ● api:openai/gpt-5.6-luna — live round-trip ok
```

`--check` makes a real round-trip. Plain `pulse status` only proves the config
parsed, which is a weaker claim than it looks: it cannot tell a working key from
a missing one, or a valid model id from a typo.

Unknown config fields are now reported rather than ignored:

```
  config        ● unknown keys ignored: PULSE_API_KEY
```

---

## Two failures this setup already hit

Both produced *no error at all* before, which is why they are worth recording.

**1. `PULSE_API_KEY:` used as a config field.** That is an environment variable
name; the config field is `apiKey`. viper ignores unrecognised keys silently, so
the key was never loaded — and because the `api` provider treats a key as
optional (local runtimes need none), Pulse reported a healthy
`● api:openai/gpt-5.6-luna` while sending unauthenticated requests. Fixed, and
unknown keys are now reported.

**2. `openai/flex/gpt-5.6-luna` as the model id.** The real id has no `flex`
segment: `openai/gpt-5.6-luna`. OpenRouter returned *"is not a valid model ID"*,
`Phrase` caught it, fell back to the template, and said nothing — by design,
since a nudge must not be lost to a phrasing failure. `--check` now surfaces
exactly this.

The common thread: silent fallback is right at *delivery* time and wrong at
*setup* time. `--check` is the setup-time counterpart.

---

## Where each stage lives

| Stage | File |
|---|---|
| Collect | `internal/pulse/collect.go`, `git.go`, `github.go`, `focus.go`, `canon.go` |
| Rules | `internal/pulse/rules.go` |
| Arbiter | `internal/pulse/arbiter.go` |
| Phrase | `internal/pulse/phrase.go`, `internal/pulse/llm/` |
| Deliver | `internal/pulse/notify.go`, `store.go` |
| Orchestration | `internal/pulse/cycle.go` |

`rules.go` and `arbiter.go` are deliberately separate files: generating what
could be said is a different concern from deciding what gets said, and the noise
ceiling belongs in exactly one auditable place.
