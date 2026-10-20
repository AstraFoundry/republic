# magpie

Default API base: `http://127.0.0.1:3425/v1`.
Supported formats, in preference order: `chat`, `responses`, `messages`, `system_one`.

```go
model, err := republic.GetModel("magpie:MODEL_ID", republic.ProviderConfig{})
if err != nil { return err }
defer model.Close()
```

Defaults to the local Magpie gateway and the key `magpie`. Supports multiple wire protocols and System One decision models.

API keys and base URLs can be provided through `ProviderConfig` or
`REPUBLIC_MAGPIE_API_KEY` and
`REPUBLIC_MAGPIE_API_BASE`. `EnvPrefix` changes the prefix.
An explicitly selected APIFormat only affects models of the same kind;
embedding, image and decision models still select their supported format.

See [authentication](../guides/authentication.md) and
[configuration](../reference/configuration.md).
