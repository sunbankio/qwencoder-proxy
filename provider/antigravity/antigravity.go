// Package antigravity provides the Google Antigravity provider implementation
package antigravity

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/sunbankio/qwencoder-proxy/auth"
	"github.com/sunbankio/qwencoder-proxy/logging"
	"github.com/sunbankio/qwencoder-proxy/provider"
	"github.com/sunbankio/qwencoder-proxy/provider/gemini"
)

const (
	// DefaultDailyBaseURL is the default Antigravity daily environment base URL
	DefaultDailyBaseURL = "https://daily-cloudcode-pa.sandbox.googleapis.com"
	// DefaultAutopushBaseURL is the default Antigravity autopush environment base URL
	DefaultAutopushBaseURL = "https://autopush-cloudcode-pa.sandbox.googleapis.com"
	// DefaultUserAgent is the default user agent string
	DefaultUserAgent = "antigravity/1.11.5 windows/amd64"
	// APIVersion is the Antigravity API version
	APIVersion = "v1internal"
)

// SupportedModels lists all supported Antigravity models
var SupportedModels = []string{
	"gemini-2.5-computer-use-preview-10-2025",
	"gemini-3-pro-image-preview",
	"gemini-3-pro-preview",
	"gemini-3-flash-preview",
	"gemini-2.5-flash",
	"gemini-claude-sonnet-4-5",
	"gemini-claude-sonnet-4-5-thinking",
	"gemini-claude-opus-4-5-thinking",
	"gpt-oss-120b-medium",
	"gemini-3-pro-low",
	"gemini-2.5-flash-lite",
	"gemini-2.5-pro",
	"gemini-2.5-flash-thinking",
}

// ModelAliasMapping maps aliases to actual model names
// Keys are the "Friendly Names" (what the user/client asks for)
// Values are the "Internal Names" (what the Antigravity API expects)
var ModelAliasMapping = map[string]string{
	"gemini-2.5-computer-use-preview-10-2025": "rev19-uic3-1p",
	"gemini-3-pro-image-preview":              "gemini-3-pro-image",
	"gemini-3-pro-preview":                    "gemini-3-pro-high",
	"gemini-3-flash-preview":                  "gemini-3-flash",
	"gemini-2.5-flash":                        "gemini-2.5-flash",
	"gemini-claude-sonnet-4-5":                "claude-sonnet-4-5",
	"gemini-claude-sonnet-4-5-thinking":       "claude-sonnet-4-5-thinking",
	"gemini-claude-opus-4-5-thinking":         "claude-opus-4-5-thinking",
}

// Provider implements the provider.Provider interface for Antigravity
type Provider struct {
	dailyBaseURL    string
	autopushBaseURL string
	authenticator   *auth.GeminiAuthenticator // Antigravity uses similar auth to Gemini CLI
	httpClient      *http.Client
	logger          *logging.Logger
	projectID       string
	isInitialized   bool
}

// NewProvider creates a new Antigravity provider
func NewProvider(authenticator *auth.GeminiAuthenticator) *Provider {
	if authenticator == nil {
		authenticator = auth.NewGeminiAuthenticator(&auth.GeminiOAuthConfig{
			ClientID:     "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com",
			ClientSecret: "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",
			Scope:        "https://www.googleapis.com/auth/cloud-platform",
			RedirectPort: 8086,
			CredsDir:     ".antigravity",
			CredsFile:    "oauth_creds.json",
		})
	}
	return &Provider{
		dailyBaseURL:    DefaultDailyBaseURL,
		autopushBaseURL: DefaultAutopushBaseURL,
		authenticator:   authenticator,
		httpClient:      &http.Client{Timeout: 5 * time.Minute},
		logger:          logging.NewLogger(),
	}
}

// Name returns the provider identifier
func (p *Provider) Name() provider.ProviderType {
	return provider.ProviderAntigravity
}

// Protocol returns the native protocol
func (p *Provider) Protocol() provider.ProtocolType {
	return provider.ProtocolGemini
}

// SupportedModels returns list of supported model IDs
func (p *Provider) SupportedModels() []string {
	return SupportedModels
}

// GetAuthenticator returns the auth handler for this provider
func (p *Provider) GetAuthenticator() provider.Authenticator {
	return p.authenticator
}

