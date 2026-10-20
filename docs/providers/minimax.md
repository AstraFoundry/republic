# minimax

Default API base: `https://api.minimax.io`.
Supported formats, in preference order: `messages`, `chat`.

```go
model, err := republic.GetModel("minimax:MODEL_ID", republic.ProviderConfig{})
if err != nil { return err }
defer model.Close()
```

Messages is preferred. Messages requests use `/anthropic/v1`; Chat and model listing use `/v1`. Chat requests set reasoning_split and replay reasoning_content. APIBase may be `https://api.minimaxi.com` for China.

API keys and base URLs can be provided through `ProviderConfig` or
`REPUBLIC_MINIMAX_API_KEY` and
`REPUBLIC_MINIMAX_API_BASE`. `EnvPrefix` changes the prefix.
An explicitly selected APIFormat only affects models of the same kind;
embedding, image and decision models still select their supported format.

See [authentication](../guides/authentication.md) and
[configuration](../reference/configuration.md).
