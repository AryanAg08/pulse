# Pulse — demo video script

**Runtime:** ~5 minutes · **Audience:** developers · **Format:** screen recording with voiceover

Every command here is real and runs against a live account. Nothing is staged,
and nothing you type on camera edits your config — the "before" demo uses
environment overrides, so the running 14-day experiment is never disturbed.

---

## Before you record

- [ ] `cd ~/pulse-go && make build`
- [ ] Terminal at ~16pt, plenty of width — the candidate table is 100 cols
- [ ] **Test that a notification banner actually appears:**
      `osascript -e 'display notification "test" with title "Pulse"'`
      If nothing shows, macOS has not granted **Script Editor** notification
      permission and the payoff shot in Scene 6 will be invisible.
      System Settings → Notifications → Script Editor → Allow.
- [ ] Close Slack/Mail so no foreign banner lands mid-take
- [ ] Have `~/.pulse/config.yaml` open in a second window for Scene 5

---

## Scene 1 — Cold open (0:00–0:20)

**SCREEN:** black, then one line typed slowly.

```
   52 stale_pr   Hacktoberfest2022#886 has sat for 25894h with no review.
```

> **VO:** Twenty-five thousand hours. That's a pull request I opened three years
> ago, and it is exactly the kind of thing a reminder app would cheerfully tell
> me about every single morning until I deleted it.
>
> This is Pulse. Most of what it does is decide *not* to tell you things.

---

## Scene 2 — The problem (0:20–0:55)

**SCREEN:** slow scroll over a few habit-tracker screenshots, or just the VO over black.

> **VO:** Every habit app and every to-do app fails the same way. You open it,
> you type in the thing you already know you should do, and it reads it back to
> you later. The app has no idea what you're actually working on. It only knows
> what you typed.
>
> So the reminders are generic, they're wrong about half the time, and within two
> weeks you've muted it. That's not a design flaw you can fix with a nicer UI. The
> app is structurally ignorant.
>
> Pulse never asks you what to remind you about.

---

## Scene 3 — The inversion (0:55–1:30)

**SCREEN:** type this, let the output land.

```bash
pulse status
```

Expected output — point the cursor at each line as you say it:

```
now
  focus         12m in pulse-go
  open PRs      13 (6 red)
  reviews owed  1
  dirty repos   LogSense:20
  quiet hours   no  (22:30–08:00)
  background    installed
  phrasing      api:gpt-luna
```

> **VO:** It reads your actual work state. Your git repos, your open pull
> requests, which builds are red, who's waiting on your review, and how long
> you've been typing without a break.
>
> None of this was entered by hand. All of it is already sitting on your machine
> and in your GitHub account. Pulse just looks.

---

## Scene 4 — The firehose (1:30–2:30) — *the important scene*

> **VO:** Here's where it gets interesting. Let me turn off the filtering and show
> you what the rules alone would generate.

**SCREEN:**

```bash
PULSE_THRESHOLDS_ABANDONEDAFTERDAYS=3650 \
PULSE_THRESHOLDS_MAXPERKIND=99 \
  pulse run --dry --now
```

Let the full list land. **Do not talk over it.** Twenty candidates.

> **VO:** Twenty. Six red builds, a review I owe someone, and a graveyard of
> stale pull requests going back three years.
>
> If I shipped that, you'd get a notification every twenty minutes for a week and
> then you'd mute me forever. And here's the trap — every one of these is *true*.
> Being right is not the same as being worth an interruption.

**SCREEN:** clear, then the real thing.

```bash
pulse run --dry --now
```

```
candidates (1)
   54 stale_pr   mongowrapper#4 has sat for 209h with no review. Worth a ping.

decision  top candidate (stale_pr, priority 54)
```

> **VO:** One. Same moment, same data, same account. Twenty down to one.
>
> That ratio is the entire product. The rules are the easy part — anyone can
> write a rule that finds a stale PR. The restraint is the hard part.

---

## Scene 5 — How the restraint works (2:30–3:30)

**SCREEN:** `~/.pulse/config.yaml`, scrolling the `thresholds` block.

> **VO:** Nine gates, and they're all boring on purpose.
>
> A pull request older than fourteen days isn't stale, it's dead — silence, permanently.
> That killed the three-year-old Hacktoberfest PRs outright.
>
> Priority decays with age, so the longer something sits the less a ping helps.
>
> Two candidates maximum per category, so a backlog of thirteen PRs can't
> become thirteen notifications dripped out one a day for a fortnight.
>
> One nudge per cycle. Six a day, hard ceiling. Forty-five minutes minimum
> between any two. Quiet hours. And once I've said something, twenty hours of
> silence about it even if it's still true.

**SCREEN:** back to terminal.

```bash
pulse run --dry
```

```
decision  only 30m since last nudge (min 45m)
```

> **VO:** And it tells you exactly why it stayed quiet. Every decision is
> inspectable.

---

## Scene 6 — The nudge itself (3:30–4:00)

**SCREEN:** trigger a real cycle. Frame so the top-right corner is visible for the banner.

```bash
pulse run --verbose
```

> **VO:** When it does speak, it's one sentence, with the numbers in it, and one
> action.

**SCREEN:** `⌘` over to the banner, then:

```bash
pulse ack bc1a7c
```

> **VO:** Ack, and it opens the pull request. Dismiss if it wasn't useful. Both
> get recorded — and that recording is the whole point, which I'll come back to.

---

## Scene 7 — The AI, and what it is *not* allowed to do (4:00–4:30)

**SCREEN:** `docs` or the `internal/pulse/llm/` file tree.

> **VO:** There's a model in here, and it has exactly one job: wording. It takes a
> nudge that the rules have *already* decided to send, and rewrites it so it
> doesn't sound like the same template every time.
>
> It never decides what to send. It never decides whether to send. That stays in
> Go, where I can read it and test it — because if a prompt change could raise
> the noise ceiling, the noise ceiling isn't real.
>
> Point it at Claude, or at any OpenAI-compatible endpoint — your own gateway, a
> local model, whatever. And if it times out or errors or says something
> weird, you get the plain wording instead. A late notification is worse than a
> boring one.

---

## Scene 8 — The honest part (4:30–5:00)

**SCREEN:**

```bash
pulse metrics
```

```
Pulse — the only metric that matters
  day             1 of 14
  nudges sent     2 (2.0/active day)
  engaged         50%  (ack or dismiss)
  ignored         50%

  verdict  Day 1 of 14. Too early to call.
```

> **VO:** Last thing, and this is the part I'd want to hear if you were showing
> this to me.
>
> I don't know if this works yet. Not "will people like it" — whether a developer
> will still *accept interruptions from software* two weeks in. Every notification
> product dies at exactly that question.
>
> So Pulse measures itself. Fourteen days. If I've muted it by then, that's the
> answer, and the honest move is to stop. It'll tell me that in those words.
>
> The code is on GitHub. If you run it, don't tune it to be nicer halfway through
> — the annoyance is the data.

**SCREEN:** repo URL, hold 3 seconds.

---

## Notes

- **Tone:** understated. The product's whole argument is restraint; overselling
  it on camera contradicts the thesis.
- **Scene 4 is the video.** If you cut anything, cut Scene 2 and let Scene 4
  breathe. The 20→1 moment is the only thing people will remember.
- **Don't fake a nudge.** If nothing is due when you record, say so on camera —
  "it's got nothing to say right now, which is most of the time" is a stronger
  line than a staged notification.
- The env-override trick in Scene 4 changes nothing on disk. Safe to run live.
