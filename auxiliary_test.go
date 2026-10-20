package republic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func auxiliaryProvider(t *testing.T, name string, h http.HandlerFunc) *Provider {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	p, err := GetProvider(name, ProviderConfig{APIBase: s.URL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}
func readRequest(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var b map[string]any
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		t.Fatal(err)
	}
	return b
}
func TestEmbeddings(t *testing.T) {
	for _, provider := range []string{"openai", "mistral", "google"} {
		t.Run(provider, func(t *testing.T) {
			p := auxiliaryProvider(t, provider, func(w http.ResponseWriter, r *http.Request) {
				b := readRequest(t, r)
				w.Header().Set("x-request-id", "embedding")
				switch provider {
				case "google":
					if r.URL.Path != "/models/embed:batchEmbedContents" {
						t.Error(r.URL)
					}
					requests := arr(b["requests"])
					if len(requests) != 2 || obj(requests[0])["outputDimensionality"] != float64(2) {
						t.Error(b)
					}
					fmt.Fprint(w, `{"embeddings":[{"values":[1,2]},{"values":[3,4]}]}`)
				default:
					key := "dimensions"
					if provider == "mistral" {
						key = "output_dimension"
					}
					if b[key] != float64(2) || b["encoding_format"] != "float" {
						t.Error(b)
					}
					fmt.Fprint(w, `{"data":[{"index":1,"embedding":[3,4]},{"index":0,"embedding":[1,2]}],"usage":{"prompt_tokens":7},"model":"embed"}`)
				}
			})
			m, err := p.GetEmbeddingModel("embed")
			if err != nil {
				t.Fatal(err)
			}
			r, err := m.EmbedMany(context.Background(), []string{"a", "b"}, ptr(2))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(r.Vectors, [][]float64{{1, 2}, {3, 4}}) || r.RequestID != "embedding" {
				t.Fatal(r)
			}
			v, err := r.Vector()
			if err != nil || !reflect.DeepEqual(v, []float64{1, 2}) {
				t.Fatalf("%v %v", v, err)
			}
			if provider != "google" && r.TokenUsage.InputTokens != 7 {
				t.Fatal(r)
			}
		})
	}
}
func TestImagesJSONAndMultipart(t *testing.T) {
	image := Part{Type: "image", MediaType: "image/png", Data: []byte("png")}
	for _, provider := range []string{"openai", "azure-openai", "google"} {
		t.Run(provider, func(t *testing.T) {
			p := auxiliaryProvider(t, provider, func(w http.ResponseWriter, r *http.Request) {
				if provider == "azure-openai" {
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Fatal(err)
					}
					defer r.MultipartForm.RemoveAll()
					if r.FormValue("model") != "image-model" || r.FormValue("n") != "1" || r.FormValue("custom") != `{"a":true}` {
						t.Error(r.MultipartForm.Value)
					}
					files := r.MultipartForm.File["image[]"]
					if len(files) != 1 || files[0].Filename != "image-0.png" || len(r.MultipartForm.File["mask"]) != 1 {
						t.Error(r.MultipartForm.File)
					}
				} else {
					b := readRequest(t, r)
					if provider == "google" {
						if len(arr(obj(arr(b["contents"])[0])["parts"])) != 2 {
							t.Error(b)
						}
					} else {
						if obj(arr(b["images"])[0])["image_url"] != "data:image/png;base64,cG5n" {
							t.Error(b)
						}
					}
				}
				if provider == "google" {
					fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"thought":true,"inlineData":{"mimeType":"image/png","data":"aGk="}},{"inlineData":{"mimeType":"image/png","data":"cG5n"}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"thoughtsTokenCount":1}}`)
				} else {
					fmt.Fprint(w, `{"data":[{"b64_json":"cG5n","revised_prompt":"better"}],"usage":{"input_tokens":3,"output_tokens":2}}`)
				}
			})
			m, err := p.GetImageModel("image-model")
			if err != nil {
				t.Fatal(err)
			}
			var mask *Part
			if provider == "azure-openai" {
				mask = &image
			}
			r, err := m.Edit(context.Background(), "edit", []Part{image}, mask, Options{"n": 1, "extra_body": map[string]any{"custom": map[string]any{"a": true}}})
			if err != nil {
				t.Fatal(err)
			}
			if len(r.ImageParts) != 1 || string(r.ImageParts[0].Data) != "png" || r.TokenUsage.InputTokens != 3 {
				t.Fatal(r)
			}
		})
	}
}
func TestImageErrors(t *testing.T) {
	p, err := GetProvider("google", ProviderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	m, _ := p.GetImageModel("x")
	if _, err := m.imageRequest("x", nil, &Part{}, nil, true); err == nil {
		t.Fatal("accepted mask")
	}
	if _, err := m.imageRequest("x", nil, nil, Options{"quality": "high"}, false); err == nil {
		t.Fatal("accepted quality")
	}
	for _, payload := range []map[string]any{{"promptFeedback": map[string]any{"blockReason": "SAFETY"}}, {"candidates": []any{map[string]any{"finishReason": "SAFETY", "content": map[string]any{"parts": []any{map[string]any{"text": "blocked"}}}}}}} {
		if _, err := parseImages("gemini_image", payload); err == nil {
			t.Fatal("accepted no-image response")
		}
	}
	if _, err := m.Edit(context.Background(), "x", nil, nil, nil); err == nil {
		t.Fatal("accepted empty edit")
	}
}
func TestDecisions(t *testing.T) {
	p := auxiliaryProvider(t, "typesafe", func(w http.ResponseWriter, r *http.Request) {
		b := readRequest(t, r)
		qs := obj(b["questions"])
		if r.URL.Path != "/systemone" || obj(qs["yes"])["criteria"] != nil || !reflect.DeepEqual(obj(obj(qs["pick"])["criteria"]), map[string]any{"a": nil, "b": nil}) {
			t.Error(b)
		}
		fmt.Fprint(w, `{"answers":{"yes":{"type":"noul","noul":0.9},"pick":{"type":"choice","choice":"a","probabilities":{"a":0.8,"b":0.2},"confidence":0.8},"score":{"type":"score","score":1.2,"probabilities":{"0":0.1,"1":0.6,"2":0.3},"confidence":0.6,"legend":{"0":"bad"}}},"usage":{"input_tokens":10,"output_tokens":4},"model":"jev"}`)
	})
	m, err := p.GetDecisionModel("jev")
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.Decide(context.Background(), map[string]any{"text": "good"}, map[string]Question{"yes": Noul("ok?", nil), "pick": Choice("pick", []string{"a", "b"}), "score": Score("rate", []any{"bad", "ok", "great"})})
	if err != nil {
		t.Fatal(err)
	}
	if r.Answers["yes"].Noul != 0.9 || r.Answers["pick"].Choice != "a" || r.Answers["score"].Score != 1.2 || r.TokenUsage.TotalTokens() != 14 {
		t.Fatal(r)
	}
}
func TestModelPagination(t *testing.T) {
	for _, provider := range []string{"google", "anthropic", "openai"} {
		t.Run(provider, func(t *testing.T) {
			pages := 0
			p := auxiliaryProvider(t, provider, func(w http.ResponseWriter, r *http.Request) {
				pages++
				if r.Method != "GET" || r.URL.Path != "/models" {
					t.Error(r.URL)
				}
				switch provider {
				case "google":
					if r.URL.Query().Get("pageSize") != "1000" {
						t.Error(r.URL)
					}
					if pages == 1 {
						fmt.Fprint(w, `{"models":[{"name":"models/a","displayName":"A"}],"nextPageToken":"next"}`)
					} else {
						if r.URL.Query().Get("pageToken") != "next" {
							t.Error(r.URL)
						}
						fmt.Fprint(w, `{"models":[{"name":"models/b"}]}`)
					}
				case "anthropic":
					if r.URL.Query().Get("limit") != "1000" {
						t.Error(r.URL)
					}
					if pages == 1 {
						fmt.Fprint(w, `{"data":[{"id":"a","display_name":"A"}],"has_more":true,"last_id":"a"}`)
					} else {
						if r.URL.Query().Get("after_id") != "a" {
							t.Error(r.URL)
						}
						fmt.Fprint(w, `{"data":[{"id":"b"}],"has_more":false}`)
					}
				default:
					fmt.Fprint(w, `{"data":[{"id":"a","name":"A"},{"id":"b","name":"b"}]}`)
				}
			})
			models, err := p.ListModels(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(models) != 2 || models[0].ID != "a" || models[0].DisplayName != "A" || models[1].ID != "b" || models[1].DisplayName != "" {
				t.Fatal(models)
			}
			if provider != "openai" && pages != 2 {
				t.Fatal(pages)
			}
		})
	}
}
func TestModelKinds(t *testing.T) {
	for _, spec := range []string{"typesafe:jev", "openai:embed", "google:image"} {
		p, name, err := providerForSpec(spec, ProviderConfig{})
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		if strings.HasPrefix(spec, "typesafe") {
			if _, err := p.GetModel(name); err == nil {
				t.Fatal("typesafe accepted chat")
			}
		}
	}
}
