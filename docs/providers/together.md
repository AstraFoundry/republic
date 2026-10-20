# together

Default API base: `https://api.together.ai/v1`.
Supported formats, in preference order: `chat`, `embeddings`.

```go
model, err := republic.GetModel("together:MODEL_ID", republic.ProviderConfig{})
if err != nil { return err }
defer model.Close()
```

Supports chat and embeddings. Chat uses max_tokens.

API keys and base URLs can be provided through `ProviderConfig` or
`REPUBLIC_TOGETHER_API_KEY` and
`REPUBLIC_TOGETHER_API_BASE`. `EnvPrefix` changes the prefix.
An explicitly selected APIFormat only affects models of the same kind;
embedding, image and decision models still select their supported format.

See [authentication](../guides/authentication.md) and
[configuration](../reference/configuration.md).
