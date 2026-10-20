# Tool use

Republic declares tools and returns calls. The caller validates arguments and
executes tools. Function schemas are JSON Schema objects.

```go
weather := republic.Tool{
    Name: "weather",
    Description: "Get the weather for a city",
    Parameters: map[string]any{
        "type": "object",
        "properties": map[string]any{"city": map[string]any{"type": "string"}},
        "required": []string{"city"},
    },
}
response, err := model.Chat(ctx, "Weather in Paris?", republic.Options{
    "tools": []republic.Tool{weather},
    "tool_choice": "auto",
})
if err != nil { return err }
messages := []republic.Message{republic.User(republic.Text("Weather in Paris?")), response.Message}
for _, call := range response.ToolCalls() {
    args, err := call.Args()
    if err != nil { return err }
    _ = args // Validate and execute in your application.
    messages = append(messages, republic.ToolResult(call, false, republic.Text("Sunny")))
}
```

Pass a Tool as tool_choice to require that function. Other choices are auto, none
and required. Strict is sent where supported; Gemini rejects strict function tools.

ToolResult retains the full call, including provider signatures. An orphan result
is automatically preceded by an assistant announcement. Preserve response.Message
for continuations; rebuilding it from text loses reasoning and provider data.

## Provider-side tools

WebSearch(), WebFetch(), CodeExecution() and ImageGeneration() return BuiltinTool
values. Configure MaxUses, AllowedDomains, BlockedDomains and UserLocation where
supported. Mix function and built-in tools using `[]any`. NativeTool(format, body)
sends a provider-native declaration only through its matching format.

Unsupported tools and settings return ErrUnsupportedFeature. Results expose
BuiltinToolCalls and Citations. Streaming emits builtin_tool_call_ready and
citation_added alongside text. Republic never executes these tools locally.
