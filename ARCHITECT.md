# Qwencoder-Proxy System Architecture

## 1. High-Level System Architecture

The qwencoder-proxy is a sophisticated proxy service that forwards requests to an upstream Qwen service while providing enhanced features for handling streaming responses. The system is built with a component-based architecture that separates concerns and provides resilience through circuit breaker and retry mechanisms.

```mermaid
graph TD
    A[Client] --> B[Proxy Handler]
    B --> C{Request Type}
    C -->|Streaming| D[Streaming Handler]
    C -->|Non-Streaming| E[Direct Forwarding]
    D --> F[Stream Processor]
    F --> G[Chunk Parser]
    F --> H[Error Recovery Manager]
    F --> I[Stuttering Detector]
    F --> J[Circuit Breaker]
    E --> K[Upstream Qwen Service]
    D --> K
```

### Core Components

1. **Proxy Handler** (`proxy/handler.go`): Main entry point that routes requests based on type
2. **Streaming Handler** (`proxy/streaming_handler.go`): Manages streaming request processing
3. **Stream Processor** (`proxy/streaming.go`): Coordinates stream processing with state management
4. **Chunk Parser** (`proxy/streaming.go`): Robust parsing with comprehensive error handling
5. **Error Recovery Manager** (`proxy/streaming.go`): Configurable recovery strategies with circuit breaker
6. **Stuttering Detector** (`proxy/advanced_stuttering.go`): Advanced stuttering detection algorithms
7. **Circuit Breaker** (`proxy/circuit_breaker.go`): Circuit breaker pattern for upstream resilience

## 2. Control Flow

### Non-Streaming Requests

Non-streaming requests follow a straightforward path:

1. Client sends request to proxy
2. Proxy handler determines it's a non-streaming request
3. Request is forwarded directly to upstream Qwen service
4. Response is copied back to client without additional processing

```mermaid
sequenceDiagram
    participant C as Client
    participant P as Proxy
    participant U as Upstream Qwen
    
    C->>P: HTTP Request (non-streaming)
    P->>P: Parse request, check auth
    P->>U: Forward request
    U->>P: Response
    P->>C: Forward response directly
```

### Streaming Requests

Streaming requests use a sophisticated processing pipeline:

1. Client sends streaming request to proxy
2. Proxy handler identifies streaming request and delegates to streaming handler
3. Streaming handler creates StreamProcessor with all components
4. Response is read line-by-line from upstream
5. Each line is processed through the component pipeline
6. Processed content is forwarded to client with appropriate buffering

```mermaid
sequenceDiagram
    participant C as Client
    participant P as Proxy
    participant SP as Stream Processor
    participant U as Upstream Qwen
    
    C->>P: HTTP Request (streaming)
    P->>P: Parse request, check auth
    P->>SP: Create StreamProcessor
    SP->>U: Forward request
    U->>SP: Streaming response
    loop For each chunk
        SP->>SP: Parse chunk
        SP->>SP: Detect stuttering
        SP->>SP: Apply error recovery if needed
        SP->>C: Forward processed chunk
    end
```

## 3. Detailed Processing Flow for Streaming Requests

### Component-Based Architecture

The refactored streaming architecture uses a component-based approach with clear separation of concerns:

```mermaid
graph TD
    A[StreamProcessor] --> B[StreamState]
    A --> C[ChunkParser]
    A --> D[ErrorRecoveryManager]
    A --> E[AdvancedStutteringDetector]
    D --> F[CircuitBreaker]
    D --> G[RetryHandler]
```

### Stream State Machine

The processing follows a state machine with five distinct states:

1. **StateInitial**: Initial state when processing begins
2. **StateStuttering**: Buffering chunks while detecting stuttering patterns
3. **StateNormalFlow**: Normal streaming with direct forwarding
4. **StateRecovering**: Error recovery in progress
5. **StateTerminating**: Stream completion or error termination

```mermaid
stateDiagram-v2
    [*] --> Initial
    Initial --> Stuttering: First content chunk
    Initial --> NormalFlow: Non-content chunk
    Stuttering --> NormalFlow: Stuttering resolved
    Stuttering --> Terminating: DONE received
    NormalFlow --> Recovering: Error detected
    NormalFlow --> Terminating: DONE received
    Recovering --> NormalFlow: Recovery successful
    Recovering --> Terminating: Recovery failed
    Terminating --> [*]
```

### Chunk Processing Pipeline

Each chunk goes through a detailed processing pipeline:

1. **Parsing**: ChunkParser converts raw line into structured ParsedChunk
2. **State Handling**: StreamProcessor routes chunk based on current state
3. **Stuttering Detection**: Advanced algorithms analyze content patterns
4. **Error Handling**: ErrorRecoveryManager applies appropriate strategies
5. **Forwarding**: Processed content is sent to client

