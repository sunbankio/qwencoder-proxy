# Provider Technical Details

This document provides comprehensive technical details for the three providers to be implemented in the Qwen proxy: Gemini CLI, KiroDev, and Google Antigravity.

## 1. Gemini CLI (gemini-cli-oauth)

### Authentication Method
- **OAuth 2.0 with Google**: Uses Google OAuth 2.0 web user flow for authentication
- **Client ID**: `681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com`
- **Client Secret**: `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`
- **Credentials Directory**: `.gemini`
- **Credentials File**: `oauth_creds.json`
- **Scope**: `https://www.googleapis.com/auth/cloud-platform`
- **Authentication Flow**: Web user flow (OAuth callback server on port 8085) - opens browser for user to authenticate and waits for callback

### Authentication Endpoints
- **OAuth Callback Port**: 8085
- **Token Storage**: Local file system (`~/.gemini/oauth_creds.json`)

### Provider's Native LLM Endpoints
- **Base URL**: `https://generativelanguage.googleapis.com/v1beta`
- **API Format**: Gemini API native format
- **Model Endpoint**: `/models/{model}`
- **Generate Content**: `/models/{model}:generateContent`
- **Stream Generate Content**: `/models/{model}:streamGenerateContent`
- **Model List**: `/models`

### OpenAI-Compatible Endpoints (Provided by Proxy)
- **Chat Completions**: `/v1/chat/completions` - Maps to Gemini's generateContent API
- **Model List**: `/v1/models` - Lists available models with OpenAI format
- **Native Gemini Endpoints**: `/v1beta/models` and `/v1beta/models/{model}:{generateContent|streamGenerateContent}`

### Model Protocol
- **Protocol Prefix**: `gemini`
- **Request Format**: Gemini API format with `contents` array containing `role` and `parts`
- **Response Format**: Gemini API format with `candidates` containing model responses
- **Streaming Format**: Server-sent events with Gemini API response chunks

### Supported Models
- `gemini-2.5-flash`
- `gemini-2.5-flash-lite`
- `gemini-2.5-pro`
- `gemini-2.5-pro-preview-06-05`
- `gemini-2.5-flash-preview-09-2025`
- `gemini-3-pro-preview`
- `gemini-3-flash-preview`

### Model Specifications
- **gemini-2.5-flash**: Fast, efficient model for multi-turn conversations
- **gemini-2.5-pro**: More capable model for complex reasoning tasks
- **gemini-3-pro-preview**: Preview of next-generation Gemini model

## 2. KiroDev (claude-kiro-oauth)

### Authentication Method
- **AWS SSO Authentication**: Uses AWS SSO with Kiro/CodeWhisperer service
- **Credentials File**: `~/.aws/sso/cache/kiro-auth-token.json` (default path)
- **Authentication Method**: Social authentication (`social`)
- **Token Refresh**: Automatic refresh when token expiry is near
- **Authentication Flow**: Pre-existing credentials expected in AWS SSO cache (no explicit OAuth flow in code)
- **Default Region**: `us-east-1` (used when region is not specified in credentials)

### Authentication Endpoints
- **Refresh URL**: `https://prod.{{region}}.auth.desktop.kiro.dev/refreshToken` (default: `https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken`)
- **Refresh IDC URL**: `https://oidc.{{region}}.amazonaws.com/token` (default: `https://oidc.us-east-1.amazonaws.com/token`)
- **Token Storage**: Local file system (`~/.aws/sso/cache/kiro-auth-token.json`)

### Provider's Native LLM Endpoints
- **Base URL**: `https://codewhisperer.{{region}}.amazonaws.com/generateAssistantResponse` (default: `https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse`)
- **Amazon Q URL**: `https://codewhisperer.{{region}}.amazonaws.com/SendMessageStreaming` (default: `https://codewhisperer.us-east-1.amazonaws.com/SendMessageStreaming`)
- **Usage Limits URL**: `https://q.{{region}}.amazonaws.com/getUsageLimits` (default: `https://q.us-east-1.amazonaws.com/getUsageLimits`)
- **API Format**: Claude API native format
- **Messages Endpoint**: `/SendMessageStreaming` (for streaming) and `/generateAssistantResponse` (for sync)

