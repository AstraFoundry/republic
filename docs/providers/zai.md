# zai

Default API base: `https://api.z.ai/api/paas/v4`.
Supported formats, in preference order: `chat`.

```go
model, err := republic.GetModel("zai:MODEL_ID", republic.ProviderConfig{})
if err != nil { return err }
defer model.Close()
```

Chat uses max_tokens and replays reasoning_content. APIBase may be `https://open.bigmodel.cn/api/paas/v4` for China.

API keys and base URLs can be provided through `ProviderConfig` or
`REPUBLIC_ZAI_API_KEY` and
`REPUBLIC_ZAI_API_BASE`. `EnvPrefix` changes the prefix.
An explicitly selected APIFormat only affects models of the same kind;
embedding, image and decision models still select their supported format.

See [authentication](../guides/authentication.md) and
[configuration](../reference/configuration.md).
