package pulse

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"pulse/internal/pulse/llm"
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

// llmConfig maps the user-facing phrasing block onto the llm package's config.
func llmConfig(cfg Config) llm.Config {
	return llm.Config{
		Provider: cfg.Phrasing.Provider,
		Model:    cfg.Phrasing.ResolvedModel(),
		APIKey:   cfg.Phrasing.APIKey,
		APIURL:   cfg.Phrasing.APIURL,
	}
}

// PhraseProvider reports which backend phrasing would use, or the reason it is
// unavailable. `pulse status` surfaces this so a misconfigured provider is
// visible rather than silently degrading to templates forever.
func PhraseProvider(cfg Config) (string, error) {
	if !cfg.Phrasing.UseLLM {
		return "", nil
	}
	p, err := llm.New(llmConfig(cfg))
	if err != nil {
		return "", err
	}
	return p.Name(), nil
}

// VerifyPhrasing makes one real round-trip. Resolving a provider only proves
// the config parsed; it says nothing about whether the credential is accepted
// or the model id exists, which are the two things that actually go wrong.
func VerifyPhrasing(cfg Config) (string, error) {
	if !cfg.Phrasing.UseLLM {
		return "", fmt.Errorf("useLLM is false")
	}
	provider, err := llm.New(llmConfig(cfg))
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), phraseTimeout*2)
	defer cancel()

	resp, err := provider.Call(ctx,
		"Reply with exactly the word: ok", "Reply with exactly the word: ok")
	if err != nil {
		return provider.Name(), err
	}
	if strings.TrimSpace(resp.Text) == "" {
		return provider.Name(), fmt.Errorf("provider returned empty text")
	}
	return provider.Name(), nil
}

func buildPrompt(c Candidate, recent []Nudge) string {
	facts, err := json.Marshal(c.Facts)
	if err != nil {
		facts = []byte("{}")
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
	return b.String()
}

// cleanPhrasing rejects output that would be worse than the template it
// replaced: empty, over-long, or multi-line. Some models wrap a single line in
// quotes despite the instruction, which is cosmetic and worth stripping rather
// than discarding the whole response over.
func cleanPhrasing(raw string) (string, bool) {
	text := strings.TrimSpace(raw)
	text = strings.Trim(text, "\"'")
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 200 || strings.Contains(text, "\n") {
		return "", false
	}
	return text, true
}

// Phrase rewords a candidate. The model only phrases — it never decides what to
// send or whether to send it, so the noise ceiling stays in auditable code.
// Every failure path falls back to the deterministic template.
func Phrase(cfg Config, c Candidate, recent []Nudge) PhraseResult {
	fallback := PhraseResult{Text: c.Text, By: "template"}
	if !cfg.Phrasing.UseLLM {
		return fallback
	}

	provider, err := llm.New(llmConfig(cfg))
	if err != nil {
		return fallback
	}

	ctx, cancel := context.WithTimeout(context.Background(), phraseTimeout)
	defer cancel()

	resp, err := provider.Call(ctx, phraseSystem, buildPrompt(c, recent))
	if err != nil {
		return fallback
	}
	text, ok := cleanPhrasing(resp.Text)
	if !ok {
		return fallback
	}
	return PhraseResult{Text: text, By: "llm"}
}
