# Republic

One Go interface for chat, streaming, tool calls, structured output, embeddings,
images and decisions across AI providers. Republic stops at providers: agent
loops, tool execution and gateways stay in your application.

Requires Go 1.23 or later.

```sh
go get github.com/AstraFoundry/republic
export REPUBLIC_OPENAI_API_KEY="your-api-key"
```

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AstraFoundry/republic"
)

func main() {
    model, err := republic.GetModel("openai:gpt-6-sol", republic.ProviderConfig{})
    if err != nil { log.Fatal(err) }
    defer model.Close()

    response, err := model.Chat(context.Background(), "Explain a tool call.", nil)
    if err != nil { log.Fatal(err) }
    fmt.Println(response.Text())

    stream, err := model.Stream(context.Background(), "Explain streaming.", nil)
    if err != nil { log.Fatal(err) }
    defer stream.Close()
    for event, err := range stream.Events() {
        if err != nil { log.Print(err); return }
        if event.Type == "text_delta" { fmt.Print(event.Chunk) }
    }
}
```

Model names use `provider:model`. Credentials default to
`REPUBLIC_<PROVIDER>_API_KEY`; hyphens become underscores. Codex, GitHub Copilot
and Grok can reuse their CLI credentials. Every request accepts a context for
cancellation and deadlines.

## Documentation

- [Quickstart](docs/quickstart.md)
- [Providers and supported formats](docs/providers/index.md)
- [Authentication](docs/guides/authentication.md)
- [Tools](docs/guides/tools.md) and [structured output](docs/guides/structured-output.md)
- [Configuration](docs/reference/configuration.md) and [API reference](docs/reference/provider-api.md)
- [Migration and compatibility](MIGRATION.md)

## Development

```sh
make check
make test
make parity
make build
```

Go is the runtime implementation and has no Python runtime dependency.
`src/republic`, `tests`, `pyproject.toml` and `uv.lock` retain the original Python
implementation and its tests as an executable migration oracle. `make parity`
compares Go requests and responses against that implementation without provider
keys or live inference requests. See [Contributing](CONTRIBUTING.md).

## License

[Apache 2.0](LICENSE).
