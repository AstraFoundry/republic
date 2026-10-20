package republic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

type EmbeddingResponse struct {
	Vectors          [][]float64
	TokenUsage       TokenUsage
	Model, RequestID string
	Headers          http.Header
}

func (r EmbeddingResponse) Vector() ([]float64, error) {
	if len(r.Vectors) == 0 {
		return nil, fmt.Errorf("embedding response has no vectors")
	}
	return r.Vectors[0], nil
}

type ImageResponse struct {
	ImageParts     []Part
	RevisedPrompts []*string
	TokenUsage     TokenUsage
	RequestID      string
	Headers        http.Header
}
type Question struct {
	Kind         string
	Instructions any
	Criteria     any
}

func Noul(instructions any, criteria map[string]any) Question {
	return Question{"noul", instructions, criteria}
}
func Choice(instructions, criteria any) Question { return Question{"choice", instructions, criteria} }
func Score(instructions any, criteria []any) Question {
	return Question{"score", instructions, criteria}
}

type Answer struct {
	Type          string             `json:"type"`
	Noul          float64            `json:"noul,omitempty"`
	Choice        string             `json:"choice,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}
type DecisionResponse struct {
	Answers          map[string]Answer
	TokenUsage       TokenUsage
	Model, RequestID string
	Headers          http.Header
}
type EmbeddingModel struct {
	Provider     *Provider
	Name, Format string
	APIFormat    EmbeddingAPIFormat
}
type ImageModel struct {
	Provider     *Provider
	Name, Format string
	APIFormat    ImageAPIFormat
}
type DecisionModel struct {
	Provider     *Provider
	Name, Format string
	APIFormat    DecisionAPIFormat
}

func (p *Provider) GetEmbeddingModel(name string) (*EmbeddingModel, error) {
	f, err := p.format("embedding")
	if err != nil {
		return nil, err
	}
	m := &EmbeddingModel{Provider: p, Name: name, Format: f}
	if p.spec.SelectEmbeddingFormat != nil {
		m.APIFormat = p.spec.SelectEmbeddingFormat(name, f)
	}
	return m, nil
}
func (p *Provider) GetImageModel(name string) (*ImageModel, error) {
	f, err := p.format("image")
	if err != nil {
		return nil, err
	}
	m := &ImageModel{Provider: p, Name: name, Format: f}
	if p.spec.SelectImageFormat != nil {
		m.APIFormat = p.spec.SelectImageFormat(name, f)
	}
	return m, nil
}
func (p *Provider) GetDecisionModel(name string) (*DecisionModel, error) {
	f, err := p.format("decision")
	if err != nil {
		return nil, err
	}
	m := &DecisionModel{Provider: p, Name: name, Format: f}
	if p.spec.SelectDecisionFormat != nil {
		m.APIFormat = p.spec.SelectDecisionFormat(name, f)
	}
	return m, nil
}
func providerForSpec(spec string, c ProviderConfig) (*Provider, string, error) {
	name, model, err := splitSpec(spec)
	if err != nil {
		return nil, "", err
	}
	p, err := GetProvider(name, c)
	return p, model, err
}
func GetEmbeddingModel(spec string, c ProviderConfig) (*EmbeddingModel, error) {
	p, name, err := providerForSpec(spec, c)
	if err != nil {
		return nil, err
	}
	m, err := p.GetEmbeddingModel(name)
	if err != nil {
		_ = p.Close()
	}
	return m, err
}
func GetImageModel(spec string, c ProviderConfig) (*ImageModel, error) {
	p, name, err := providerForSpec(spec, c)
	if err != nil {
		return nil, err
	}
	m, err := p.GetImageModel(name)
	if err != nil {
		_ = p.Close()
	}
	return m, err
}
func GetDecisionModel(spec string, c ProviderConfig) (*DecisionModel, error) {
	p, name, err := providerForSpec(spec, c)
	if err != nil {
		return nil, err
	}
	m, err := p.GetDecisionModel(name)
	if err != nil {
		_ = p.Close()
	}
	return m, err
}
func (m *EmbeddingModel) Close() error { return m.Provider.Close() }
func (m *ImageModel) Close() error     { return m.Provider.Close() }
func (m *DecisionModel) Close() error  { return m.Provider.Close() }
func (p *Provider) postJSON(ctx context.Context, format string, req HTTPRequest, ob *observation) (map[string]any, http.Header, error) {
	h := (WireFormat{Format: format}).Headers()
	return p.postJSONHeaders(ctx, format, req, h, ob)
}
func (p *Provider) postJSONHeaders(ctx context.Context, format string, req HTTPRequest, h http.Header, ob *observation) (map[string]any, http.Header, error) {
	res, err := p.request(ctx, "POST", format, req, h, ob, false)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, res.Header, connectionError(err, res.Header)
	}
	var v map[string]any
	if err = json.Unmarshal(data, &v); err != nil {
		return nil, res.Header, withHeaders(err, res.Header)
	}
	if err = errorPayload(v); err != nil {
		return nil, res.Header, withHeaders(err, res.Header)
	}
	return v, res.Header, nil
}
func (m *EmbeddingModel) Embed(ctx context.Context, text string, dimensions *int) (EmbeddingResponse, error) {
	return m.EmbedMany(ctx, []string{text}, dimensions)
}
func (m *EmbeddingModel) EmbedMany(ctx context.Context, texts []string, dimensions *int) (r EmbeddingResponse, err error) {
	ob := observe(m.Provider, "embed", m.Name)
	defer func() { ob.finish(err) }()
	if f := m.APIFormat; f != nil {
		req, err := f.Build(m.Name, texts, dimensions)
		if err != nil {
			return r, err
		}
		v, h, err := m.Provider.postJSONHeaders(ctx, f.Name(), req, f.Headers(), ob)
		if err != nil {
			return r, err
		}
		r, err = f.Parse(v)
		if err != nil {
			return r, withHeaders(err, h)
		}
		r.Headers = h.Clone()
		r.RequestID = requestID(h)
		ob.response = r
		return r, nil
	}
	req := HTTPRequest{Path: "/embeddings", Body: map[string]any{"model": m.Name, "input": texts, "encoding_format": "float"}}
	if m.Format == "embed_content" {
		requests := []any{}
		for _, text := range texts {
			item := map[string]any{"model": "models/" + m.Name, "content": map[string]any{"parts": []any{map[string]any{"text": text}}}}
			if dimensions != nil {
				item["outputDimensionality"] = *dimensions
			}
			requests = append(requests, item)
		}
		req = HTTPRequest{Path: "/models/" + m.Name + ":batchEmbedContents", Body: map[string]any{"requests": requests}}
	} else if dimensions != nil {
		key := "dimensions"
		if m.Provider.Name == "mistral" {
			key = "output_dimension"
		}
		req.Body[key] = *dimensions
	}
	v, h, err := m.Provider.postJSON(ctx, m.Format, req, ob)
	if err != nil {
		return r, err
	}
	r.Headers = h.Clone()
	r.RequestID = requestID(h)
	r.Vectors = [][]float64{}
	items, ok := v["data"].([]any)
	field := "embedding"
	if m.Format == "embed_content" {
		items, ok = v["embeddings"].([]any)
		field = "values"
	} else {
		sort.SliceStable(items, func(i, j int) bool { return num(obj(items[i])["index"]) < num(obj(items[j])["index"]) })
		r.Model = str(v["model"])
		r.TokenUsage.InputTokens = num(obj(v["usage"])["prompt_tokens"])
	}
	if !ok {
		return r, withHeaders(failure(ErrAPIResponse, "embedding response has no vector list"), h)
	}
	for _, item := range items {
		raw, ok := obj(item)[field].([]any)
		if !ok {
			return r, withHeaders(failure(ErrAPIResponse, "embedding vector is not an array"), h)
		}
		vector := make([]float64, len(raw))
		for i, x := range raw {
			n, ok := x.(float64)
			if !ok {
				return r, withHeaders(failure(ErrAPIResponse, "embedding coordinate is not a number"), h)
			}
			vector[i] = n
		}
		r.Vectors = append(r.Vectors, vector)
	}
	ob.response = r
	return r, nil
}
func (m *ImageModel) Generate(ctx context.Context, prompt string, options Options) (ImageResponse, error) {
	return m.images(ctx, prompt, nil, nil, options, false)
}
func (m *ImageModel) Edit(ctx context.Context, prompt string, images []Part, mask *Part, options Options) (ImageResponse, error) {
	return m.images(ctx, prompt, images, mask, options, true)
}
func (m *ImageModel) images(ctx context.Context, prompt string, images []Part, mask *Part, o Options, edit bool) (r ImageResponse, err error) {
	operation := "generate"
	if edit {
		operation = "edit"
	}
	ob := observe(m.Provider, operation, m.Name)
	defer func() { ob.finish(err) }()
	if edit && len(images) == 0 {
		return r, fmt.Errorf("pass at least one image to edit")
	}
	if f := m.APIFormat; f != nil {
		var req HTTPRequest
		if edit {
			req, err = f.Edit(m.Name, prompt, images, mask, o)
		} else {
			req, err = f.Generate(m.Name, prompt, o)
		}
		if err != nil {
			return r, err
		}
		v, h, err := m.Provider.postJSONHeaders(ctx, f.Name(), req, f.Headers(), ob)
		if err != nil {
			return r, err
		}
		r, err = f.Parse(v)
		if err != nil {
			return r, withHeaders(err, h)
		}
		r.Headers = h.Clone()
		r.RequestID = requestID(h)
		ob.response = r
		return r, nil
	}
	req, err := m.imageRequest(prompt, images, mask, o, edit)
	if err != nil {
		return r, err
	}
	v, h, err := m.Provider.postJSON(ctx, m.Format, req, ob)
	if err != nil {
		return r, err
	}
	r, err = parseImages(m.Format, v)
	if err != nil {
		return r, withHeaders(err, h)
	}
	r.Headers = h.Clone()
	r.RequestID = requestID(h)
	ob.response = r
	return r, nil
}
func (m *ImageModel) imageRequest(prompt string, images []Part, mask *Part, o Options, edit bool) (HTTPRequest, error) {
	body := map[string]any{"model": m.Name, "prompt": prompt}
	path := "/images/generations"
	files := []FilePart{}
	if m.Format == "gemini_image" {
		if mask != nil {
			return HTTPRequest{}, failure(ErrUnsupportedFeature, "the gemini_image API format does not support masks")
		}
		if err := reject(m.Format, o, "quality", "background", "output_format", "output_compression", "moderation"); err != nil {
			return HTTPRequest{}, err
		}
		parts := []any{map[string]any{"text": prompt}}
		for _, image := range images {
			v, err := (WireFormat{Format: "gemini"}).userPart(image)
			if err != nil {
				return HTTPRequest{}, err
			}
			parts = append(parts, v)
		}
		config := map[string]any{"responseModalities": []string{"IMAGE"}}
		imageConfig := renamed(o, map[string]string{"aspect_ratio": "aspectRatio", "size": "imageSize"})
		if len(imageConfig) > 0 {
			config["imageConfig"] = imageConfig
		}
		if n, ok := o["n"]; ok {
			config["candidateCount"] = n
		}
		body = map[string]any{"contents": []any{map[string]any{"role": "user", "parts": parts}}, "generationConfig": config}
		path = "/models/" + m.Name + ":generateContent"
	} else {
		if err := reject(m.Format, o, "aspect_ratio"); err != nil {
			return HTTPRequest{}, err
		}
		for _, key := range []string{"n", "size", "quality", "background", "output_format", "output_compression", "moderation"} {
			if v, ok := o[key]; ok {
				body[key] = v
			}
		}
		if edit {
			path = "/images/edits"
			if m.Provider.Name == "azure-openai" {
				upload := func(field, stem string, image Part) error {
					if image.Data == nil {
						return failure(ErrUnsupportedFeature, "multipart image edits need inline image data, not a URL: %s", image.URL)
					}
					ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif"}[image.MediaType]
					files = append(files, FilePart{Field: field, Filename: stem + ext, MediaType: image.MediaType, Data: image.Data})
					return nil
				}
				for i, image := range images {
					if err := upload("image[]", fmt.Sprintf("image-%d", i), image); err != nil {
						return HTTPRequest{}, err
					}
				}
				if mask != nil {
					if err := upload("mask", "mask", *mask); err != nil {
						return HTTPRequest{}, err
					}
				}
			} else {
				items := []any{}
				for _, image := range images {
					u, err := image.DataURL()
					if err != nil {
						return HTTPRequest{}, err
					}
					items = append(items, map[string]any{"image_url": u})
				}
				body["images"] = items
				if mask != nil {
					u, err := mask.DataURL()
					if err != nil {
						return HTTPRequest{}, err
					}
					body["mask"] = map[string]any{"image_url": u}
				}
			}
		}
	}
	body = DeepMerge(body, obj(o["extra_body"]))
	if len(files) > 0 {
		for key, v := range body {
			if _, ok := v.(string); !ok {
				encoded, err := json.Marshal(v)
				if err != nil {
					return HTTPRequest{}, err
				}
				body[key] = string(encoded)
			}
		}
	}
	return HTTPRequest{Path: path, Body: body, Files: files}, nil
}
func parseImages(format string, v map[string]any) (ImageResponse, error) {
	r := ImageResponse{ImageParts: []Part{}, RevisedPrompts: []*string{}}
	usage := obj(v["usage"])
	if format != "gemini_image" {
		items, ok := v["data"].([]any)
		if !ok {
			return r, failure(ErrAPIResponse, "image response has no data array")
		}
		media := "image/" + str(first(v["output_format"], "png"))
		for _, x := range items {
			item := obj(x)
			p := Part{Type: "image", MediaType: media}
			if nonempty(item["b64_json"]) {
				b, err := base64.StdEncoding.DecodeString(str(item["b64_json"]))
				if err != nil {
					return r, err
				}
				p.Data = b
			} else {
				p.URL = str(item["url"])
				if p.URL == "" {
					return r, failure(ErrAPIResponse, "image has no data or URL")
				}
			}
			r.ImageParts = append(r.ImageParts, p)
			var prompt *string
			if s, ok := item["revised_prompt"].(string); ok {
				prompt = &s
			}
			r.RevisedPrompts = append(r.RevisedPrompts, prompt)
		}
		r.TokenUsage = TokenUsage{InputTokens: num(usage["input_tokens"]), OutputTokens: num(usage["output_tokens"])}
		return r, nil
	}
	candidates := arr(v["candidates"])
	if len(candidates) == 0 && nonempty(obj(v["promptFeedback"])["blockReason"]) {
		return r, failure(ErrAPIResponse, "Prompt blocked: %s", obj(v["promptFeedback"])["blockReason"])
	}
	text := ""
	reasons := []any{}
	for _, x := range candidates {
		c := obj(x)
		if nonempty(c["finishReason"]) {
			reasons = append(reasons, c["finishReason"])
		}
		for _, y := range arr(obj(c["content"])["parts"]) {
			p := obj(y)
			if p["thought"] == true {
				continue
			}
			inline := obj(p["inlineData"])
			if strings.HasPrefix(str(inline["mimeType"]), "image/") {
				b, err := base64.StdEncoding.DecodeString(str(inline["data"]))
				if err != nil {
					return r, err
				}
				r.ImageParts = append(r.ImageParts, Part{Type: "image", MediaType: str(inline["mimeType"]), Data: b})
			} else {
				text += str(p["text"])
			}
		}
	}
	if len(r.ImageParts) == 0 {
		return r, failure(ErrAPIResponse, "%s", jsonText(map[string]any{"finish_reasons": reasons, "text": text}))
	}
	usage = obj(v["usageMetadata"])
	r.TokenUsage = TokenUsage{InputTokens: num(usage["promptTokenCount"]), OutputTokens: num(usage["candidatesTokenCount"]) + num(usage["thoughtsTokenCount"]), ReasoningTokens: num(usage["thoughtsTokenCount"]), CachedTokens: num(usage["cachedContentTokenCount"])}
	return r, nil
}
func (m *DecisionModel) Decide(ctx context.Context, state any, questions map[string]Question) (r DecisionResponse, err error) {
	ob := observe(m.Provider, "decide", m.Name)
	defer func() { ob.finish(err) }()
	if f := m.APIFormat; f != nil {
		req, err := f.Build(m.Name, state, questions)
		if err != nil {
			return r, err
		}
		v, h, err := m.Provider.postJSONHeaders(ctx, f.Name(), req, f.Headers(), ob)
		if err != nil {
			return r, err
		}
		r, err = f.Parse(v)
		if err != nil {
			return r, withHeaders(err, h)
		}
		r.Headers = h.Clone()
		r.RequestID = requestID(h)
		ob.response = r
		return r, nil
	}
	qs := map[string]any{}
	for id, q := range questions {
		if q.Kind != "noul" && q.Kind != "choice" && q.Kind != "score" {
			return r, fmt.Errorf("unknown question kind %q", q.Kind)
		}
		item := map[string]any{"type": q.Kind, "instructions": q.Instructions}
		criteria := q.Criteria
		if q.Kind == "choice" {
			if choices, ok := criteria.([]string); ok {
				c := map[string]any{}
				for _, name := range choices {
					c[name] = nil
				}
				criteria = c
			}
		}
		if q.Kind != "noul" || criteria != nil && !(reflectNilMap(criteria)) {
			item["criteria"] = criteria
		}
		qs[id] = item
	}
	v, h, err := m.Provider.postJSON(ctx, m.Format, HTTPRequest{Path: "/systemone", Body: map[string]any{"model": m.Name, "state": state, "questions": qs}}, ob)
	if err != nil {
		return r, err
	}
	answers, ok := v["answers"].(map[string]any)
	if !ok {
		return r, withHeaders(failure(ErrAPIResponse, "decision response has no answers object"), h)
	}
	r.Answers = map[string]Answer{}
	for id, value := range answers {
		a := obj(value)
		kind := str(a["type"])
		required := []string{}
		switch kind {
		case "noul":
			required = []string{"noul"}
		case "choice":
			required = []string{"choice", "probabilities", "confidence"}
		case "score":
			required = []string{"score", "probabilities", "confidence"}
		default:
			return r, withHeaders(failure(ErrAPIResponse, "unknown answer type %q", kind), h)
		}
		for _, key := range required {
			if _, ok := a[key]; !ok {
				return r, withHeaders(failure(ErrAPIResponse, "answer %q is missing %s", id, key), h)
			}
		}
		var answer Answer
		if err := json.Unmarshal([]byte(jsonText(a)), &answer); err != nil {
			return r, withHeaders(err, h)
		}
		r.Answers[id] = answer
	}
	u := obj(v["usage"])
	r.TokenUsage = TokenUsage{InputTokens: num(u["input_tokens"]), OutputTokens: num(u["output_tokens"])}
	r.Model = str(v["model"])
	r.Headers = h.Clone()
	r.RequestID = requestID(h)
	ob.response = r
	return r, nil
}
func reflectNilMap(v any) bool { m, ok := v.(map[string]any); return ok && m == nil }

type ModelInfo struct {
	ID, DisplayName string
	Raw             map[string]any
}

func (p *Provider) ListModels(ctx context.Context) (models []ModelInfo, err error) {
	ob := observe(p, "list_models", "")
	defer func() { ob.finish(err) }()
	path := p.spec.ModelsPath
	if path == "-" {
		return nil, failure(ErrUnsupportedFeature, "provider %q cannot list models", p.Name)
	}
	if path == "" {
		path = "/models"
	}
	format := p.config.APIFormat
	if format == "" {
		format = p.spec.Formats[0]
	}
	params := map[string]string{}
	if p.Name == "google" {
		params["pageSize"] = "1000"
	}
	if p.Name == "anthropic" {
		params["limit"] = "1000"
	}
	if p.spec.ModelsParams != nil {
		params = p.spec.ModelsParams(nil)
	}
	models = []ModelInfo{}
	seenPages := map[string]bool{}
	for params != nil {
		res, err := p.request(ctx, "GET", format, HTTPRequest{Path: path, Params: params}, (WireFormat{Format: format}).Headers(), ob, false)
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil {
			return nil, connectionError(err, res.Header)
		}
		var data any
		if err := json.Unmarshal(b, &data); err != nil {
			return nil, withHeaders(err, res.Header)
		}
		v := obj(data)
		items := arr(data)
		if p.spec.ParseModels != nil {
			page, err := p.spec.ParseModels(data)
			if err != nil {
				return nil, withHeaders(err, res.Header)
			}
			models = append(models, page...)
			items = []any{}
		}
		if items == nil {
			items = arr(first(v["data"], v["models"]))
		}
		for _, x := range items {
			item := obj(x)
			id := str(first(item["id"], item["name"]))
			display := str(first(item["display_name"], item["displayName"], item["name"]))
			if p.Name == "google" {
				id = strings.TrimPrefix(str(item["name"]), "models/")
				display = str(item["displayName"])
			} else if display == id {
				display = ""
			}
			models = append(models, ModelInfo{ID: id, DisplayName: display, Raw: cloneMap(item)})
		}
		if p.spec.ModelsParams != nil {
			params = p.spec.ModelsParams(data)
			if params != nil {
				pageKey := jsonText(params)
				if seenPages[pageKey] {
					return nil, withHeaders(failure(ErrAPIResponse, "model pagination repeated parameters"), res.Header)
				}
				seenPages[pageKey] = true
			}
			continue
		}
		token, key := "", ""
		if p.Name == "google" {
			token = str(v["nextPageToken"])
			key = "pageToken"
		} else if p.Name == "anthropic" && v["has_more"] == true {
			token = str(v["last_id"])
			key = "after_id"
		}
		if token == "" {
			break
		}
		if seenPages[token] {
			return nil, withHeaders(failure(ErrAPIResponse, "model pagination repeated cursor %q", token), res.Header)
		}
		seenPages[token] = true
		params[key] = token
	}
	ob.response = models
	return models, nil
}
