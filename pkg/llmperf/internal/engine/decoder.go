package engine

import (
	"errors"
	"time"

	"github.com/lwmacct/260714-go-pkg-llmperf/pkg/llmperf/internal/protocol"
	"github.com/lwmacct/260714-go-pkg-llmperf/pkg/llmperf/internal/sse"
)

var (
	ErrFinished         = errors.New("decoder finished")
	ErrInvalidTimestamp = errors.New("invalid timestamp")
	ErrInvalidLifecycle = errors.New("invalid lifecycle")
)

type Format string

const (
	FormatJSON Format = "json"
	FormatSSE  Format = "sse"
)

type Options struct {
	Protocol            protocol.Kind
	Format              Format
	RequestStartedAt    time.Time
	MaxSSEMetadataBytes int
	MaxRetainedBytes    int
	MaxNestingDepth     int
}

type Basis string

const (
	BasisRequestStart      Basis = "request_start"
	BasisResponseHeaders   Basis = "response_headers"
	BasisBodyBytes         Basis = "body_bytes"
	BasisProtocolEvent     Basis = "protocol_event"
	BasisProtocolCandidate Basis = "protocol_candidate"
	BasisTransportEOF      Basis = "transport_eof"
	BasisResponseEnd       Basis = "response_end"
)

type Precision string

const (
	PrecisionTransport Precision = "transport_observed"
	PrecisionChunk     Precision = "chunk_observed"
)

type Milestone struct {
	Observed   bool
	At         time.Time
	Sequence   uint64
	Basis      Basis
	OutputKind protocol.OutputKind
	Precision  Precision
}

type Timeline struct {
	RequestStarted      Milestone
	ResponseHeaders     Milestone
	FirstByte           Milestone
	FirstOutput         Milestone
	FirstText           Milestone
	GenerationCompleted Milestone
	ResponseEnded       Milestone
}

type UpdateKind uint8

const (
	UpdateFirstByte UpdateKind = iota + 1
	UpdateFirstOutput
	UpdateFirstText
	UpdateGenerationCompleted
)

type Update struct {
	Kind       UpdateKind
	At         time.Time
	Sequence   uint64
	OutputKind protocol.OutputKind
	Precision  Precision
}

type Snapshot struct {
	Protocol protocol.Kind
	Terminal protocol.TerminalState
	Timeline Timeline
}

type Decoder struct {
	options        Options
	detector       protocol.Detector
	parser         *sse.Parser
	timeline       Timeline
	terminal       protocol.TerminalState
	lastObservedAt time.Time
	bodySeen       bool
	finished       bool
	currentUpdates []Update
}

func New(options Options) (*Decoder, error) {
	d := &Decoder{options: options, terminal: protocol.TerminalUnknown, lastObservedAt: options.RequestStartedAt}
	d.timeline.RequestStarted = Milestone{Observed: true, At: options.RequestStartedAt, Basis: BasisRequestStart, Precision: PrecisionTransport}
	if options.Format == FormatSSE {
		detector, err := protocol.New(options.Protocol, options.MaxNestingDepth)
		if err != nil {
			return nil, err
		}
		d.detector = detector
		d.parser = sse.NewParser(options.MaxSSEMetadataBytes, options.MaxRetainedBytes, d.processEvent)
	}
	return d, nil
}

func (d *Decoder) Offset() int64 {
	if d == nil || d.parser == nil {
		return 0
	}
	return d.parser.Offset()
}

func (d *Decoder) Sequence() uint64 {
	if d == nil || d.parser == nil {
		return 0
	}
	return d.parser.Sequence()
}

func (d *Decoder) ResponseHeadersAt(at time.Time) error {
	if d.finished {
		return ErrFinished
	}
	if d.bodySeen {
		return ErrInvalidLifecycle
	}
	if err := d.validateAt(at); err != nil {
		return err
	}
	if !d.timeline.ResponseHeaders.Observed {
		d.timeline.ResponseHeaders = Milestone{Observed: true, At: at, Basis: BasisResponseHeaders, Precision: PrecisionTransport}
	}
	d.lastObservedAt = at
	return nil
}

