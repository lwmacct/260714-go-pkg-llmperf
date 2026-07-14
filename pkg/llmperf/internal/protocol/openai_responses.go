package protocol

import (
	"encoding/json"
	"strings"
	"time"
)

type openAIResponsesDetector struct{ maxDepth int }

func (*openAIResponsesDetector) Kind() Kind { return OpenAIResponses }

func (d *openAIResponsesDetector) Process(event Event) ([]Fact, error) {
	var payload struct {
		Type  string          `json:"type"`
		Delta json.RawMessage `json:"delta"`
	}
	if err := decodeJSON(event.Data, d.maxDepth, &payload); err != nil {
		return nil, err
	}
	eventType := payload.Type
	if eventType == "" && event.Type != "message" {
		eventType = event.Type
	}
	switch eventType {
	case "response.output_text.delta":
		if nonEmptyString(payload.Delta) {
			return outputFacts(event, OutputText, true), nil
		}
	case "response.refusal.delta":
		if nonEmptyString(payload.Delta) {
			return outputFacts(event, OutputRefusal, true), nil
		}
	case "response.function_call_arguments.delta", "response.custom_tool_call_input.delta", "response.code_interpreter_call_code.delta":
		if nonEmptyString(payload.Delta) {
			return outputFacts(event, OutputTool, false), nil
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if nonEmptyString(payload.Delta) {
			return outputFacts(event, OutputReasoning, false), nil
		}
	case "response.completed":
		return terminalFacts(event, TerminalCompleted), nil
	case "response.incomplete":
		return terminalFacts(event, TerminalIncomplete), nil
	case "response.failed", "error":
		return terminalFacts(event, TerminalFailed), nil
	default:
		if strings.HasPrefix(eventType, "response.") {
			return nil, nil
		}
	}
	return nil, nil
}

func (*openAIResponsesDetector) Finish(time.Time, bool) ([]Fact, error) { return nil, nil }

func outputFacts(event Event, kind OutputKind, visible bool) []Fact {
	facts := []Fact{{Kind: FactOutputStarted, At: event.At, Sequence: event.Sequence, OutputKind: kind, Basis: BasisProtocolEvent}}
	if visible {
		facts = append(facts, Fact{Kind: FactTextStarted, At: event.At, Sequence: event.Sequence, OutputKind: kind, Basis: BasisProtocolEvent})
	}
	return facts
}

func terminalFacts(event Event, terminal TerminalState) []Fact {
	return []Fact{
		{Kind: FactGenerationCompleted, At: event.At, Sequence: event.Sequence, Basis: BasisProtocolEvent},
		{Kind: FactTerminal, At: event.At, Sequence: event.Sequence, Terminal: terminal, Basis: BasisProtocolEvent},
	}
}
