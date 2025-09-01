package proxy

import (
	"testing"
)


func TestExtractDeltaContent(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		expected string
	}{
		{
			name: "Valid delta content",
			input: map[string]interface{}{
				"choices": []interface{}{
					map[string]interface{}{
						"delta": map[string]interface{}{
							"content": "Hello world",
						},
					},
				},
			},
			expected: "Hello world",
		},
		{
			name: "Empty delta content",
			input: map[string]interface{}{
				"choices": []interface{}{
					map[string]interface{}{
						"delta": map[string]interface{}{
							"content": "",
						},
					},
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractDeltaContent(tt.input)
			if result != tt.expected {
				t.Errorf("extractDeltaContent() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestHasPrefixRelationship(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected bool
	}{
		{
			name:     "a is prefix of b",
			a:        "Hello",
			b:        "Hello world",
			expected: true,
		},
		{
			name:     "b is prefix of a",
			a:        "Hello world",
			b:        "Hello",
			expected: true,
		},
		{
			name:     "identical strings",
			a:        "Hello",
			b:        "Hello",
			expected: true,
		},
		{
			name:     "no prefix relationship",
			a:        "Hello",
			b:        "World",
			expected: false,
		},
		{
			name:     "empty string a",
			a:        "",
			b:        "Hello",
			expected: true,
		},
		{
			name:     "empty string b",
			a:        "Hello",
			b:        "",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasPrefixRelationship(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("hasPrefixRelationship(%q, %q) = %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}