func (d *Decoder) FeedAt(at time.Time, data []byte) ([]Update, error) {
	if d.finished {
		return nil, ErrFinished
	}
	if len(data) == 0 {
		return nil, nil
	}
	if err := d.validateAt(at); err != nil {
		return nil, err
	}
	d.bodySeen = true
	d.lastObservedAt = at
	d.currentUpdates = d.currentUpdates[:0]
	if !d.timeline.FirstByte.Observed {
		d.timeline.FirstByte = Milestone{Observed: true, At: at, Basis: BasisBodyBytes, Precision: PrecisionTransport}
		d.currentUpdates = append(d.currentUpdates, Update{Kind: UpdateFirstByte, At: at, Precision: PrecisionTransport})
	}
	if d.options.Format == FormatSSE {
		if err := d.parser.FeedAt(at, data); err != nil {
			return append([]Update(nil), d.currentUpdates...), err
		}
	}
	return append([]Update(nil), d.currentUpdates...), nil
}

func (d *Decoder) FinishAt(at time.Time, clean bool) (Snapshot, error) {
	if d.finished {
		return d.snapshot(), nil
	}
	if err := d.validateAt(at); err != nil {
		return Snapshot{}, err
	}
	if d.options.Format == FormatSSE {
		d.currentUpdates = d.currentUpdates[:0]
		if err := d.parser.FinishAt(at); err != nil {
			return Snapshot{}, err
		}
		facts, err := d.detector.Finish(at, clean)
		if err != nil {
			return Snapshot{}, err
		}
		d.applyFacts(facts)
		if clean && d.timeline.FirstOutput.Observed && !d.timeline.GenerationCompleted.Observed {
			d.timeline.GenerationCompleted = Milestone{
				Observed: true, At: at, Basis: BasisTransportEOF, Precision: PrecisionTransport,
			}
		}
	}
	d.timeline.ResponseEnded = Milestone{Observed: true, At: at, Basis: BasisResponseEnd, Precision: PrecisionTransport}
	d.lastObservedAt = at
	d.finished = true
	return d.snapshot(), nil
}

func (d *Decoder) snapshot() Snapshot {
	kind := d.options.Protocol
	if d.detector != nil {
		kind = d.detector.Kind()
	}
	return Snapshot{Protocol: kind, Terminal: d.terminal, Timeline: d.timeline}
}

func (d *Decoder) processEvent(event sse.Event) error {
	facts, err := d.detector.Process(protocol.Event{Sequence: event.Sequence, Type: event.Type, Data: event.Data, At: event.At})
	if err != nil {
		return err
	}
	d.applyFacts(facts)
	return nil
}

func (d *Decoder) applyFacts(facts []protocol.Fact) {
	for _, fact := range facts {
		basis := BasisProtocolEvent
		if fact.Basis == protocol.BasisProtocolCandidate {
			basis = BasisProtocolCandidate
		}
		switch fact.Kind {
		case protocol.FactOutputStarted:
			if !d.timeline.FirstOutput.Observed {
				d.timeline.FirstOutput = Milestone{Observed: true, At: fact.At, Sequence: fact.Sequence, Basis: basis, OutputKind: fact.OutputKind, Precision: PrecisionChunk}
				d.currentUpdates = append(d.currentUpdates, Update{Kind: UpdateFirstOutput, At: fact.At, Sequence: fact.Sequence, OutputKind: fact.OutputKind, Precision: PrecisionChunk})
			}
		case protocol.FactTextStarted:
			if !d.timeline.FirstText.Observed {
				d.timeline.FirstText = Milestone{Observed: true, At: fact.At, Sequence: fact.Sequence, Basis: basis, OutputKind: fact.OutputKind, Precision: PrecisionChunk}
				d.currentUpdates = append(d.currentUpdates, Update{Kind: UpdateFirstText, At: fact.At, Sequence: fact.Sequence, OutputKind: fact.OutputKind, Precision: PrecisionChunk})
			}
		case protocol.FactGenerationCompleted:
			if !d.timeline.GenerationCompleted.Observed {
				d.timeline.GenerationCompleted = Milestone{Observed: true, At: fact.At, Sequence: fact.Sequence, Basis: basis, Precision: PrecisionChunk}
				d.currentUpdates = append(d.currentUpdates, Update{Kind: UpdateGenerationCompleted, At: fact.At, Sequence: fact.Sequence, Precision: PrecisionChunk})
			}
		case protocol.FactTerminal:
			d.terminal = fact.Terminal
		}
	}
}

func (d *Decoder) validateAt(at time.Time) error {
	if at.IsZero() || at.Before(d.options.RequestStartedAt) || at.Before(d.lastObservedAt) {
		return ErrInvalidTimestamp
	}
	return nil
}