### OpenAI-Compatible Endpoints (Provided by Proxy)
- **Claude Messages**: `/v1/messages` - Maps to Claude's messages API
- **Chat Completions**: `/v1/chat/completions` - Maps to Claude's messages API via conversion
- **Model List**: `/v1/models` - Lists available models with OpenAI format
- **Default User Agent**: `antigravity/1.11.5 windows/amd64`

### Model Protocol
- **Protocol Prefix**: `claude`
- **Request Format**: Claude API format with `messages` array containing `role` and `content`
- **Response Format**: Claude API format with `content` containing model responses
- **Streaming Format**: Server-sent events with Claude API response chunks

### Supported Models
- `claude-opus-4-5`
- `claude-opus-4-5-20251101`
- `claude-haiku-4-5`
- `claude-sonnet-4-5`
- `claude-sonnet-4-5-20250929`
- `claude-sonnet-4-20250514`
- `claude-3-7-sonnet-20250219`

### Model Specifications
- **claude-opus**: Most capable model for complex, nuanced tasks
- **claude-sonnet**: Balanced model for everyday use
- **claude-haiku**: Fastest model for simple tasks

### Configuration Options
- **KIRO_OAUTH_CREDS_BASE64**: Base64 encoded credentials (default: not set)
- **KIRO_OAUTH_CREDS_FILE_PATH**: Path to credentials file (default: `~/.aws/sso/cache/kiro-auth-token.json`)
- **KIRO_REFRESH_URL**: Custom refresh URL (default: `https://prod.{{region}}.auth.desktop.kiro.dev/refreshToken`)
- **KIRO_REFRESH_IDC_URL**: Custom refresh IDC URL (default: `https://oidc.{{region}}.amazonaws.com/token`)
- **KIRO_BASE_URL**: Custom base URL (default: `https://codewhisperer.{{region}}.amazonaws.com/generateAssistantResponse`)
- **USE_SYSTEM_PROXY_KIRO**: Enable system proxy (default: false)

## 3. Google Antigravity (gemini-antigravity)

### Authentication Method
- **OAuth 2.0 with Google**: Uses Google OAuth 2.0 web user flow for authentication
- **Client ID**: `1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com`
- **Client Secret**: `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`
- **Credentials Directory**: `.antigravity`
- **Credentials File**: `oauth_creds.json`
- **Scope**: `https://www.googleapis.com/auth/cloud-platform`
- **Authentication Flow**: Web user flow (OAuth callback server on port 8086) - opens browser for user to authenticate and waits for callback
- **Token Refresh Skew**: 3000 seconds (50 minutes) before expiry

### Authentication Endpoints
- **OAuth Callback Port**: 8086
- **Token Storage**: Local file system (`~/.antigravity/oauth_creds.json`)

### Provider's Native LLM Endpoints
- **Daily Base URL**: `https://daily-cloudcode-pa.sandbox.googleapis.com` (default, configurable via `ANTIGRAVITY_BASE_URL_DAILY`)
- **Autopush Base URL**: `https://autopush-cloudcode-pa.sandbox.googleapis.com` (default, configurable via `ANTIGRAVITY_BASE_URL_AUTOPUSH`)
- **API Version**: `v1internal`
- **API Format**: Custom Antigravity format (converted from Gemini API format)
- **Model Endpoint**: `/v1internal:fetchAvailableModels`
- **Generate Content**: Custom endpoint based on model alias mapping
- **Load Code Assist**: `/loadCodeAssist`
- **Onboard User**: `/onboardUser`

### OpenAI-Compatible Endpoints (Provided by Proxy)
- **Chat Completions**: `/v1/chat/completions` - Maps to Antigravity's generateAssistantResponse API
- **Model List**: `/v1/models` - Lists available models with OpenAI format
- **Native Gemini Endpoints**: `/v1beta/models` and `/v1beta/models/{model}:{generateContent|streamGenerateContent}`

### Model Protocol
- **Protocol Prefix**: `gemini`
- **Request Format**: Custom Antigravity format converted from Gemini API format
- **Response Format**: Converted back to Gemini API format with `candidates` containing model responses
- **Streaming Format**: Server-sent events with Gemini API response chunks

### Supported Models
- `gemini-2.5-computer-use-preview-10-2025` (alias: `rev19-uic3-1p`)
- `gemini-3-pro-image-preview` (alias: `gemini-3-pro-image`)
- `gemini-3-pro-preview` (alias: `gemini-3-pro-high`)
- `gemini-3-flash-preview` (alias: `gemini-3-flash`)
- `gemini-2.5-flash` (alias: `gemini-2.5-flash`)
- `gemini-claude-sonnet-4-5` (alias: `claude-sonnet-4-5`)
- `gemini-claude-sonnet-4-5-thinking` (alias: `claude-sonnet-4-5-thinking`)
- `gemini-claude-opus-4-5-thinking` (alias: `claude-opus-4-5-thinking`)

