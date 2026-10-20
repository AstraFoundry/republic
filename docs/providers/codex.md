# codex

Default API base: `https://chatgpt.com/backend-api/codex`.
Supported formats, in preference order: `responses`.

```go
model, err := republic.GetModel("codex:MODEL_ID", republic.ProviderConfig{})
if err != nil { return err }
defer model.Close()
```

Uses Codex OAuth credentials from `$CODEX_HOME/auth.json` by default. Both Chat and Stream use the streaming Responses endpoint. Requests force `store: false`, include encrypted reasoning, and default instructions to `You are Codex.`. `max_tokens` and model listing are unsupported.

API keys and base URLs can be provided through `ProviderConfig` or
`REPUBLIC_CODEX_API_KEY` and
`REPUBLIC_CODEX_API_BASE`. `EnvPrefix` changes the prefix.
An explicitly selected APIFormat only affects models of the same kind;
embedding, image and decision models still select their supported format.

See [authentication](../guides/authentication.md) and
[configuration](../reference/configuration.md).
