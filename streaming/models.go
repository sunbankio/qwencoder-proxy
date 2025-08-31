package streaming

// Define structs to match the OpenAI API response structure for streaming
type ChatCompletionChunk struct {
	ID      string   `json:"id,omitempty"`
	Object  string   `json:"object,omitempty"`
	Created int64    `json:"created,omitempty"`
	Model   string   `json:"model,omitempty"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

type Choice struct {
	Index        int    `json:"index,omitempty"`
	Delta        Delta  `json:"delta"`
	FinishReason string `json:"finish_reason,omitempty"`
	Logprobs     any    `json:"logprobs,omitempty"`
}

type Delta struct {
	Content string `json:"content"`
}

type Usage struct {
	PromptTokens        int            `json:"prompt_tokens,omitempty"`
	CompletionTokens    int            `json:"completion_tokens,omitempty"`
	TotalTokens         int            `json:"total_tokens,omitempty"`
	PromptTokensDetails *TokensDetails `json:"prompt_tokens_details,omitempty"`
	// Note: The upstream API doesn't seem to send completion_tokens_details
	// but we'll keep this field in case it's added in the future
	CompletionTokensDetails *TokensDetails `json:"completion_tokens_details,omitempty"`
}

type TokensDetails struct {
	CacheType       string `json:"cache_type,omitempty"`
	CachedTokens    int    `json:"cached_tokens,omitempty"`
	ReasoningTokens int    `json:"reasoning_tokens,omitempty"`
}