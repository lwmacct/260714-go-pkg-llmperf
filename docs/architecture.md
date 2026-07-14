# 内部架构设计

## 设计目标

`llmperf` 的核心对象是一个 response timeline，而不是日志事件或 usage snapshot。它接收调用方观测到的时间和响应 bytes，识别协议里程碑，最终派生性能指标。

依赖保持单向：

```text
pkg/llmperf public facade
  -> internal/engine
       -> internal/protocol
       -> internal/sse
  public metric derivation
       <- normalized timeline + TokenCount
```

当前目录：

```text
pkg/llmperf/
  decoder.go
  options.go
  protocol.go
  timeline.go
  metric.go
  token.go
  errors.go
  doc.go
  internal/
    engine/
    protocol/
    sse/
  testdata/
    openai-responses/
    openai-chat-completions/
    anthropic-messages/
    google-generate-content/
```

### public facade

负责稳定公共类型、options 和时间顺序校验、内部错误包装、内部 timeline 到公共 `Update`/`Result` 的转换。公共层不暴露协议 decoder 或 parser 接口。

### engine

负责一个 response 的生命周期：

```text
created
  -> headers observed (optional)
  -> body feeding
  -> finalized

any parser/limit error -> terminal error
```

Engine 记录 byte offset、SSE data-event sequence、首次 body byte、协议里程碑和最终 transport outcome。所有时间必须来自调用方，engine 不调用 `time.Now`。

### protocol

每个协议 detector 只输出规范化事实：

```go
type Fact struct {
	Kind       FactKind
	OutputKind OutputKind
	Sequence   uint64
}
```

事实包括 `output_started`、`text_started`、`generation_completed`、`terminal_observed`，并可区分 text、tool、reasoning、refusal 等 output kind。协议层不计算 duration、TPOT 或 TPS。

JSON format 不产生 TTFT/TTFC 协议事实。它被当作 opaque body，只测量 transport milestones；这样不会把完整 JSON 被客户端读到的时间误称为首 token 时间。

### sse

只实现 WHATWG SSE framing，不理解 LLM event、`[DONE]` 或 JSON。它支持 BOM、LF、CRLF、CR、多行 data、comment、event、id、retry 和任意 chunk 边界。

每个完成的 SSE event 携带造成 event 完成的 `FeedAt` 时间。若同一次 `FeedAt` 完成多个 event，它们共享同一个时间戳和 `chunk_observed` 精度。

### protocol event decoding

SSE parser 对单个 data event 使用 `MaxRetainedBytes` 建立有界 buffer；协议 detector 再用只声明目标字段的结构体解码。未知正文不会物化为 map/interface 状态，event 处理完成后立即释放。解码前单独验证最大 JSON nesting。

### metric derivation

只接收已规范化 timeline、transport outcome 和可选 `TokenCount`。它负责：

- 计算 duration/rate；
- 检查 milestone 是否存在及时间顺序；
- 根据 token scope 选择 first-output 或 first-text 起点；
- 为不可用 metric 返回稳定 reason；
- 避免整数溢出、除零、NaN 和 Inf。

## 公共模型草案

### Options

```go
type Options struct {
	Protocol         Protocol
	Format           Format
	RequestStartedAt time.Time

	MaxSSEMetadataBytes int
	MaxRetainedBytes    int
	MaxNestingDepth     int
}
```

- `ProtocolAuto` 首版只支持 SSE。普通 JSON 没有语义时间价值，不为 auto detection 增加扫描成本。
- `RequestStartedAt` 必须非零；需要只做协议里程碑检测的调用方也应提供自己的观测基线。
- limit 为 0 使用默认值，负数无效。

默认限制为：SSE metadata 64 KiB、单个 SSE data event 64 KiB、JSON nesting 128。这些限制独立于模型 context window，用于约束 decoder 的本地资源占用。

### Update

```go
type Update struct {
	Kind       UpdateKind
	At         time.Time
	Sequence   uint64
	OutputKind OutputKind
	Precision  TimestampPrecision
}
```

Update 只在 milestone 第一次出现时返回。它不包含原始文本，调用方在实时输出 `llm.perf.first_text` 等事件时不会意外泄漏模型内容。

`OutputKind` 首版稳定值为 `text`、`refusal`、`tool`、`reasoning`。未知协议字段不能自动归入 output；只有协议 detector 明确理解其生成语义后才能产生 milestone。

### Milestone

最终结果使用显式 presence，而不是用零时间猜测：

```go
type Milestone struct {
	Observed   bool
	At         time.Time
	Sequence   uint64
	Basis      MilestoneBasis
	OutputKind OutputKind
}
```

`Milestones` 至少包含：request started、response headers、first byte、first output、first text、generation completed、response ended。

### Completion

```go
type Completion struct {
	At         time.Time
	Outcome    Outcome
	TokenCount *TokenCount
}
```

`Outcome` 首版包含 `completed`、`interrupted` 和 `canceled`。协议 terminal event 与 transport outcome 分开记录：看见 `finish_reason` 不代表 HTTP body 一定完整到达，clean EOF 也不代表供应商返回了成功 terminal event。

### Result

