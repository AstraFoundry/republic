# Republic

Republic is a Go library with one interface for AI providers. It supports chat,
streaming, tools, structured output, embeddings, image generation and decisions.
It does not execute tools or run an agent loop.

Start with the [quickstart](quickstart.md), choose a [provider](providers/index.md),
then see [configuration](reference/configuration.md) and the
[API reference](reference/provider-api.md).

The library and all tests run with Go alone. Frozen original-behavior baselines
and protocol recordings in testdata preserve compatibility checks without a
second runtime implementation.
