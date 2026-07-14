package llmperf_test

import (
	"fmt"
	"time"

	"github.com/lwmacct/260714-go-pkg-llmperf/pkg/llmperf"
)

func ExampleDecoder() {
	startedAt := time.Unix(100, 0)
	decoder, err := llmperf.NewDecoder(llmperf.Options{
		Protocol:         llmperf.ProtocolOpenAIResponses,
		Format:           llmperf.FormatSSE,
		RequestStartedAt: startedAt,
	})
	if err != nil {
		panic(err)
	}

	stream := []byte(`event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"Hello"}

event: response.completed
data: {"type":"response.completed"}

`)
	updates, err := decoder.FeedAt(startedAt.Add(500*time.Millisecond), stream)
	if err != nil {
		panic(err)
	}
	result, err := decoder.FinishAt(llmperf.Completion{
		At:      startedAt.Add(time.Second),
		Outcome: llmperf.OutcomeCompleted,
		TokenCount: &llmperf.TokenCount{
			OutputTokens: 6,
			Basis:        llmperf.TokenBasisProviderReported,
			Scope:        llmperf.TokenScopeProviderOutput,
		},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(len(updates), result.Terminal, result.Metrics.TimeToFirstOutput.Value)
	// Output: 4 completed 500ms
}
