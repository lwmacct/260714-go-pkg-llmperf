package protocol

import (
	"bytes"
	"encoding/json"
	"time"
)

type openAIChatDetector struct {
	maxDepth     int
	lastFinishAt time.Time
	lastSequence uint64
	terminal     TerminalState
}

func (*openAIChatDetector) Kind() Kind { return OpenAIChatCompletions }

func (d *openAIChatDetector) Process(event Event) ([]Fact, error) {
	if bytes.Equal(bytes.TrimSpace(event.Data), []byte("[DONE]")) {
		at := event.At
		sequence := event.Sequence
		basis := BasisProtocolEvent
		if !d.lastFinishAt.IsZero() {
			at = d.lastFinishAt
			sequence = d.lastSequence
			basis = BasisProtocolCandidate
		}
		terminal := d.terminal
		if terminal == TerminalUnknown {
			terminal = TerminalCompleted
		}
		return []Fact{
			{Kind: FactGenerationCompleted, At: at, Sequence: sequence, Basis: basis},
			{Kind: FactTerminal, At: event.At, Sequence: event.Sequence, Terminal: terminal, Basis: BasisProtocolEvent},
		}, nil
	}
	var payload struct {
		Object  string `json:"object"`
		Choices []struct {
			Index int `json:"index"`
			Delta struct {
				Content   json.RawMessage `json:"content"`
				Refusal   json.RawMessage `json:"refusal"`
				ToolCalls []struct {
					Function struct {
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := decodeJSON(event.Data, d.maxDepth, &payload); err != nil {
		return nil, err
	}
	var facts []Fact
	for _, choice := range payload.Choices {
		if nonEmptyString(choice.Delta.Content) {
			facts = append(facts, outputFacts(event, OutputText, true)...)
		}
		if nonEmptyString(choice.Delta.Refusal) {
			facts = append(facts, outputFacts(event, OutputRefusal, true)...)
		}
		for _, toolCall := range choice.Delta.ToolCalls {
			if nonEmptyString(toolCall.Function.Arguments) {
				facts = append(facts, outputFacts(event, OutputTool, false)...)
			}
		}
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			d.lastFinishAt = event.At
			d.lastSequence = event.Sequence
			if incompleteChatReason(*choice.FinishReason) {
				d.terminal = TerminalIncomplete
			} else if d.terminal == TerminalUnknown {
				d.terminal = TerminalCompleted
			}
		}
	}
	return facts, nil
}

func (d *openAIChatDetector) Finish(_ time.Time, clean bool) ([]Fact, error) {
	if !clean || d.lastFinishAt.IsZero() {
		return nil, nil
	}
	terminal := d.terminal
	if terminal == TerminalUnknown {
		terminal = TerminalCompleted
	}
	return []Fact{
		{Kind: FactGenerationCompleted, At: d.lastFinishAt, Sequence: d.lastSequence, Basis: BasisProtocolCandidate},
		{Kind: FactTerminal, At: d.lastFinishAt, Sequence: d.lastSequence, Terminal: terminal, Basis: BasisProtocolCandidate},
	}, nil
}

func incompleteChatReason(reason string) bool {
	switch reason {
	case "length", "content_filter":
		return true
	default:
		return false
	}
}
