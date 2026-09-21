# Running Pulse on your Mac

Start-to-finish setup, assuming nothing is installed. Roughly ten minutes,
most of it waiting on Homebrew.

---

## 1. Prerequisites

```bash
# Homebrew, if you don't have it
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

brew install go gh
```

Pulse needs **Go 1.25 or newer** — check with `go version`. It reads the
required version from `go.mod`, so an older toolchain refuses to build rather
than failing strangely later.

## 2. Authenticate GitHub

Pulse gets all GitHub data through the `gh` CLI, so it never handles a token and
needs no OAuth app. It reads your PRs and review queue; it never writes.

```bash
gh auth login        # choose HTTPS, authenticate in the browser
gh auth status       # confirm: "Logged in to github.com account <you>"
```

Without this, Pulse still runs — it just falls back to local git signals only.

## 3. Install

Once the tap is published, this is the whole install. **The repository must be
public first** — `brew` fetches the release tarball anonymously, and a private
repo returns 404 to anonymous requests. See `dist/homebrew/` for the formula
and `make formula` for generating it.

```bash
brew install AryanAg08/tap/pulse
pulse init
pulse doctor
```

`brew` pulls in `gh` as a dependency, puts `pulse` on your PATH, and prints the
notification-permission caveat. Skip to step 4.

### Building from source instead

```bash
git clone https://github.com/AryanAg08/pulse.git ~/pulse-go
cd ~/pulse-go
make install
```

`make install` builds the binary and symlinks it into `/opt/homebrew/bin`. If
that directory is not on your PATH (Intel Macs use `/usr/local/bin`):

```bash
make install PREFIX=/usr/local
```

Verify:

```bash
pulse          # prints the command list
```

> **If you get `zsh: command not found: pulse`**, the symlink target is not on
> your PATH. Check with `echo $PATH | tr ':' '\n' | grep bin`, then re-run
> `make install PREFIX=<a directory on that list>`.
>
> Note that `npm link`-style global installs and `go install` both fail on a
> Homebrew prefix without root; the symlink above avoids that entirely.

## 4. Configure

```bash
pulse init
```

A short questionnaire. It detects your GitHub login from `gh` and looks for
repos in the usual places (`~/code`, `~/src`, `~/dev`, `~/projects`, `~/work`,
`~/repos`, `~/Developer`, `~/github`), then asks about:

- **your working day** — quiet hours are derived from it, not asked separately
- **routines** — stand-up, gym, posture breaks, or anything you name
- **how much it may talk** — quiet (3/day), normal (6/day), or chatty (10/day)
- **wording** — whether to use `pulse-ai`, optional and off by default

Enter accepts every default and **Escape goes back a question**, so a mistyped
answer costs one keystroke rather than a restart. Times accept `9`, `09:00`,
`0900` or `19.30`; days are a tick list.

The one answer worth getting right is where your code lives — if it falls back
to your home directory the scan is slower than it needs to be.

`pulse init --yes` takes defaults without asking, for a scripted install.
`pulse init --force` re-runs the questionnaire later.

For every available option, copy the documented reference instead:

```bash
cp example.yml ~/.pulse/config.yml
```

## 5. Turn on notifications — *do not skip this*

Pulse posts banners through AppleScript, which macOS attributes to **Script
Editor**. If that app has never been granted notification permission, every
nudge is silently logged and never shown, and Pulse will look like it does
nothing.

Test it:

```bash
osascript -e 'display notification "test" with title "Pulse"'
```

**If no banner appears:** System Settings → Notifications → **Script Editor** →
Allow Notifications. Set the style to **Alerts** rather than Banners so nudges
persist instead of vanishing after a few seconds.

Note that `osascript` exits 0 whether or not the banner renders, so this visual
check is the only reliable test.

## 6. Check the install

```bash
pulse doctor
```

It verifies the binary is on your PATH, the config parses, no config keys are
being silently ignored, repos are discoverable, `gh` is installed and
authenticated, GitHub queries work, `pulse-ai` can actually be reached, the
background agent is loaded, and that a notification banner renders.

Every check names the exact command that fixes it. The notification check is
the one that needs you: it posts a banner and asks whether you saw it, because
macOS gives no way to detect that from code.

```
  ● binary             /opt/homebrew/bin/pulse
  ● github auth        AryanAg08
  ! pulse-ai           provider error: no credentials
    nudges still work with fixed wording; check phrasing.* and PULSE_API_KEY
  ● background agent   installed and loaded
```

## 7. See what it would do