// IsHealthy checks if the provider is available
func (p *Provider) IsHealthy(ctx context.Context) bool {
	// Initialize the provider as part of health check
	if err := p.Initialize(ctx); err != nil {
		p.logger.ErrorLog("[Antigravity] Health check failed during initialization: %v", err)
		return false
	}

	// Try to list models as a health check
	_, err := p.ListModels(ctx)
	return err == nil
}

// doRequestWithRetry executes a request with retry logic for 401 updates
func (p *Provider) doRequestWithRetry(ctx context.Context, reqFunc func(string) (*http.Response, error)) (*http.Response, error) {
	// First attempt
	token, err := p.authenticator.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	resp, err := reqFunc(token)
	if err != nil {
		return nil, err
	}

	// Check for 401
	if resp.StatusCode == http.StatusUnauthorized {
		p.logger.DebugLog("[Antigravity] Received 401 Unauthorized. Retrying with fresh token...")
		resp.Body.Close() // Close the failed response body

		// Force refresh
		if err := p.authenticator.ForceRefresh(ctx); err != nil {
			p.logger.ErrorLog("[Antigravity] Failed to force refresh token: %v", err)
			return nil, fmt.Errorf("failed to force refresh token: %w", err)
		}

		// Get the new token
		token, err = p.authenticator.GetToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get refreshed token: %w", err)
		}

		return reqFunc(token)
	}

	return resp, nil
}

// ListModels returns available models in native format
func (p *Provider) ListModels(ctx context.Context) (interface{}, error) {
	// Initialize if not already done
	if !p.isInitialized {
		if err := p.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize provider: %w", err)
		}
	}

	url := fmt.Sprintf("%s/%s:fetchAvailableModels", p.dailyBaseURL, APIVersion)

	reqFunc := func(token string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", DefaultUserAgent)

		p.logger.DebugLog("[Antigravity] Sending fetchAvailableModels request to %s", url)
		return p.httpClient.Do(req)
	}

	resp, err := p.doRequestWithRetry(ctx, reqFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var modelsResp gemini.GeminiModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &modelsResp, nil
}

// discoverProjectAndModels discovers the project ID and available models
func (p *Provider) discoverProjectAndModels(ctx context.Context) (string, error) {
	p.logger.DebugLog("[Antigravity] Discovering Project ID...")

	// Prepare client metadata
	clientMetadata := map[string]interface{}{
		"ideType":     "IDE_UNSPECIFIED",
		"platform":    "PLATFORM_UNSPECIFIED",
		"pluginType":  "GEMINI",
		"duetProject": "",
	}

	// Call loadCodeAssist to discover the actual project ID
	loadRequest := map[string]interface{}{
		"cloudaicompanionProject": "",
		"metadata":                clientMetadata,
	}

	// Try each base URL in sequence (similar to JavaScript implementation)
	baseURLs := []string{p.dailyBaseURL, p.autopushBaseURL}
	var lastErr error

	for _, baseURL := range baseURLs {
		projectID, err := p.callAPI(ctx, "loadCodeAssist", loadRequest, baseURL)
		if err != nil {
			p.logger.ErrorLog("[Antigravity] Error calling loadCodeAssist on %s: %v", baseURL, err)
			lastErr = err
			continue
		}

		// Check if we already have a project ID from the response
		if project, exists := projectID["cloudaicompanionProject"]; exists && project != nil {
			if projectStr, ok := project.(string); ok && projectStr != "" {
				p.logger.DebugLog("[Antigravity] Discovered existing Project ID: %s", projectStr)
				return projectStr, nil
			}
		}

		// If no existing project, we need to onboard
		var tierId string
		if allowedTiers, exists := projectID["allowedTiers"]; exists {
			if tiers, ok := allowedTiers.([]interface{}); ok {
				for _, tier := range tiers {
					if tierMap, ok := tier.(map[string]interface{}); ok {
						if isDefault, exists := tierMap["isDefault"]; exists {
							if isDefaultBool, ok := isDefault.(bool); ok && isDefaultBool {
								if id, exists := tierMap["id"]; exists {
									if idStr, ok := id.(string); ok {
										tierId = idStr
										break
									}
								}
							}
						}
					}
				}
			}
		}

		if tierId == "" {
			tierId = "free-tier"
		}

		onboardRequest := map[string]interface{}{
			"tierId":                  tierId,
			"cloudaicompanionProject": "",
			"metadata":                clientMetadata,
		}

		// Call onboardUser
		lroResponse, err := p.callAPI(ctx, "onboardUser", onboardRequest, baseURL)
		if err != nil {
			p.logger.ErrorLog("[Antigravity] Error calling onboardUser on %s: %v", baseURL, err)
			lastErr = err
			continue
		}

		// Poll until operation is complete with timeout protection
		MAX_RETRIES := 30 // Maximum number of retries (60 seconds total)
		retryCount := 0

		for {
			done, exists := lroResponse["done"].(bool)
			if exists && done {
				break
			}

			if retryCount >= MAX_RETRIES {
				return "", fmt.Errorf("onboarding timeout: Operation did not complete within expected time")
			}

			time.Sleep(2 * time.Second)

			// Retry the onboardUser request to check status
			lroResponse, err = p.callAPI(ctx, "onboardUser", onboardRequest, baseURL)
			if err != nil {
				return "", fmt.Errorf("failed to send onboard status check request: %w", err)
			}

			retryCount++
		}

		// Extract project ID from response
		var discoveredProjectId string
		if response, exists := lroResponse["response"]; exists {
			if responseMap, ok := response.(map[string]interface{}); ok {
				if project, exists := responseMap["cloudaicompanionProject"]; exists {
					if projectMap, ok := project.(map[string]interface{}); ok {
						if id, exists := projectMap["id"]; exists {
							if idStr, ok := id.(string); ok {
								discoveredProjectId = idStr
							}
						}
					}
				}
			}
		}

		if discoveredProjectId != "" {
			p.logger.DebugLog("[Antigravity] Onboarded and discovered Project ID: %s", discoveredProjectId)
			return discoveredProjectId, nil
		}
	}

	// If all base URLs failed, return fallback project ID
	fallbackProjectId := p.generateProjectID()
	p.logger.DebugLog("[Antigravity] Generated fallback Project ID: %s", fallbackProjectId)

	if lastErr != nil {
		return fallbackProjectId, fmt.Errorf("all base URLs failed, using fallback: %w", lastErr)
	}

	return fallbackProjectId, nil
}

