// Package kiro provides the Kiro/Claude provider implementation
package kiro

// ClaudeRequest represents an Anthropic/Claude messages API request
type ClaudeRequest struct {
	Model         string         `json:"model"`
	Messages      []Message      `json:"messages"`
	MaxTokens     int            `json:"max_tokens"`
	System        string         `json:"system,omitempty"`
	Temperature   *float64       `json:"temperature,omitempty"`
	TopP          *float64       `json:"top_p,omitempty"`
	TopK          *int           `json:"top_k,omitempty"`
	StopSequences []string       `json:"stop_sequences,omitempty"`
	Stream        bool           `json:"stream,omitempty"`
	Metadata      *Metadata      `json:"metadata,omitempty"`
	Tools         []Tool         `json:"tools,omitempty"`
	ToolChoice    *ToolChoice    `json:"tool_choice,omitempty"`
}

// Message represents a message in the conversation
type Message struct {
	Role    string        `json:"role"`
	Content MessageContent `json:"content"`
}

// MessageContent can be a string or an array of content blocks
type MessageContent interface{}

// ContentBlock represents a content block in a message
type ContentBlock struct {
	Type      string     `json:"type"`
	Text      string     `json:"text,omitempty"`
	Source    *Source    `json:"source,omitempty"`
	ID        string     `json:"id,omitempty"`
	Name      string     `json:"name,omitempty"`
	Input     interface{} `json:"input,omitempty"`
	ToolUseID string     `json:"tool_use_id,omitempty"`
	Content   string     `json:"content,omitempty"`
}

// Source represents an image source
type Source struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// Metadata represents request metadata
type Metadata struct {
	UserID string `json:"user_id,omitempty"`
}

// Tool represents a tool definition
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// ToolChoice represents tool choice configuration
type ToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// ClaudeResponse represents an Anthropic/Claude messages API response
type ClaudeResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   string         `json:"stop_reason,omitempty"`
	StopSequence string         `json:"stop_sequence,omitempty"`
	Usage        *Usage         `json:"usage,omitempty"`
}

// Usage represents token usage information
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// StreamEvent represents a streaming event
type StreamEvent struct {
	Type         string          `json:"type"`
	Message      *ClaudeResponse `json:"message,omitempty"`
	Index        int             `json:"index,omitempty"`
	ContentBlock *ContentBlock   `json:"content_block,omitempty"`
	Delta        *Delta          `json:"delta,omitempty"`
	Usage        *Usage          `json:"usage,omitempty"`
}

// Delta represents incremental content in streaming
type Delta struct {
	Type         string `json:"type,omitempty"`
	Text         string `json:"text,omitempty"`
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
}

// ClaudeModel represents a model in the Claude API
type ClaudeModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

// ClaudeModelsResponse represents the response from listing models
type ClaudeModelsResponse struct {
	Data    []ClaudeModel `json:"data"`
	HasMore bool          `json:"has_more,omitempty"`
}

// KiroCredentials represents the Kiro AWS SSO credentials
type KiroCredentials struct {
	AccessToken  string `json:"accessToken"`
	ExpiresAt    string `json:"expiresAt"`
	RefreshToken string `json:"refreshToken,omitempty"`
	Region       string `json:"region,omitempty"`
	StartURL     string `json:"startUrl,omitempty"`
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	RegistrationExpiresAt string `json:"registrationExpiresAt,omitempty"`
}

// KiroRefreshRequest represents a token refresh request
type KiroRefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	GrantType    string `json:"grant_type,omitempty"`
}

// KiroRefreshResponse represents a token refresh response
type KiroRefreshResponse struct {
	AccessToken  string `json:"accessToken"`
	ExpiresIn    int    `json:"expiresIn"`
	RefreshToken string `json:"refreshToken,omitempty"`
}
