package llmperf

import "time"

const (
	defaultMaxSSEMetadataBytes = 64 << 10
	defaultMaxRetainedBytes    = 64 << 10
	defaultMaxNestingDepth     = 128
)

// Options configures one response decoder.
type Options struct {
	Protocol         Protocol
	Format           Format
	RequestStartedAt time.Time

	// MaxSSEMetadataBytes limits cumulative SSE metadata retained for one
	// event. Zero uses 64 KiB.
	MaxSSEMetadataBytes int
	// MaxRetainedBytes limits response event data and detector state retained
	// by one decoder. Zero uses 64 KiB.
	MaxRetainedBytes int
	// MaxNestingDepth limits JSON nesting in SSE data events. Zero uses 128.
	MaxNestingDepth int
}

func normalizeOptions(options Options) (Options, error) {
	if options.Protocol == "" || options.Format == "" || options.RequestStartedAt.IsZero() {
		return options, &MeasureError{Protocol: options.Protocol, Format: options.Format, Stage: "options", Err: ErrInvalidOptions}
	}
	switch options.Protocol {
	case ProtocolAuto, ProtocolOpenAIResponses, ProtocolOpenAIChatCompletions, ProtocolAnthropicMessages, ProtocolGoogleGenerateContent:
	default:
		return options, &MeasureError{Protocol: options.Protocol, Format: options.Format, Stage: "options", Err: ErrUnsupported}
	}
	switch options.Format {
	case FormatJSON, FormatSSE:
	default:
		return options, &MeasureError{Protocol: options.Protocol, Format: options.Format, Stage: "options", Err: ErrUnsupported}
	}
	if options.Protocol == ProtocolAuto && options.Format != FormatSSE {
		return options, &MeasureError{Protocol: options.Protocol, Format: options.Format, Stage: "options", Err: ErrUnsupported}
	}
	if options.MaxSSEMetadataBytes < 0 || options.MaxRetainedBytes < 0 || options.MaxNestingDepth < 0 {
		return options, &MeasureError{Protocol: options.Protocol, Format: options.Format, Stage: "options", Err: ErrInvalidOptions}
	}
	if options.MaxSSEMetadataBytes == 0 {
		options.MaxSSEMetadataBytes = defaultMaxSSEMetadataBytes
	}
	if options.MaxRetainedBytes == 0 {
		options.MaxRetainedBytes = defaultMaxRetainedBytes
	}
	if options.MaxNestingDepth == 0 {
		options.MaxNestingDepth = defaultMaxNestingDepth
	}
	return options, nil
}
