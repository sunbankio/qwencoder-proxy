package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const (
	BaseURL = "http://localhost:8143"
)

type TestCase struct {
	Provider string
	Model    string
	Path     string
}

var allTestCases = []TestCase{
	{"qwen", "qwen3-coder-plus", "/qwen/v1/chat/completions"},
	{"kiro", "claude-haiku-4-5", "/kiro/v1/chat/completions"},
	{"gemini", "gemini-2.5-flash", "/gemini/v1/chat/completions"},
	{"antigravity", "gemini-3-flash", "/antigravity/v1/chat/completions"},
	{"iflow", "kimi-k2", "/iflow/v1/chat/completions"},
}

// OpenAI Response Structures for Validation
type OpenAIMessage struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OpenAIChoice struct {
	Index        int            `json:"index"`
	Message      *OpenAIMessage `json:"message,omitempty"`
	Delta        *OpenAIMessage `json:"delta,omitempty"`
	FinishReason interface{}    `json:"finish_reason"` // Can be string or null
}

type OpenAIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   *OpenAIUsage   `json:"usage,omitempty"`
}

type ModelListResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

func main() {
	providerFlag := flag.String("provider", "", "Specify a single provider to test (qwen, kiro, gemini, antigravity, iflow)")
	flag.Parse()

	// Always test /v1/models (General)
	fmt.Println("=== Testing /v1/models (General) ===")
	if err := testModelsEndpoint("/v1/models"); err != nil {
		fmt.Printf("FAIL: %v\n", err)
	} else {
		fmt.Println("PASS")
	}

	selectedTests := []TestCase{}
	if *providerFlag != "" {
		found := false
		for _, tc := range allTestCases {
			if tc.Provider == *providerFlag {
				selectedTests = append(selectedTests, tc)
				found = true
			}
		}
		if !found {
			fmt.Printf("Error: Unknown provider '%s'\n", *providerFlag)
			os.Exit(1)
		}
	} else {
		selectedTests = allTestCases
	}

	for _, tc := range selectedTests {
		fmt.Printf("\n=== Testing Provider: %s (Model: %s) ===\n", tc.Provider, tc.Model)

		// Test Models Endpoint for this provider
		modelsPath := fmt.Sprintf("/%s/v1/models", tc.Provider)
		fmt.Printf("--- Testing %s ---\n", modelsPath)
		if err := testModelsEndpoint(modelsPath); err != nil {
			fmt.Printf("FAIL: %v\n", err)
		} else {
			fmt.Println("PASS")
		}

		// Test Non-Stream
		fmt.Printf("--- Testing Non-Stream Chat Completion ---\n")
		if err := testChatCompletion(tc, false); err != nil {
			fmt.Printf("FAIL: %v\n", err)
		} else {
			fmt.Println("PASS")
		}

		// Test Stream
		fmt.Printf("--- Testing Stream Chat Completion ---\n")
		if err := testChatCompletion(tc, true); err != nil {
			fmt.Printf("FAIL: %v\n", err)
		} else {
			fmt.Println("PASS")
		}
	}
}

func testModelsEndpoint(path string) error {
	resp, err := http.Get(BaseURL + path)
	if err != nil {
		return fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var result ModelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode JSON: %v", err)
	}

	if result.Object != "list" {
		return fmt.Errorf("expected object 'list', got '%s'", result.Object)
	}
	if len(result.Data) == 0 {
		return fmt.Errorf("models list is empty")
	}

	// Basic validation of model objects
	for _, m := range result.Data {
		if m.ID == "" {
			return fmt.Errorf("model ID is empty")
		}
	}

	return nil
}

func testChatCompletion(tc TestCase, stream bool) error {
	reqBody := map[string]interface{}{
		"model": tc.Model,
		"messages": []map[string]string{
			{"role": "user", "content": "Say hello!"},
		},
		"stream": stream,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %v", err)
	}

	resp, err := http.Post(BaseURL+tc.Path, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	if stream {
		return validateStreamResponse(resp.Body)
	}

	// Read the entire response body for non-stream validation
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %v", err)
	}

	return validateNonStreamResponse(bodyBytes)
}

func validateNonStreamResponse(body []byte) error {
	var resp OpenAIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("failed to decode JSON: %v, raw response: %s", err, string(body))
	}

	if resp.Object != "chat.completion" {
		return fmt.Errorf("expected object 'chat.completion', got '%s', raw response: %s", resp.Object, string(body))
	}
	if len(resp.Choices) == 0 {
		return fmt.Errorf("no choices returned, raw response: %s", string(body))
	}
	if resp.Choices[0].Message == nil {
		return fmt.Errorf("choice message is nil, raw response: %s", string(body))
	}
	if resp.Choices[0].Message.Content == "" {
		return fmt.Errorf("message content is empty, raw response: %s", string(body))
	}
	if resp.ID == "" {
		return fmt.Errorf("response ID is empty, raw response: %s", string(body))
	}
	if resp.Created == 0 {
		return fmt.Errorf("created timestamp is 0, raw response: %s", string(body))
	}

	return nil
}

func validateStreamResponse(body io.Reader) error {
	scanner := bufio.NewScanner(body)
	hasContent := false
	hasDone := false

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data:") {
			return fmt.Errorf("invalid stream line format: %s", line)
		}

		data := strings.TrimPrefix(line, "data:")
		data = strings.TrimPrefix(data, " ") // Remove leading space if present
		if data == "[DONE]" {
			hasDone = true
			continue
		}

		var chunk OpenAIResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("failed to unmarshal chunk: %v, data: %s", err, data)
		}

		if chunk.Object != "chat.completion.chunk" {
			return fmt.Errorf("expected object 'chat.completion.chunk', got '%s'", chunk.Object)
		}
		if len(chunk.Choices) > 0 {
			if chunk.Choices[0].Delta != nil && chunk.Choices[0].Delta.Content != "" {
				hasContent = true
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading stream: %v", err)
	}

	if !hasContent {
		return fmt.Errorf("stream finished but no content received")
	}
	if !hasDone {
		fmt.Printf("WARNING: [DONE] message not received (this is normal for some providers like iFlow)\n")
	}

	return nil
}
