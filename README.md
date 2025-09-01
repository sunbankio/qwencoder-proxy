# Qwen Proxy

A lightweight HTTP proxy server for Qwen AI models that provides enhanced streaming capabilities and authentication handling.

## Overview

Qwen Proxy is a Go-based proxy server that sits between your applications and Qwen's API, providing:

- **Stuttering Detection**: Automatically detects and filters out stuttering in streaming responses
- **OAuth2 Authentication**: Handles Qwen OAuth2 authentication flow automatically
- **Enhanced Logging**: Detailed request/response logging with performance metrics
- **Connection Pooling**: Efficient HTTP connection management for better performance
- **Configuration Management**: Environment-based configuration with sensible defaults

## Features

- Transparent proxy for Qwen API endpoints
- Automatic OAuth2 device flow authentication
- Token refresh handling
- Streaming response optimization
- Detailed request logging with metrics
- Configurable timeouts and connection pooling
- Debug mode for development

## Installation

### Option 1: Install via `go install` (Recommended)

```bash
go install github.com/sunbankio/qwencoder-proxy/cmd/qwencoder-proxy@latest
```

This will download, build, and install the `qwencoder-proxy` executable to your `$GOPATH/bin` directory.

### Option 2: Build from source

1. Clone the repository:
   ```bash
   git clone <repository-url>
   cd qwencoder-proxy
   ```

2. Build the binary:
   ```bash
   go build -o qwencoder-proxy cmd/qwencoder-proxy/main.go
   ```

## Configuration

The proxy can be configured using environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8143` | Port for the proxy server |
| `DEBUG` | `false` | Enable debug logging |
| `MAX_IDLE_CONNS` | `50` | Maximum idle connections |
| `MAX_IDLE_CONNS_PER_HOST` | `50` | Maximum idle connections per host |
| `IDLE_CONN_TIMEOUT_SECONDS` | `180` | Idle connection timeout |
| `REQUEST_TIMEOUT_SECONDS` | `300` | Request timeout |
| `STREAMING_TIMEOUT_SECONDS` | `900` | Streaming request timeout |
| `READ_TIMEOUT_SECONDS` | `45` | Read timeout |

## Usage

### If installed via `go install`:
1. Ensure `$GOPATH/bin` is in your `$PATH`
2. Start the proxy server:
   ```bash
   qwencoder-proxy
   ```

3. For debug mode:
   ```bash
   qwencoder-proxy -debug
   ```

### If built from source:
1. Start the proxy server:
   ```bash
   ./qwencoder-proxy
   ```

2. For debug mode:
   ```bash
   ./qwencoder-proxy -debug
   ```

3. The proxy will automatically handle authentication on first start. Follow the prompts to authenticate with your Qwen account.

## API Endpoints

- `POST /v1/chat/completions` - Chat completions endpoint
- `GET /v1/models` - List available models

The proxy forwards all requests to the Qwen API while adding necessary authentication headers.

## Testing

Run unit tests with:
```bash
go test ./...
```

See [TESTING.md](TESTING.md) for more details on the test suite.

## Logging

The proxy provides detailed logging with the following information:
- Client IP addresses
- Request methods and paths
- User agents
- Request/response sizes
- Streaming status
- Response status codes
- Request duration

## Authentication

The proxy handles Qwen OAuth2 authentication automatically:
1. On first start, it initiates the device authorization flow
2. Opens the verification URL in your browser
3. Saves credentials to `~/.qwen/qwenproxy_creds.json`
4. Automatically refreshes tokens when they expire

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.