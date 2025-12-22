# Qwencoder Proxy

Qwencoder Proxy is a unified interface for multiple LLM providers, offering smart routing, protocol conversion, and a consistent OpenAI-compatible API.

## Features

- **Multi-Provider Support**: Integrate with Qwen, Gemini, Claude (Kiro), Antigravity, and iFlow.
- **Smart Routing**: Automatically routes requests to the appropriate provider based on the model name.
- **OpenAI Compatibility**: precise OpenAI API compatibility for `/v1/chat/completions` and `/v1/models`.
- **Provider-Specific Routes**: Force requests to a specific provider using dedicated endpoints.
- **Protocol Conversion**: Seamlessly converts between OpenAI format and provider-specific formats (Gemini, Claude, etc.).
- **Streaming Support**: Full support for streaming responses (Server-Sent Events).

## Getting Started

### Prerequisites

- Go 1.21+


### Installation

```bash
git clone https://github.com/sunbankio/qwencoder-proxy.git
cd qwencoder-proxy
go build -o qwencoder-proxy cmd/qwencoder-proxy/main.go
```

### Running the Server

```bash
./qwencoder-proxy
```

The server listens on port defined in your config (default: `8143`).

## API Usage

### 1. OpenAI Compatible Endpoints (`/v1/*`)

These are the primary endpoints for general usage. The proxy handles routing to the correct provider.

-   **List Models**
    ```http
    GET /v1/models
    ```
    Returns a unified list of models from all registered providers.

-   **Chat Completions**
    ```http
    POST /v1/chat/completions
    ```
    Standard OpenAI Chat Completion API. The `model` field determines the routing.
    
    **Example:**
    ```json
    {
      "model": "qwen-max",
      "messages": [{"role": "user", "content": "Hello!"}],
      "stream": true
    }
    ```

### 2. Provider-Specific OpenAI Routes (`/{provider}/v1/*`)

Force a request to use a specific provider, bypassing smart routing. These endpoints also use the OpenAI format.

-   **Prefixes:**
    -   `/qwen/v1/` (qwen modesl)
    -   `/gemini/v1/` (google gemini models)
    -   `/kiro/v1/` (for Claude/Anthropic models)
    -   `/antigravity/v1/` (google gemini and Anthropic models)
    -   `/iflow/v1/` (various opensource models)

-   **List Provider Models**
    ```http
    GET /{provider}/v1/models
    ```

-   **Chat Completions (Forced Provider)**
    ```http
    POST /{provider}/v1/chat/completions
    ```

### 3. Native Provider Routes

Direct access to provider-specific APIs (pass-through/native format).

-   **Gemini**
    -   `GET /gemini/models`
    -   `POST /gemini/models/{model}:generateContent`
    -   `POST /gemini/models/{model}:streamGenerateContent`

-   **Anthropic (Claude)**
    -   `GET /anthropic/models`
    -   `POST /anthropic/messages`

