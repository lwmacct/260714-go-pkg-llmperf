// Package llmperf measures protocol-visible milestones and derived performance
// metrics for one LLM API response.
//
// A Decoder consumes exactly one response. The caller supplies all timestamps;
// the package does not read the system clock. Streaming SSE responses can
// produce first-output and first-text milestones. Non-streaming JSON responses
// intentionally expose only transport and end-to-end metrics.
package llmperf
