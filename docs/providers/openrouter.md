# openrouter

Default API base: `https://openrouter.ai/api/v1`.
Supported formats, in preference order: `responses`, `messages`, `chat`, `embeddings`, `system_one`.

```go
model, err := republic.GetModel("openrouter:MODEL_ID", republic.ProviderConfig{})
if err != nil { return err }
defer model.Close()
```

The default chat model format is Responses. Select APIFormat: "chat" for Chat Completions. Chat reasoning options are nested under reasoning. OpenRouter accepts extra audio encodings. LoginOpenRouter performs PKCE login and returns an API key for caller-managed storage.

API keys and base URLs can be provided through `ProviderConfig` or
`REPUBLIC_OPENROUTER_API_KEY` and
`REPUBLIC_OPENROUTER_API_BASE`. `EnvPrefix` changes the prefix.
An explicitly selected APIFormat only affects models of the same kind;
embedding, image and decision models still select their supported format.

See [authentication](../guides/authentication.md) and
[configuration](../reference/configuration.md).