// callAPI makes an API call to the Antigravity service with retry logic
func (p *Provider) callAPI(ctx context.Context, method string, body map[string]interface{}, baseURL string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/%s:%s", baseURL, APIVersion, method)

	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	reqFunc := func(token string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", DefaultUserAgent)

		p.logger.DebugLog("[Antigravity] Sending %s request to %s", method, url)
		return p.httpClient.Do(req)
	}

	resp, err := p.doRequestWithRetry(ctx, reqFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyContent, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyContent))
	}

	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response, nil
}

// Initialize performs the initialization process for the Antigravity provider
func (p *Provider) Initialize(ctx context.Context) error {
	if p.isInitialized {
		return nil
	}

	p.logger.DebugLog("[Antigravity] Initializing Antigravity API Service...")

	// Initialize auth
	if err := p.initializeAuth(ctx); err != nil {
		return fmt.Errorf("failed to initialize auth: %w", err)
	}

	// Discover project ID if not already set
	if p.projectID == "" {
		projectID, err := p.discoverProjectAndModels(ctx)
		if err != nil {
			return fmt.Errorf("failed to discover project: %w", err)
		}
		p.projectID = projectID
	} else {
		p.logger.DebugLog("[Antigravity] Using provided Project ID: %s", p.projectID)
	}

	p.isInitialized = true
	p.logger.DebugLog("[Antigravity] Initialization complete. Project ID: %s", p.projectID)
	return nil
}

// initializeAuth initializes the authentication
func (p *Provider) initializeAuth(ctx context.Context) error {
	// Get token to ensure authentication is working
	_, err := p.authenticator.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get token: %w", err)
	}
	return nil
}

// generateUUID generates a random UUID
func (p *Provider) generateUUID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		// fallback to a simple random string if crypto/rand fails
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 2
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// generateRequestID generates a random request ID
func (p *Provider) generateRequestID() string {
	return "agent-" + p.generateUUID()
}

