// Package llm phrases nudges through a pluggable AI backend.
//
// The model only ever rewords a nudge that the rules have already decided is
// worth sending. It never decides what to send or whether to send it, so the
// noise ceiling stays in auditable Go regardless of which provider is wired up.
package llm

import "context"

// Response is the normalized response from any provider.
type Response struct {
	Text      string
	TokensIn  int64
	TokensOut int64
}

// Provider is the interface every AI backend must satisfy.
type Provider interface {
	Call(ctx context.Context, systemPrompt, userMessage string) (Response, error)
	// Name returns the provider:model string, used in logs and `pulse log`.
	Name() string
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
