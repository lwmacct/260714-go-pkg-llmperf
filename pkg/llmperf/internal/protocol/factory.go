package protocol

func New(kind Kind, maxDepth int) (Detector, error) {
	switch kind {
	case OpenAIResponses:
		return &openAIResponsesDetector{maxDepth: maxDepth}, nil
	case OpenAIChatCompletions:
		return &openAIChatDetector{maxDepth: maxDepth, terminal: TerminalUnknown}, nil
	case AnthropicMessages:
		return &anthropicDetector{maxDepth: maxDepth}, nil
	case GoogleGenerateContent:
		return &googleDetector{maxDepth: maxDepth, terminal: TerminalUnknown}, nil
	case Auto:
		return &autoDetector{maxDepth: maxDepth}, nil
	default:
		return nil, ErrUnsupported
	}
}
