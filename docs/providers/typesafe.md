# typesafe

Default API base: `https://api.typesafe.ai/v1`.
Supported formats, in preference order: `system_one`.

```go
model, err := republic.GetDecisionModel("typesafe:MODEL_ID", republic.ProviderConfig{})
if err != nil { return err }
defer model.Close()
```

Use GetDecisionModel and Decide with Noul, Choice and Score questions. This provider has no chat format.

API keys and base URLs can be provided through `ProviderConfig` or
`REPUBLIC_TYPESAFE_API_KEY` and
`REPUBLIC_TYPESAFE_API_BASE`. `EnvPrefix` changes the prefix.
An explicitly selected APIFormat only affects models of the same kind;
embedding, image and decision models still select their supported format.

See [authentication](../guides/authentication.md) and
[configuration](../reference/configuration.md).
