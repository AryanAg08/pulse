package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// anthropicProvider calls Claude through the official SDK.
type anthropicProvider struct {
	client anthropic.Client
	model  string
}

func (p *anthropicProvider) Name() string { return label(p.model) }

func (p *anthropicProvider) Call(ctx context.Context, systemPrompt, userMessage string) (Response, error) {
	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(p.model),
		MaxTokens: 2000,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}},
		// Rewording one sentence needs no deep reasoning; low effort keeps it
		// fast and cheap, which matters when this runs every cycle forever.
		OutputConfig: anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffortLow},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userMessage)),
		},
	})
	if err != nil {
		return Response{}, err
	}
	// A refusal is a normal 200 response, not an error — check before reading text.
	if resp.StopReason == "refusal" {
		return Response{}, fmt.Errorf("model declined to answer")
	}

	var text strings.Builder
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(tb.Text)
		}
	}
	return Response{
		Text:      text.String(),
		TokensIn:  resp.Usage.InputTokens,
		TokensOut: resp.Usage.OutputTokens,
	}, nil
}

func newAnthropic(cfg Config) Provider {
	var opts []option.RequestOption
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}
	// A custom URL lets an Anthropic-compatible proxy or gateway be used.
	if cfg.APIURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.APIURL))
	}
	return &anthropicProvider{client: anthropic.NewClient(opts...), model: cfg.Model}
}
