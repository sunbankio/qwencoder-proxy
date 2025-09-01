# Qwen Proxy - Unit Tests

This document describes the unit tests implemented for the Qwen Proxy to ensure core functionality remains intact after code updates.

## Test Coverage

The unit tests cover the following key areas:

### 1. JSON Chunk Processing
- `chunkToJson`: Tests JSON parsing of streaming chunks
- `extractDeltaContent`: Tests extraction of content from delta objects
- `hasPrefixRelationship`: Tests prefix relationship detection between strings

### 2. Stuttering Detection Logic
- `stutteringProcess`: Tests the logic for detecting and handling stuttering in streaming responses

### 3. Authentication and Token Management
- `IsTokenValid`: Tests token validity checking with expiration dates
- `generateCodeVerifier` and `generateCodeChallenge`: Tests PKCE parameter generation
- `generatePKCEParams`: Tests complete PKCE parameter generation

### 4. Configuration Loading
- `DefaultConfig`: Tests default configuration values
- `LoadConfig`: Tests loading configuration from environment variables
- `SharedHTTPClient` and `StreamingHTTPClient`: Tests HTTP client creation with proper timeouts

### 5. Proxy Handler Functions
- `checkIfStreaming`: Tests detection of streaming requests
- `constructTargetURL`: Tests URL construction for proxy requests
- `SetProxyHeaders`: Tests proper header setting for proxy requests

## Running Tests

To run all tests:

```bash
go test ./...
```

To run tests with verbose output:

```bash
go test -v ./...
```

To run tests for a specific package:

```bash
go test ./proxy
go test ./auth
go test ./config
```

## Test Structure

Tests follow the standard Go testing conventions:
- Each package has its own `_test.go` files
- Test functions are named `TestXxx` where `Xxx` is the function being tested
- Table-driven tests are used for multiple test cases
- Environment variable isolation is implemented where needed

## Continuous Integration

These tests should be run as part of any continuous integration pipeline to ensure code changes don't break core functionality.