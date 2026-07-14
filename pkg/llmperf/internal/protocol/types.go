package protocol

import "time"

type Kind string

const (
	Auto                  Kind = "auto"
	OpenAIResponses       Kind = "openai.responses"
	OpenAIChatCompletions Kind = "openai.chat-completions"
	AnthropicMessages     Kind = "anthropic.messages"
	GoogleGenerateContent Kind = "google.generate-content"
)

type OutputKind string

const (
	OutputText      OutputKind = "text"
	OutputRefusal   OutputKind = "refusal"
	OutputTool      OutputKind = "tool"
	OutputReasoning OutputKind = "reasoning"
)

type TerminalState string

const (
	TerminalUnknown    TerminalState = "unknown"
	TerminalCompleted  TerminalState = "completed"
	TerminalIncomplete TerminalState = "incomplete"
	TerminalFailed     TerminalState = "failed"
)

type FactKind uint8

const (
	FactOutputStarted FactKind = iota + 1
	FactTextStarted
	FactGenerationCompleted
	FactTerminal
)

type Basis string

const (
	BasisProtocolEvent     Basis = "protocol_event"
	BasisProtocolCandidate Basis = "protocol_candidate"
)

type Event struct {
	Sequence uint64
	Type     string
	Data     []byte
	At       time.Time
}

type Fact struct {
	Kind       FactKind
	At         time.Time
	Sequence   uint64
	OutputKind OutputKind
	Terminal   TerminalState
	Basis      Basis
}

type Detector interface {
	Kind() Kind
	Process(Event) ([]Fact, error)
	Finish(at time.Time, clean bool) ([]Fact, error)
}
