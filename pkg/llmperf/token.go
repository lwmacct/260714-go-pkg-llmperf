package llmperf

// TokenBasis identifies how an output token count was obtained.
type TokenBasis string

const (
	TokenBasisProviderReported TokenBasis = "provider_reported"
	TokenBasisTokenizerCounted TokenBasis = "tokenizer_counted"
	TokenBasisEstimated        TokenBasis = "estimated"
)

// TokenScope identifies which output the token count covers.
type TokenScope string

const (
	TokenScopeProviderOutput TokenScope = "provider_output"
	TokenScopeVisibleText    TokenScope = "visible_text"
	TokenScopeUnknown        TokenScope = "unknown"
)

// TokenCount supplies an optional output token count for rate calculations.
type TokenCount struct {
	OutputTokens int64      `json:"output_tokens"`
	Basis        TokenBasis `json:"basis"`
	Scope        TokenScope `json:"scope"`
}

func normalizeTokenCount(count *TokenCount) (*TokenCount, error) {
	if count == nil {
		return nil, nil
	}
	copyCount := *count
	if copyCount.OutputTokens < 0 {
		return nil, ErrInvalidOptions
	}
	switch copyCount.Basis {
	case TokenBasisProviderReported, TokenBasisTokenizerCounted, TokenBasisEstimated:
	default:
		return nil, ErrInvalidOptions
	}
	switch copyCount.Scope {
	case TokenScopeProviderOutput, TokenScopeVisibleText, TokenScopeUnknown:
	default:
		return nil, ErrInvalidOptions
	}
	return &copyCount, nil
}
