package llmperf

import (
	"strings"
	"testing"
	"time"
)

func BenchmarkDecoderDenseSSE(b *testing.B) {
	event := `data: {"object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"x"},"finish_reason":null}]}

`
	stream := []byte(strings.Repeat(event, 256) + `data: [DONE]

`)
	start := time.Unix(100, 0)
	b.ReportAllocs()
	b.SetBytes(int64(len(stream)))
	b.ResetTimer()
	for b.Loop() {
		decoder, err := NewDecoder(Options{Protocol: ProtocolOpenAIChatCompletions, Format: FormatSSE, RequestStartedAt: start})
		if err != nil {
			b.Fatal(err)
		}
		if _, err = decoder.FeedAt(start.Add(time.Second), stream); err != nil {
			b.Fatal(err)
		}
		if _, err = decoder.FinishAt(Completion{At: start.Add(2 * time.Second), Outcome: OutcomeCompleted}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecoderLargeDelta(b *testing.B) {
	delta := strings.Repeat("x", 32<<10)
	stream := []byte(`event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"` + delta + `"}

event: response.completed
data: {"type":"response.completed"}

`)
	start := time.Unix(100, 0)
	b.ReportAllocs()
	b.SetBytes(int64(len(stream)))
	b.ResetTimer()
	for b.Loop() {
		decoder, err := NewDecoder(Options{Protocol: ProtocolOpenAIResponses, Format: FormatSSE, RequestStartedAt: start})
		if err != nil {
			b.Fatal(err)
		}
		if _, err = decoder.FeedAt(start.Add(time.Second), stream); err != nil {
			b.Fatal(err)
		}
		if _, err = decoder.FinishAt(Completion{At: start.Add(2 * time.Second), Outcome: OutcomeCompleted}); err != nil {
			b.Fatal(err)
		}
	}
}
