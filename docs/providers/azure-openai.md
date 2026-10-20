# azure-openai

Default API base: `(resource required)`.
Supported formats, in preference order: `responses`, `chat`, `embeddings`, `images`.

```go
model, err := republic.GetModel("azure-openai:MODEL_ID", republic.ProviderConfig{})
if err != nil { return err }
defer model.Close()
```

Set `Resource` or `REPUBLIC_AZURE_OPENAI_RESOURCE`, or provide a full APIBase ending in `/openai/v1`. Model names are deployment names. Keys use the `api-key` header. Image edits upload multipart data and require inline images and masks.

API keys and base URLs can be provided through `ProviderConfig` or
`REPUBLIC_AZURE_OPENAI_API_KEY` and
`REPUBLIC_AZURE_OPENAI_API_BASE`. `EnvPrefix` changes the prefix.
An explicitly selected APIFormat only affects models of the same kind;
embedding, image and decision models still select their supported format.

See [authentication](../guides/authentication.md) and
[configuration](../reference/configuration.md).
