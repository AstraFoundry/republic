# openai

Default API base: `https://api.openai.com/v1`.
Supported formats, in preference order: `responses`, `chat`, `embeddings`, `images`.

```go
model, err := republic.GetModel("openai:MODEL_ID", republic.ProviderConfig{})
if err != nil { return err }
defer model.Close()
```



API keys and base URLs can be provided through `ProviderConfig` or
`REPUBLIC_OPENAI_API_KEY` and
`REPUBLIC_OPENAI_API_BASE`. `EnvPrefix` changes the prefix.
An explicitly selected APIFormat only affects models of the same kind;
embedding, image and decision models still select their supported format.

See [authentication](../guides/authentication.md) and
[configuration](../reference/configuration.md).
