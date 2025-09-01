package qwenclient

import (
	"fmt"
	"log"
	"strings"

	"github.com/sunbankio/qwencoder-proxy/auth"
)

// GetValidTokenAndEndpoint gets a valid token and determines the correct endpoint
func GetValidTokenAndEndpoint() (string, string, error) {
	credentials, err := auth.LoadQwenCredentials()
	if err != nil {
		// If credentials file doesn't exist, return a special error that can be handled by the caller
		return "", "", fmt.Errorf("credentials not found: %v. Please authenticate with Qwen by restarting the proxy", err)
	}

	// If token is expired or about to expire, try to refresh it
	if !auth.IsTokenValid(credentials) {
		log.Println("Token is expired or about to expire, attempting to refresh...")
		credentials, err = auth.RefreshAccessToken(credentials)
		if err != nil {
			// If token refresh fails, return a special error that can be handled by the caller
			return "", "", fmt.Errorf("failed to refresh token: %v. Please re-authenticate with Qwen by restarting the proxy", err)
		}
		log.Println("Token successfully refreshed")
	}

	if credentials.AccessToken == "" {
		return "", "", fmt.Errorf("no access token found in credentials")
	}

	// Use resource_url from credentials if available, otherwise fallback to default from auth package
	baseEndpoint := credentials.ResourceURL
	if baseEndpoint == "" {
		baseEndpoint = auth.DefaultQwenBaseURL
	}

	// Normalize the URL: add protocol if missing, ensure /v1 suffix
	if !strings.HasPrefix(baseEndpoint, "http") {
		baseEndpoint = "https://" + baseEndpoint
	}

	const suffix = "/v1"
	if !strings.HasSuffix(baseEndpoint, suffix) {
		baseEndpoint = baseEndpoint + suffix
	}
	return credentials.AccessToken, baseEndpoint, nil
}
