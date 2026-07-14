package llmperf

import (
	"math"
	"time"
)

// UnavailableReason explains why a metric could not be calculated.
type UnavailableReason string

const (
	UnavailableNonStreaming                UnavailableReason = "non_streaming"
	UnavailableMissingHeaders              UnavailableReason = "missing_headers"
	UnavailableMissingFirstByte            UnavailableReason = "missing_first_byte"
	UnavailableMissingFirstOutput          UnavailableReason = "missing_first_output"
	UnavailableMissingFirstText            UnavailableReason = "missing_first_text"
	UnavailableMissingGenerationCompletion UnavailableReason = "missing_generation_completion"
	UnavailableInterrupted                 UnavailableReason = "interrupted"
	UnavailableMissingTokenCount           UnavailableReason = "missing_token_count"
	UnavailableInsufficientTokenCount      UnavailableReason = "insufficient_token_count"
	UnavailableAmbiguousTokenScope         UnavailableReason = "ambiguous_token_scope"
	UnavailableZeroDuration                UnavailableReason = "zero_duration"
	UnavailableInvalidTimeOrder            UnavailableReason = "invalid_time_order"
)

// MetricBasis describes the evidence used for a metric.
type MetricBasis string

const (
	MetricBasisTransport         MetricBasis = "transport"
	MetricBasisProtocolChunk     MetricBasis = "protocol_chunk"
	MetricBasisProtocolEvent     MetricBasis = "protocol_event"
	MetricBasisProtocolCandidate MetricBasis = "protocol_candidate"
	MetricBasisTransportEOF      MetricBasis = "transport_eof"
	MetricBasisDerived           MetricBasis = "derived"
)

// DurationMetric is an explicitly available or unavailable duration.
type DurationMetric struct {
	Available bool              `json:"available"`
	Value     time.Duration     `json:"value"`
	Reason    UnavailableReason `json:"reason,omitempty"`
	Basis     MetricBasis       `json:"basis,omitempty"`
}

// RateMetric is an explicitly available or unavailable token rate.
type RateMetric struct {
	Available       bool              `json:"available"`
	TokensPerSecond float64           `json:"tokens_per_second"`
	Reason          UnavailableReason `json:"reason,omitempty"`
	Basis           MetricBasis       `json:"basis,omitempty"`
}

// Metrics contains durations and rates derived from one response timeline.
type Metrics struct {
	ResponseHeaderLatency     DurationMetric `json:"response_header_latency"`
	TimeToFirstByte           DurationMetric `json:"time_to_first_byte"`
	TimeToFirstOutput         DurationMetric `json:"time_to_first_output"`
	TimeToFirstText           DurationMetric `json:"time_to_first_text"`
	GenerationDuration        DurationMetric `json:"generation_duration"`
	EndToEndLatency           DurationMetric `json:"end_to_end_latency"`
	TimePerOutputToken        DurationMetric `json:"time_per_output_token"`
	GenerationTokensPerSecond RateMetric     `json:"generation_tokens_per_second"`
	EndToEndTokensPerSecond   RateMetric     `json:"end_to_end_tokens_per_second"`
}

