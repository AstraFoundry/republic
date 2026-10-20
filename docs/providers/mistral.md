# mistral

Default API base: `https://api.mistral.ai/v1`.
Supported formats, in preference order: `chat`, `embeddings`.

```go
model, err := republic.GetModel("mistral:MODEL_ID", republic.ProviderConfig{})
if err != nil { return err }
defer model.Close()
```

Chat uses max_tokens and random_seed; top_k is rejected. Mistral thinking content is parsed and replayed. Embedding dimensions map to output_dimension.

API keys and base URLs can be provided through `ProviderConfig` or
`REPUBLIC_MISTRAL_API_KEY` and
`REPUBLIC_MISTRAL_API_BASE`. `EnvPrefix` changes the prefix.
An explicitly selected APIFormat only affects models of the same kind;
embedding, image and decision models still select their supported format.

See [authentication](../guides/authentication.md) and
[configuration](../reference/configuration.md).
