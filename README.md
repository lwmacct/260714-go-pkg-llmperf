# LLM Performance Go 测量库

`pkg/llmperf` 从一个 LLM API response 的带时间戳响应片段中识别生成里程碑，并计算 TTFB、TTFT、TTFC、端到端延迟、TPOT 和 token throughput。

这个包面向透明代理、API gateway、SDK instrumentation、录制回放和离线性能分析。它只处理单个 response 的协议事实和性能计算，不负责 HTTP transport、token usage 解析、日志投递或供应商计费。

## 核心设计

```text
timestamped response bytes
  -> SSE framing
  -> protocol milestone detector
  -> response timeline
  -> metric derivation (+ optional output token count)
```

包名和 import path：

```go
import "github.com/lwmacct/260714-go-pkg-llmperf/pkg/llmperf"
```

当前支持：

| Protocol | JSON | SSE semantic milestones |
| --- | --- | --- |
| OpenAI Responses (`openai.responses`) | transport metrics | 支持 |
| OpenAI Chat Completions (`openai.chat-completions`) | transport metrics | 支持 |
| Anthropic Messages (`anthropic.messages`) | transport metrics | 支持 |
| Google GenerateContent (`google.generate-content`) | transport metrics | 支持 |

普通 JSON response 无法还原服务端实际产生首个 token 的时刻，因此只提供 transport/E2E 指标，不伪造 TTFT 或 TTFC。流式响应的语义里程碑时间是调用方传入的 chunk 到达时间；一个 chunk 内的多个 SSE event 可能具有相同时间戳。

## 公共 API

```go
decoder, err := llmperf.NewDecoder(llmperf.Options{
	Protocol:         llmperf.ProtocolOpenAIResponses,
	Format:           llmperf.FormatSSE,
	RequestStartedAt: requestStartedAt,
})
if err != nil {
	return err
}

if err := decoder.ResponseHeadersAt(responseHeadersAt); err != nil {
	return err
}

for {
	n, readErr := responseBody.Read(buffer)
	readAt := time.Now()
	if n > 0 {
		updates, decodeErr := decoder.FeedAt(readAt, buffer[:n])
		if decodeErr != nil {
			return decodeErr
		}
		consumeMilestones(updates)
	}
	if readErr != nil {
		outcome := llmperf.OutcomeCompleted
		if readErr != io.EOF {
			outcome = llmperf.OutcomeInterrupted
		}
		result, finishErr := decoder.FinishAt(llmperf.Completion{
			At:      time.Now(),
			Outcome: outcome,
			TokenCount: &llmperf.TokenCount{
				OutputTokens: 120,
				Basis:        llmperf.TokenBasisProviderReported,
				Scope:        llmperf.TokenScopeProviderOutput,
			},
		})
		consumeResult(result)
		if finishErr != nil {
			return errors.Join(readErr, finishErr)
		}
		if readErr == io.EOF {
			return nil
		}
		return readErr
	}
}
```

`ResponseHeadersAt` 是可选里程碑。`RequestStartedAt`、`FeedAt` 和 `FinishAt` 均使用调用方时间，不在包内读取系统时钟，以保证代理观测、回放和测试具有相同语义。

`FeedAt` 返回首次出现的增量里程碑，例如：

- `first_byte`：首次收到非空 body bytes；
- `first_output`：首次识别到非空生成输出，可能是文本、tool call 或 reasoning；
- `first_text`：首次识别到非空可见文本；
- `generation_completed`：协议能够确认的生成完成点。

`FinishAt` 返回一个 response 的最终 `Result`。重复调用返回同一份不可变结果；完成后再调用 `FeedAt` 返回 `ErrFinished`。Decoder 对应一个 response，且不保证并发安全。

## 指标命名

- TTFB：`first body byte - request start`；
- TTFT：`first generated output - request start`，它是协议 delta 的 chunk-arrival 近似值，不声称知道 tokenizer 内部的逐 token 时间；
- TTFC：`first non-empty visible text - request start`；
- E2E latency：`response end - request start`；
- TPOT：`(generation complete - first scoped output) / (output tokens - 1)`；
- generation TPS：`(output tokens - 1) / (generation complete - first scoped output)`；
- E2E TPS：`output tokens / (response end - request start)`。

TPOT 和 generation TPS 只有在存在兼容 scope 的 token count、至少 2 个 output token、首输出和生成完成点时才可用。不可用指标必须包含结构化原因，不能使用 `0`、`NaN` 或 `+Inf` 冒充有效结果。

完整指标语义见 [`docs/metrics.md`](docs/metrics.md)，协议映射见 [`docs/protocols.md`](docs/protocols.md)。

## 开发验证

```shell
go test ./...
go test -race ./...
go vet ./...
```

测试覆盖四类协议 fixture 的任意 byte split、SSE framing、auto detection、tool/reasoning、空 delta、多候选、资源限制、时间顺序、token scope、fuzz target 和 benchmark。

## 官方资料基线

协议事件和字段于 2026-07-15 按以下官方资料核对：

- OpenAI Responses API：<https://developers.openai.com/api/reference/resources/responses/>；
- OpenAI Chat Completions streaming events：<https://developers.openai.com/api/reference/resources/chat/subresources/completions/streaming-events>；
- Anthropic streaming Messages：<https://docs.anthropic.com/en/api/messages-streaming>；
- Google Gemini GenerateContent：<https://ai.google.dev/api/generate-content>。

新增事件必须通过官方资料与脱敏 fixture 驱动，不根据模型名称或供应商营销名称添加分支。

## 明确不负责

- HTTP request/response wrapper、Content-Type 判断或自动计时；
- token usage 提取、tokenizer、价格表、费用与配额；
- provider/model 身份推断；
- 日志、metrics backend、Fluent、OpenTelemetry 或消息投递；
- 保存 prompt、完整输出文本或 tool 参数；
- 从非流式 JSON 推断真实 TTFT；
- 将一个 SSE delta 当作一个 token。

内部结构与扩展规则见 [`docs/architecture.md`](docs/architecture.md)，后续演进计划见 [`docs/roadmap.md`](docs/roadmap.md)。