func deriveMetrics(format Format, outcome Outcome, milestones Milestones, tokenCount *TokenCount) Metrics {
	metrics := Metrics{
		ResponseHeaderLatency:     unavailableDuration(UnavailableMissingHeaders),
		TimeToFirstByte:           unavailableDuration(UnavailableMissingFirstByte),
		TimeToFirstOutput:         unavailableDuration(UnavailableMissingFirstOutput),
		TimeToFirstText:           unavailableDuration(UnavailableMissingFirstText),
		GenerationDuration:        unavailableDuration(UnavailableMissingGenerationCompletion),
		EndToEndLatency:           unavailableDuration(UnavailableMissingGenerationCompletion),
		TimePerOutputToken:        unavailableDuration(UnavailableMissingTokenCount),
		GenerationTokensPerSecond: unavailableRate(UnavailableMissingTokenCount),
		EndToEndTokensPerSecond:   unavailableRate(UnavailableMissingTokenCount),
	}
	start := milestones.RequestStarted.At
	if milestones.ResponseHeaders.Observed {
		metrics.ResponseHeaderLatency = availableDuration(milestones.ResponseHeaders.At.Sub(start), MetricBasisTransport)
	}
	if milestones.FirstByte.Observed {
		metrics.TimeToFirstByte = availableDuration(milestones.FirstByte.At.Sub(start), MetricBasisTransport)
	}
	if format == FormatJSON {
		metrics.TimeToFirstOutput = unavailableDuration(UnavailableNonStreaming)
		metrics.TimeToFirstText = unavailableDuration(UnavailableNonStreaming)
		metrics.GenerationDuration = unavailableDuration(UnavailableNonStreaming)
		metrics.TimePerOutputToken = unavailableDuration(UnavailableNonStreaming)
		metrics.GenerationTokensPerSecond = unavailableRate(UnavailableNonStreaming)
	} else {
		if milestones.FirstOutput.Observed {
			metrics.TimeToFirstOutput = availableDuration(milestones.FirstOutput.At.Sub(start), MetricBasisProtocolChunk)
		}
		if milestones.FirstText.Observed {
			metrics.TimeToFirstText = availableDuration(milestones.FirstText.At.Sub(start), MetricBasisProtocolChunk)
		}
		if milestones.FirstOutput.Observed && milestones.GenerationCompleted.Observed {
			duration := milestones.GenerationCompleted.At.Sub(milestones.FirstOutput.At)
			if duration < 0 {
				metrics.GenerationDuration = unavailableDuration(UnavailableInvalidTimeOrder)
			} else {
				metrics.GenerationDuration = availableDuration(duration, metricBasisForCompletion(milestones.GenerationCompleted.Basis))
			}
		} else if outcome != OutcomeCompleted && !milestones.GenerationCompleted.Observed {
			metrics.GenerationDuration = unavailableDuration(UnavailableInterrupted)
		}
	}
	if milestones.ResponseEnded.Observed {
		metrics.EndToEndLatency = availableDuration(milestones.ResponseEnded.At.Sub(start), MetricBasisTransport)
	}
	if tokenCount == nil {
		return metrics
	}
	if metrics.EndToEndLatency.Available {
		if metrics.EndToEndLatency.Value == 0 {
			metrics.EndToEndTokensPerSecond = unavailableRate(UnavailableZeroDuration)
		} else {
			metrics.EndToEndTokensPerSecond = availableRate(float64(tokenCount.OutputTokens)/metrics.EndToEndLatency.Value.Seconds(), MetricBasisDerived)
		}
	}
	if format == FormatJSON {
		return metrics
	}
	if tokenCount.OutputTokens < 2 {
		metrics.TimePerOutputToken = unavailableDuration(UnavailableInsufficientTokenCount)
		metrics.GenerationTokensPerSecond = unavailableRate(UnavailableInsufficientTokenCount)
		return metrics
	}
	var scopedStart Milestone
	switch tokenCount.Scope {
	case TokenScopeProviderOutput:
		scopedStart = milestones.FirstOutput
	case TokenScopeVisibleText:
		scopedStart = milestones.FirstText
	case TokenScopeUnknown:
		metrics.TimePerOutputToken = unavailableDuration(UnavailableAmbiguousTokenScope)
		metrics.GenerationTokensPerSecond = unavailableRate(UnavailableAmbiguousTokenScope)
		return metrics
	}
	if !scopedStart.Observed {
		reason := UnavailableMissingFirstOutput
		if tokenCount.Scope == TokenScopeVisibleText {
			reason = UnavailableMissingFirstText
		}
		metrics.TimePerOutputToken = unavailableDuration(reason)
		metrics.GenerationTokensPerSecond = unavailableRate(reason)
		return metrics
	}
	if !milestones.GenerationCompleted.Observed {
		reason := UnavailableMissingGenerationCompletion
		if outcome != OutcomeCompleted {
			reason = UnavailableInterrupted
		}
		metrics.TimePerOutputToken = unavailableDuration(reason)
		metrics.GenerationTokensPerSecond = unavailableRate(reason)
		return metrics
	}
	duration := milestones.GenerationCompleted.At.Sub(scopedStart.At)
	if duration < 0 {
		metrics.TimePerOutputToken = unavailableDuration(UnavailableInvalidTimeOrder)
		metrics.GenerationTokensPerSecond = unavailableRate(UnavailableInvalidTimeOrder)
		return metrics
	}
	if duration == 0 {
		metrics.TimePerOutputToken = unavailableDuration(UnavailableZeroDuration)
		metrics.GenerationTokensPerSecond = unavailableRate(UnavailableZeroDuration)
		return metrics
	}
	remaining := tokenCount.OutputTokens - 1
	metrics.TimePerOutputToken = availableDuration(duration/time.Duration(remaining), MetricBasisDerived)
	rate := float64(remaining) / duration.Seconds()
	if math.IsNaN(rate) || math.IsInf(rate, 0) {
		metrics.GenerationTokensPerSecond = unavailableRate(UnavailableZeroDuration)
	} else {
		metrics.GenerationTokensPerSecond = availableRate(rate, MetricBasisDerived)
	}
	return metrics
}

func metricBasisForCompletion(basis MilestoneBasis) MetricBasis {
	switch basis {
	case MilestoneProtocolCandidate:
		return MetricBasisProtocolCandidate
	case MilestoneTransportEOF:
		return MetricBasisTransportEOF
	default:
		return MetricBasisProtocolEvent
	}
}

func availableDuration(value time.Duration, basis MetricBasis) DurationMetric {
	return DurationMetric{Available: true, Value: value, Basis: basis}
}

func unavailableDuration(reason UnavailableReason) DurationMetric {
	return DurationMetric{Reason: reason}
}

func availableRate(value float64, basis MetricBasis) RateMetric {
	return RateMetric{Available: true, TokensPerSecond: value, Basis: basis}
}

func unavailableRate(reason UnavailableReason) RateMetric {
	return RateMetric{Reason: reason}
}
