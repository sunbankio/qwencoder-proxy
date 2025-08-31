package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"encoding/json"
)

var (
	reader      *bufio.Reader
	file        *os.File
	initialized bool
)

func main() {
	// range over lines in streamingtest.txt
	processor := stutteringProcess()
	for {
		chunk, ok := NewChunk()
		if !ok {
			break
		}

		if chunk != "" {
			// relay to client
			fmt.Printf("%s", processor(chunk))
		}
	}
	if file != nil {
		file.Close()
	}
}

// NewChunk returns the next line from the file streamingtest.txt
func NewChunk() (string, bool) {
	if !initialized {
		var err error
		file, err = os.Open("stutteringprotype/streamingtest.txt")
		if err != nil {
			fmt.Println("Error opening file:", err)
			return "", false
		}
		reader = bufio.NewReader(file)
		initialized = true
	}

	line, err := reader.ReadString('\n')
	if err != nil {
		if err != io.EOF {
			fmt.Println("Error reading file:", err)
		}
		return "", false
	}
	return line, true
}

func stutteringProcess() func(chunk string) string {
	stuttering := true
	buf := ""
	return func(chunk string) string {
		if !stuttering {
			return chunk
		}
		raw := chunkToJson(chunk)
		if len(raw) == 0 {
			return chunk
		}
		extracted := extractDeltaContent(raw)
		if hasPrefixRelationship(extracted, buf) {
			buf = extracted
			return ""
		} else {
			stuttering = false

			modifiedChunk, err := prependDeltaContent(buf, raw)
			if err != nil {
				fmt.Println("Error prepending delta content:", err)
				return "" // Return empty string or handle error as appropriate
			}
			return modifiedChunk
		}
	}
}

func hasPrefixRelationship(a, b string) bool {
	if len(a) < len(b) {
		return strings.HasPrefix(b, a)
	}
	return strings.HasPrefix(a, b)
}

func extractDeltaContent(raw map[string]interface{}) string {
	// it's safe to do this, because raw is validated in chunkToJson
	return raw["choices"].([]interface{})[0].(map[string]interface{})["delta"].(map[string]interface{})["content"].(string)
}

func prependDeltaContent(buf string, raw map[string]interface{}) (string, error) {
	prefix := "data: "
	// it's safe to do this, because raw is validated in chunkToJson
	raw["choices"].([]interface{})[0].(map[string]interface{})["delta"].(map[string]interface{})["content"] = buf + raw["choices"].([]interface{})[0].(map[string]interface{})["delta"].(map[string]interface{})["content"].(string)

	modifiedChunkBytes, err := json.Marshal(raw)
	if err != nil {
		return "", fmt.Errorf("failed to marshal modified chunk: %w", err)
	}
	return prefix + string(modifiedChunkBytes) + "\n", nil
}

func chunkToJson(chunk string) map[string]interface{} {
	trimmedChunk := strings.TrimSpace(chunk)
	if !strings.HasPrefix(trimmedChunk, "data:") {
		return nil // Not a data chunk, return nil
	}

	jsonStr := strings.TrimPrefix(trimmedChunk, "data:")

	var raw map[string]interface{}
	err := json.Unmarshal([]byte(jsonStr), &raw)
	if err != nil {
		return nil // Malformed JSON, return nil
	}

	// Check for choices[0].delta.content and its length
	if choices, ok := raw["choices"].([]interface{}); ok && len(choices) > 0 {
		if choiceMap, ok := choices[0].(map[string]interface{}); ok {
			if delta, ok := choiceMap["delta"].(map[string]interface{}); ok {
				if content, ok := delta["content"].(string); ok && len(content) > 0 {
					return raw
				}
			}
		}
	}

	return nil // Missing required fields or content is not a string, or content is empty
}
