# Providers

Use `provider:model` specifications. The first format of the requested model kind
is preferred unless APIFormat overrides it. Unsupported kinds fail before sending
an HTTP request. The `gemini` alias refers to Google with its own environment prefix.

| Provider | Formats |
| --- | --- |
| [anthropic](anthropic.md) | `messages` |
| [azure-openai](azure-openai.md) | `responses`, `chat`, `embeddings`, `images` |
| [codex](codex.md) | `responses` |
| [deepseek](deepseek.md) | `chat`, `responses`, `messages` |
| [github-copilot](github-copilot.md) | `chat`, `responses`, `messages` |
| [google](google.md) | `gemini`, `embed_content`, `gemini_image` |
| [grok](grok.md) | `responses`, `chat` |
| [magpie](magpie.md) | `chat`, `responses`, `messages`, `system_one` |
| [minimax](minimax.md) | `messages`, `chat` |
| [mistral](mistral.md) | `chat`, `embeddings` |
| [moonshot](moonshot.md) | `chat` |
| [ollama](ollama.md) | `chat`, `responses`, `embeddings` |
| [openai](openai.md) | `responses`, `chat`, `embeddings`, `images` |
| [openrouter](openrouter.md) | `responses`, `messages`, `chat`, `embeddings`, `system_one` |
| [together](together.md) | `chat`, `embeddings` |
| [typesafe](typesafe.md) | `system_one` |
| [zai](zai.md) | `chat` |

`GetProvider` creates a reusable provider. Its GetModel, GetEmbeddingModel,
GetImageModel and GetDecisionModel methods share one HTTP client. Close the
provider after all models and streams finish.

Register custom providers with `RegisterProvider` and `ProviderSpec`.
`SelectFormat` can choose a custom ChatFormat based on the model name.
