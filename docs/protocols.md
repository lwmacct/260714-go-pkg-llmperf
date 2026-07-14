# 协议里程碑映射

本文定义当前 detector 的规范化行为。新增字段必须用官方资料和脱敏 fixture 驱动。

## OpenAI Responses

第一输出候选：

- 非空 `response.output_text.delta`；
- 非空 `response.refusal.delta`；
- 非空 function/tool arguments delta；
- 协议明确的 reasoning summary delta。

第一可见文本：非空 output text 或可见 refusal delta。tool arguments 和隐藏 reasoning 不属于 first text。

terminal：`response.completed`、`response.incomplete`、`response.failed` 等 terminal event，分别保留 terminal state。成功、未完成和失败不能合并为一个 completed bool。

## OpenAI Chat Completions

第一输出候选：

- 非空 `choices[].delta.content`；
- 非空 `choices[].delta.refusal`；
- 非空 `choices[].delta.tool_calls[].function.arguments`。

第一可见文本：非空 content/refusal。role-only、空 delta 和 usage-only chunk 不产生首次输出。

candidate 完成：`choices[].finish_reason != null`。多 choice 时记录完成时间，并以最后完成的 choice 作为 generation completion candidate。`[DONE]` 是 stream terminal，不是输出；在 `[DONE]` 或 clean EOF 时固化最后一个 finish time。

usage-only chunk 可能晚于 finish_reason，只用于调用方另行取得 token count，不应推迟 generation completion。

## Anthropic Messages

第一输出候选来自 `content_block_delta`：

- `text_delta.text` -> visible text；
- `input_json_delta.partial_json` -> tool output；
- `thinking_delta.thinking` -> reasoning output。

空 delta、ping、message_start 和 content_block_start 本身不产生 first output。`signature_delta` 不视为用户内容。

terminal：`message_stop`。`message_delta.delta.stop_reason` 是结束原因/临近结束信号，但 generation completion 使用正式 `message_stop`；如果只有 clean EOF，则由通用 transport EOF fallback 提供低精度完成点，terminal state 保持 `unknown`。

## Google GenerateContent

第一输出候选：

- `candidates[].content.parts[].text` 非空；
- `functionCall` 或其他明确生成 part；
- 标记为 thought/reasoning 的 part 只算 first output，不算 first visible text。

第一可见文本：非空且不是 thought 的 text part。

candidate 完成：非空 `finishReason`。多 candidate 记录各 candidate 的完成时刻；没有独立 stream terminal 时在 clean EOF 固化最后一个 finish time。只有 usageMetadata 的 event 不产生输出。

## 错误与控制事件

任何协议中的 heartbeat、ping、metadata、usage-only、空 delta 和控制事件都不能产生 TTFT/TTFC。协议 error event 应记录 terminal failed state；如果 body 结构仍合法，返回性能 Result 而不是 parser error。只有 framing/JSON/limit/lifecycle 错误才从 decoder 返回 error。

## Auto detection

Auto detector 在同一次调用中识别并处理第一条强协议事件。比如第一个 Chat content event 同时也是识别证据，first output 时间仍是该 event 完成时的 chunk 时间，不会变成下一次 FeedAt 时间。

弱信号不能锁定协议：

- 通用 `error`；
- `ping`/heartbeat；
- 空 JSON object；
- 单独 `[DONE]`；
- 只有 usage-like 数字但缺少稳定 wire signature 的对象。
