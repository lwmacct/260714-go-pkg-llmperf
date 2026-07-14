package llmperf

import "time"

// UpdateKind identifies an incremental response milestone.
type UpdateKind string

const (
	UpdateFirstByte           UpdateKind = "first_byte"
	UpdateFirstOutput         UpdateKind = "first_output"
	UpdateFirstText           UpdateKind = "first_text"
	UpdateGenerationCompleted UpdateKind = "generation_completed"
)

// OutputKind identifies the first protocol output observed.
type OutputKind string

const (
	OutputText      OutputKind = "text"
	OutputRefusal   OutputKind = "refusal"
	OutputTool      OutputKind = "tool"
	OutputReasoning OutputKind = "reasoning"
)

// TimestampPrecision describes where a milestone timestamp came from.
type TimestampPrecision string

const (
	PrecisionTransportObserved TimestampPrecision = "transport_observed"
	PrecisionChunkObserved     TimestampPrecision = "chunk_observed"
)

// MilestoneBasis describes the evidence used for a milestone.
type MilestoneBasis string

const (
	MilestoneRequestStart      MilestoneBasis = "request_start"
	MilestoneResponseHeaders   MilestoneBasis = "response_headers"
	MilestoneBodyBytes         MilestoneBasis = "body_bytes"
	MilestoneProtocolEvent     MilestoneBasis = "protocol_event"
	MilestoneProtocolCandidate MilestoneBasis = "protocol_candidate"
	MilestoneTransportEOF      MilestoneBasis = "transport_eof"
	MilestoneResponseEnd       MilestoneBasis = "response_end"
)

// TerminalState describes the provider protocol terminal state.
type TerminalState string

const (
	TerminalUnknown    TerminalState = "unknown"
	TerminalCompleted  TerminalState = "completed"
	TerminalIncomplete TerminalState = "incomplete"
	TerminalFailed     TerminalState = "failed"
)

// Outcome describes the caller-observed response lifecycle.
type Outcome string

const (
	OutcomeCompleted   Outcome = "completed"
	OutcomeInterrupted Outcome = "interrupted"
	OutcomeCanceled    Outcome = "canceled"
)

// Update reports a milestone when it is first observed.
type Update struct {
	Kind       UpdateKind         `json:"kind"`
	At         time.Time          `json:"at"`
	Sequence   uint64             `json:"sequence,omitempty"`
	OutputKind OutputKind         `json:"output_kind,omitempty"`
	Precision  TimestampPrecision `json:"precision"`
}

// Milestone records whether and when a timeline point was observed.
type Milestone struct {
	Observed   bool               `json:"observed"`
	At         time.Time          `json:"at,omitzero"`
	Sequence   uint64             `json:"sequence,omitempty"`
	Basis      MilestoneBasis     `json:"basis,omitempty"`
	OutputKind OutputKind         `json:"output_kind,omitempty"`
	Precision  TimestampPrecision `json:"precision,omitempty"`
}

// Milestones contains the normalized timeline for one response.
type Milestones struct {
	RequestStarted      Milestone `json:"request_started"`
	ResponseHeaders     Milestone `json:"response_headers"`
	FirstByte           Milestone `json:"first_byte"`
	FirstOutput         Milestone `json:"first_output"`
	FirstText           Milestone `json:"first_text"`
	GenerationCompleted Milestone `json:"generation_completed"`
	ResponseEnded       Milestone `json:"response_ended"`
}

// Completion finalizes one response decoder.
type Completion struct {
	At         time.Time
	Outcome    Outcome
	TokenCount *TokenCount
}

// Result contains one complete response timeline and its derived metrics.
type Result struct {
	Protocol   Protocol      `json:"protocol"`
	Format     Format        `json:"format"`
	Outcome    Outcome       `json:"outcome"`
	Terminal   TerminalState `json:"terminal"`
	Milestones Milestones    `json:"milestones"`
	Metrics    Metrics       `json:"metrics"`
	TokenCount *TokenCount   `json:"token_count,omitempty"`
}
