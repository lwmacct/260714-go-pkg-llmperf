package llmperf

import (
	"errors"
	"time"

	"github.com/lwmacct/260714-go-pkg-llmperf/pkg/llmperf/internal/engine"
	"github.com/lwmacct/260714-go-pkg-llmperf/pkg/llmperf/internal/protocol"
	"github.com/lwmacct/260714-go-pkg-llmperf/pkg/llmperf/internal/sse"
)

// Decoder incrementally measures one response. It is not safe for concurrent
// use. The decoder never reads the system clock or retains caller-owned body
// slices after FeedAt returns.
type Decoder struct {
	options  Options
	engine   *engine.Decoder
	finished bool
	terminal error
	result   Result
}

// NewDecoder creates a decoder for one response.
func NewDecoder(options Options) (*Decoder, error) {
	normalized, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}
	core, err := engine.New(engine.Options{
		Protocol: protocol.Kind(normalized.Protocol), Format: engine.Format(normalized.Format), RequestStartedAt: normalized.RequestStartedAt,
		MaxSSEMetadataBytes: normalized.MaxSSEMetadataBytes, MaxRetainedBytes: normalized.MaxRetainedBytes, MaxNestingDepth: normalized.MaxNestingDepth,
	})
	if err != nil {
		return nil, &MeasureError{Protocol: normalized.Protocol, Format: normalized.Format, Stage: "options", Err: mapEngineError(err)}
	}
	return &Decoder{options: normalized, engine: core}, nil
}

// ResponseHeadersAt records when response headers became available. It must be
// called before the first non-empty FeedAt and is optional.
func (d *Decoder) ResponseHeadersAt(at time.Time) error {
	if d == nil {
		return &MeasureError{Stage: "decoder", Err: ErrInvalidOptions}
	}
	if d.terminal != nil {
		return d.terminal
	}
	if d.finished {
		return d.lifecycleError("headers", ErrFinished)
	}
	if err := d.engine.ResponseHeadersAt(at); err != nil {
		return d.fail("headers", err)
	}
	return nil
}

// FeedAt consumes response body bytes observed at at. A non-empty chunk can
// produce first-byte, first-output, first-text, and completion updates.
func (d *Decoder) FeedAt(at time.Time, data []byte) ([]Update, error) {
	if d == nil {
		return nil, &MeasureError{Stage: "decoder", Err: ErrInvalidOptions}
	}
	if d.terminal != nil {
		return nil, d.terminal
	}
	if d.finished {
		return nil, d.lifecycleError("feed", ErrFinished)
	}
	updates, err := d.engine.FeedAt(at, data)
	if err != nil {
		return publicUpdates(updates), d.fail("feed", err)
	}
	return publicUpdates(updates), nil
}

// FinishAt finalizes the response. Repeated calls return the cached immutable
// result. Completion parameters from repeated calls are ignored.
func (d *Decoder) FinishAt(completion Completion) (Result, error) {
	if d == nil {
		return Result{}, &MeasureError{Stage: "decoder", Err: ErrInvalidOptions}
	}
	if d.terminal != nil {
		return Result{}, d.terminal
	}
	if d.finished {
		return cloneResult(d.result), nil
	}
	if completion.At.IsZero() || !validOutcome(completion.Outcome) {
		return Result{}, &MeasureError{Protocol: d.options.Protocol, Format: d.options.Format, Stage: "completion", Offset: d.engine.Offset(), Sequence: d.engine.Sequence(), Err: ErrInvalidOptions}
	}
	count, err := normalizeTokenCount(completion.TokenCount)
	if err != nil {
		return Result{}, &MeasureError{Protocol: d.options.Protocol, Format: d.options.Format, Stage: "completion", Offset: d.engine.Offset(), Sequence: d.engine.Sequence(), Err: err}
	}
	snapshot, err := d.engine.FinishAt(completion.At, completion.Outcome == OutcomeCompleted)
	if err != nil {
		return Result{}, d.fail("finish", err)
	}
	d.finished = true
	d.result = Result{
		Protocol: Protocol(snapshot.Protocol), Format: d.options.Format, Outcome: completion.Outcome, Terminal: TerminalState(snapshot.Terminal),
		Milestones: publicMilestones(snapshot.Timeline), TokenCount: count,
	}
	d.result.Metrics = deriveMetrics(d.result.Format, d.result.Outcome, d.result.Milestones, d.result.TokenCount)
	return cloneResult(d.result), nil
}

