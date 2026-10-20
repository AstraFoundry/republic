# ollama

Default API base: `http://localhost:11434/v1`.
Supported formats, in preference order: `chat`, `responses`, `embeddings`.

```go
model, err := republic.GetModel("ollama:MODEL_ID", republic.ProviderConfig{})
if err != nil { return err }
defer model.Close()
```

Defaults to a local server. For Ollama Cloud, set APIBase to `https://ollama.com/v1` and supply an API key. Chat uses max_tokens. Responses is stateless and depends on server support.

API keys and base URLs can be provided through `ProviderConfig` or
`REPUBLIC_OLLAMA_API_KEY` and
`REPUBLIC_OLLAMA_API_BASE`. `EnvPrefix` changes the prefix.
An explicitly selected APIFormat only affects models of the same kind;
embedding, image and decision models still select their supported format.

See [authentication](../guides/authentication.md) and
[configuration](../reference/configuration.md).
