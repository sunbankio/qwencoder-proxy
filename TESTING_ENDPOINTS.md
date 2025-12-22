# Testing Endpoints for Qwen Proxy with Multi-Provider Support

This document provides curl commands to test the different endpoints available in the qwencoder-proxy after implementing multi-provider support for Qwen, Gemini CLI, Kiro/Claude, and Antigravity.

## Prerequisites

Make sure the proxy server is running on the default port (8143):

```bash
# Start the proxy server
go run cmd/qwencoder-proxy/main.go
```

## 1. OpenAI-Compatible Endpoints (Universal Access)

These endpoints work with all providers using OpenAI-compatible format:

### List Available Models
```bash
curl -X GET http://localhost:8143/v1/models
```

### Chat Completions (Model-Specific Examples)
```bash
# Using Qwen models
curl -X POST http://localhost:8143/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "model": "qwen-max",
    "messages": [
      {
        "role": "user",
        "content": "Hello, how are you?"
      }
    ],
    "temperature": 0.7
  }'

# Using Qwen Coder models
curl -X POST http://localhost:8143/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "model": "qwen3-coder-plus",
    "messages": [
      {
        "role": "user",
        "content": "Write a simple Go function to calculate factorial."
      }
    ],
    "temperature": 0.7
  }'
```

### Chat Completions (Generic - Provider Selected by Model)
```bash
# Using Qwen model
curl -X POST http://localhost:8143/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3-coder-plus",
    "messages": [
      {
        "role": "user",
        "content": "Hello, how are you?"
      }
    ],
    "temperature": 0.7
  }'

# Using Gemini model
curl -X POST http://localhost:8143/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.5-flash",
    "messages": [
      {
        "role": "user",
        "content": "Say hello in three languages."
      }
    ],
    "temperature": 0.7
  }'

# Using Claude model
curl -X POST http://localhost:8143/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "model": "claude-sonnet-4-5",
    "messages": [
      {
        "role": "user",
        "content": "Write a short poem about technology."
      }
    ],
    "temperature": 0.7
  }'
```

### Streaming Chat Completions
```bash
curl -X POST http://localhost:8143/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.5-flash",
    "messages": [
      {
        "role": "user",
        "content": "Tell me about renewable energy sources."
      }
    ],
    "temperature": 0.7,
    "stream": true
  }'
```

## 2. Native Gemini Endpoints

### List Gemini Models
```bash
curl -X GET http://localhost:8143/gemini/models
```

### Generate Content (Non-Streaming)
```bash
curl -X POST http://localhost:8143/gemini/models/gemini-2.5-flash:generateContent \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {
        "role": "user",
        "parts": [
          {
            "text": "Explain the theory of relativity in simple terms."
          }
        ]
      }
    ],
    "generationConfig": {
      "temperature": 0.7,
      "maxOutputTokens": 500
    }
  }'

```

### Stream Generate Content
```bash
curl -X POST http://localhost:8143/gemini/models/gemini-2.5-flash:streamGenerateContent \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Accept: text/event-stream" \
  -d '{
    "contents": [
      {
        "role": "user",
        "parts": [
          {
            "text": "What are the benefits of artificial intelligence?"
          }
        ]
      }
    ],
    "generationConfig": {
      "temperature": 0.7
    }
  }'
```

## 3. Native Claude/Anthropic Endpoints

### List Claude Models
```bash
curl -X GET http://localhost:8143/anthropic/models
```

### Claude Messages (Non-Streaming)
```bash
curl -X POST http://localhost:8143/anthropic/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "model": "claude-sonnet-4-5",
    "max_tokens": 1024,
    "messages": [
      {
        "role": "user",
        "content": "How does machine learning differ from traditional programming?"
      }
    ],
    "temperature": 0.7
  }'
```

### Claude Messages (Streaming)
```bash
curl -X POST http://localhost:8143/anthropic/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "model": "claude-sonnet-4-5",
    "max_tokens": 1024,
    "messages": [
      {
        "role": "user",
        "content": "Describe the impact of climate change on biodiversity."
      }
    ],
    "temperature": 0.7,
    "stream": true
  }'
```

## 4. Health Check

### Basic Health Check
```bash
curl -X GET http://localhost:8143/v1/models
```

## 5. Testing Different Models

### Gemini Models
```bash
# Gemini Flash
curl -X POST http://localhost:8143/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "model": "gemini-2.5-flash",
    "messages": [
      {
        "role": "user",
        "content": "What is the capital of France?"
      }
    ]
  }'

# Gemini Pro
curl -X POST http://localhost:8143/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "model": "gemini-2.5-pro",
    "messages": [
      {
        "role": "user",
        "content": "Explain quantum mechanics briefly."
      }
    ]
  }'
```

### Claude Models
```bash
# Claude Sonnet
curl -X POST http://localhost:8143/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "model": "claude-sonnet-4-5",
    "messages": [
      {
        "role": "user",
        "content": "Compare renewable and non-renewable energy sources."
      }
    ]
  }'

# Claude Opus
curl -X POST http://localhost:8143/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "model": "claude-opus-4-5",
    "messages": [
      {
        "role": "user",
        "content": "Write an essay about the future of space exploration."
      }
    ]
  }'
```

## Notes

1. **Authentication**: Replace `YOUR_TOKEN` with a valid authentication token for your setup
2. **Model Names**: Use the exact model names as returned by the `/v1/models` endpoint
3. **Streaming**: For streaming responses, the server will return Server-Sent Events (SSE) format
4. **Error Handling**: If a model is not supported by a particular provider, you'll receive an appropriate error response
5. **Rate Limits**: Be mindful of rate limits imposed by the underlying providers

## Troubleshooting

If you encounter issues:
- Verify the proxy server is running on the correct port
- Check that authentication credentials are properly configured
- Confirm that the requested model is supported by the corresponding provider
- Review the proxy logs for detailed error information