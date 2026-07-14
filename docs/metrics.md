# 性能指标语义

## 为什么需要三个“首次”指标

HTTP、LLM 协议和用户可见内容处在不同层次：

```text
request start
  -> response headers
  -> first body byte        (TTFB)
  -> first generated delta  (TTFT approximation)
  -> first visible text     (TTFC)
  -> generation complete
  -> response body end
```

它们可能落在不同时间：首个 body event 可能只是 heartbeat；首个生成 delta 可能是 reasoning 或 tool arguments；tool-only response 可能永远没有可见文本。

## 时间指标

| Metric | 公式 | 可用条件 |
| --- | --- | --- |
| response header latency | headers - request start | 调用方报告 headers |
| TTFB | first non-empty body bytes - request start | 至少一次非空 FeedAt |
| TTFT | first generated output - request start | 流式协议识别到非空 output delta |
| TTFC | first visible text - request start | 流式协议识别到非空可见文本 |
| generation duration | generation complete - first generated output | 首输出与完成点存在 |
| E2E latency | response end - request start | FinishAt 成功调用 |

这里的 TTFT 是协议可观测的近似值，不是 tokenizer 内部时钟。一个 delta 可以包含多个 token；一个 HTTP read 也可以同时完成多个 SSE event。

`first generated output` 根据协议可包括：

- visible text/refusal；
- tool/function call arguments；
- 协议明确标记的 reasoning/thinking；
- 协议明确标记的其他生成模态。

`first visible text` 只接受调用方通常会展示给终端用户的非空文本。首版不根据供应商扩展字段或模型名称猜测可见性。

## TokenCount

`llmperf` 不解析 usage，也不实现 tokenizer。调用方可以在完成时注入：

```go
type TokenCount struct {
	OutputTokens int64
	Basis        TokenBasis
	Scope        TokenScope
}
```

建议值：

- Basis：`provider_reported`、`tokenizer_counted`、`estimated`；
- Scope：`provider_output`、`visible_text`、`unknown`。

`provider_output` 可能包含隐藏 reasoning、tool use 或多个 candidates；`visible_text` 只表示实际可见文本。结果必须携带 basis/scope，输出系统不能只发布一个没有语义来源的 TPS 数字。

负 token count 无效。0 或 1 是合法计数，但不足以计算 token 间隔。

## TPOT 与 generation TPS

对于 `N >= 2`：

```text
scoped duration = generation complete - scoped first output
TPOT            = scoped duration / (N - 1)
generation TPS  = (N - 1) / scoped duration
```

`N - 1` 的原因是首个 output 已经在起点出现；后续 duration 覆盖余下 token。它使 TPOT 和 generation TPS 严格互为倒数，并满足常见近似关系：

```text
total latency ~= TTFT + (output tokens - 1) * TPOT
```

scope 决定起点：

- `provider_output` 使用 first generated output；
- `visible_text` 使用 first visible text；
- `unknown` 不计算 TPOT/generation TPS，返回 `ambiguous_token_scope`。

如果 provider token count 包含未流式暴露的隐藏 reasoning，计算结果仍只是 provider-output throughput 的观测近似，而不是可见文本打字速度。调用方应保留 basis/scope，不应重新命名为“用户可见 TPS”。

## E2E TPS

```text
E2E TPS = output tokens / (response end - request start)
```

E2E TPS 包含排队、网络、首 token 等待和生成时间，适合容量/成本视角，不等价于解码速度。只要 token count 与 E2E latency 存在即可计算；scope 仍必须随结果输出。

## 完成点

Timeline 同时保留：

- protocol generation completion；
- transport response end；
- caller-provided outcome。

generation metric 优先使用协议能够确认的生成完成点。若协议没有明确 terminal，只能在 clean EOF 使用 response end，并把 basis 标为 `transport_eof`。interrupted/canceled response 不使用错误发生时间伪造 generation completion。

对于多候选 response，完成点是所有已观测候选的最后完成时刻；无法确认候选集合时，等待协议 stream terminal 或 clean EOF。Token count 的 scope 如果聚合多个候选，first-output 起点也表示整个 response 的第一个输出，因此它是 response-level throughput，不是单 candidate throughput。

## 非流式 JSON

一个完整 JSON body 到达代理时，模型可能早已生成完全部内容。将首次读到 JSON bytes 的时间当作 TTFT 会把 buffering、网络分块和 body 大小混入首 token 延迟。

因此 JSON format：

- 可以测量 headers、TTFB 和 E2E latency；
- TTFT、TTFC、generation duration、TPOT 和 generation TPS 返回 `non_streaming`；
- 如果有 token count，仍可计算 E2E TPS。

## 数值规则

- duration 必须非负；发现时间逆序返回错误，不做 clamp；
- duration 为 0 时 TPOT/TPS 不可用，避免 Inf；
- rate 使用 `float64`，计算前先检查整数范围和 duration；
- 不可用 metric 使用 `Available=false + Reason`，不用 sentinel 负数、NaN 或 Inf；
- JSON/telemetry adapter 自行决定输出 duration 的单位，核心包保持 `time.Duration`。
