# anthropic

Default API base: `https://api.anthropic.com/v1`.
Supported formats, in preference order: `messages`.

```go
model, err := republic.GetModel("anthropic:MODEL_ID", republic.ProviderConfig{})
if err != nil { return err }
defer model.Close()
```

API keys use `x-api-key`; requests include `anthropic-version: 2023-06-01`. The default max_tokens is 16000. Thinking and server-tool blocks are preserved for replay. Consecutive messages of the same wire role are merged.

API keys and base URLs can be provided through `ProviderConfig` or
`REPUBLIC_ANTHROPIC_API_KEY` and
`REPUBLIC_ANTHROPIC_API_BASE`. `EnvPrefix` changes the prefix.
An explicitly selected APIFormat only affects models of the same kind;
embedding, image and decision models still select their supported format.

See [authentication](../guides/authentication.md) and
[configuration](../reference/configuration.md).
