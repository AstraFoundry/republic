# google

Default API base: `https://generativelanguage.googleapis.com/v1beta`.
Supported formats, in preference order: `gemini`, `embed_content`, `gemini_image`.

```go
model, err := republic.GetModel("google:MODEL_ID", republic.ProviderConfig{})
if err != nil { return err }
defer model.Close()
```

The `gemini` provider alias also works and reads `REPUBLIC_GEMINI_*` variables. API keys use `x-goog-api-key`. Gemini 3 reasoning uses thinking levels; use extra_body or a custom format for older thinking-budget models. Tool-result media must be inline. Thought signatures are retained.

API keys and base URLs can be provided through `ProviderConfig` or
`REPUBLIC_GOOGLE_API_KEY` and
`REPUBLIC_GOOGLE_API_BASE`. `EnvPrefix` changes the prefix.
An explicitly selected APIFormat only affects models of the same kind;
embedding, image and decision models still select their supported format.

See [authentication](../guides/authentication.md) and
[configuration](../reference/configuration.md).
