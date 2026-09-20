package llm

import (
	"fmt"
	"os"
	"strings"
)

// New builds the configured provider.
//
// Credentials resolve environment-first, so a key never has to be written to
// disk: PULSE_API_KEY, then the provider's conventional variable, then the
// config file. It returns an error rather than a nil provider when a required
// field is missing, so misconfiguration surfaces at `pulse run --dry` instead
// of silently falling back to templates forever.
func New(cfg Config) (Provider, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = ProviderAnthropic
	}
	cfg.APIKey = resolveKey(provider, cfg.APIKey)

	switch provider {
	case ProviderAnthropic:
		if cfg.Model == "" {
			cfg.Model = "claude-opus-5"
		}
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("no API key: set ANTHROPIC_API_KEY, PULSE_API_KEY, or phrasing.apiKey")
		}
		return newAnthropic(cfg), nil

	case ProviderAPI:
		if cfg.APIURL == "" {
			return nil, fmt.Errorf("provider %q requires phrasing.apiUrl", provider)
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf("provider %q requires phrasing.modelName", provider)
		}
		return &apiProvider{baseURL: cfg.APIURL, apiKey: cfg.APIKey, model: cfg.Model}, nil

	default:
		return nil, fmt.Errorf("unknown provider %q (want %q or %q)", provider, ProviderAnthropic, ProviderAPI)
	}
}

// resolveKey prefers the environment over the config file, so the documented
// setup never requires a plaintext secret in ~/.pulse/config.yaml.
func resolveKey(provider, fromConfig string) string {
	if k := os.Getenv("PULSE_API_KEY"); k != "" {
		return k
	}
	if provider == ProviderAnthropic {
		if k := os.Getenv("ANTHROPIC_API_KEY"); k != "" {
			return k
		}
		if k := os.Getenv("ANTHROPIC_AUTH_TOKEN"); k != "" {
			return k
		}
	} else if k := os.Getenv("OPENAI_API_KEY"); k != "" {
		return k
	}
	return fromConfig
}
