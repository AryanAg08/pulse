package llm

import (
	"strings"
	"testing"
)

func TestProviderNameHidesTheModel(t *testing.T) {
	t.Setenv("PULSE_SHOW_MODEL", "")
	t.Setenv("PULSE_API_KEY", "k")

	for _, cfg := range []Config{
		{Provider: ProviderAPI, APIURL: "http://x", Model: "vendor/some-model"},
		{Provider: ProviderAnthropic, Model: "claude-opus-5"},
	} {
		p, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if p.Name() != DisplayName {
			t.Errorf("want %q, got %q", DisplayName, p.Name())
		}
		if strings.Contains(p.Name(), cfg.Model) {
			t.Errorf("provider name leaked the model id: %q", p.Name())
		}
	}
}

func TestShowModelEscapeHatchIsOptIn(t *testing.T) {
	// Debugging a provider needs the real id; everything else does not.
	t.Setenv("PULSE_API_KEY", "k")
	t.Setenv("PULSE_SHOW_MODEL", "1")
	p, _ := New(Config{Provider: ProviderAPI, APIURL: "http://x", Model: "some/model"})
	if !strings.Contains(p.Name(), "some/model") {
		t.Fatalf("PULSE_SHOW_MODEL should reveal the id, got %q", p.Name())
	}
	if !strings.HasPrefix(p.Name(), DisplayName) {
		t.Fatalf("the product name should still lead, got %q", p.Name())
	}
}