```mermaid
flowchart TD
    A[Raw Chunk] --> B[Chunk Parser]
    B --> C{Valid?}
    C -->|Yes| D[State Handler]
    C -->|No| E[Error Handler]
    D --> F{State?}
    F -->|Initial| G[Initial Handler]
    F -->|Stuttering| H[Stuttering Handler]
    F -->|Normal| I[Normal Handler]
    F -->|Recovering| J[Recovery Handler]
    G --> K[Stuttering Detection]
    H --> K
    I --> L[Direct Forward]
    J --> M[Recovery Strategy]
    K --> N{Still Stuttering?}
    N -->|Yes| O[Buffer Content]
    N -->|No| P[Flush Buffer + Forward]
    M --> Q[Apply Recovery Action]
    E --> R[Error Recovery Manager]
    R --> S[Circuit Breaker Check]
    S --> T[Retry Logic]
```

## 4. Component Interactions and Data Flow

### StreamProcessor and Components

The StreamProcessor orchestrates all components:

```mermaid
graph TD
    A[StreamProcessor] --> B[ProcessLine]
    B --> C[Parse Chunk]
    C --> D{State-Based Routing}
    D --> E[Initial Handler]
    D --> F[Stuttering Handler]
    D --> G[Normal Handler]
    D --> H[Recovery Handler]
    E --> I[Stuttering Detection]
    F --> I
    G --> J[Direct Forward]
    H --> K[Error Recovery]
    I --> L{Buffer or Forward}
    L -->|Buffer| M[Buffer Content]
    L -->|Forward| N[Send to Client]
    K --> O[Recovery Strategy]
    O --> P[Circuit Breaker]
    O --> Q[Retry Handler]
```

### Advanced Stuttering Detection

The sophisticated stuttering detection uses multiple analysis methods:

```mermaid
graph TD
    A[Content Chunk] --> B{Analysis Methods}
    B --> C[Prefix Pattern Analysis]
    B --> D[Length Progression Analysis]
    B --> E[Timing Pattern Analysis]
    B --> F[Content Similarity Analysis]
    C --> G[Weighted Scoring]
    D --> G
    E --> G
    F --> G
    G --> H[Combined Confidence Score]
    H --> I{Confidence > Threshold?}
    I -->|Yes| J[Continue Buffering]
    I -->|No| K[Flush Buffer]
```

### Error Recovery with Circuit Breaker

Error handling is enhanced with circuit breaker and retry mechanisms:

```mermaid
graph TD
    A[Error Occurs] --> B[Error Classification]
    B --> C[Recovery Strategy]
    C --> D{Action Type}
    D -->|Retry| E[Circuit Breaker]
    D -->|Skip| F[Skip Chunk]
    D -->|Continue| G[Continue Processing]
    D -->|Terminate| H[Terminate Stream]
    E --> I[Can Execute?]
    I -->|Yes| J[Retry with Backoff]
    I -->|No| F
    J --> K{Retry Successful?}
    K -->|Yes| G
    K -->|No| L[Increment Failure Count]
    L --> M{Failure Count > Threshold?}
    M -->|Yes| N[Open Circuit]
    M -->|No| F
```

## 5. Resilience Mechanisms

### Circuit Breaker Pattern

The system implements the circuit breaker pattern to prevent cascade failures:

1. **Closed State**: Normal operation, all requests allowed
2. **Open State**: Failure threshold exceeded, requests blocked
3. **Half-Open State**: Testing recovery with limited requests

```mermaid
stateDiagram-v2
    [*] --> Closed
    Closed --> Open: Failure threshold exceeded
    Open --> HalfOpen: Timeout period elapsed
    HalfOpen --> Closed: Successful requests
    HalfOpen --> Open: Failed request
```

### Retry with Exponential Backoff

Retry logic uses exponential backoff with jitter:

1. Configurable max retries
2. Exponential delay increase
3. Random jitter to prevent thundering herd
4. Smart error classification for retryable errors

## 6. Configuration and Monitoring

The system supports configuration through environment variables:

- `STREAMING_MAX_ERRORS`: Maximum errors before termination
- `STREAMING_BUFFER_SIZE`: Buffer size for processing
- `STREAMING_TIMEOUT_SECONDS`: Processing timeout

Monitoring capabilities include:
- Detailed logging at each processing stage
- State transition tracking
- Error statistics and circuit breaker status
- Performance metrics

This architecture provides a robust, scalable solution for handling streaming responses with sophisticated stuttering detection and error recovery mechanisms.