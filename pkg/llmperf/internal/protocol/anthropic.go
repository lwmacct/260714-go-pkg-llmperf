package protocol

import (
	"encoding/json"
	"time"
)

type anthropicDetector struct{ maxDepth int }

func (*anthropicDetector) Kind() Kind { return AnthropicMessages }

func (d *anthropicDetector) Process(event Event) ([]Fact, error) {
	var payload struct {
		Type  string `json:"type"`
		Delta struct {
			Type        string          `json:"type"`
			Text        json.RawMessage `json:"text"`
			PartialJSON json.RawMessage `json:"partial_json"`
			Thinking    json.RawMessage `json:"thinking"`
		} `json:"delta"`
	}
	if err := decodeJSON(event.Data, d.maxDepth, &payload); err != nil {
		return nil, err
	}
	eventType := payload.Type
	if eventType == "" && event.Type != "message" {
		eventType = event.Type
	}
	switch eventType {
	case "content_block_delta":
		switch payload.Delta.Type {
		case "text_delta":
			if nonEmptyString(payload.Delta.Text) {
				return outputFacts(event, OutputText, true), nil
			}
		case "input_json_delta":
			if nonEmptyString(payload.Delta.PartialJSON) {
				return outputFacts(event, OutputTool, false), nil
			}
		case "thinking_delta":
			if nonEmptyString(payload.Delta.Thinking) {
				return outputFacts(event, OutputReasoning, false), nil
			}
		}
	case "message_stop":
		return terminalFacts(event, TerminalCompleted), nil
	case "error":
		return terminalFacts(event, TerminalFailed), nil
	}
	return nil, nil
}

func (*anthropicDetector) Finish(time.Time, bool) ([]Fact, error) { return nil, nil }
