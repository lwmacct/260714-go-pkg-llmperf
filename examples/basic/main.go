package main

import (
	"fmt"
	"time"

	"github.com/lwmacct/260714-go-pkg-llmperf/pkg/llmperf"
)

func main() {
	startedAt := time.Now()
	decoder, err := llmperf.NewDecoder(llmperf.Options{
		Protocol:         llmperf.ProtocolOpenAIResponses,
		Format:           llmperf.FormatSSE,
		RequestStartedAt: startedAt,
	})
	if err != nil {
		panic(err)
	}

	chunkAt := startedAt.Add(250 * time.Millisecond)
	stream := []byte(`event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"Hello"}

event: response.completed
data: {"type":"response.completed"}

`)
	updates, err := decoder.FeedAt(chunkAt, stream)
	if err != nil {
		panic(err)
	}
	result, err := decoder.FinishAt(llmperf.Completion{
		At:      startedAt.Add(500 * time.Millisecond),
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

	fmt.Printf("updates=%d ttft=%s terminal=%s\n", len(updates), result.Metrics.TimeToFirstOutput.Value, result.Terminal)
}
