// Package gemini provides the Gemini CLI provider implementation
package gemini

// GeminiRequest represents a Gemini API generateContent request
type GeminiRequest struct {
	Contents          []Content          `json:"contents"`
	SystemInstruction *Content           `json:"systemInstruction,omitempty"`
	GenerationConfig  *GenerationConfig  `json:"generationConfig,omitempty"`
	SafetySettings    []SafetySetting    `json:"safetySettings,omitempty"`
	Tools             []Tool             `json:"tools,omitempty"`
	ToolConfig        *ToolConfig        `json:"toolConfig,omitempty"`
}

// Content represents a content block in Gemini format
type Content struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

// Part represents a part of content (text, image, etc.)
type Part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *InlineData `json:"inlineData,omitempty"`
	FileData   *FileData   `json:"fileData,omitempty"`
}

// InlineData represents inline binary data (e.g., images)
type InlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // base64 encoded
}

// FileData represents a file reference
type FileData struct {
	MimeType string `json:"mimeType"`
	FileURI  string `json:"fileUri"`
}

// GenerationConfig contains generation parameters
type GenerationConfig struct {
	Temperature      *float64 `json:"temperature,omitempty"`
	TopP             *float64 `json:"topP,omitempty"`
	TopK             *int     `json:"topK,omitempty"`
	MaxOutputTokens  *int     `json:"maxOutputTokens,omitempty"`
	StopSequences    []string `json:"stopSequences,omitempty"`
	CandidateCount   *int     `json:"candidateCount,omitempty"`
	ResponseMimeType string   `json:"responseMimeType,omitempty"`
}

// SafetySetting represents a safety setting
type SafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

// Tool represents a tool definition
type Tool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations,omitempty"`
}

// FunctionDeclaration represents a function that can be called
type FunctionDeclaration struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// ToolConfig represents tool configuration
type ToolConfig struct {
	FunctionCallingConfig *FunctionCallingConfig `json:"functionCallingConfig,omitempty"`
}

// FunctionCallingConfig represents function calling configuration
type FunctionCallingConfig struct {
	Mode                 string   `json:"mode,omitempty"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

// GeminiResponse represents a Gemini API response
type GeminiResponse struct {
	Candidates     []Candidate     `json:"candidates"`
	PromptFeedback *PromptFeedback `json:"promptFeedback,omitempty"`
	UsageMetadata  *UsageMetadata  `json:"usageMetadata,omitempty"`
}

// Candidate represents a response candidate
type Candidate struct {
	Content       *Content       `json:"content,omitempty"`
	FinishReason  string         `json:"finishReason,omitempty"`
	Index         int            `json:"index"`
	SafetyRatings []SafetyRating `json:"safetyRatings,omitempty"`
}

// SafetyRating represents a safety rating
type SafetyRating struct {
	Category    string `json:"category"`
	Probability string `json:"probability"`
}

// PromptFeedback represents feedback about the prompt
type PromptFeedback struct {
	BlockReason   string         `json:"blockReason,omitempty"`
	SafetyRatings []SafetyRating `json:"safetyRatings,omitempty"`
}

// UsageMetadata represents token usage information
type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// GeminiModel represents a model in the Gemini API
type GeminiModel struct {
	Name                       string   `json:"name"`
	Version                    string   `json:"version,omitempty"`
	DisplayName                string   `json:"displayName,omitempty"`
	Description                string   `json:"description,omitempty"`
	InputTokenLimit            int      `json:"inputTokenLimit,omitempty"`
	OutputTokenLimit           int      `json:"outputTokenLimit,omitempty"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods,omitempty"`
	Temperature                float64  `json:"temperature,omitempty"`
	TopP                       float64  `json:"topP,omitempty"`
	TopK                       int      `json:"topK,omitempty"`
}

// GeminiModelsResponse represents the response from listing models
type GeminiModelsResponse struct {
	Models        []GeminiModel `json:"models"`
	NextPageToken string        `json:"nextPageToken,omitempty"`
}

// StreamChunk represents a streaming response chunk
type StreamChunk struct {
	Candidates    []Candidate    `json:"candidates,omitempty"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
}
