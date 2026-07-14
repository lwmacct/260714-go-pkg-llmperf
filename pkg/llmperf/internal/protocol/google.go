package protocol

import (
	"encoding/json"
	"time"
)

type googleDetector struct {
	maxDepth     int
	lastFinishAt time.Time
	lastSequence uint64
	terminal     TerminalState
}

func (*googleDetector) Kind() Kind { return GoogleGenerateContent }

func (d *googleDetector) Process(event Event) ([]Fact, error) {
	var payload struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text           json.RawMessage `json:"text"`
					Thought        bool            `json:"thought"`
					FunctionCall   json.RawMessage `json:"functionCall"`
					ExecutableCode json.RawMessage `json:"executableCode"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
	}
	if err := decodeJSON(event.Data, d.maxDepth, &payload); err != nil {
		return nil, err
	}
	var facts []Fact
	for _, candidate := range payload.Candidates {
		for _, part := range candidate.Content.Parts {
			if nonEmptyString(part.Text) {
				if part.Thought {
					facts = append(facts, outputFacts(event, OutputReasoning, false)...)
				} else {
					facts = append(facts, outputFacts(event, OutputText, true)...)
				}
			}
			if nonNullObject(part.FunctionCall) || nonNullObject(part.ExecutableCode) {
				facts = append(facts, outputFacts(event, OutputTool, false)...)
			}
		}
		if candidate.FinishReason != "" && candidate.FinishReason != "FINISH_REASON_UNSPECIFIED" {
			d.lastFinishAt = event.At
			d.lastSequence = event.Sequence
			if candidate.FinishReason == "STOP" {
				if d.terminal == TerminalUnknown {
					d.terminal = TerminalCompleted
				}
			} else {
				d.terminal = TerminalIncomplete
			}
		}
	}
	return facts, nil
}

func (d *googleDetector) Finish(_ time.Time, clean bool) ([]Fact, error) {
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
