package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

type autoDetector struct {
	maxDepth int
	selected Detector
}

func (d *autoDetector) Kind() Kind {
	if d.selected == nil {
		return Auto
	}
	return d.selected.Kind()
}

func (d *autoDetector) Process(event Event) ([]Fact, error) {
	if d.selected != nil {
		return d.selected.Process(event)
	}
	kind, matched, err := detect(event, d.maxDepth)
	if err != nil {
		return nil, err
	}
	if !matched {
		return nil, nil
	}
	selected, err := New(kind, d.maxDepth)
	if err != nil {
		return nil, err
	}
	d.selected = selected
	return d.selected.Process(event)
}

func (d *autoDetector) Finish(at time.Time, clean bool) ([]Fact, error) {
	if d.selected == nil {
		return nil, ErrUnsupported
	}
	return d.selected.Finish(at, clean)
}

func detect(event Event, maxDepth int) (Kind, bool, error) {
	trimmed := bytes.TrimSpace(event.Data)
	if bytes.Equal(trimmed, []byte("[DONE]")) {
		return "", false, nil
	}
	var probe struct {
		Type       string          `json:"type"`
		Object     string          `json:"object"`
		Candidates json.RawMessage `json:"candidates"`
	}
	if err := decodeJSON(event.Data, maxDepth, &probe); err != nil {
		if event.Type == "message" || event.Type == "error" {
			return "", false, nil
		}
		return "", false, err
	}
	eventType := probe.Type
	if eventType == "" && event.Type != "message" {
		eventType = event.Type
	}
	if strings.HasPrefix(eventType, "response.") {
		return OpenAIResponses, true, nil
	}
	if probe.Object == "chat.completion.chunk" {
		return OpenAIChatCompletions, true, nil
	}
	if isStrongAnthropicEvent(eventType) {
		return AnthropicMessages, true, nil
	}
	if len(probe.Candidates) > 0 && !bytes.Equal(bytes.TrimSpace(probe.Candidates), []byte("null")) {
		return GoogleGenerateContent, true, nil
	}
	return "", false, nil
}

func isStrongAnthropicEvent(eventType string) bool {
	switch eventType {
	case "message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop":
		return true
	default:
		return false
	}
}