func (d *Decoder) lifecycleError(stage string, err error) error {
	return &MeasureError{
		Protocol: d.options.Protocol, Format: d.options.Format, Stage: stage,
		Offset: d.engine.Offset(), Sequence: d.engine.Sequence(), Err: err,
	}
}

func (d *Decoder) fail(stage string, err error) error {
	measureErr := &MeasureError{
		Protocol: d.options.Protocol, Format: d.options.Format, Stage: stage,
		Offset: d.engine.Offset(), Sequence: d.engine.Sequence(), Err: mapEngineError(err),
	}
	d.terminal = measureErr
	return measureErr
}

func publicUpdates(updates []engine.Update) []Update {
	if len(updates) == 0 {
		return nil
	}
	result := make([]Update, len(updates))
	for index, update := range updates {
		result[index] = Update{
			Kind: UpdateKind(mapUpdateKind(update.Kind)), At: update.At, Sequence: update.Sequence,
			OutputKind: OutputKind(update.OutputKind), Precision: TimestampPrecision(update.Precision),
		}
	}
	return result
}

func mapUpdateKind(kind engine.UpdateKind) string {
	switch kind {
	case engine.UpdateFirstByte:
		return string(UpdateFirstByte)
	case engine.UpdateFirstOutput:
		return string(UpdateFirstOutput)
	case engine.UpdateFirstText:
		return string(UpdateFirstText)
	case engine.UpdateGenerationCompleted:
		return string(UpdateGenerationCompleted)
	default:
		return ""
	}
}

func publicMilestones(value engine.Timeline) Milestones {
	return Milestones{
		RequestStarted: publicMilestone(value.RequestStarted), ResponseHeaders: publicMilestone(value.ResponseHeaders),
		FirstByte: publicMilestone(value.FirstByte), FirstOutput: publicMilestone(value.FirstOutput), FirstText: publicMilestone(value.FirstText),
		GenerationCompleted: publicMilestone(value.GenerationCompleted), ResponseEnded: publicMilestone(value.ResponseEnded),
	}
}

func publicMilestone(value engine.Milestone) Milestone {
	return Milestone{
		Observed: value.Observed, At: value.At, Sequence: value.Sequence, Basis: MilestoneBasis(value.Basis),
		OutputKind: OutputKind(value.OutputKind), Precision: TimestampPrecision(value.Precision),
	}
}

func validOutcome(outcome Outcome) bool {
	switch outcome {
	case OutcomeCompleted, OutcomeInterrupted, OutcomeCanceled:
		return true
	default:
		return false
	}
}

func cloneResult(result Result) Result {
	if result.TokenCount != nil {
		count := *result.TokenCount
		result.TokenCount = &count
	}
	return result
}

func mapEngineError(err error) error {
	switch {
	case errors.Is(err, engine.ErrFinished):
		return ErrFinished
	case errors.Is(err, engine.ErrInvalidTimestamp):
		return ErrInvalidTimestamp
	case errors.Is(err, engine.ErrInvalidLifecycle):
		return ErrInvalidLifecycle
	case errors.Is(err, protocol.ErrUnsupported):
		return errors.Join(ErrUnsupported, err)
	case errors.Is(err, protocol.ErrNestingLimit), errors.Is(err, sse.ErrMetadataLimit), errors.Is(err, sse.ErrDataLimit):
		return errors.Join(ErrLimitExceeded, err)
	case errors.Is(err, protocol.ErrMalformed), errors.Is(err, sse.ErrMalformed):
		return errors.Join(ErrMalformedStream, err)
	default:
		return errors.Join(ErrMalformedStream, err)
	}
}
