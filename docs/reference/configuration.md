# Configuration

## ProviderConfig

| Field | Behavior |
| --- | --- |
| APIKey, APIBase | Explicit credentials/base override environment defaults |
| EnvPrefix | Defaults to REPUBLIC_PROVIDER with hyphens replaced by underscores |
| APIFormat | Preferred wire format for its model kind |
| Auth | Custom signer; overrides key authentication |
| Headers | Additional headers, copied on construction |
| ExtraBody | Deep-merged defaults for chat requests |
| HTTPClient | Caller-owned client; closing the provider does not close it |
| Timeout | Owned-client I/O timeout; zero selects 600 seconds |
| MaxRetries | Pointer; nil selects 2, pointer to zero disables retries |
| RetryDelay | Pointer; nil selects 500 ms exponential backoff with jitter |
| MaxRetryDelay | Pointer; nil selects 60 seconds |
| Resource | Azure resource name |
| Extensions | Ordered synchronous observation hooks |

The default owned client uses a 10-second connection timeout and 600-second
read/write idle timeouts. An explicit Timeout applies to connection and I/O
operations. Active streams may exceed that duration in total. Context cancellation stops
requests and backoff. Retryable HTTP statuses are 408, 409, 429 and 5xx. Retry-After
seconds, dates and retry-after-ms are honored within the configured cap. Opened
streams are never replayed. Ordinary response read failures can be retried.

## Options

Options is `map[string]any`. Use the portable keys:

- tools, tool_choice, parallel_tool_calls
- max_tokens, temperature, top_p, top_k
- presence_penalty, frequency_penalty, stop, seed
- reasoning_effort, include_reasoning
- output_schema, output
- extra_body

Zero and false remain explicit values. Unsupported portable options fail rather
than being silently discarded. extra_body is deep-merged last and may override
wire fields. Reasoning effort values follow the selected service's vocabulary.

Image options are n, size, aspect_ratio, quality, background, output_format,
output_compression, moderation and extra_body. Unsupported settings fail locally.

## Extensions

Implement Extension or use ExtensionFuncs. Start returns per-call state; Retry
receives that state, the error, attempt number and delay. Finish receives the
state, result and error exactly once. Hook panics are logged and isolated.
Callbacks run synchronously and must not mutate borrowed responses. Concurrent
calls may invoke the same extension concurrently, so protect shared state.

A failed output validation/history write can produce both a result and an error.
An abandoned stream finishes with neither. A successful stream notifies Finish
before emitting completed.
