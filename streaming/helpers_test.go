package streaming

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func TestStutteringHandler(t *testing.T) {
	// Read the test file
	file, err := os.Open("streamingtest.txt")
	if err != nil {
		t.Fatalf("failed to open streamingtest.txt: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	stutteringHandler := NewStutteringHandler()
	var actualOutput []string
expectedOutput := []string{
	`{"choices":[{"finish_reason":null,"logprobs":null,"delta":{"content":"**"},"index":0}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":"The Weight"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" of Words"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":"**\n\nMay"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":"a stared at the"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" acceptance"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" letter for Harvard Medical"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" School—her dream"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":", finally within reach"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":". But her hands"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" trembled as she"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" remembered her father's"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" voice"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" from that morning:"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" "},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":"I need you"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" to take"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" over the clinic,"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" mija. This"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" neighborhood depends on us"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":".\"\n\nShe thought of"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" Mrs"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":". Chen, who"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" came in weekly for"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" her diabetes medication,"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" always"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" smiling despite working three"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" jobs. Old Mr"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":". Williams"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":", who couldn't"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" afford"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" the specialist downtown."},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"delta":{"content":" The mothers who brought"},"finish_reason":null,"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`,
	`{"choices":[{"finish_reason":"length","delta":{"content":""},"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756618477,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-69c25da8-a5a6-9ecc-9f9c-d2f9f359d92e"}`, // Line 67, last element.
}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			// Extract the JSON part of the line
			jsonData := strings.TrimPrefix(line, "data: ")
			if jsonData == "[DONE]" {
				continue // Handle DONE message separately if needed, or simply skip
			}
			result, err := stutteringHandler(jsonData)
			if err != nil {
				t.Fatalf("stutteringHandler returned an error: %v", err)
			}
			// Only append if the result is not empty (i.e., not suppressed)
			if result != "" {
				// The handler returns "data: " prefix and "\n\n" suffix, so we need to clean it for comparison
				cleanedResult := strings.TrimPrefix(result, "data: ")
				cleanedResult = strings.TrimSuffix(cleanedResult, "\n\n")
				actualOutput = append(actualOutput, cleanedResult)
			}
		} else if line == "" {
			continue // Skip empty lines between "data:" chunks
		} else {
			t.Logf("Skipping unexpected line: %s", line)
		}
	}

	if len(actualOutput) != len(expectedOutput) {
		t.Errorf("Mismatch in number of output lines. Expected %d, got %d", len(expectedOutput), len(actualOutput))
		t.Logf("Actual Output:\n%s", strings.Join(actualOutput, "\n"))
		t.Logf("Expected Output:\n%s", strings.Join(expectedOutput, "\n"))
	}

	for i := 0; i < len(actualOutput); i++ {
		if i >= len(expectedOutput) {
			t.Errorf("Actual output has more lines than expected. Unexpected line: %s", actualOutput[i])
			break
		}
		if actualOutput[i] != expectedOutput[i] {
			t.Errorf("Mismatch at line %d:\nExpected: %s\nActual: %s", i+1, expectedOutput[i], actualOutput[i])
		}
	}
}