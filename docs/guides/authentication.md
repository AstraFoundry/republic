# Authentication

Keys default to `REPUBLIC_<PROVIDER>_API_KEY`. An explicit `ProviderConfig.Auth`
overrides APIKey; APIKey overrides the environment. Headers are signed on each
attempt so refreshable credentials can rotate between retries.

```go
provider, err := republic.GetProvider("anthropic", republic.ProviderConfig{
    APIKey: "your-key",
})
if err != nil { return err }
defer provider.Close()
```

For custom authentication implement `Auth.Apply(context.Context, *http.Request)`
or use AuthFunc, HeaderAuth or OAuth2Auth. Bearer-token persistence belongs to the
caller unless a file-backed credential helper was explicitly selected.

## CLI credentials

- `CodexAuthFromFile(path)` reads `$CODEX_HOME/auth.json` when path is empty.
  Access tokens refresh when expiry metadata says they are near expiry. Opaque
  tokens without expiry are sent unchanged. Account IDs are sent separately.
- `GrokAuthFromFile(path)` honors GROK_AUTH_PATH and GROK_HOME. Refreshes acquire
  the CLI-compatible OS lock and reread the file under the lock.
- `GitHubCLIAuth` reads `gh auth token --hostname HOST` for each request. It does
  not copy the GitHub CLI credential store.

Codex and Grok file helpers preserve unrelated JSON fields and atomically replace
credential files with private permissions. Token() returns a snapshot for
caller-managed persistence when credentials were created in memory.

## Explicit login

`LoginCodex`, `LoginGrok` and `LoginGitHubCLI` run the official CLI in the caller's
terminal. They accept a context and optional executable path. Login is never
started automatically by an inference request.

`LoginCopilot(ctx, callback)` uses GitHub's plugin device flow. The callback receives
the verification URL and user code; nil prints them to stderr. Persist the returned
GitHubToken() yourself. NewCopilotAuth restores it. Exchanges use the inference
client's transport and follow the validated HTTPS API origin returned by GitHub.

`LoginOpenRouter(ctx, callback)` supplies a PKCE authorization URL and expects an
authorization code. Save APIKey from the returned OpenRouterAuth yourself.

When supplying an HTTP client whose RoundTripper handles authentication, also set
Auth explicitly (a no-op AuthFunc is sufficient) to suppress CLI credential lookup.
