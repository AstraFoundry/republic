# moonshot

Default API base: `https://api.moonshot.ai/v1`.
Supported formats, in preference order: `chat`.

```go
model, err := republic.GetModel("moonshot:MODEL_ID", republic.ProviderConfig{})
if err != nil { return err }
defer model.Close()
```

Chat replays reasoning_content for tool continuations. APIBase may be `https://api.moonshot.cn/v1` for China.

API keys and base URLs can be provided through `ProviderConfig` or
`REPUBLIC_MOONSHOT_API_KEY` and
`REPUBLIC_MOONSHOT_API_BASE`. `EnvPrefix` changes the prefix.
An explicitly selected APIFormat only affects models of the same kind;
embedding, image and decision models still select their supported format.

See [authentication](../guides/authentication.md) and
[configuration](../reference/configuration.md).
