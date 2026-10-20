# github-copilot

Default API base: `https://api.githubcopilot.com`.
Supported formats, in preference order: `chat`, `responses`, `messages`.

```go
model, err := republic.GetModel("github-copilot:MODEL_ID", republic.ProviderConfig{})
if err != nil { return err }
defer model.Close()
```

Uses `gh auth token` on each request by default. Supply `CopilotAuth` to exchange a plugin credential for a cached inference token, or call `LoginCopilot` for device authorization. Messages uses `/v1/messages`; stream adapters preserve stable tool IDs and recognize the Copilot terminal marker.

API keys and base URLs can be provided through `ProviderConfig` or
`REPUBLIC_GITHUB_COPILOT_API_KEY` and
`REPUBLIC_GITHUB_COPILOT_API_BASE`. `EnvPrefix` changes the prefix.
An explicitly selected APIFormat only affects models of the same kind;
embedding, image and decision models still select their supported format.

See [authentication](../guides/authentication.md) and
[configuration](../reference/configuration.md).
