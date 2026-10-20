# Recorded protocol inputs

Run `go test ./...` or `make parity`. These 58 JSON/SSE bodies cover ordinary and
streaming chat, tool calls, parallel tools, reasoning/signatures, cache usage,
refusals, keepalive events and completion markers. They are parsed by Go and
compared with the original-behavior results in `../compatibility/responses.json`.
Native tests also check usage-delta totals, tool-call identity and protocol
completion invariants. No provider keys or network inference calls are required.

## Provenance

Fantasy is pinned to
[`b0fadc9ae2e7da81a5d0262563d3ced9b594bd75`](https://github.com/charmbracelet/fantasy/tree/b0fadc9ae2e7da81a5d0262563d3ced9b594bd75/providertests).
The recordings retain all interactions from simple, tool and multi-tool cassettes,
including their streaming counterparts, for Chat, Responses, Messages and Gemini.

Goose is pinned to
[`9560429ff982bbfecec5094963e5f34696b63a1c`](https://github.com/block/goose/tree/9560429ff982bbfecec5094963e5f34696b63a1c/crates/goose-provider-types/src/formats).
Its focused cases cover length termination, empty arguments, Messages thinking
and redacted blocks, fragmented signatures, parallel calls and cache usage,
Gemini usage/signatures, and Responses keepalive/refusal/call IDs.

The bodies were moved unchanged from `tests/test_provider_golden/cases` when the
original language implementation was removed. SSE bytes and event order remain
intact, protected by .gitattributes and the EOF-hook exclusion. The original
adaptation notes remain accessible in Git history before the removal.

Both sources use Apache-2.0. Their original licenses and applicable notices are
retained as LICENSE.fantasy, NOTICE.fantasy and LICENSE.goose. Do not change fixture
bodies to make a parser pass; explain intended behavior changes using the source
case and wire contract, then adjust reviewed expectations and native tests.
