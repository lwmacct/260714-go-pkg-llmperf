# 路线图

本文只记录 `llmperf` 包自身的完成状态和后续演进条件，不包含具体消费方的接入计划，也不绑定发布版本号。

## 当前基线

### 公共契约

- `Protocol`、`Format`、`Options`、`Outcome` 和 `TerminalState`；
- timestamped `ResponseHeadersAt`、`FeedAt` 和 `FinishAt`；
- 增量 `Update`、最终 `Result` 和显式 `Milestone` presence；
- `TokenCount` basis/scope；
- duration/rate availability、稳定 unavailable reason 和 metric basis；
- 严格时间顺序、sticky terminal error 和幂等 finalization。

### 协议与 framing

- OpenAI Responses；
- OpenAI Chat Completions；
- Anthropic Messages；
- Google GenerateContent；
- SSE `ProtocolAuto`；
- JSON transport-only 行为；
- BOM、LF/CRLF/CR、多行 data、comment、id、retry 和任意 chunk boundary；
- bounded SSE metadata、event data 和 JSON nesting。

### 质量基线

- 四类协议的 text fixture；
- tool-only、reasoning/thinking、refusal、空 delta 和 usage-only/control event；
- provider completed/incomplete/failed、clean EOF 和 transport interruption；
- 多 choice/candidate completion；
- 任意 byte split、buffer reuse、时间逆序、资源限制和结果不可变性；
- SSE/parser fuzz target；
- dense-event 与 large-delta benchmark；
- package example、架构、指标和协议语义文档。

每次变更至少执行：

```shell
go test ./...
go test -race ./...
go vet ./...
golangci-lint run
```

## 下一阶段：协议一致性加固

- 随官方 schema 变化增加脱敏 fixture，不根据模型名称添加特殊分支；
- 为每个已支持的 output kind 增加正常、空值、null、错误类型和未知字段测试；
- 扩充多 choice/candidate 的交错输出、不同完成原因和部分中断场景；
- 核对兼容服务对 `[DONE]`、terminal event 和 usage-only 尾事件的差异；
- 对新增 fixture 保持任意 byte split 等价性。

## 下一阶段：资源与性能加固

- 定期运行 fuzz，并只将能固定新行为的最小样本纳入 corpus；
- 持续记录 dense-event、large-delta 的 throughput、allocations/op 和 bytes/op；
- 增加长时间流、密集 heartbeat、极深未知字段和极端 chunk fragmentation benchmark；
- 验证 decoder retained memory 受 options 上限约束，不随完整响应累计增长；
- 只有 benchmark 证明标准 JSON event decoding 是真实瓶颈时，才实现更复杂的选择性 scanner。

## 下一阶段：公共 API 稳定性

- 发布前审计所有 exported identifier、JSON field、enum value 和 error identity；
- 为 `Result` JSON 表示增加 golden tests，确保可用的零值 metric 不被省略；
- 明确 minor release 中允许新增的 protocol event、output kind 和 unavailable reason；
- 破坏性语义修正必须同时更新 fixture、指标文档和迁移说明。

## 按需求评估

以下能力不预先实现，只有出现真实调用场景和 fixture 后才进入计划：

- 新的内置 LLM wire protocol；
- SSE 以外的流式 framing，例如 NDJSON；
- 多模态 output kind；
- 可插拔 protocol detector；
- provider timestamp 或 server timing 与本地 chunk timestamp 的联合分析；
- 更细粒度的 candidate-level timeline。

## 非目标

- HTTP transport/body wrapper 和自动读取系统时钟；
- token usage 提取、tokenizer、定价和计费；
- provider/model 身份推断；
- 日志、metrics backend、消息投递或 telemetry exporter；
- 保存 prompt、完整输出文本、reasoning 或 tool arguments；
- 从非流式 JSON 推断真实 TTFT/TTFC；
- 将一个 SSE delta 等同于一个 token。