```go
type Result struct {
	Protocol   Protocol
	Format     Format
	Outcome    Outcome
	Terminal   TerminalState
	Milestones Milestones
	Metrics    Metrics
	TokenCount *TokenCount
}
```

Result 不重复提取 response ID、model、raw usage 或输出内容。这些属于 usage、capture 或调用方 envelope 的职责。

`TerminalState` 首版包含 `unknown`、`completed`、`incomplete`、`failed`。它表示 provider protocol terminal；`Outcome` 表示调用方看到的 transport 生命周期。两者可以组合，例如 protocol `completed` 之后读取 trailer 失败，或 clean EOF 但从未收到正式 terminal event。

### Metric availability

Duration 和 rate 使用不同类型，二者都有显式 availability：

```go
type DurationMetric struct {
	Available bool
	Value     time.Duration
	Reason    UnavailableReason
	Basis     MetricBasis
}

type RateMetric struct {
	Available       bool
	TokensPerSecond float64
	Reason          UnavailableReason
	Basis           MetricBasis
}
```

`Metrics` 使用完整语义名称，缩写只出现在文档和 adapter：

```go
type Metrics struct {
	ResponseHeaderLatency      DurationMetric
	TimeToFirstByte            DurationMetric
	TimeToFirstOutput          DurationMetric
	TimeToFirstText            DurationMetric
	GenerationDuration         DurationMetric
	EndToEndLatency            DurationMetric
	TimePerOutputToken         DurationMetric
	GenerationTokensPerSecond  RateMetric
	EndToEndTokensPerSecond    RateMetric
}
```

其中 `TimeToFirstOutput` 对应本文定义的 TTFT，`TimeToFirstText` 对应 TTFC，`TimePerOutputToken` 对应 TPOT。完整名称可以避免调用方把首包、首输出和首字符混为同一字段。

当 `Available` 为 false 时 `Value` 只是零值，调用方必须读取 `Reason`。稳定 reason 至少包括：

- `non_streaming`；
- `missing_headers`、`missing_first_byte`、`missing_first_output`、`missing_first_text`；
- `missing_generation_completion`、`interrupted`；
- `missing_token_count`、`insufficient_token_count`；
- `ambiguous_token_scope`、`invalid_time_order`。

## 时间模型

时间单调性按调用顺序校验：

- headers、每次非空 FeedAt、FinishAt 不得早于 request start；
- 后一次 body observation 不得早于前一次；
- 相同时间允许，尤其是同一 read 中的多个 SSE event；
- 发现逆序时间立即进入 terminal error，不能静默 clamp 为 0。

调用方传入的 `time.Time` 若携带 Go monotonic component，duration 计算自然使用 monotonic clock。序列化后的 wall time 仅用于展示，不能用于跨进程重新计算纳秒级 duration。

## Decoder 生命周期与错误

公共错误通过 `errors.Is` 判断：

- `ErrInvalidOptions`；
- `ErrInvalidTimestamp`；
- `ErrInvalidLifecycle`；
- `ErrUnsupported`；
- `ErrMalformedStream`；
- `ErrLimitExceeded`；
- `ErrFinished`。

`MeasureError` 包含 protocol、format、stage、byte offset 和 SSE sequence。Feed/headers/finish 阶段的 parser、limit、timestamp 或 lifecycle error 是 sticky terminal error；之后的调用返回同一错误。无效的 `Completion` 参数在进入 engine 前拒绝，可以修正后重试。第一次成功的 `FinishAt` 固化 token count 和 outcome，重复调用返回缓存结果，不接受新的完成参数。

## 自动识别

`ProtocolAuto` 只识别 wire contract，不识别 provider：

- `response.*` event/type -> OpenAI Responses；
- `object = chat.completion.chunk` -> OpenAI Chat Completions；
- Anthropic 强事件和 `type` -> Anthropic Messages；
- candidates/finishReason 等稳定字段 -> Google GenerateContent。

heartbeat、ping、error 和只有 `[DONE]` 的 stream 不能作为唯一识别依据。第一条强协议事件在同一次 `FeedAt` 中完成识别并交给选中的 detector，因此 milestone 保留该 event 原始 chunk 时间，不使用下一次调用时间。

## 资源与隐私

- `FeedAt` 不保留调用方 slice，返回后可以立即复用 buffer；
- 不保留完整 output text、reasoning 或 tool arguments；
- 对 delta 只判断类型和是否非空，不把内容复制进结果；
- `MaxRetainedBytes` 限制单个 SSE data event，处理完成后复用 buffer；
- 包不记录日志，不启动 goroutine，不访问网络，不读取环境变量。

## 扩展规则

新增协议时必须：

1. 为输出、可见文本和 terminal 点给出官方 wire 依据；
2. 实现显式 detector，并在 auto detector 中使用唯一稳定签名；
3. 增加脱敏 fixture、逐字节 chunk 边界和多 event 同 chunk 测试；
4. 明确 tool-only、reasoning-only、多候选和异常 terminal 的语义；
5. 不在协议层加入供应商、模型名、定价或业务标签判断。

首版不提供公开 protocol registration API。外部扩展接口会冻结内部事实模型，应等真实的第三方协议需求出现后再设计。
