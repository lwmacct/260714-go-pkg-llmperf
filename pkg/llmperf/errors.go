package llmperf

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidOptions   = errors.New("llmperf: invalid options")
	ErrInvalidTimestamp = errors.New("llmperf: invalid timestamp")
	ErrInvalidLifecycle = errors.New("llmperf: invalid decoder lifecycle")
	ErrUnsupported      = errors.New("llmperf: unsupported protocol or format")
	ErrMalformedStream  = errors.New("llmperf: malformed stream")
	ErrLimitExceeded    = errors.New("llmperf: limit exceeded")
	ErrFinished         = errors.New("llmperf: decoder finished")
)

// MeasureError adds protocol, format, stage, byte offset, and SSE sequence to
// a decoder error. Use errors.Is and errors.As to inspect it.
type MeasureError struct {
	Protocol Protocol
	Format   Format
	Stage    string
	Offset   int64
	Sequence uint64
	Err      error
}

func (e *MeasureError) Error() string {
	if e == nil {
		return "<nil>"
	}
	location := ""
	if e.Offset > 0 {
		location = fmt.Sprintf(" at byte %d", e.Offset)
	}
	if e.Sequence > 0 {
		location += fmt.Sprintf(" event %d", e.Sequence)
	}
	return fmt.Sprintf("llmperf: measure %s/%s during %s%s: %v", e.Protocol, e.Format, e.Stage, location, e.Err)
}

func (e *MeasureError) Unwrap() error { return e.Err }
