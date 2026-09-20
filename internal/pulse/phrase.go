package pulse

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

const phraseSystem = `You phrase notifications for Pulse, an assistant that watches a developer's real work state.

You are given a fact that has ALREADY been judged worth interrupting for. Your only job is wording.
Rules:
- One sentence, under 140 characters. It appears in a macOS notification.
- Lead with the concrete fact (numbers, repo, PR number). Never open with "Hey" or "Just a reminder".
- Never invent detail that is not in the facts you were given.
- No emoji. No exclamation marks. No motivational language.
- Vary the phrasing from the recent nudges you are shown; repetition is what gets an app muted.
- Reply with the sentence only. No preamble, no quotes around it.`

// phraseTimeout: a nudge that arrives late is worse than one that arrives plain.
const phraseTimeout = 8 * time.Second

type PhraseResult struct {
	Text string
	By   string // llm | template
}

func hasCredentials() bool {
	return os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("ANTHROPIC_AUTH_TOKEN") != ""
}

// Phrase rewords a candidate. The model only phrases — it never decides what to
// send or whether to send it, so the noise ceiling stays in auditable code.
// Any failure falls back to the deterministic template rather than shipping
// something worse or later.
func Phrase(cfg Config, c Candidate, recent []Nudge) PhraseResult {
	fallback := PhraseResult{Text: c.Text, By: "template"}
	if !cfg.Phrasing.UseLLM || !hasCredentials() {
		return fallback
	}

	facts, err := json.Marshal(c.Facts)
	if err != nil {
		return fallback
	}

	var b strings.Builder
	b.WriteString("Nudge kind: " + string(c.Kind) + "\n")
	b.WriteString("Facts: " + string(facts) + "\n")
	b.WriteString("Plain version (rephrase this, keep every number): " + c.Text + "\n")
	if len(recent) == 0 {
		b.WriteString("No recent nudges.")
	} else {
		b.WriteString("Recent nudges to avoid echoing:\n")
		for _, n := range recent {
			b.WriteString("- " + n.Text + "\n")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), phraseTimeout)
	defer cancel()

	client := anthropic.NewClient()
	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(cfg.Phrasing.Model),
		MaxTokens: 2000,
		System:    []anthropic.TextBlockParam{{Text: phraseSystem}},
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}},
		// Rewording one sentence needs no deep reasoning; low effort keeps it
		// fast and cheap, which matters when this runs every cycle forever.
		OutputConfig: anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffortLow},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(b.String())),
		},
	})
	if err != nil {
		return fallback
	}
	// A refusal is a normal 200 response, not an error — check before reading text.
	if resp.StopReason == "refusal" {
		return fallback
	}

	var text string
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text += tb.Text
		}
	}
	text = strings.TrimSpace(text)

	// An over-long or multi-line answer is worse than the template it replaced.
	if text == "" || len(text) > 200 || strings.Contains(text, "\n") {
		return fallback
	}
	return PhraseResult{Text: text, By: "llm"}
}
