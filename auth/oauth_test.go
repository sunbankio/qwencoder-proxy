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
				ExpiryDate: currentTime + TokenRefreshBufferMs + 10000,
			},
			expected: true,
		},
		{
			name: "Expired token",
			credentials: OAuthCreds{
				ExpiryDate: currentTime - 1000,
			},
			expected: false,
		},
		{
			name: "Token about to expire (within buffer)",
			credentials: OAuthCreds{
				ExpiryDate: currentTime + TokenRefreshBufferMs - 1000,
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

func TestAuthenticate(t *testing.T) {
	err := AuthenticateWithOAuth()
	if err != nil {
		t.Errorf("Authentication failed with error: %v", err)
	}
}
