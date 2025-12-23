// Package gemini provides the Gemini CLI provider implementation
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sunbankio/qwencoder-proxy/auth"
	"github.com/sunbankio/qwencoder-proxy/logging"
	"github.com/sunbankio/qwencoder-proxy/provider"
)

const (
	// DefaultBaseURL is the default Gemini API base URL
	DefaultBaseURL = "https://cloudcode-pa.googleapis.com/v1internal"
)

// SupportedModels lists all supported Gemini models
var SupportedModels = []string{
	"gemini-2.5-flash",
	"gemini-2.5-flash-lite",
	"gemini-2.5-pro",
	"gemini-2.5-pro-preview-06-05",
	"gemini-2.5-flash-preview-09-2025",
	"gemini-3-pro-preview",
	"gemini-3-flash-preview",
}

// Provider implements the provider.Provider interface for Gemini CLI
type Provider struct {
	baseURL          string
	authenticator    *auth.GeminiAuthenticator
	httpClient       *http.Client
	logger           *logging.Logger
	projectID        string
	projectInitError error // Store any initialization error to prevent repeated attempts
}

// NewProvider creates a new Gemini provider
func NewProvider(authenticator *auth.GeminiAuthenticator) *Provider {
	if authenticator == nil {
		authenticator = auth.NewGeminiAuthenticator(nil)
	}
	return &Provider{
		baseURL:       DefaultBaseURL,
		authenticator: authenticator,
		httpClient:    &http.Client{Timeout: 5 * time.Minute},
		logger:        logging.NewLogger(),
	}
}

// Name returns the provider identifier
func (p *Provider) Name() provider.ProviderType {
	return provider.ProviderGeminiCLI
}

// Protocol returns the native protocol
func (p *Provider) Protocol() provider.ProtocolType {
	return provider.ProtocolGemini
}

// SupportedModels returns list of supported model IDs
func (p *Provider) SupportedModels() []string {
	return SupportedModels
}

// SupportsModel checks if the provider supports the given model
func (p *Provider) SupportsModel(model string) bool {
	modelLower := strings.ToLower(model)
	if strings.HasPrefix(modelLower, "gemini-") {
		return true
	}
	for _, m := range SupportedModels {
		if strings.EqualFold(m, model) {
			return true
		}
	}
	return false
}

// GetAuthenticator returns the auth handler for this provider
func (p *Provider) GetAuthenticator() provider.Authenticator {
	return p.authenticator
}

// IsHealthy checks if the provider is available
func (p *Provider) IsHealthy(ctx context.Context) bool {
	// Try to list models as a health check
	_, err := p.ListModels(ctx)
	return err == nil
}

// ClearInitializationError clears any cached initialization errors
func (p *Provider) ClearInitializationError() {
	p.projectInitError = nil
}

// ListModels returns available models in native Gemini format
func (p *Provider) ListModels(ctx context.Context) (interface{}, error) {
	// Try to initialize project to get actual models
	if p.projectID == "" {
		if err := p.initializeProject(ctx); err != nil {
			p.logger.DebugLog("[Gemini] Failed to initialize project for model discovery: %v", err)
			// Fall back to hardcoded models if initialization fails
			return p.getHardcodedModels(), nil
		}
	}

	// Try to discover actual models from the API
	if actualModels, err := p.discoverModels(ctx); err == nil {
		return actualModels, nil
	} else {
		p.logger.DebugLog("[Gemini] Failed to discover models from API: %v", err)
		// Fall back to hardcoded models if discovery fails
		return p.getHardcodedModels(), nil
	}
}

// getHardcodedModels returns the hardcoded models as fallback
func (p *Provider) getHardcodedModels() interface{} {
	modelsResp := GeminiModelsResponse{
		Models: []GeminiModel{},
	}
	for _, modelID := range SupportedModels {
		modelsResp.Models = append(modelsResp.Models, GeminiModel{
			Name:                       modelID,
			DisplayName:                modelID,
			Description:                fmt.Sprintf("A generative model for text and chat generation. ID: %s", modelID),
			InputTokenLimit:            32768,
			OutputTokenLimit:           8192,
			SupportedGenerationMethods: []string{"generateContent", "streamGenerateContent"},
		})
	}
	return &modelsResp
}

// discoverModels attempts to discover actual models from the Gemini API
func (p *Provider) discoverModels(ctx context.Context) (interface{}, error) {
	// For now, we'll use the hardcoded models as the Gemini API doesn't have
	// a public endpoint to list models like the Antigravity API does
	// In the future, this could be enhanced to call an actual discovery endpoint
	return p.getHardcodedModels(), nil
}

