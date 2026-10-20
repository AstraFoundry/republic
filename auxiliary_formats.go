package republic

import "net/http"

// EmbeddingAPIFormat implements a custom embedding protocol.
type EmbeddingAPIFormat interface {
	Name() string
	Headers() http.Header
	Build(model string, texts []string, dimensions *int) (HTTPRequest, error)
	Parse(map[string]any) (EmbeddingResponse, error)
}

// ImageAPIFormat implements image generation and editing.
type ImageAPIFormat interface {
	Name() string
	Headers() http.Header
	Generate(model, prompt string, options Options) (HTTPRequest, error)
	Edit(model, prompt string, images []Part, mask *Part, options Options) (HTTPRequest, error)
	Parse(map[string]any) (ImageResponse, error)
}

// DecisionAPIFormat implements a typed decision protocol.
type DecisionAPIFormat interface {
	Name() string
	Headers() http.Header
	Build(model string, state any, questions map[string]Question) (HTTPRequest, error)
	Parse(map[string]any) (DecisionResponse, error)
}
