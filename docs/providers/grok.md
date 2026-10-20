# grok

Default API base: `https://api.x.ai/v1`.
Supported formats, in preference order: `responses`, `chat`.

```go
model, err := republic.GetModel("grok:MODEL_ID", republic.ProviderConfig{})
if err != nil { return err }
defer model.Close()
```

Uses the official Grok CLI OAuth credentials by default. `GROK_AUTH_PATH` overrides `$GROK_HOME/auth.json`. Refreshes preserve unrelated credential fields and use the CLI-compatible file lock. An explicit API key or Auth overrides local login.

API keys and base URLs can be provided through `ProviderConfig` or
`REPUBLIC_GROK_API_KEY` and
`REPUBLIC_GROK_API_BASE`. `EnvPrefix` changes the prefix.
An explicitly selected APIFormat only affects models of the same kind;
embedding, image and decision models still select their supported format.

See [authentication](../guides/authentication.md) and
[configuration](../reference/configuration.md).
