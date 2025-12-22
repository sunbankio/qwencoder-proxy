// Package gemini provides the Gemini CLI provider implementation
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	baseURL       string
	authenticator *auth.GeminiAuthenticator
	httpClient    *http.Client
	logger        *logging.Logger
	projectID     string
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

// ListModels returns available models in native Gemini format
func (p *Provider) ListModels(ctx context.Context) (interface{}, error) {
	// For Cloud Code Assist API, we return the supported models directly
	// since they are fixed and don't need to be fetched from the API
	modelsResp := GeminiModelsResponse{
		Models: []GeminiModel{},
	}
	for _, modelID := range SupportedModels {
		modelsResp.Models = append(modelsResp.Models, GeminiModel{
			Name:        fmt.Sprintf("models/%s", modelID),
			DisplayName: modelID,
			Description: fmt.Sprintf("A generative model for text and chat generation. ID: %s", modelID),
			InputTokenLimit: 32768,
			OutputTokenLimit: 8192,
			SupportedGenerationMethods: []string{"generateContent", "streamGenerateContent"},
		})
	}
	return &modelsResp, nil
}

// initializeProject discovers or creates a project for the Cloud Code Assist API
func (p *Provider) initializeProject(ctx context.Context) error {
	token, err := p.authenticator.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get token: %w", err)
	}

	// Prepare client metadata for the API call
	clientMetadata := map[string]interface{}{
		"ideType":      "IDE_UNSPECIFIED",
		"platform":     "PLATFORM_UNSPECIFIED",
		"pluginType":   "GEMINI",
		"duetProject":  "",
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
		jsonBytes, err := json.Marshal(request)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		if err := json.Unmarshal(jsonBytes, &requestMap); err != nil {
			return nil, fmt.Errorf("failed to unmarshal request: %w", err)
		}
	}

	// Format the request for Cloud Code Assist API
	cloudCodeRequest := map[string]interface{}{
		"model": model,
		"project": p.projectID,
		"request": requestMap,
	}

	// Marshal the Cloud Code Assist API request
	reqBody, err := json.Marshal(cloudCodeRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
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

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var geminiResp GeminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
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
		jsonBytes, err := json.Marshal(request)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		if err := json.Unmarshal(jsonBytes, &requestMap); err != nil {
			return nil, fmt.Errorf("failed to unmarshal request: %w", err)
		}
	}

	// Format the request for Cloud Code Assist API
	cloudCodeRequest := map[string]interface{}{
		"model": model,
		"project": p.projectID,
		"request": requestMap,
	}

	// Marshal the Cloud Code Assist API request
	reqBody, err := json.Marshal(cloudCodeRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s:streamGenerateContent", p.baseURL)
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

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	return resp.Body, nil
}
