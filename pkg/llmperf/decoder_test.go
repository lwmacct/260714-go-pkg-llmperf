package llmperf

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"testing"
	"time"
)

func TestJSONProvidesTransportMetricsOnly(t *testing.T) {
	start := time.Unix(1000, 0)
	decoder, err := NewDecoder(Options{Protocol: ProtocolOpenAIResponses, Format: FormatJSON, RequestStartedAt: start})
	if err != nil {
		t.Fatal(err)
	}
	if err = decoder.ResponseHeadersAt(start.Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	updates, err := decoder.FeedAt(start.Add(200*time.Millisecond), []byte(`{"output":[{"text":"hello"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 || updates[0].Kind != UpdateFirstByte {
		t.Fatalf("unexpected updates: %#v", updates)
	}
	result, err := decoder.FinishAt(Completion{
		At: start.Add(time.Second), Outcome: OutcomeCompleted,
		TokenCount: &TokenCount{OutputTokens: 10, Basis: TokenBasisProviderReported, Scope: TokenScopeProviderOutput},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Metrics.ResponseHeaderLatency.Value != 100*time.Millisecond || result.Metrics.TimeToFirstByte.Value != 200*time.Millisecond || result.Metrics.EndToEndLatency.Value != time.Second {
		t.Fatalf("unexpected transport metrics: %#v", result.Metrics)
	}
	if result.Metrics.TimeToFirstOutput.Reason != UnavailableNonStreaming || result.Metrics.TimePerOutputToken.Reason != UnavailableNonStreaming {
		t.Fatalf("JSON semantic metrics must be unavailable: %#v", result.Metrics)
	}
	if !result.Metrics.EndToEndTokensPerSecond.Available || result.Metrics.EndToEndTokensPerSecond.TokensPerSecond != 10 {
		t.Fatalf("unexpected E2E TPS: %#v", result.Metrics.EndToEndTokensPerSecond)
	}
}

func TestOpenAIResponsesTimelineAndRates(t *testing.T) {
	start := time.Unix(2000, 0)
	decoder, err := NewDecoder(Options{Protocol: ProtocolOpenAIResponses, Format: FormatSSE, RequestStartedAt: start})
	if err != nil {
		t.Fatal(err)
	}
	if err = decoder.ResponseHeadersAt(start.Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	chunks := []struct {
		at   time.Time
		data string
	}{
		{start.Add(150 * time.Millisecond), `: heartbeat

`},
		{start.Add(500 * time.Millisecond), `event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"Hello"}

`},
		{start.Add(1500 * time.Millisecond), `event: response.completed
data: {"type":"response.completed","response":{}}

`},
	}
	var updates []Update
	for _, chunk := range chunks {
		part, feedErr := decoder.FeedAt(chunk.at, []byte(chunk.data))
		if feedErr != nil {
			t.Fatal(feedErr)
		}
		updates = append(updates, part...)
	}
	if len(updates) != 4 || updates[0].Kind != UpdateFirstByte || updates[1].Kind != UpdateFirstOutput || updates[2].Kind != UpdateFirstText || updates[3].Kind != UpdateGenerationCompleted {
		t.Fatalf("unexpected updates: %#v", updates)
	}
	result, err := decoder.FinishAt(Completion{
		At: start.Add(1600 * time.Millisecond), Outcome: OutcomeCompleted,
		TokenCount: &TokenCount{OutputTokens: 11, Basis: TokenBasisProviderReported, Scope: TokenScopeProviderOutput},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Terminal != TerminalCompleted || result.Metrics.TimeToFirstByte.Value != 150*time.Millisecond || result.Metrics.TimeToFirstOutput.Value != 500*time.Millisecond || result.Metrics.TimeToFirstText.Value != 500*time.Millisecond {
		t.Fatalf("unexpected timeline result: %#v", result)
	}
	if result.Metrics.GenerationDuration.Value != time.Second || result.Metrics.TimePerOutputToken.Value != 100*time.Millisecond {
		t.Fatalf("unexpected generation metrics: %#v", result.Metrics)
	}
	if math.Abs(result.Metrics.GenerationTokensPerSecond.TokensPerSecond-10) > 1e-9 {
		t.Fatalf("unexpected generation TPS: %#v", result.Metrics.GenerationTokensPerSecond)
	}
}

func TestProtocolFixturesAcrossEveryChunkBoundary(t *testing.T) {
	start := time.Unix(3000, 0)
	tests := []struct {
		name      string
		protocol  Protocol
		path      string
		firstKind OutputKind
		terminal  TerminalState
	}{
		{"responses", ProtocolOpenAIResponses, "testdata/openai-responses/text.sse", OutputText, TerminalCompleted},
		{"chat", ProtocolOpenAIChatCompletions, "testdata/openai-chat-completions/text.sse", OutputText, TerminalCompleted},
		{"anthropic", ProtocolAnthropicMessages, "testdata/anthropic-messages/text.sse", OutputText, TerminalCompleted},
		{"google", ProtocolGoogleGenerateContent, "testdata/google-generate-content/text.sse", OutputReasoning, TerminalCompleted},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			for split := 0; split <= len(data); split++ {
				decoder, newErr := NewDecoder(Options{Protocol: test.protocol, Format: FormatSSE, RequestStartedAt: start})
				if newErr != nil {
					t.Fatal(newErr)
				}
				if _, newErr = decoder.FeedAt(start.Add(time.Second), data[:split]); newErr != nil {
					t.Fatalf("split %d first feed: %v", split, newErr)
				}
				if _, newErr = decoder.FeedAt(start.Add(2*time.Second), data[split:]); newErr != nil {
					t.Fatalf("split %d second feed: %v", split, newErr)
				}
				result, finishErr := decoder.FinishAt(Completion{At: start.Add(3 * time.Second), Outcome: OutcomeCompleted})
				if finishErr != nil {
					t.Fatalf("split %d finish: %v", split, finishErr)
				}
				if !result.Milestones.FirstOutput.Observed || result.Milestones.FirstOutput.OutputKind != test.firstKind || !result.Milestones.FirstText.Observed || result.Terminal != test.terminal {
					t.Fatalf("split %d result: %#v", split, result)
				}
			}
		})
	}
}

func TestProtocolAutoResolvesAllFixtures(t *testing.T) {
	start := time.Unix(4000, 0)
	tests := []struct {
		protocol Protocol
		path     string
	}{
		{ProtocolOpenAIResponses, "testdata/openai-responses/text.sse"},
		{ProtocolOpenAIChatCompletions, "testdata/openai-chat-completions/text.sse"},
		{ProtocolAnthropicMessages, "testdata/anthropic-messages/text.sse"},
		{ProtocolGoogleGenerateContent, "testdata/google-generate-content/text.sse"},
	}
	for _, test := range tests {
		data, err := os.ReadFile(test.path)
		if err != nil {
			t.Fatal(err)
		}
		decoder, err := NewDecoder(Options{Protocol: ProtocolAuto, Format: FormatSSE, RequestStartedAt: start})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = decoder.FeedAt(start.Add(time.Second), data); err != nil {
			t.Fatal(err)
		}
		result, err := decoder.FinishAt(Completion{At: start.Add(2 * time.Second), Outcome: OutcomeCompleted})
		if err != nil {
			t.Fatal(err)
		}
		if result.Protocol != test.protocol {
			t.Fatalf("got %q, want %q", result.Protocol, test.protocol)
		}
	}
}

func TestToolOnlyAndVisibleTokenScope(t *testing.T) {
	start := time.Unix(5000, 0)
	decoder, err := NewDecoder(Options{Protocol: ProtocolAnthropicMessages, Format: FormatSSE, RequestStartedAt: start})
	if err != nil {
		t.Fatal(err)
	}
	stream := `event: content_block_delta
data: {"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"x\":"}}

event: message_stop
data: {"type":"message_stop"}

`
	if _, err = decoder.FeedAt(start.Add(time.Second), []byte(stream)); err != nil {
		t.Fatal(err)
	}
	result, err := decoder.FinishAt(Completion{
		At: start.Add(2 * time.Second), Outcome: OutcomeCompleted,
		TokenCount: &TokenCount{OutputTokens: 3, Basis: TokenBasisTokenizerCounted, Scope: TokenScopeVisibleText},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Milestones.FirstOutput.OutputKind != OutputTool || result.Milestones.FirstText.Observed {
		t.Fatalf("unexpected tool timeline: %#v", result.Milestones)
	}
	if result.Metrics.TimePerOutputToken.Reason != UnavailableMissingFirstText {
		t.Fatalf("unexpected visible scope metric: %#v", result.Metrics.TimePerOutputToken)
	}
}

func TestLifecycleTimestampAndStickyErrors(t *testing.T) {
	start := time.Unix(6000, 0)
	if _, err := NewDecoder(Options{Protocol: ProtocolAuto, Format: FormatJSON, RequestStartedAt: start}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected unsupported auto JSON, got %v", err)
	}
	decoder, err := NewDecoder(Options{Protocol: ProtocolOpenAIResponses, Format: FormatSSE, RequestStartedAt: start})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decoder.FeedAt(start.Add(time.Second), []byte("data: {\n\n")); !errors.Is(err, ErrMalformedStream) {
		t.Fatalf("expected malformed stream, got %v", err)
	}
	if _, nextErr := decoder.FeedAt(start.Add(2*time.Second), []byte("data: {}\n\n")); nextErr != err && nextErr.Error() != err.Error() {
		t.Fatalf("terminal error not sticky: %v / %v", err, nextErr)
	}

	decoder, err = NewDecoder(Options{Protocol: ProtocolOpenAIResponses, Format: FormatJSON, RequestStartedAt: start})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decoder.FeedAt(start.Add(time.Second), []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err = decoder.ResponseHeadersAt(start.Add(2 * time.Second)); !errors.Is(err, ErrInvalidLifecycle) {
		t.Fatalf("expected lifecycle error, got %v", err)
	}
}

func TestFinishIsIdempotentAndCopiesTokenCount(t *testing.T) {
	start := time.Unix(7000, 0)
	decoder, err := NewDecoder(Options{Protocol: ProtocolOpenAIResponses, Format: FormatJSON, RequestStartedAt: start})
	if err != nil {
		t.Fatal(err)
	}
	count := &TokenCount{OutputTokens: 2, Basis: TokenBasisProviderReported, Scope: TokenScopeProviderOutput}
	first, err := decoder.FinishAt(Completion{At: start.Add(time.Second), Outcome: OutcomeCompleted, TokenCount: count})
	if err != nil {
		t.Fatal(err)
	}
	count.OutputTokens = 99
	first.TokenCount.OutputTokens = 77
	second, err := decoder.FinishAt(Completion{At: start.Add(2 * time.Second), Outcome: OutcomeInterrupted})
	if err != nil {
		t.Fatal(err)
	}
	if first.TokenCount.OutputTokens != 77 || second.TokenCount.OutputTokens != 2 || second.Outcome != OutcomeCompleted {
		t.Fatalf("finish not idempotent: %#v / %#v", first, second)
	}
	if _, err = decoder.FeedAt(start.Add(3*time.Second), []byte("x")); !errors.Is(err, ErrFinished) {
		t.Fatalf("expected finished, got %v", err)
	}
	third, err := decoder.FinishAt(Completion{At: start.Add(4 * time.Second), Outcome: OutcomeCanceled})
	if err != nil || third.TokenCount.OutputTokens != 2 || third.Outcome != OutcomeCompleted {
		t.Fatalf("post-finish misuse corrupted result: %#v, %v", third, err)
	}
}

func TestLimitsAndInvalidCompletionCanBeRetried(t *testing.T) {
	start := time.Unix(11000, 0)
	decoder, err := NewDecoder(Options{
		Protocol: ProtocolOpenAIResponses, Format: FormatSSE, RequestStartedAt: start,
		MaxRetainedBytes: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decoder.FeedAt(start.Add(time.Second), []byte(`data: {"type":"response.completed"}

`)); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("expected retained data limit, got %v", err)
	}

	decoder, err = NewDecoder(Options{
		Protocol: ProtocolOpenAIResponses, Format: FormatSSE, RequestStartedAt: start,
		MaxNestingDepth: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decoder.FeedAt(start.Add(time.Second), []byte(`data: {"a":{"b":{}}}

`)); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("expected nesting limit, got %v", err)
	}

	decoder, err = NewDecoder(Options{Protocol: ProtocolOpenAIResponses, Format: FormatJSON, RequestStartedAt: start})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decoder.FinishAt(Completion{At: start.Add(time.Second), Outcome: OutcomeCompleted, TokenCount: &TokenCount{OutputTokens: -1, Basis: TokenBasisProviderReported, Scope: TokenScopeProviderOutput}}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("expected invalid token count, got %v", err)
	}
	if _, err = decoder.FinishAt(Completion{At: start.Add(time.Second), Outcome: OutcomeCompleted}); err != nil {
		t.Fatalf("valid completion should be retryable: %v", err)
	}
}

func TestTimeOrderingAndCleanEOFFallback(t *testing.T) {
	start := time.Unix(12000, 0)
	decoder, err := NewDecoder(Options{Protocol: ProtocolOpenAIResponses, Format: FormatSSE, RequestStartedAt: start})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decoder.FeedAt(start.Add(time.Second), []byte(`event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"x"}

`)); err != nil {
		t.Fatal(err)
	}
	result, err := decoder.FinishAt(Completion{At: start.Add(2 * time.Second), Outcome: OutcomeCompleted})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Milestones.GenerationCompleted.Observed || result.Milestones.GenerationCompleted.Basis != MilestoneTransportEOF || result.Terminal != TerminalUnknown {
		t.Fatalf("unexpected EOF fallback: %#v", result)
	}

	decoder, err = NewDecoder(Options{Protocol: ProtocolOpenAIResponses, Format: FormatJSON, RequestStartedAt: start})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decoder.FeedAt(start.Add(time.Second), []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err = decoder.FeedAt(start.Add(500*time.Millisecond), []byte("{}")); !errors.Is(err, ErrInvalidTimestamp) {
		t.Fatalf("expected timestamp error, got %v", err)
	}
}

func TestAutoRequiresStrongProtocolEvidence(t *testing.T) {
	start := time.Unix(13000, 0)
	decoder, err := NewDecoder(Options{Protocol: ProtocolAuto, Format: FormatSSE, RequestStartedAt: start})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decoder.FeedAt(start.Add(time.Second), []byte(`event: ping
data: {"type":"ping"}

data: [DONE]

`)); err != nil {
		t.Fatal(err)
	}
	if _, err = decoder.FinishAt(Completion{At: start.Add(2 * time.Second), Outcome: OutcomeCompleted}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected unsupported weak stream, got %v", err)
	}
}

func TestMeasureErrorContextAndZeroMetricJSON(t *testing.T) {
	start := time.Unix(14000, 0)
	decoder, err := NewDecoder(Options{Protocol: ProtocolOpenAIResponses, Format: FormatSSE, RequestStartedAt: start})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decoder.FeedAt(start.Add(time.Second), []byte("data: {\n\n")); err == nil {
		t.Fatal("expected malformed event")
	}
	var measureErr *MeasureError
	if !errors.As(err, &measureErr) || measureErr.Protocol != ProtocolOpenAIResponses || measureErr.Format != FormatSSE || measureErr.Stage != "feed" || measureErr.Offset == 0 || measureErr.Sequence != 1 {
		t.Fatalf("missing error context: %#v", measureErr)
	}

	encoded, err := json.Marshal(RateMetric{Available: true, TokensPerSecond: 0, Basis: MetricBasisDerived})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"available":true,"tokens_per_second":0,"basis":"derived"}` {
		t.Fatalf("zero rate was omitted: %s", encoded)
	}
}