// initializeProject discovers or creates a project for the Cloud Code Assist API
func (p *Provider) initializeProject(ctx context.Context) error {
	p.logger.DebugLog("[Gemini] Starting project initialization...")

	// Check if we have cached credentials
	if p.authenticator.IsAuthenticated() {
		p.logger.DebugLog("[Gemini] Found valid cached credentials")
	} else {
		p.logger.DebugLog("[Gemini] No valid cached credentials found")
	}

	token, err := p.authenticator.GetToken(ctx)
	if err != nil {
		// Store the error to prevent repeated initialization attempts when auth fails
		p.projectInitError = err
		p.logger.ErrorLog("[Gemini] Failed to get token for project initialization: %v", err)
		return fmt.Errorf("failed to get token for project initialization: %w", err)
	}

	p.logger.DebugLog("[Gemini] Successfully obtained access token (length: %d)", len(token))

	// Clear any previous initialization errors since we got the token successfully
	p.projectInitError = nil

	// Prepare client metadata for the API call
	clientMetadata := map[string]interface{}{
		"ideType":     "IDE_UNSPECIFIED",
		"platform":    "PLATFORM_UNSPECIFIED",
		"pluginType":  "GEMINI",
		"duetProject": "",
	}

	// Prepare the loadCodeAssist request
	loadRequest := map[string]interface{}{
		"cloudaicompanionProject": "",
		"metadata":                clientMetadata,
	}

	reqBody, err := json.Marshal(loadRequest)
	if err != nil {
		return fmt.Errorf("failed to marshal load request: %w", err)
	}

	// Call loadCodeAssist to discover the project ID
	url := fmt.Sprintf("%s:loadCodeAssist", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create load request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send load request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read load response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("loadCodeAssist failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var loadResponse map[string]interface{}
	if err := json.Unmarshal(respBody, &loadResponse); err != nil {
		return fmt.Errorf("failed to decode load response: %w", err)
	}

	// Check if we already have a project ID from the response
	if projectID, ok := loadResponse["cloudaicompanionProject"].(string); ok && projectID != "" {
		p.projectID = projectID
		p.logger.DebugLog("[Gemini] Using existing project ID: %s", p.projectID)
		return nil
	}

	// If no existing project, we need to onboard
	allowedTiers, ok := loadResponse["allowedTiers"].([]interface{})
	var tierId string
	if ok && len(allowedTiers) > 0 {
		// Find the default tier
		for _, tier := range allowedTiers {
			if tierMap, ok := tier.(map[string]interface{}); ok {
				if isDefault, exists := tierMap["isDefault"].(bool); exists && isDefault {
					if id, idExists := tierMap["id"].(string); idExists {
						tierId = id
						break
					}
				}
			}
		}
	}

	if tierId == "" {
		tierId = "free-tier" // default fallback
	}

	// Prepare the onboardUser request
	onboardRequest := map[string]interface{}{
		"tierId":                  tierId,
		"cloudaicompanionProject": "",
		"metadata":                clientMetadata,
	}

	onboardReqBody, err := json.Marshal(onboardRequest)
	if err != nil {
		return fmt.Errorf("failed to marshal onboard request: %w", err)
	}

	// Call onboardUser to create a project
	onboardUrl := fmt.Sprintf("%s:onboardUser", p.baseURL)
	onboardReq, err := http.NewRequestWithContext(ctx, "POST", onboardUrl, bytes.NewReader(onboardReqBody))
	if err != nil {
		return fmt.Errorf("failed to create onboard request: %w", err)
	}

	onboardReq.Header.Set("Authorization", "Bearer "+token)
	onboardReq.Header.Set("Content-Type", "application/json")

	// For long-running operations, we might need to handle LRO (Long Running Operations)
	onboardResp, err := p.httpClient.Do(onboardReq)
	if err != nil {
		return fmt.Errorf("failed to send onboard request: %w", err)
	}
	defer onboardResp.Body.Close()

	onboardRespBody, err := io.ReadAll(onboardResp.Body)
	if err != nil {
		return fmt.Errorf("failed to read onboard response: %w", err)
	}

	if onboardResp.StatusCode != http.StatusOK {
		return fmt.Errorf("onboardUser failed (status %d): %s", onboardResp.StatusCode, string(onboardRespBody))
	}

	var onboardResponse map[string]interface{}
	if err := json.Unmarshal(onboardRespBody, &onboardResponse); err != nil {
		return fmt.Errorf("failed to decode onboard response: %w", err)
	}

	// Extract the project ID from the response
	if response, ok := onboardResponse["response"].(map[string]interface{}); ok {
		if project, exists := response["cloudaicompanionProject"].(map[string]interface{}); exists {
			if id, idExists := project["id"].(string); idExists {
				p.projectID = id
				p.logger.DebugLog("[Gemini] Created new project ID: %s", p.projectID)
				return nil
			}
		}
	}

	// Fallback: try to get project ID directly from response
	if id, exists := onboardResponse["cloudaicompanionProject"].(string); exists {
		p.projectID = id
		p.logger.DebugLog("[Gemini] Discovered project ID: %s", p.projectID)
		return nil
	}

	return fmt.Errorf("failed to discover or create project ID")
}

// GenerateContent handles non-streaming requests with native format
func (p *Provider) GenerateContent(ctx context.Context, model string, request interface{}) (interface{}, error) {
	// Check if there was a previous initialization error to avoid repeated auth failures
	if p.projectInitError != nil {
		return nil, fmt.Errorf("project initialization failed due to authentication error, please re-authenticate: %w", p.projectInitError)
	}

	// Ensure project is initialized
	if p.projectID == "" {
		if err := p.initializeProject(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize project: %w", err)
		}
	}

	// First attempt to get token
	token, err := p.authenticator.GetToken(ctx)
	if err != nil {
		p.logger.ErrorLog("[Gemini] Token retrieval failed on first attempt: %v", err)
		// The authenticator should handle refresh internally, but if it failed,
		// we return the error which will propagate up to the user
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	// Prepare the request body with model information for Cloud Code Assist API
	requestMap, ok := request.(map[string]interface{})
	if !ok {
		// If it's not a map, try to convert it from a GeminiRequest struct
		jsonBytes, marshalErr := json.Marshal(request)
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", marshalErr)
		}
		if err := json.Unmarshal(jsonBytes, &requestMap); err != nil {
			return nil, fmt.Errorf("failed to unmarshal request: %w", err)
		}
	}
	// Check if this is already a Cloud Code Assist API formatted request
	// by checking if it has the expected fields for the Cloud Code Assist API
	_, hasModel := requestMap["model"]
	_, hasProject := requestMap["project"]
	_, hasRequest := requestMap["request"]

	// Prepare the final request structure based on the format
	var finalRequest map[string]interface{}
	if hasModel && hasProject && hasRequest {
		// This is already a Cloud Code Assist API formatted request
		// Just update the project ID
		requestMap["project"] = p.projectID
		finalRequest = requestMap
	} else {
		// This is a standard Gemini API request, format it for Cloud Code Assist API
		finalRequest = map[string]interface{}{
			"model":   model,
			"project": p.projectID,
			"request": requestMap,
		}
	}

	// Marshal the initial request
	reqBody, marshalErr := json.Marshal(finalRequest)
	if marshalErr != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", marshalErr)
	}

	url := fmt.Sprintf("%s:generateContent", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	p.logger.DebugLog("[Gemini] Sending generateContent request to %s", url)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		// Try to refresh the token and retry the request once, following the JavaScript reference implementation
		p.logger.DebugLog("[Gemini] Received %d, attempting to refresh token and retry", resp.StatusCode)

		// Read and close the original response body
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// Refresh the token by calling initializeAuth with force refresh
		// We need to implement a method to force refresh the token
		_, refreshErr := p.authenticator.GetToken(ctx)
		if refreshErr != nil {
			p.logger.ErrorLog("[Gemini] Token refresh failed: %v", refreshErr)
			return nil, fmt.Errorf("API error (status %d) and token refresh failed: %s", resp.StatusCode, string(bodyBytes))
		}

		// Retry the request with the refreshed token
		// Recreate the request body with the same finalRequest
		retryReqBody, marshalErr := json.Marshal(finalRequest)
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to marshal request for retry: %w", marshalErr)
		}

		retryReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(retryReqBody))
		if err != nil {
			return nil, fmt.Errorf("failed to create retry request: %w", err)
		}

		// Get the potentially updated token
		refreshedToken, tokenErr := p.authenticator.GetToken(ctx)
		if tokenErr != nil {
			return nil, fmt.Errorf("failed to get refreshed token: %w", tokenErr)
		}

		retryReq.Header.Set("Authorization", "Bearer "+refreshedToken)
		retryReq.Header.Set("Content-Type", "application/json")

		retryResp, err := p.httpClient.Do(retryReq)
		if err != nil {
			return nil, fmt.Errorf("failed to send retry request: %w", err)
		}
		defer retryResp.Body.Close()

		if retryResp.StatusCode != http.StatusOK {
			retryBody, _ := io.ReadAll(retryResp.Body)
			return nil, fmt.Errorf("retry failed with API error (status %d): %s", retryResp.StatusCode, string(retryBody))
		}

		resp = retryResp // Use the successful retry response
	} else if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	// The Cloud Code Assist API returns responses wrapped in a "response" field
	var rawResponse map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract the actual Gemini response from the "response" field
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

	var geminiResp GeminiResponse
	if err := json.Unmarshal(responseJSON, &geminiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Gemini response: %w", err)
	}

	return &geminiResp, nil
}

