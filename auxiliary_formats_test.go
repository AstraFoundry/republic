package republic

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type customEmbedding struct{}

func (customEmbedding) Name() string         { return "custom-embedding" }
func (customEmbedding) Headers() http.Header { return http.Header{"X-Custom": []string{"embedding"}} }
func (customEmbedding) Build(model string, texts []string, dimensions *int) (HTTPRequest, error) {
	return HTTPRequest{Path: "/vectors", Body: map[string]any{"model": model, "texts": texts}}, nil
}
func (customEmbedding) Parse(v map[string]any) (EmbeddingResponse, error) {
	return EmbeddingResponse{Vectors: [][]float64{{float64(num(v["value"]))}}}, nil
}

type customImage struct{}

func (customImage) Name() string         { return "custom-image" }
func (customImage) Headers() http.Header { return http.Header{"X-Custom": []string{"image"}} }
func (customImage) Generate(model, prompt string, o Options) (HTTPRequest, error) {
	return HTTPRequest{Path: "/draw", Body: map[string]any{"model": model, "prompt": prompt}}, nil
}
func (customImage) Edit(model, prompt string, images []Part, mask *Part, o Options) (HTTPRequest, error) {
	return HTTPRequest{Path: "/edit", Body: map[string]any{"model": model, "prompt": prompt}}, nil
}
func (customImage) Parse(v map[string]any) (ImageResponse, error) {
	return ImageResponse{ImageParts: []Part{{Type: "image", URL: str(v["url"]), MediaType: "image/png"}}}, nil
}

type customDecision struct{}

func (customDecision) Name() string         { return "custom-decision" }
func (customDecision) Headers() http.Header { return http.Header{"X-Custom": []string{"decision"}} }
func (customDecision) Build(model string, state any, questions map[string]Question) (HTTPRequest, error) {
	return HTTPRequest{Path: "/judge", Body: map[string]any{"state": state}}, nil
}
func (customDecision) Parse(v map[string]any) (DecisionResponse, error) {
	return DecisionResponse{Answers: map[string]Answer{"yes": {Type: "noul", Noul: 1}}}, nil
}

func TestCustomAuxiliaryFormats(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "custom-id")
		switch r.URL.Path {
		case "/vectors":
			if r.Header.Get("X-Custom") != "embedding" {
				t.Error(r.Header)
			}
			fmt.Fprint(w, `{"value":2}`)
		case "/draw", "/edit":
			if r.Header.Get("X-Custom") != "image" {
				t.Error(r.Header)
			}
			fmt.Fprint(w, `{"url":"https://example.org/image.png"}`)
		case "/judge":
			if r.Header.Get("X-Custom") != "decision" {
				t.Error(r.Header)
			}
			fmt.Fprint(w, `{}`)
		case "/models":
			if r.URL.Query().Get("cursor") == "start" {
				fmt.Fprint(w, `{"next":"second","value":"one"}`)
			} else {
				fmt.Fprint(w, `{"value":"two"}`)
			}
		default:
			t.Error(r.URL)
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	name := "test-auxiliary"
	RegisterProvider(name, ProviderSpec{
		Name: name, APIBase: s.URL, Formats: []string{"embeddings", "images", "system_one"},
		SelectEmbeddingFormat: func(model, format string) EmbeddingAPIFormat { return customEmbedding{} },
		SelectImageFormat:     func(model, format string) ImageAPIFormat { return customImage{} },
		SelectDecisionFormat:  func(model, format string) DecisionAPIFormat { return customDecision{} },
		ModelsParams: func(previous any) map[string]string {
			if previous == nil {
				return map[string]string{"cursor": "start"}
			}
			if next := str(obj(previous)["next"]); next != "" {
				return map[string]string{"cursor": next}
			}
			return nil
		},
		ParseModels: func(data any) ([]ModelInfo, error) { return []ModelInfo{{ID: str(obj(data)["value"])}}, nil },
	})
	defer func() { registryMu.Lock(); delete(registry, name); registryMu.Unlock() }()
	ctx := context.Background()
	embedding, err := GetEmbeddingModel(name+":e", ProviderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer embedding.Close()
	e, err := embedding.Embed(ctx, "text", nil)
	if err != nil || len(e.Vectors) != 1 || e.Vectors[0][0] != 2 || e.RequestID != "custom-id" {
		t.Fatalf("%+v %v", e, err)
	}
	image, err := GetImageModel(name+":i", ProviderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	im, err := image.Generate(ctx, "draw", nil)
	if err != nil || len(im.ImageParts) != 1 || im.RequestID != "custom-id" {
		t.Fatalf("%+v %v", im, err)
	}
	if _, err := image.Edit(ctx, "edit", []Part{{Type: "image"}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	decision, err := GetDecisionModel(name+":d", ProviderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer decision.Close()
	d, err := decision.Decide(ctx, "state", nil)
	if err != nil || d.Answers["yes"].Noul != 1 || d.RequestID != "custom-id" {
		t.Fatalf("%+v %v", d, err)
	}
	models, err := decision.Provider.ListModels(ctx)
	if err != nil || len(models) != 2 || models[0].ID != "one" || models[1].ID != "two" {
		t.Fatalf("%+v %v", models, err)
	}
}
