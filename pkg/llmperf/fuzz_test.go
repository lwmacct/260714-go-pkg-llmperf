package llmperf

import (
	"testing"
	"time"
)

func FuzzSSEDecoder(f *testing.F) {
	f.Add([]byte(`data: {"type":"response.output_text.delta","delta":"hello"}

`))
	f.Add([]byte(`event: content_block_delta
data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"x"}}

`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			t.Skip()
		}
		start := time.Unix(100, 0)
		decoder, err := NewDecoder(Options{
			Protocol: ProtocolAuto, Format: FormatSSE, RequestStartedAt: start,
			MaxSSEMetadataBytes: 4096, MaxRetainedBytes: 4096, MaxNestingDepth: 64,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = decoder.FeedAt(start.Add(time.Second), data)
		_, _ = decoder.FinishAt(Completion{At: start.Add(2 * time.Second), Outcome: OutcomeCompleted})
	})
}
