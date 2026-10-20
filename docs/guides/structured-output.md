# Structured output

Pass an explicit JSON Schema through output_schema. Republic adapts it to the
selected provider and validates the returned JSON locally before updating history.

```go
schema := &republic.OutputSchema{
    Name: "Answer",
    Schema: map[string]any{
        "type": "object",
        "properties": map[string]any{"answer": map[string]any{"type": "string"}},
        "required": []string{"answer"},
    },
}
var output struct { Answer string `json:"answer"` }
response, err := model.Chat(ctx, "Give a short answer.", republic.Options{
    "output_schema": schema,
    "output": &output,
})
if err != nil { return err }
_ = response
```

Without output, Response.Output is the decoded JSON value. With output, the
validated JSON is also decoded into the supplied pointer. Refusals skip output
validation and leave Output nil. Validation failure leaves history unchanged.

OpenAI strict schemas forbid unknown object properties and require every property.
Anthropic uses its own strict schema rules. Gemini receives the supplied schema.
External schema references are not fetched; use local $defs for reusable types.

Python-specific Pydantic type inference and coercion are replaced by explicit JSON
Schema and Go decoding. JSON formatting and object-key ordering are not contractual;
compare decoded values. The portable request schema and response semantics remain
the compatibility boundary.