```bash
pulse status            # what it sees right now, and what it's holding back
pulse browse            # dashboard: repositories · metrics · config
pulse run --dry --now   # full reasoning: signals, candidates, decision
```

`pulse browse` needs a real terminal — it will tell you so rather than failing
strangely if you pipe its output.

`--dry` sends nothing. `--now` additionally ignores quiet hours and rate limits
so you can preview the decision at any hour.

To see why the filtering matters, compare against the unfiltered rules:

```bash
PULSE_THRESHOLDS_ABANDONEDAFTERDAYS=3650 PULSE_THRESHOLDS_MAXPERKIND=99 \
  pulse run --dry --now
```

Neither command writes to your config.

## 8. Run it for real

```bash
pulse start          # foreground, in this terminal — good for a first hour
pulse daemon         # install as a launchd agent, survives reboots
```

`pulse daemon` is the one you want for actual use. Remove it any time with
`pulse daemon --uninstall`. Logs land in `~/.pulse/agent.log`.

## 9. Optional — AI phrasing

Without this, Pulse uses fixed wording and works completely offline.

```bash
export PULSE_API_KEY=sk-...        # add to ~/.zshrc to persist
```

Then set a provider in `~/.pulse/config.yaml`:

```yaml
phrasing:
  useLLM: true
  provider: anthropic
  modelName: claude-opus-5
```

or any OpenAI-compatible endpoint:

```yaml
phrasing:
  useLLM: true
  provider: api
  apiUrl: https://your-gateway.example/v1
  modelName: gpt-luna
```

Confirm it resolved:

```bash
pulse status | grep phrasing
#   phrasing      ● api:gpt-luna
```

If it shows `templates — <reason>`, the reason tells you what is missing.

---

## Developing in GoLand

Open `~/pulse-go` as a project. Six run/debug configurations are committed in
`.idea/runConfigurations/` and appear in the dropdown automatically:

| Configuration | What it does |
|---|---|
| `run --dry --now` | The main one. Breakpoint `GenerateCandidates` or `Arbitrate` and step the decision. |
| `demo: no filters` | Same, with the abandonment cutoff and per-kind caps lifted via env. |
| `status` | Signal collection only. |
| `metrics` | The day-14 verdict path. |
| `run --verbose` | A real cycle — **this one can send a notification.** |
| `browse (TUI)` | The interactive browser. **Note:** the GoLand run console is not a terminal, so the TUI refuses to start there — run `pulse browse` in a real shell and attach the debugger, or debug `internal/pulse/tui` through `All tests` instead. |
| `All tests` | Whole suite; breakpoints inside tests work. |

All of them set `CLICOLOR_FORCE=1`, because the GoLand run console is not a TTY
and styling would otherwise switch itself off.

**Useful breakpoints:**

- `internal/pulse/rules.go` → `GenerateCandidates` — what *could* be said
- `internal/pulse/arbiter.go` → `Arbitrate` — what actually gets said, and why
- `internal/pulse/phrase.go` → `Phrase` — the AI hand-off
- `internal/pulse/collect.go` → `CollectSignals` — inspect a whole snapshot
- `internal/pulse/tui/update.go` → `handleKey` — browser navigation

If Delve fails to start with a code-signing error, run `xcode-select --install`.

Command line equivalent:

```bash
go install github.com/go-delve/delve/cmd/dlv@latest
dlv debug . -- run --dry --now
```

---

## Troubleshooting

Run `pulse doctor` first — it diagnoses everything in this table and names the
fix. The table is here for the cases where doctor itself will not run.

| Symptom | Cause and fix |
|---|---|
| `command not found: pulse` | Install prefix not on PATH — see step 3. |
| No notifications ever appear | Script Editor lacks permission — see step 5. |
| `github: exit status 4` | `gh` not authenticated, or the token lost its scopes. Re-run `gh auth login`. |
| `focus` always `0m` | Focus is inferred from consecutive samples; a cycle interval above 10 minutes never accumulates. Use the default. Reading code also reads as idle, since it is inferred from file mtimes. |
| Nothing is ever nudged | Usually correct behaviour. Run `pulse run --dry --now` — it prints exactly which gate suppressed what. |
| Too many nudges | Lower `maxNudgesPerDay`, raise `minMinutesBetweenNudges`, or `pulse mute <kind>`. |
| Want it to stop now | `pulse mute --hours=4`, or `pulse daemon --uninstall`. |

**A note if you are running the 14-day experiment:** resist tuning the
thresholds partway through. How annoying it is *is* the measurement — adjusting
mid-run replaces the result with a number about your patience for configuration.