### Model Specifications
- **gemini-2.5-computer-use-preview**: Advanced model with computer use capabilities
- **gemini-3-pro-preview**: Next-generation Gemini model with enhanced reasoning
- **gemini-3-flash**: Fast, efficient model for multi-turn conversations
- **gemini-claude-sonnet-4-5-thinking**: Hybrid model combining Gemini and Claude capabilities with advanced reasoning

### Configuration Options
- **ANTIGRAVITY_BASE_URL_DAILY**: Daily environment base URL (default: `https://daily-cloudcode-pa.sandbox.googleapis.com`)
- **ANTIGRAVITY_BASE_URL_AUTOPUSH**: Autopush environment base URL (default: `https://autopush-cloudcode-pa.sandbox.googleapis.com`)
- **ANTIGRAVITY_OAUTH_CREDS_FILE_PATH**: Path to OAuth credentials file (default: `~/.antigravity/oauth_creds.json`)
- **Default User Agent**: `antigravity/1.11.5 windows/amd64`

## OpenAI-Compatible Endpoints (Unified Proxy)

The reference implementation provides a unified proxy that exposes OpenAI-compatible endpoints for all providers:

### Common Endpoints for All Providers
- **`/v1/chat/completions`**: OpenAI-compatible chat completions endpoint
  - Maps to provider-specific chat APIs (Gemini: generateContent, Claude: messages, Kiro: messages)
- **`/v1/models`**: OpenAI-compatible model listing endpoint
  - Returns available models in OpenAI format for the selected provider
- **`/v1/messages`**: Claude-compatible messages endpoint (for Claude-based providers)
- **`/v1/responses`**: OpenAI Responses API endpoint
- **`/v1beta/models`**: Native Gemini-compatible model listing endpoint
- **`/v1beta/models/{model}:{generateContent|streamGenerateContent}`**: Native Gemini-compatible content generation endpoints
- **`/health`**: Health check endpoint

### Provider-Specific Endpoint Routing
- **Provider Selection**: Can be overridden via `model-provider` header or first path segment
- **Request Conversion**: Automatic conversion between OpenAI format and provider-specific formats
- **Response Conversion**: Automatic conversion from provider-specific format to OpenAI format

## Authentication Flow Comparison

### Current Implementation
- **Qwen (Device Flow)**: Uses device authorization flow with polling - user visits a URL and enters a code
- **Gemini CLI (Web User Flow)**: Opens browser for user authentication and waits for callback on port 8085
- **Antigravity (Web User Flow)**: Opens browser for user authentication and waits for callback on port 8086
- **Kiro (Pre-existing Credentials)**: Expects credentials to already exist in AWS SSO cache

### Recommended Implementation for New Providers
For consistency with the Qwen provider and better user experience in headless environments, it would be preferable to implement device flow for the new providers instead of web user flow. This would involve:

1. **Gemini CLI and Antigravity**: Modify to use device authorization flow instead of web user flow
2. **Kiro**: Implement device flow or web user flow instead of requiring pre-existing AWS SSO credentials

## Common Implementation Patterns

### Authentication Flow
All providers follow a similar OAuth 2.0 flow:
1. Generate OAuth URL with appropriate scopes
2. Start local callback server to receive tokens (for web flow) or poll for tokens (for device flow)
3. Store tokens in provider-specific directories
4. Implement automatic token refresh when near expiry

### Request/Response Conversion
The system uses a converter architecture to handle protocol differences:
- **OpenAI to Gemini**: Convert `messages` to `contents` format
- **OpenAI to Claude**: Convert `messages` to Claude format
- **Response conversion**: Map responses back to client's expected format

### Provider Pool Management
- Each provider can have multiple configurations in a pool
- Round-robin selection for load distribution
- Health checks to ensure provider availability
- Automatic failover when providers become unhealthy

### Configuration Options
- **API Keys**: Support for API key authentication where applicable
- **Base URLs**: Configurable base URLs for each provider
- **Model Restrictions**: Ability to specify models not supported by specific providers
- **Health Check Settings**: Configurable health check models and intervals