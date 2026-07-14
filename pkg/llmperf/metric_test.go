package llmperf

import (
	"math"
	"testing"
	"time"
)

func TestRateScopeSelectsMatchingMilestone(t *testing.T) {
	start := time.Unix(100, 0)
	milestones := Milestones{
		RequestStarted:      observedAt(start, MilestoneRequestStart),
		FirstOutput:         observedAt(start.Add(time.Second), MilestoneProtocolEvent),
		FirstText:           observedAt(start.Add(2*time.Second), MilestoneProtocolEvent),
		GenerationCompleted: observedAt(start.Add(6*time.Second), MilestoneProtocolEvent),
		ResponseEnded:       observedAt(start.Add(7*time.Second), MilestoneResponseEnd),
	}
	provider := deriveMetrics(FormatSSE, OutcomeCompleted, milestones, &TokenCount{OutputTokens: 6, Basis: TokenBasisProviderReported, Scope: TokenScopeProviderOutput})
	visible := deriveMetrics(FormatSSE, OutcomeCompleted, milestones, &TokenCount{OutputTokens: 6, Basis: TokenBasisTokenizerCounted, Scope: TokenScopeVisibleText})
	if provider.TimePerOutputToken.Value != time.Second || provider.GenerationTokensPerSecond.TokensPerSecond != 1 {
		t.Fatalf("unexpected provider-scope metrics: %#v", provider)
	}
	if visible.TimePerOutputToken.Value != 800*time.Millisecond || math.Abs(visible.GenerationTokensPerSecond.TokensPerSecond-1.25) > 1e-9 {
		t.Fatalf("unexpected visible-scope metrics: %#v", visible)
	}
	if provider.GenerationDuration.Value != 5*time.Second || visible.GenerationDuration.Value != 5*time.Second {
		t.Fatalf("generation duration must not depend on token scope")
	}
}

func TestRateAvailabilityReasons(t *testing.T) {
	start := time.Unix(200, 0)
	base := Milestones{
		RequestStarted:      observedAt(start, MilestoneRequestStart),
		FirstOutput:         observedAt(start.Add(time.Second), MilestoneProtocolEvent),
		GenerationCompleted: observedAt(start.Add(2*time.Second), MilestoneProtocolEvent),
		ResponseEnded:       observedAt(start.Add(3*time.Second), MilestoneResponseEnd),
	}
	tests := []struct {
		name   string
		count  *TokenCount
		reason UnavailableReason
	}{
		{"missing", nil, UnavailableMissingTokenCount},
		{"zero", &TokenCount{OutputTokens: 0, Basis: TokenBasisProviderReported, Scope: TokenScopeProviderOutput}, UnavailableInsufficientTokenCount},
		{"one", &TokenCount{OutputTokens: 1, Basis: TokenBasisProviderReported, Scope: TokenScopeProviderOutput}, UnavailableInsufficientTokenCount},
		{"unknown scope", &TokenCount{OutputTokens: 2, Basis: TokenBasisProviderReported, Scope: TokenScopeUnknown}, UnavailableAmbiguousTokenScope},
		{"visible without text", &TokenCount{OutputTokens: 2, Basis: TokenBasisTokenizerCounted, Scope: TokenScopeVisibleText}, UnavailableMissingFirstText},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metrics := deriveMetrics(FormatSSE, OutcomeCompleted, base, test.count)
			if metrics.TimePerOutputToken.Reason != test.reason || metrics.GenerationTokensPerSecond.Reason != test.reason {
				t.Fatalf("got %#v / %#v", metrics.TimePerOutputToken, metrics.GenerationTokensPerSecond)
			}
		})
	}
}

func TestInterruptedCompletionAvailability(t *testing.T) {
	start := time.Unix(300, 0)
	milestones := Milestones{
		RequestStarted: observedAt(start, MilestoneRequestStart),
		FirstOutput:    observedAt(start.Add(time.Second), MilestoneProtocolEvent),
		ResponseEnded:  observedAt(start.Add(2*time.Second), MilestoneResponseEnd),
	}
	count := &TokenCount{OutputTokens: 3, Basis: TokenBasisProviderReported, Scope: TokenScopeProviderOutput}
	metrics := deriveMetrics(FormatSSE, OutcomeInterrupted, milestones, count)
	if metrics.GenerationDuration.Reason != UnavailableInterrupted || metrics.TimePerOutputToken.Reason != UnavailableInterrupted {
		t.Fatalf("unexpected interruption metrics: %#v", metrics)
	}

	milestones.GenerationCompleted = observedAt(start.Add(1500*time.Millisecond), MilestoneProtocolEvent)
	metrics = deriveMetrics(FormatSSE, OutcomeInterrupted, milestones, count)
	if !metrics.TimePerOutputToken.Available || metrics.TimePerOutputToken.Value != 250*time.Millisecond {
		t.Fatalf("protocol completion should survive later transport failure: %#v", metrics)
	}
}

func TestZeroDurationsDoNotProduceInfinity(t *testing.T) {
	start := time.Unix(400, 0)
	milestones := Milestones{
		RequestStarted:      observedAt(start, MilestoneRequestStart),
		FirstOutput:         observedAt(start, MilestoneProtocolEvent),
		GenerationCompleted: observedAt(start, MilestoneProtocolEvent),
		ResponseEnded:       observedAt(start, MilestoneResponseEnd),
	}
	metrics := deriveMetrics(FormatSSE, OutcomeCompleted, milestones, &TokenCount{OutputTokens: 2, Basis: TokenBasisProviderReported, Scope: TokenScopeProviderOutput})
	if metrics.GenerationTokensPerSecond.Reason != UnavailableZeroDuration || metrics.EndToEndTokensPerSecond.Reason != UnavailableZeroDuration {
		t.Fatalf("unexpected zero duration metrics: %#v", metrics)
	}
}

func observedAt(at time.Time, basis MilestoneBasis) Milestone {
	return Milestone{Observed: true, At: at, Basis: basis}
}
