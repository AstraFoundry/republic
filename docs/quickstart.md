# Quickstart

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
    ctx := context.Background()
    model, err := republic.GetModel("openai:gpt-6-sol", republic.ProviderConfig{})
    if err != nil { log.Fatal(err) }
    defer model.Close()
    response, err := model.Chat(ctx, "Explain a tool call in one sentence.", nil)
    if err != nil { log.Print(err); return }
    fmt.Println(response.Text())

    stream, err := model.Stream(ctx, "Explain streaming.", nil)
    if err != nil { log.Print(err); return }
    defer stream.Close()
    for event, err := range stream.Events() {
        if err != nil { log.Print(err); return }
        if event.Type == "text_delta" { fmt.Print(event.Chunk) }
    }
    result, err := stream.Response()
    if err != nil { log.Print(err); return }
    fmt.Println(result.TokenUsage.TotalTokens())
}
```

`Chat` and `Stream` accept a string, a `Message`, `[]string`, `[]Message`, or a
mixed `[]any` of strings and messages. Strings become user messages. Use
`User(Text("..."), image)` for multimodal inputs.

Every operation accepts `context.Context`. Use a deadline to bound a request.
Always close a stream; stopping its event loop closes its body and leaves the
provider usable. Close a shared provider once all its models are finished.

See [tools](guides/tools.md), [structured output](guides/structured-output.md),
and [authentication](guides/authentication.md).
