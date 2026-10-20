# Go migration

Republic now contains the Go implementation and Go tests. The original Python
library, tests, examples, packaging and dependency configuration have been removed.
Compatibility checks use frozen original-behavior data; neither the library nor
the tests start Python.

## Acceptance checklist

- [x] Content, message serialization, normalization, history, response accumulation.
- [x] Chat, Responses, Messages, Gemini requests and streaming parsers.
- [x] All built-in provider defaults, dialects, model selection and pagination.
- [x] Authentication, local credentials, refresh and login flows.
- [x] Embeddings, image generation/editing and System One decisions.
- [x] HTTP errors, request IDs, retries, cancellation and resource lifecycle.
- [x] Extensions and custom provider/format hooks.
- [x] Structured output and schema adaptation.
- [x] Go compatibility tests against frozen original-behavior data and protocol fixtures.
- [x] Go race tests, fuzz seeds, coverage, static checks, CI and examples.
- [x] Migration documentation.

Language-specific APIs use Go conventions (`context.Context`, errors, `Close`,
interfaces and explicit schema/decoding). Wire requests, preserved metadata,
usage accounting, stream completion and failure behavior must stay equivalent.
Provider-side tool execution remains outside this package.

## Compatibility boundary

The Go module and its tests require only Go. Before removing the original source,
its request and response results were exported into `testdata/compatibility`.
The raw protocol recordings and their original licenses remain in
`testdata/protocol`. Baseline provenance and maintenance rules are documented
alongside the data; changing Go behavior cannot silently rewrite expectations.

Names and ownership follow Go conventions:

| Python | Go |
| --- | --- |
| `await model.chat(...)` | `model.Chat(ctx, prompt, options)` |
| `async with model.stream(...)` | `Stream`, `defer Close()`, range over `Events()` |
| `response.text` | `response.Text()` |
| `republic.user("text", image)` | `republic.User(republic.Text("text"), image)` |
| `output_schema=PythonType` | explicit `OutputSchema` and optional Go output pointer |
| `InMemoryHistory(max_entries=None)` | `NewHistory(0)` |
| exceptions | returned errors and `errors.As` / `errors.Is` |
| model/provider context managers | close a shared Provider after all models finish |
| Pillow images | `Part.Data` / `Part.URL`, decoded with Go image packages as needed |

Structured output uses JSON Schema validation rather than Pydantic type inference,
coercion or Python validators. Applications relying on custom validators must
apply their equivalent in Go. JSON object key order and formatting of reconstructed
tool arguments are not semantic guarantees. Supplied wire argument strings and
opaque provider payload values are retained; generated Gemini IDs are checked for
uniqueness and identity rather than compared to another run's random IDs.

## Verification

- 570 frozen request cases cover all four chat protocols, compatible
  provider dialects, tools, built-ins, schema mapping, option rejection, and streaming.
- All 58 existing JSON/SSE recordings are parsed by Go and checked against
  captured original responses. Tests compare messages, tool arguments and
  metadata, token usage, finish reasons,
  citations, built-in calls, IDs/models and the emitted event-type sequence.
- Native tests cover HTTP retries, body-read failures, cancellation, stream
  truncation/early exit, history isolation, client ownership, provider registration,
  auxiliary models, pagination, schema validation, OAuth refresh/persistence,
  cross-instance file locks, PKCE/device login and Copilot stream repairs.
- Before removing the original source, all 553 original Python tests passed.
  They are not part of the current repository or CI. Earlier migration reports
  incorrectly counted 608 request cases; the exported corpus contains 570.
- Race testing, Go vet/build, Go 1.23 testing, Linux/Windows cross-compilation and
  strict documentation builds are part of local verification. CI additionally
  executes Go tests on Linux, macOS and Windows.
- Fuzz targets exercise arbitrary message JSON and stream payloads. Short local
  campaigns have executed over 980,000 inputs without a crash.

These are offline compatibility tests, not live provider integration tests.
No real provider keys, user credential stores or paid inference calls are used.
CI status for the published candidate is reported separately.

Candidate publication follows AGENTS.md: open the PR and explicitly dispatch
Main with the branch, PR number and full candidate SHA. Native CI links are
reported with the candidate rather than recorded as a permanent checklist item.
