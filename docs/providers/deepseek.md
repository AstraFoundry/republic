# deepseek

Default API base: `https://api.deepseek.com`.
Supported formats, in preference order: `chat`, `responses`, `messages`.

```go
model, err := republic.GetModel("deepseek:MODEL_ID", republic.ProviderConfig{})
if err != nil { return err }
defer model.Close()
```

Chat uses max_tokens and replays reasoning_content. Messages requests are routed under `/anthropic/v1`. The selected format controls which options can be expressed.

API keys and base URLs can be provided through `ProviderConfig` or
`REPUBLIC_DEEPSEEK_API_KEY` and
`REPUBLIC_DEEPSEEK_API_BASE`. `EnvPrefix` changes the prefix.
An explicitly selected APIFormat only affects models of the same kind;
embedding, image and decision models still select their supported format.

See [authentication](../guides/authentication.md) and
[configuration](../reference/configuration.md).
