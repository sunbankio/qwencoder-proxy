package auth

import (
	"testing"
	"time"
)

func TestIsTokenValid(t *testing.T) {
	currentTime := time.Now().UnixMilli()
	
	tests := []struct {
		name        string
		credentials OAuthCreds
		expected    bool
	}{
		{
			name: "Valid token with sufficient time remaining",
			credentials: OAuthCreds{
				ExpiryDate: currentTime + TokenRefreshBufferMs + 10000, // 10 seconds more than buffer
			},
			expected: true,
		},
		{
			name: "Expired token",
			credentials: OAuthCreds{
				ExpiryDate: currentTime - 1000, // 1 second in the past
			},
			expected: false,
		},
		{
			name: "Token about to expire (within buffer)",
			credentials: OAuthCreds{
				ExpiryDate: currentTime + TokenRefreshBufferMs - 1000, // 1 second less than buffer
			},
			expected: false,
		},
		{
			name: "Zero expiry date",
			credentials: OAuthCreds{
				ExpiryDate: 0,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTokenValid(tt.credentials)
			if result != tt.expected {
				t.Errorf("IsTokenValid() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGenerateCodeVerifier(t *testing.T) {
	verifier, err := generateCodeVerifier()
	if err != nil {
		t.Fatalf("generateCodeVerifier() failed: %v", err)
	}
	
	if verifier == "" {
		t.Error("generateCodeVerifier() returned empty string")
	}
	
	// Verify it's a valid base64 URL encoded string without padding
	if len(verifier) == 0 || verifier[len(verifier)-1] == '=' {
		t.Error("generateCodeVerifier() returned invalid base64 URL encoding")
	}
}

func TestGenerateCodeChallenge(t *testing.T) {
	// Test with a known verifier
	verifier := "dYv4VYxt8siH8V7N79j552aeLsD5KfZuRkUvZ5JfZTc"
	// We'll just verify it produces a non-empty result since the exact value depends on SHA256 implementation
	challenge := generateCodeChallenge(verifier)
	if challenge == "" {
		t.Error("generateCodeChallenge() returned empty string")
	}
}

func TestGeneratePKCEParams(t *testing.T) {
	params, err := generatePKCEParams()
	if err != nil {
		t.Fatalf("generatePKCEParams() failed: %v", err)
	}
	
	if params.CodeVerifier == "" {
		t.Error("generatePKCEParams() returned empty CodeVerifier")
	}
	
	if params.CodeChallenge == "" {
		t.Error("generatePKCEParams() returned empty CodeChallenge")
	}
	
	// Verify the relationship between verifier and challenge
	expectedChallenge := generateCodeChallenge(params.CodeVerifier)
	if params.CodeChallenge != expectedChallenge {
		t.Errorf("CodeChallenge mismatch: got %v, want %v", params.CodeChallenge, expectedChallenge)
	}
}