// Package llm phrases nudges through a pluggable AI backend.
//
// The model only ever rewords a nudge that the rules have already decided is
// worth sending. It never decides what to send or whether to send it, so the
// noise ceiling stays in auditable Go regardless of which provider is wired up.
package llm

import (
	"context"
	"os"
)

// Response is the normalized response from any provider.
type Response struct {
	Text      string
	TokensIn  int64
	TokensOut int64
}

// Provider is the interface every AI backend must satisfy.
type Provider interface {
	Call(ctx context.Context, systemPrompt, userMessage string) (Response, error)
	// Name is the display label. Every backend reports the same product name
	// rather than its model id — see DisplayName.
	Name() string
}

// DisplayName is what the user sees wherever phrasing is attributed. Which
// model is behind it is an implementation detail: it changes without notice,
// it is not the user's decision, and a dashboard is the sort of thing that
// gets screen-shared. The configured value stays in the config file, and
// PULSE_SHOW_MODEL=1 reveals it when actually debugging a provider.
const DisplayName = "pulse-ai"

func label(model string) string {
	if os.Getenv("PULSE_SHOW_MODEL") != "" {
		return DisplayName + " (" + model + ")"
	}
	return DisplayName
}

// Provider identifiers — the `provider` value in config.yaml.
const (
	ProviderAnthropic = "anthropic"
	// ProviderAPI is any OpenAI-compatible /chat/completions endpoint:
	// OpenAI, Groq, DeepSeek, OpenRouter, a private gateway, a local runtime.
	ProviderAPI = "api"
)

// Config selects and configures a provider. It is deliberately independent of
// the main pulse config so this package can be reused without importing it.
type Config struct {
	Provider string
	Model    string
	APIKey   string
	APIURL   string
}