// GenerateContentStream handles streaming requests with native format
func (p *Provider) GenerateContentStream(ctx context.Context, model string, request interface{}) (io.ReadCloser, error) {
	// Ensure project is initialized
	if p.projectID == "" {
		if err := p.initializeProject(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize project: %w", err)
		}
	}

	token, err := p.authenticator.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	// Prepare the request body with model information for Cloud Code Assist API
	requestMap, ok := request.(map[string]interface{})
	if !ok {
		// If it's not a map, try to convert it from a GeminiRequest struct
		jsonBytes, marshalErr := json.Marshal(request)
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", marshalErr)
		}
		if err := json.Unmarshal(jsonBytes, &requestMap); err != nil {
			return nil, fmt.Errorf("failed to unmarshal request: %w", err)
		}
	}

	// Check if this is already a Cloud Code Assist API formatted request
	// by checking if it has the expected fields for the Cloud Code Assist API
	_, hasModel := requestMap["model"]
	_, hasProject := requestMap["project"]
	_, hasRequest := requestMap["request"]

	// Prepare the final request structure based on the format
	var finalRequest map[string]interface{}
	if hasModel && hasProject && hasRequest {
		// This is already a Cloud Code Assist API formatted request
		// Just update the project ID
		requestMap["project"] = p.projectID
		finalRequest = requestMap
	} else {
		// This is a standard Gemini API request, format it for Cloud Code Assist API
		finalRequest = map[string]interface{}{
			"model":   model,
			"project": p.projectID,
			"request": requestMap,
		}
	}

	// Marshal the request
	reqBody, marshalErr := json.Marshal(finalRequest)
	if marshalErr != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", marshalErr)
	}

	// Add the crucial alt=sse query parameter for streaming
	url := fmt.Sprintf("%s:streamGenerateContent?alt=sse", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	p.logger.DebugLog("[Gemini] Sending streamGenerateContent request to %s", url)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		// Try to refresh the token and retry the request once, following the JavaScript reference implementation
		p.logger.DebugLog("[Gemini] Received %d, attempting to refresh token and retry", resp.StatusCode)

		// Read and close the original response body
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// Refresh the token by calling GetToken which handles refresh internally
		_, refreshErr := p.authenticator.GetToken(ctx)
		if refreshErr != nil {
			p.logger.ErrorLog("[Gemini] Token refresh failed: %v", refreshErr)
			return nil, fmt.Errorf("API error (status %d) and token refresh failed: %s", resp.StatusCode, string(bodyBytes))
		}

		// Retry the request with the refreshed token
		// Recreate the request body with the same finalRequest
		retryReqBody, marshalErr := json.Marshal(finalRequest)
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to marshal request for retry: %w", marshalErr)
		}

		retryReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(retryReqBody))
		if err != nil {
			return nil, fmt.Errorf("failed to create retry request: %w", err)
		}

		// Get the potentially updated token
		refreshedToken, tokenErr := p.authenticator.GetToken(ctx)
		if tokenErr != nil {
			return nil, fmt.Errorf("failed to get refreshed token: %w", tokenErr)
		}

		retryReq.Header.Set("Authorization", "Bearer "+refreshedToken)
		retryReq.Header.Set("Content-Type", "application/json")
		retryReq.Header.Set("Accept", "text/event-stream")

		retryResp, err := p.httpClient.Do(retryReq)
		if err != nil {
			return nil, fmt.Errorf("failed to send retry request: %w", err)
		}

		if retryResp.StatusCode != http.StatusOK {
			retryBody, _ := io.ReadAll(retryResp.Body)
			retryResp.Body.Close()
			return nil, fmt.Errorf("retry failed with API error (status %d): %s", retryResp.StatusCode, string(retryBody))
		}

		return retryResp.Body, nil
	} else if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	return resp.Body, nil
}
