package llmperf

import (
	"testing"
	"time"
)

func TestProtocolOutputKindsAndTerminalStates(t *testing.T) {
	start := time.Unix(8000, 0)
	tests := []struct {
		name       string
		protocol   Protocol
		stream     string
		outputKind OutputKind
		text       bool
		terminal   TerminalState
	}{
		{
			name: "responses refusal incomplete", protocol: ProtocolOpenAIResponses,
			stream: `event: response.refusal.delta
data: {"type":"response.refusal.delta","delta":"No"}

event: response.incomplete
data: {"type":"response.incomplete","response":{}}

`,
			outputKind: OutputRefusal, text: true, terminal: TerminalIncomplete,
		},
		{
			name: "responses tool failed", protocol: ProtocolOpenAIResponses,
			stream: `event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","delta":"{"}

event: response.failed
data: {"type":"response.failed","response":{}}

`,
			outputKind: OutputTool, text: false, terminal: TerminalFailed,
		},
		{
			name: "chat tool length", protocol: ProtocolOpenAIChatCompletions,
			stream: `data: {"object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"function":{"arguments":"{"}}]},"finish_reason":null}]}

data: {"object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}

data: [DONE]

`,
			outputKind: OutputTool, text: false, terminal: TerminalIncomplete,
		},
		{
			name: "anthropic thinking failed", protocol: ProtocolAnthropicMessages,
			stream: `event: content_block_delta
data: {"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"hmm"}}

event: error
data: {"type":"error","error":{"type":"overloaded_error"}}

`,
			outputKind: OutputReasoning, text: false, terminal: TerminalFailed,
		},
		{
			name: "google function safety", protocol: ProtocolGoogleGenerateContent,
			stream: `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{}}}]}}]}

data: {"candidates":[{"finishReason":"SAFETY"}]}

`,
			outputKind: OutputTool, text: false, terminal: TerminalIncomplete,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decoder, err := NewDecoder(Options{Protocol: test.protocol, Format: FormatSSE, RequestStartedAt: start})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = decoder.FeedAt(start.Add(time.Second), []byte(test.stream)); err != nil {
				t.Fatal(err)
			}
			result, err := decoder.FinishAt(Completion{At: start.Add(2 * time.Second), Outcome: OutcomeCompleted})
			if err != nil {
				t.Fatal(err)
			}
			if result.Milestones.FirstOutput.OutputKind != test.outputKind || result.Milestones.FirstText.Observed != test.text || result.Terminal != test.terminal {
				t.Fatalf("unexpected result: %#v", result)
			}
		})
	}
}

func TestEmptyAndControlEventsDoNotProduceOutput(t *testing.T) {
	start := time.Unix(9000, 0)
	streams := map[Protocol]string{
		ProtocolOpenAIResponses: `: heartbeat

event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":""}

`,
		ProtocolOpenAIChatCompletions: `data: {"object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: [DONE]

`,
		ProtocolAnthropicMessages: `event: ping
data: {"type":"ping"}

event: content_block_start
data: {"type":"content_block_start","content_block":{"type":"text","text":""}}

`,
		ProtocolGoogleGenerateContent: `data: {"candidates":[],"usageMetadata":{"candidatesTokenCount":0}}

`,
	}
	for protocol, stream := range streams {
		decoder, err := NewDecoder(Options{Protocol: protocol, Format: FormatSSE, RequestStartedAt: start})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = decoder.FeedAt(start.Add(time.Second), []byte(stream)); err != nil {
			t.Fatalf("%s: %v", protocol, err)
		}
		result, err := decoder.FinishAt(Completion{At: start.Add(2 * time.Second), Outcome: OutcomeCompleted})
		if err != nil {
			t.Fatalf("%s: %v", protocol, err)
		}
		if result.Milestones.FirstOutput.Observed || result.Milestones.FirstText.Observed {
			t.Fatalf("%s produced output: %#v", protocol, result.Milestones)
		}
	}
}

func TestMultipleChatChoicesUseLastFinishCandidate(t *testing.T) {
	start := time.Unix(10000, 0)
	decoder, err := NewDecoder(Options{Protocol: ProtocolOpenAIChatCompletions, Format: FormatSSE, RequestStartedAt: start})
	if err != nil {
		t.Fatal(err)
	}
	chunks := []string{
		`data: {"object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"a"},"finish_reason":null},{"index":1,"delta":{"content":"b"},"finish_reason":null}]}

`,
		`data: {"object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

`,
		`data: {"object":"chat.completion.chunk","choices":[{"index":1,"delta":{},"finish_reason":"stop"}]}

`,
		"data: [DONE]\n\n",
	}
	for index, chunk := range chunks {
		if _, err = decoder.FeedAt(start.Add(time.Duration(index+1)*time.Second), []byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	result, err := decoder.FinishAt(Completion{At: start.Add(5 * time.Second), Outcome: OutcomeCompleted})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Milestones.GenerationCompleted.At.Equal(start.Add(3*time.Second)) || result.Milestones.GenerationCompleted.Basis != MilestoneProtocolCandidate {
		t.Fatalf("unexpected completion: %#v", result.Milestones.GenerationCompleted)
	}
}

func TestChatCleanEOFUsesFinishReasonCandidate(t *testing.T) {
	start := time.Unix(10500, 0)
	decoder, err := NewDecoder(Options{Protocol: ProtocolOpenAIChatCompletions, Format: FormatSSE, RequestStartedAt: start})
	if err != nil {
		t.Fatal(err)
	}
	stream := `data: {"object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"a"},"finish_reason":null}]}

data: {"object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

`
	if _, err = decoder.FeedAt(start.Add(time.Second), []byte(stream)); err != nil {
		t.Fatal(err)
	}
	result, err := decoder.FinishAt(Completion{At: start.Add(2 * time.Second), Outcome: OutcomeCompleted})
	if err != nil {
		t.Fatal(err)
	}
	if result.Terminal != TerminalCompleted || result.Milestones.GenerationCompleted.Basis != MilestoneProtocolCandidate || result.Metrics.GenerationDuration.Basis != MetricBasisProtocolCandidate {
		t.Fatalf("unexpected clean EOF candidate: %#v", result)
	}
}