// generateSessionID generates a random session ID
func (p *Provider) generateSessionID() string {
	n, err := rand.Int(rand.Reader, big.NewInt(9000000000000000000))
	if err != nil {
		// fallback to time-based ID if crypto/rand fails
		return "-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return "-" + n.String()
}

// generateProjectID generates a random project ID
func (p *Provider) generateProjectID() string {
	adjectives := []string{"useful", "bright", "swift", "calm", "bold"}
	nouns := []string{"fuze", "wave", "spark", "flow", "core"}

	adjIdx, err := rand.Int(rand.Reader, big.NewInt(int64(len(adjectives))))
	if err != nil {
		adjIdx = big.NewInt(0) // fallback to first element
	}

	nounIdx, err := rand.Int(rand.Reader, big.NewInt(int64(len(nouns))))
	if err != nil {
		nounIdx = big.NewInt(0) // fallback to first element
	}

	adj := adjectives[adjIdx.Int64()]
	noun := nouns[nounIdx.Int64()]

	randomPart := p.generateUUID()[:5]
	return fmt.Sprintf("%s-%s-%s", adj, noun, randomPart)
}

// geminiToAntigravity converts a Gemini request to Antigravity format
func (p *Provider) geminiToAntigravity(modelName string, payload map[string]interface{}, projectID string) map[string]interface{} {
	// Create a deep copy of the payload
	result := make(map[string]interface{})
	for k, v := range payload {
		result[k] = v
	}

	// Set basic fields
	result["model"] = modelName
	result["userAgent"] = "antigravity"
	result["project"] = projectID
	if projectID == "" {
		result["project"] = p.generateProjectID()
	}
	result["requestId"] = p.generateRequestID()

	// Ensure request object exists
	if _, exists := result["request"]; !exists {
		result["request"] = make(map[string]interface{})
	}

	// Set session ID
	request := result["request"].(map[string]interface{})
	request["sessionId"] = p.generateSessionID()

	// Remove safety settings
	if _, exists := request["safetySettings"]; exists {
		delete(request, "safetySettings")
	}

	// Set tool configuration
	if _, exists := request["toolConfig"]; exists {
		toolConfig := request["toolConfig"].(map[string]interface{})
		if _, exists := toolConfig["functionCallingConfig"]; !exists {
			toolConfig["functionCallingConfig"] = make(map[string]interface{})
		}
		functionCallingConfig := toolConfig["functionCallingConfig"].(map[string]interface{})
		functionCallingConfig["mode"] = "VALIDATED"
	}

	// Remove maxOutputTokens
	if genConfig, exists := request["generationConfig"]; exists {
		if genConfigMap, ok := genConfig.(map[string]interface{}); ok {
			if _, exists := genConfigMap["maxOutputTokens"]; exists {
				delete(genConfigMap, "maxOutputTokens")
			}
		}
	}

	// Handle Thinking configuration for non-gemini-3 models
	if !strings.HasPrefix(modelName, "gemini-3-") {
		if genConfig, exists := request["generationConfig"]; exists {
			if genConfigMap, ok := genConfig.(map[string]interface{}); ok {
				if thinkingConfig, exists := genConfigMap["thinkingConfig"]; exists {
					if thinkingConfigMap, ok := thinkingConfig.(map[string]interface{}); ok {
						if _, exists := thinkingConfigMap["thinkingLevel"]; exists {
							delete(thinkingConfigMap, "thinkingLevel")
							thinkingConfigMap["thinkingBudget"] = -1
						}
					}
				}
			}
		}
	}

	// Handle Claude models' tool declarations
	if strings.HasPrefix(modelName, "claude-sonnet-") || strings.HasPrefix(modelName, "claude-opus-") {
		if tools, exists := request["tools"]; exists {
			if toolsArr, ok := tools.([]interface{}); ok {
				for _, tool := range toolsArr {
					if toolMap, ok := tool.(map[string]interface{}); ok {
						if funcDecls, exists := toolMap["functionDeclarations"]; exists {
							if funcDeclsArr, ok := funcDecls.([]interface{}); ok {
								for _, funcDecl := range funcDeclsArr {
									if funcDeclMap, ok := funcDecl.(map[string]interface{}); ok {
										if paramsSchema, exists := funcDeclMap["parametersJsonSchema"]; exists {
											funcDeclMap["parameters"] = paramsSchema
											if paramsMap, ok := paramsSchema.(map[string]interface{}); ok {
												delete(paramsMap, "$schema")
											}
											delete(funcDeclMap, "parametersJsonSchema")
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return result
}

// GenerateContent handles non-streaming requests with native format
func (p *Provider) GenerateContent(ctx context.Context, model string, request interface{}) (interface{}, error) {
	// Initialize if not already done
	if !p.isInitialized {
		if err := p.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize provider: %w", err)
		}
	}

	// Map model alias if needed
	actualModel := model
	if alias, exists := ModelAliasMapping[model]; exists {
		actualModel = alias
	}

	// Convert request to map for processing
	var reqMap map[string]interface{}
	if reqBytes, err := json.Marshal(request); err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	} else if err := json.Unmarshal(reqBytes, &reqMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal request: %w", err)
	}

	// Transform the request to Antigravity format
	// Wrap the request in a "request" object as expected by Antigravity API
	wrappedReq := map[string]interface{}{
		"request": reqMap,
	}

	// Transform the request to Antigravity format
	antigravityReq := p.geminiToAntigravity(actualModel, wrappedReq, p.projectID)

	// Marshal the transformed request
	reqBody, err := json.Marshal(antigravityReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal antigravity request: %w", err)
	}

	// Use the correct endpoint
	url := fmt.Sprintf("%s/%s:generateContent", p.getBaseURLForModel(actualModel), APIVersion)

	reqFunc := func(token string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", DefaultUserAgent)

		p.logger.DebugLog("[Antigravity] Sending generateContent request to %s", url)
		return p.httpClient.Do(req)
	}

	resp, err := p.doRequestWithRetry(ctx, reqFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var rawResponse map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// The Antigravity API returns responses wrapped in a "response" field
	responseData, ok := rawResponse["response"].(map[string]interface{})
	if !ok {
		// If there's no "response" wrapper, use the raw response as-is
		responseData = rawResponse
	}

	// Convert back to JSON and then decode to GeminiResponse
	responseJSON, err := json.Marshal(responseData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response data: %w", err)
	}

	var geminiResp gemini.GeminiResponse
	if err := json.Unmarshal(responseJSON, &geminiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Gemini response: %w", err)
	}

	return &geminiResp, nil
}

// GenerateContentStream handles streaming requests with native format
func (p *Provider) GenerateContentStream(ctx context.Context, model string, request interface{}) (io.ReadCloser, error) {
	// Initialize if not already done
	if !p.isInitialized {
		if err := p.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize provider: %w", err)
		}
	}

	// Map model alias if needed
	actualModel := model
	if alias, exists := ModelAliasMapping[model]; exists {
		actualModel = alias
	}

	// Convert request to map for processing
	var reqMap map[string]interface{}
	if reqBytes, err := json.Marshal(request); err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	} else if err := json.Unmarshal(reqBytes, &reqMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal request: %w", err)
	}

	// Transform the request to Antigravity format
	// Wrap the request in a "request" object as expected by Antigravity API
	wrappedReq := map[string]interface{}{
		"request": reqMap,
	}

	// Transform the request to Antigravity format
	antigravityReq := p.geminiToAntigravity(actualModel, wrappedReq, p.projectID)

	// Marshal the transformed request
	reqBody, err := json.Marshal(antigravityReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal antigravity request: %w", err)
	}

	// Use the correct streaming endpoint
	url := fmt.Sprintf("%s/%s:streamGenerateContent", p.getBaseURLForModel(actualModel), APIVersion)

	reqFunc := func(token string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", DefaultUserAgent)
		req.Header.Set("Accept", "text/event-stream")

		p.logger.DebugLog("[Antigravity] Sending streaming generateContent request to %s", url)
		return p.httpClient.Do(req)
	}

	// For streaming, we also want retry logic on 401
	resp, err := p.doRequestWithRetry(ctx, reqFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	return resp.Body, nil
}

// getBaseURLForModel returns the appropriate base URL for the given model
func (p *Provider) getBaseURLForModel(model string) string {
	// For now, default to daily environment
	// In a real implementation, we might have logic to determine which environment to use
	return p.dailyBaseURL
}

// GetDailyBaseURL returns the daily environment base URL
func (p *Provider) GetDailyBaseURL() string {
	return p.dailyBaseURL
}

// GetAutopushBaseURL returns the autopush environment base URL
func (p *Provider) GetAutopushBaseURL() string {
	return p.autopushBaseURL
}

// SetDailyBaseURL sets the daily environment base URL
func (p *Provider) SetDailyBaseURL(url string) {
	p.dailyBaseURL = url
}

// SetAutopushBaseURL sets the autopush environment base URL
func (p *Provider) SetAutopushBaseURL(url string) {
	p.autopushBaseURL = url
}
