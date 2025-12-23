## Provider Native Formats

**Qwen** (`provider/qwen`):
- **Native Protocol**: `ProtocolQwen` (but OpenAI-compatible)
- **Request Format**: OpenAI-compatible chat completions format
- **Response Format**: OpenAI-compatible chat completions format
- **Verification**: The `QwenConverter` returns the native request/response unchanged when converting to OpenAI format, confirming it uses OpenAI format natively.

**Iflow** (`provider/iflow`):
- **Native Protocol**: `ProtocolOpenAI`
- **Request Format**: Standard OpenAI chat completions API format
- **Response Format**: Standard OpenAI chat completions response format
- **Verification**: The provider directly uses OpenAI structures (`OpenAIChatResponse`, `OpenAIModel`, etc.) and sends requests to `/chat/completions` endpoint.

**Gemini** (`provider/gemini`):
- **Native Protocol**: `ProtocolGemini`
- **Request Format**: Google's Gemini API format (with `contents`, `generationConfig`, etc.)
- **Response Format**: Google's Gemini API format (with `candidates`, `usageMetadata`, etc.)
- **Verification**: The `GeminiConverter` performs full conversion between Gemini and OpenAI formats.

**Antigravity** (`provider/antigravity`):
- **Native Protocol**: `ProtocolGemini`
- **Request Format**: Google's Gemini API format (same as Gemini CLI)
- **Response Format**: Google's Gemini API format
- **Verification**: Uses `ProtocolGemini` and leverages Gemini structures/types.

**Kiro** (`provider/kiro`):
- **Native Protocol**: `ProtocolClaude`
- **Request Format**: Anthropic Claude API format (with `messages`, `system`, etc.)
- **Response Format**: Anthropic Claude API format (with `content` blocks, `usage`, etc.)
- **Verification**: The `ClaudeConverter` performs conversion between Claude and OpenAI formats, and the provider builds Kiro-specific requests internally.

## Protocol Definitions

From `provider/provider.go`:
- `ProtocolOpenAI`: OpenAI-compatible format
- `ProtocolGemini`: Google Gemini format  
- `ProtocolClaude`: Anthropic Claude format
- `ProtocolQwen`: Qwen format (OpenAI-compatible in practice)

