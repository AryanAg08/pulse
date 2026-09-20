package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFactoryRejectsIncompleteAPIConfig(t *testing.T) {
	t.Setenv("PULSE_API_KEY", "k")
	for _, tc := range []struct {
		name string
		cfg  Config
		want string
	}{
		{"no url", Config{Provider: ProviderAPI, Model: "gpt-luna"}, "apiUrl"},
		{"no model", Config{Provider: ProviderAPI, APIURL: "http://x"}, "modelName"},
		{"unknown provider", Config{Provider: "wat"}, "unknown provider"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.cfg)
			if err == nil {
				t.Fatal("want an error, got none")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error should name the missing field %q, got %q", tc.want, err)
			}
		})
	}
}

func TestFactoryDefaultsToAnthropic(t *testing.T) {
	t.Setenv("PULSE_API_KEY", "k")
	p, err := New(Config{})
	if err != nil {
		t.Fatalf("empty config should default, got %v", err)
	}
	// The display name is deliberately uniform, so assert on the concrete type
	// and its configured model rather than on the label.
	ap, ok := p.(*anthropicProvider)
	if !ok {
		t.Fatalf("empty config should default to the Anthropic provider, got %T", p)
	}
	if ap.model != "claude-opus-5" {
		t.Fatalf("want claude-opus-5, got %q", ap.model)
	}
}

func TestAnthropicNeedsAKey(t *testing.T) {
	// A missing key must be an error, not a provider that fails on every call.
	t.Setenv("PULSE_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	if _, err := New(Config{Provider: ProviderAnthropic}); err == nil {
		t.Fatal("want an error when no key is available")
	}
}

func TestEnvKeyBeatsConfigFile(t *testing.T) {
	t.Setenv("PULSE_API_KEY", "from-env")
	p, err := New(Config{Provider: ProviderAPI, APIURL: "http://x", Model: "m", APIKey: "from-file"})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.(*apiProvider).apiKey; got != "from-env" {
		t.Fatalf("env must win so secrets need not touch disk, got %q", got)
	}
}

// fakeAPI stands in for any OpenAI-compatible endpoint.
func fakeAPI(t *testing.T, status int, body string, capture *http.Request) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			*capture = *r.Clone(r.Context())
			raw, _ := io.ReadAll(r.Body)
			capture.Body = io.NopCloser(strings.NewReader(string(raw)))
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestAPIProviderHappyPath(t *testing.T) {
	var got http.Request
	srv := fakeAPI(t, 200, `{"choices":[{"message":{"content":"CI is red on app#12."}}],
		"usage":{"prompt_tokens":11,"completion_tokens":7}}`, &got)
	defer srv.Close()

	p, err := New(Config{Provider: ProviderAPI, APIURL: srv.URL + "/v1", Model: "gpt-luna", APIKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Call(context.Background(), "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "CI is red on app#12." {
		t.Fatalf("unexpected text %q", resp.Text)
	}
	if resp.TokensIn != 11 || resp.TokensOut != 7 {
		t.Fatalf("usage not parsed: in=%d out=%d", resp.TokensIn, resp.TokensOut)
	}
	if got.URL.Path != "/v1/chat/completions" {
		t.Fatalf("path should be appended to the base URL, got %q", got.URL.Path)
	}
	if auth := got.Header.Get("Authorization"); auth != "Bearer secret" {
		t.Fatalf("bad auth header %q", auth)
	}

	var sent oaiRequest
	raw, _ := io.ReadAll(got.Body)
	if err := json.Unmarshal(raw, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Model != "gpt-luna" {
		t.Fatalf("model must pass through verbatim, got %q", sent.Model)
	}
	if len(sent.Messages) != 2 || sent.Messages[0].Role != "system" {
		t.Fatalf("want a system then user message, got %+v", sent.Messages)
	}
}

func TestAPIProviderAcceptsFullPathBaseURL(t *testing.T) {
	var got http.Request
	srv := fakeAPI(t, 200, `{"choices":[{"message":{"content":"x"}}]}`, &got)
	defer srv.Close()

	p, _ := New(Config{Provider: ProviderAPI, APIURL: srv.URL + "/v1/chat/completions", Model: "m"})
	if _, err := p.Call(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	if got.URL.Path != "/v1/chat/completions" {
		t.Fatalf("path must not be doubled, got %q", got.URL.Path)
	}
}

func TestAPIProviderSurfacesFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       string
	}{
		{"error field", `{"error":{"message":"bad model"}}`, 400, "bad model"},
		{"bare non-2xx", `upstream exploded`, 502, "502"},
		{"no choices", `{"choices":[]}`, 200, "no choices"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := fakeAPI(t, tc.status, tc.body, nil)
			defer srv.Close()
			p, _ := New(Config{Provider: ProviderAPI, APIURL: srv.URL, Model: "m"})
			_, err := p.Call(context.Background(), "s", "u")
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error should mention %q, got %q", tc.want, err)
			}
		})
	}
}

func TestAPIProviderOmitsAuthWhenNoKey(t *testing.T) {
	// A local runtime (ollama, llama.cpp) needs no credential; sending an empty
	// bearer token makes some of them reject the request outright.
	var got http.Request
	srv := fakeAPI(t, 200, `{"choices":[{"message":{"content":"x"}}]}`, &got)
	defer srv.Close()
	t.Setenv("PULSE_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	p, _ := New(Config{Provider: ProviderAPI, APIURL: srv.URL, Model: "local"})
	if _, err := p.Call(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	if got.Header.Get("Authorization") != "" {
		t.Fatal("no key means no Authorization header")
	}
}
