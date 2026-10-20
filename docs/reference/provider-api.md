# Provider API

## Models

GetModel, GetEmbeddingModel, GetImageModel and GetDecisionModel accept a
`provider:model` specification and ProviderConfig. The corresponding Provider
methods accept a bare model name and share the provider's HTTP client.

- Chat(ctx, prompt, options) returns Response.
- Stream(ctx, prompt, options) returns a single-pass Stream. Iterate Events(),
  close it, and read Response() after completed. Early access returns
  ErrStreamNotFinished.
- Embed(ctx, text, dimensions) and EmbedMany(ctx, texts, dimensions) return ordered
  vectors. A nil dimensions pointer omits the option.
- Generate(ctx, prompt, options) and Edit(ctx, prompt, images, mask, options) return
  ImageResponse. Edits require at least one image. Masks are optional and
  unsupported by Gemini.
- Decide(ctx, state, questions) returns answers keyed by question ID. Construct
  questions using Noul, Choice and Score.
- Provider.ListModels(ctx) follows pagination and returns ModelInfo entries.

Close providers after all active work finishes. Closing a model closes its shared
provider. Caller-supplied HTTP clients remain usable. A provider is safe for
concurrent requests; a single stream is consumed by one goroutine. Configuration
and model fields must not be mutated during active calls.

## Messages and media

System(text), User(parts...), Assistant(parts...) and ToolResult(call, isError,
parts...) construct messages. Text and Reasoning construct readable parts.
ToolCalls is an explicit assistant-message field. ProviderData is opaque and is
replayed only to its matching format.

Image(source, mediaType), Audio and Video accept a file path, remote URL, data URL
or []byte. Raw bytes require a MIME type. Remote URLs are not downloaded. A Part
provides Base64Data() and DataURL(); its inline bytes can be decoded using Go's
image libraries. JSON message serialization preserves typed media and metadata.

Use Message.Text() and Reasoning() for concatenated readable content. ToolCall.Args()
decodes JSON arguments, treating an empty argument string as an empty object.

## Responses and events

Response includes Message, TokenUsage, Output, FinishReason, Refusal, Citations,
BuiltinToolCalls, ID, Model, RequestID and Headers. Text(), Reasoning(), ToolCalls()
and ImageParts() are convenience accessors. TokenUsage.TotalTokens() includes
prompt and output tokens; cached tokens are a subset of prompt tokens.

Event.Type is text_delta, reasoning_delta, refusal_delta, tool_call_delta,
tool_call_ready, usage_delta, image_ready, citation_added,
builtin_tool_call_ready or completed. Usage deltas sum to final usage. Calls are
released once; incomplete streams never emit completed or write history.

## History

NewHistory(maxEntries) creates a synchronized InMemoryHistory. Zero is unbounded;
negative values fail. Bounded history restarts at a user message after trimming,
so retained tool results are not separated from their calls. Implement History
for external persistence and assign it to ChatModel.History before use.

## Errors and custom formats

Use errors.As with *republic.Error and inspect Kind. HTTP status errors retain
StatusCode, Body, Headers and RequestID. Connection/timeout errors preserve Cause.
Context cancellation can be checked with errors.Is. Stream truncation returns
ErrStreamIncomplete even if a finish reason appeared without a terminal marker.

RegisterProvider installs a ProviderSpec. SelectFormat can return any ChatFormat
implementation. Embed WireFormat to reuse existing behavior and override Build,
Parse or NewParser. A custom parser emits Delta values for ResponseBuilder and
must validate completion in Finish. HTTPRequest supports JSON and multipart
bodies; tools are schemas only and are never executed by formats.

EmbeddingAPIFormat, ImageAPIFormat and DecisionAPIFormat provide equivalent
interfaces for custom auxiliary protocols. ProviderSpec.SelectEmbeddingFormat,
SelectImageFormat and SelectDecisionFormat choose them by model name. ModelsParams
and ParseModels customize model-list pagination and decoding.
