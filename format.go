package republic

import (
	"fmt"
	"net/http"
	"strings"
)

// Options uses Republic's portable option names.
// Presence matters: false and zero are sent when explicitly supplied.
type Options map[string]any
type OutputSchema struct {
	Name   string
	Schema map[string]any
}
type ChatRequest struct {
	Model        string
	Messages     []Message
	Options      Options
	OutputSchema *OutputSchema
}
type FilePart struct {
	Field, Filename, MediaType string
	Data                       []byte
}
type HTTPRequest struct {
	Path   string
	Body   map[string]any
	Params map[string]string
	Files  []FilePart
}

// ChatFormat is implemented by custom wire protocols. Methods must be safe for
// concurrent calls; NewParser returns fresh state for each stream.
type ChatFormat interface {
	Name() string
	Headers() http.Header
	Build(ChatRequest, bool) (HTTPRequest, error)
	Parse([]byte) ([]Delta, error)
	NewParser() StreamParser
}
type WireFormat struct {
	Format  string
	Dialect string
}

func (f WireFormat) Name() string { return f.Format }
func (f WireFormat) Headers() http.Header {
	h := http.Header{}
	if f.Format == "messages" {
		h.Set("anthropic-version", "2023-06-01")
	}
	return h
}
func (f WireFormat) Parse(b []byte) ([]Delta, error) {
	p, err := newParser(f.Format, f.Dialect)
	if err != nil {
		return nil, err
	}
	return p.parse(b)
}
func (f WireFormat) NewParser() StreamParser { p, _ := newParser(f.Format, f.Dialect); return p }

type UserLocation struct {
	City     string `json:"city"`
	Region   string `json:"region"`
	Country  string `json:"country"`
	Timezone string `json:"timezone"`
}

func (l UserLocation) fields() map[string]any {
	m := map[string]any{}
	for k, v := range map[string]string{"city": l.City, "region": l.Region, "country": l.Country, "timezone": l.Timezone} {
		if v != "" {
			m[k] = v
		}
	}
	return m
}

type BuiltinTool struct {
	Kind           string         `json:"kind"`
	MaxUses        *int           `json:"max_uses"`
	AllowedDomains []string       `json:"allowed_domains"`
	BlockedDomains []string       `json:"blocked_domains"`
	UserLocation   *UserLocation  `json:"user_location"`
	APIFormat      string         `json:"api_format"`
	Definition     map[string]any `json:"definition"`
}

func WebSearch() BuiltinTool       { return BuiltinTool{Kind: "web_search"} }
func WebFetch() BuiltinTool        { return BuiltinTool{Kind: "web_fetch"} }
func CodeExecution() BuiltinTool   { return BuiltinTool{Kind: "code_execution"} }
func ImageGeneration() BuiltinTool { return BuiltinTool{Kind: "image_generation"} }
func NativeTool(format string, definition map[string]any) BuiltinTool {
	return BuiltinTool{Kind: "native", APIFormat: format, Definition: definition}
}
func splitTools(options Options) ([]Tool, []BuiltinTool, error) {
	functions := []Tool{}
	builtins := []BuiltinTool{}
	var values []any
	switch ts := options["tools"].(type) {
	case nil:
	case []any:
		values = ts
	case []Tool:
		for _, t := range ts {
			values = append(values, t)
		}
	case []BuiltinTool:
		for _, t := range ts {
			values = append(values, t)
		}
	default:
		return nil, nil, fmt.Errorf("tools must be []Tool, []BuiltinTool or []any")
	}
	for _, t := range values {
		switch v := t.(type) {
		case Tool:
			functions = append(functions, v)
		case BuiltinTool:
			builtins = append(builtins, v)
		default:
			return nil, nil, fmt.Errorf("invalid tool type %T", t)
		}
	}
	return functions, builtins, nil
}
func reject(format string, o Options, names ...string) error {
	for _, n := range names {
		if _, ok := o[n]; ok {
			return failure(ErrUnsupportedFeature, "the %q API format does not support %s", format, n)
		}
	}
	return nil
}
func renamed(o Options, names map[string]string) map[string]any {
	m := map[string]any{}
	for from, to := range names {
		if v, ok := o[from]; ok {
			m[to] = v
		}
	}
	return m
}
func allText(m Message) bool {
	for _, p := range m.Parts {
		if p.Type != "text" {
			return false
		}
	}
	return true
}
func payloads(m Message, format string) []any {
	a := []any{}
	for _, p := range m.Parts {
		if p.Type == "provider_data" && p.APIFormat == format {
			a = append(a, cloneMap(p.Payload))
		}
	}
	return a
}
func unsupportedMedia(format, kind string) error {
	return failure(ErrUnsupportedFeature, "the %q API format does not accept %s input", format, kind)
}
func (f WireFormat) userPart(p Part) (map[string]any, error) {
	switch f.Format {
	case "chat":
		switch p.Type {
		case "text":
			return map[string]any{"type": "text", "text": p.Text}, nil
		case "image", "video":
			u, err := p.DataURL()
			return map[string]any{"type": p.Type + "_url", p.Type + "_url": map[string]any{"url": u}}, err
		case "audio":
			if p.Data == nil {
				return nil, unsupportedMedia(f.Format, "remote audio")
			}
			formats := map[string]string{"audio/wav": "wav", "audio/x-wav": "wav", "audio/mpeg": "mp3"}
			if f.Dialect == "openrouter" {
				for mime, name := range map[string]string{"audio/aiff": "aiff", "audio/x-aiff": "aiff", "audio/aac": "aac", "audio/ogg": "ogg", "audio/flac": "flac", "audio/x-flac": "flac", "audio/mp4": "m4a", "audio/x-m4a": "m4a"} {
					formats[mime] = name
				}
			}
			ext := formats[p.MediaType]
			if ext == "" {
				return nil, unsupportedMedia(f.Format, p.MediaType)
			}
			b, err := p.Base64Data()
			return map[string]any{"type": "input_audio", "input_audio": map[string]any{"data": b, "format": ext}}, err
		}
	case "responses":
		if p.Type == "text" {
			return map[string]any{"type": "input_text", "text": p.Text}, nil
		}
		if p.Type == "image" {
			u, err := p.DataURL()
			return map[string]any{"type": "input_image", "image_url": u}, err
		}
	case "messages":
		if p.Type == "text" {
			return map[string]any{"type": "text", "text": p.Text}, nil
		}
		if p.Type == "image" {
			source := map[string]any{"type": "url", "url": p.URL}
			if p.URL == "" {
				b, err := p.Base64Data()
				if err != nil {
					return nil, err
				}
				source = map[string]any{"type": "base64", "media_type": p.MediaType, "data": b}
			}
			return map[string]any{"type": "image", "source": source}, nil
		}
	case "gemini":
		if p.Type == "text" {
			return map[string]any{"text": p.Text}, nil
		}
		if p.Type == "image" || p.Type == "audio" || p.Type == "video" {
			media := p.MediaType
			switch media {
			case "audio/x-wav":
				media = "audio/wav"
			case "audio/x-aiff":
				media = "audio/aiff"
			}
			if p.URL != "" {
				return map[string]any{"fileData": map[string]any{"mimeType": media, "fileUri": p.URL}}, nil
			}
			b, err := p.Base64Data()
			return map[string]any{"inlineData": map[string]any{"mimeType": media, "data": b}}, err
		}
	}
	return nil, unsupportedMedia(f.Format, p.Type)
}
func (f WireFormat) parts(m Message) ([]any, error) {
	a := []any{}
	for _, p := range m.Parts {
		if f.Format == "chat" && (p.Type == "reasoning" || p.Type == "provider_data") {
			continue
		}
		v, err := f.userPart(p)
		if err != nil {
			return nil, err
		}
		a = append(a, v)
	}
	return a, nil
}
func (f WireFormat) textOrParts(m Message) (any, error) {
	if allText(m) {
		return m.Text(), nil
	}
	return f.parts(m)
}
func toolUse(c ToolCall) (map[string]any, error) {
	args, err := c.Args()
	return map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": args}, err
}
func assistantBlocks(m Message) ([]any, error) {
	items := payloads(m, "messages")
	markedText := ""
	markedCalls := map[string]bool{}
	calls := map[string]ToolCall{}
	for _, c := range m.ToolCalls {
		calls[c.ID] = c
	}
	valid := true
	for _, x := range items {
		v := obj(x)
		if v["type"] == "text" {
			markedText += str(v["text"])
		}
		if v["type"] == "tool_use" {
			id := str(v["id"])
			markedCalls[id] = true
			if _, ok := calls[id]; !ok {
				valid = false
			}
		}
	}
	if !strings.HasPrefix(m.Text(), markedText) {
		valid = false
	}
	if !valid {
		filtered := []any{}
		for _, x := range items {
			v := obj(x)
			if v["type"] != "text" && v["type"] != "tool_use" {
				filtered = append(filtered, x)
			}
		}
		items = filtered
		markedText = ""
		markedCalls = map[string]bool{}
	}
	out := []any{}
	for _, x := range items {
		v := obj(x)
		if v["type"] == "tool_use" {
			b, err := toolUse(calls[str(v["id"])])
			if err != nil {
				return nil, err
			}
			out = append(out, b)
		} else {
			out = append(out, x)
		}
	}
	if rest := strings.TrimPrefix(m.Text(), markedText); rest != "" {
		out = append(out, map[string]any{"type": "text", "text": rest})
	}
	for _, c := range m.ToolCalls {
		if !markedCalls[c.ID] {
			b, err := toolUse(c)
			if err != nil {
				return nil, err
			}
			out = append(out, b)
		}
	}
	return out, nil
}
func (f WireFormat) entries(m Message) ([]any, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	switch f.Format {
	case "chat":
		e := map[string]any{"role": m.Role}
		switch m.Role {
		case "system":
			e["content"] = m.Text()
		case "assistant":
			e["content"] = nil
			if m.Text() != "" {
				e["content"] = m.Text()
			}
			if len(m.ToolCalls) > 0 {
				a := []any{}
				for _, c := range m.ToolCalls {
					a = append(a, map[string]any{"id": c.ID, "type": "function", "function": map[string]any{"name": c.Name, "arguments": c.Arguments}})
				}
				e["tool_calls"] = a
			}
			if m.Reasoning() != "" {
				switch f.Dialect {
				case "deepseek", "moonshot", "zai", "minimax":
					e["reasoning_content"] = m.Reasoning()
				case "mistral":
					a := []any{map[string]any{"type": "thinking", "thinking": []any{map[string]any{"type": "text", "text": m.Reasoning()}}}}
					if m.Text() != "" {
						a = append(a, map[string]any{"type": "text", "text": m.Text()})
					}
					e["content"] = a
				}
			}
		case "user", "tool":
			content, err := f.textOrParts(m)
			if err != nil {
				return nil, err
			}
			e["content"] = content
			if m.Role == "tool" {
				e["tool_call_id"] = m.ToolCall.ID
			}
		default:
			return nil, fmt.Errorf("invalid message role %q", m.Role)
		}
		return []any{e}, nil
	case "responses":
		switch m.Role {
		case "system":
			return []any{map[string]any{"role": "system", "content": m.Text()}}, nil
		case "user":
			a, err := f.parts(m)
			return []any{map[string]any{"role": "user", "content": a}}, err
		case "assistant":
			a := payloads(m, "responses")
			if m.Text() != "" {
				a = append(a, map[string]any{"role": "assistant", "content": m.Text()})
			}
			for _, c := range m.ToolCalls {
				a = append(a, map[string]any{"type": "function_call", "call_id": c.ID, "name": c.Name, "arguments": c.Arguments})
			}
			return a, nil
		case "tool":
			content, err := f.textOrParts(m)
			return []any{map[string]any{"type": "function_call_output", "call_id": m.ToolCall.ID, "output": content}}, err
		}
	case "messages":
		if m.Role == "tool" {
			content, err := f.textOrParts(m)
			return []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": m.ToolCall.ID, "content": content, "is_error": m.IsError}}}}, err
		}
		if m.Role == "user" {
			content, err := f.parts(m)
			return []any{map[string]any{"role": "user", "content": content}}, err
		}
		content, err := assistantBlocks(m)
		return []any{map[string]any{"role": "assistant", "content": content}}, err
	case "gemini":
		if m.Role == "tool" {
			c := m.ToolCall
			key := "output"
			if m.IsError {
				key = "error"
			}
			response := map[string]any{"name": c.Name, "response": map[string]any{key: m.Text()}}
			if id := c.Metadata["gemini_call_id"]; nonempty(id) {
				response["id"] = id
			}
			media := []any{}
			for _, p := range m.Parts {
				if p.Type == "image" || p.Type == "audio" || p.Type == "video" {
					if p.URL != "" {
						return nil, unsupportedMedia("gemini", "remote media in a tool result")
					}
					v, err := f.userPart(p)
					if err != nil {
						return nil, err
					}
					media = append(media, v)
				}
			}
			if len(media) > 0 {
				response["parts"] = media
			}
			return []any{map[string]any{"role": "user", "parts": []any{map[string]any{"functionResponse": response}}}}, nil
		}
		a := payloads(m, "gemini")
		for _, p := range m.Parts {
			if p.Type == "text" || p.Type == "image" || p.Type == "audio" || p.Type == "video" {
				v, err := f.userPart(p)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
		}
		for _, c := range m.ToolCalls {
			args, err := c.Args()
			if err != nil {
				return nil, err
			}
			call := map[string]any{"name": c.Name, "args": args}
			if id := c.Metadata["gemini_call_id"]; nonempty(id) {
				call["id"] = id
			}
			part := map[string]any{"functionCall": call}
			if sig := c.Metadata["gemini_thought_signature"]; nonempty(sig) {
				part["thoughtSignature"] = sig
			}
			a = append(a, part)
		}
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		return []any{map[string]any{"role": role, "parts": a}}, nil
	}
	return nil, fmt.Errorf("invalid message role %q", m.Role)
}
func mergeRoles(entries []any, key string) []any {
	out := []any{}
	for _, x := range entries {
		e := obj(x)
		if len(out) > 0 && obj(out[len(out)-1])["role"] == e["role"] {
			prev := obj(out[len(out)-1])
			prev[key] = append(arr(prev[key]), arr(e[key])...)
		} else {
			out = append(out, e)
		}
	}
	return out
}
func (f WireFormat) reasoning(o Options) map[string]any {
	effort, has := o["reasoning_effort"]
	include := o["include_reasoning"] == true
	switch f.Format {
	case "chat":
		if f.Dialect == "openrouter" {
			m := map[string]any{}
			if has {
				m["effort"] = effort
			}
			if include {
				m["exclude"] = false
			}
			if len(m) > 0 {
				return map[string]any{"reasoning": m}
			}
			return map[string]any{}
		}
		m := map[string]any{}
		if has {
			m["reasoning_effort"] = effort
		}
		if f.Dialect == "minimax" {
			m["reasoning_split"] = true
		}
		return m
	case "responses":
		m := map[string]any{}
		if has {
			m["effort"] = effort
		}
		if include {
			m["summary"] = "auto"
		}
		if len(m) > 0 {
			return map[string]any{"reasoning": m}
		}
	case "messages":
		if effort == "none" {
			return map[string]any{"thinking": map[string]any{"type": "disabled"}}
		}
		if has || include {
			thinking := map[string]any{"type": "adaptive"}
			if include {
				thinking["display"] = "summarized"
			}
			m := map[string]any{"thinking": thinking}
			if has {
				m["output_config"] = map[string]any{"effort": effort}
			}
			return m
		}
	case "gemini":
		m := map[string]any{}
		if has {
			m["thinkingLevel"] = effort
		}
		if include {
			m["includeThoughts"] = true
		}
		if len(m) > 0 {
			return map[string]any{"generationConfig": map[string]any{"thinkingConfig": m}}
		}
	}
	return map[string]any{}
}
func (f WireFormat) Build(r ChatRequest, stream bool) (HTTPRequest, error) {
	o := r.Options
	functions, builtins, err := splitTools(o)
	if err != nil {
		return HTTPRequest{}, err
	}
	var unsupported []string
	switch f.Format {
	case "chat":
		if f.Dialect == "mistral" {
			unsupported = []string{"top_k"}
		}
	case "responses":
		unsupported = []string{"stop", "seed", "top_k", "presence_penalty", "frequency_penalty"}
	case "messages":
		unsupported = []string{"seed", "presence_penalty", "frequency_penalty"}
	case "gemini":
		unsupported = []string{"parallel_tool_calls"}
	default:
		return HTTPRequest{}, failure(ErrUnsupportedFormat, "unknown chat format %q", f.Format)
	}
	if err := reject(f.Format, o, unsupported...); err != nil {
		return HTTPRequest{}, err
	}
	entries := []any{}
	systems := []string{}
	for _, m := range r.Messages {
		if m.Role == "system" && (f.Format == "messages" || f.Format == "gemini") {
			systems = append(systems, m.Text())
			continue
		}
		a, err := f.entries(m)
		if err != nil {
			return HTTPRequest{}, err
		}
		entries = append(entries, a...)
	}
	body := map[string]any{}
	path := ""
	params := map[string]string{}
	switch f.Format {
	case "chat":
		path = "/chat/completions"
		body = map[string]any{"model": r.Model, "messages": entries}
		body = DeepMerge(body, renamed(o, map[string]string{"temperature": "temperature", "top_p": "top_p", "top_k": "top_k", "presence_penalty": "presence_penalty", "frequency_penalty": "frequency_penalty", "seed": "seed", "parallel_tool_calls": "parallel_tool_calls", "stop": "stop"}))
		if v, ok := o["max_tokens"]; ok {
			key := "max_completion_tokens"
			switch f.Dialect {
			case "deepseek", "zai", "ollama", "together", "mistral":
				key = "max_tokens"
			}
			body[key] = v
		}
		if stream {
			body["stream"] = true
			if f.Dialect != "mistral" {
				body["stream_options"] = map[string]any{"include_usage": true}
			}
		}
		if f.Dialect == "mistral" {
			if v, ok := body["seed"]; ok {
				delete(body, "seed")
				body["random_seed"] = v
			}
		}
	case "responses":
		path = "/responses"
		body = map[string]any{"model": r.Model, "input": entries}
		body = DeepMerge(body, renamed(o, map[string]string{"max_tokens": "max_output_tokens", "temperature": "temperature", "top_p": "top_p", "parallel_tool_calls": "parallel_tool_calls"}))
		if stream {
			body["stream"] = true
		}
	case "messages":
		path = "/messages"
		max := any(16000)
		if v, ok := o["max_tokens"]; ok {
			max = v
		}
		body = map[string]any{"model": r.Model, "max_tokens": max, "messages": mergeRoles(entries, "content")}
		if s := strings.Join(systems, "\n\n"); s != "" {
			body["system"] = s
		}
		body = DeepMerge(body, renamed(o, map[string]string{"temperature": "temperature", "top_p": "top_p", "top_k": "top_k", "stop": "stop_sequences"}))
		if stream {
			body["stream"] = true
		}
	case "gemini":
		path = "/models/" + r.Model + ":generateContent"
		if stream {
			path = "/models/" + r.Model + ":streamGenerateContent"
			params["alt"] = "sse"
		}
		body["contents"] = mergeRoles(entries, "parts")
		if s := strings.Join(systems, "\n\n"); s != "" {
			body["systemInstruction"] = map[string]any{"parts": []any{map[string]any{"text": s}}}
		}
		cfg := renamed(o, map[string]string{"max_tokens": "maxOutputTokens", "temperature": "temperature", "top_p": "topP", "top_k": "topK", "presence_penalty": "presencePenalty", "frequency_penalty": "frequencyPenalty", "seed": "seed", "stop": "stopSequences"})
		if len(cfg) > 0 {
			body["generationConfig"] = cfg
		}
	}
	if r.OutputSchema != nil {
		s := r.OutputSchema
		schema := s.Schema
		if f.Format != "gemini" {
			schema, err = StrictSchema(schema, f.Format != "messages")
			if err != nil {
				return HTTPRequest{}, err
			}
		}
		switch f.Format {
		case "chat":
			body["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": s.Name, "schema": schema, "strict": true}}
		case "responses":
			body["text"] = map[string]any{"format": map[string]any{"type": "json_schema", "name": s.Name, "schema": schema, "strict": true}}
		case "messages":
			body["output_config"] = map[string]any{"format": map[string]any{"type": "json_schema", "schema": schema}}
		case "gemini":
			body = DeepMerge(body, map[string]any{"generationConfig": map[string]any{"responseMimeType": "application/json", "responseJsonSchema": schema}})
		}
	}
	if err := f.addTools(body, o, functions, builtins); err != nil {
		return HTTPRequest{}, err
	}
	body = DeepMerge(body, f.reasoning(o))
	body = DeepMerge(body, obj(o["extra_body"]))
	return HTTPRequest{Path: path, Body: body, Params: params}, nil
}
func (f WireFormat) addTools(body map[string]any, o Options, functions []Tool, builtins []BuiltinTool) error {
	tools := []any{}
	declarations := []any{}
	for _, t := range functions {
		switch f.Format {
		case "chat":
			fn := map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Schema()}
			if t.Strict {
				fn["strict"] = true
			}
			tools = append(tools, map[string]any{"type": "function", "function": fn})
		case "responses":
			tools = append(tools, map[string]any{"type": "function", "name": t.Name, "description": t.Description, "parameters": t.Schema(), "strict": t.Strict})
		case "messages":
			fn := map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Schema()}
			if t.Strict {
				fn["strict"] = true
			}
			tools = append(tools, fn)
		case "gemini":
			if t.Strict {
				return failure(ErrUnsupportedFeature, "the gemini API format has no strict tool mode")
			}
			declarations = append(declarations, map[string]any{"name": t.Name, "description": t.Description, "parametersJsonSchema": t.Schema()})
		}
	}
	if len(declarations) > 0 {
		tools = append(tools, map[string]any{"functionDeclarations": declarations})
	}
	for _, t := range builtins {
		def, err := f.builtinTool(t)
		if err != nil {
			return err
		}
		if f.Format == "chat" && t.Kind == "web_search" {
			body["web_search_options"] = def
		} else {
			tools = append(tools, def)
		}
		if f.Format == "responses" && t.Kind == "web_search" {
			body["include"] = []string{"web_search_call.action.sources"}
		}
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	choice, has := o["tool_choice"]
	name := ""
	if t, ok := choice.(Tool); ok {
		name = t.Name
	}
	switch f.Format {
	case "chat", "responses":
		if has {
			if name != "" {
				if f.Format == "chat" {
					choice = map[string]any{"type": "function", "function": map[string]any{"name": name}}
				} else {
					choice = map[string]any{"type": "function", "name": name}
				}
			}
			body["tool_choice"] = choice
		}
	case "messages":
		parallel, hasParallel := o["parallel_tool_calls"]
		if has || hasParallel {
			m := map[string]any{"type": "auto"}
			if name != "" {
				m = map[string]any{"type": "tool", "name": name}
			} else if choice == "none" {
				m["type"] = "none"
			} else if choice == "required" {
				m["type"] = "any"
			}
			if hasParallel && choice != "none" {
				b, ok := parallel.(bool)
				if !ok {
					return fmt.Errorf("parallel_tool_calls must be a bool")
				}
				m["disable_parallel_tool_use"] = !b
			}
			body["tool_choice"] = m
		}
	case "gemini":
		if has {
			mode := strings.ToUpper(str(choice))
			m := map[string]any{"mode": mode}
			if choice == "required" {
				m["mode"] = "ANY"
			}
			if name != "" {
				m["mode"] = "ANY"
				m["allowedFunctionNames"] = []string{name}
			}
			body["toolConfig"] = map[string]any{"functionCallingConfig": m}
		}
	}
	return nil
}
func (f WireFormat) builtinTool(t BuiltinTool) (map[string]any, error) {
	fail := func() (map[string]any, error) {
		return nil, failure(ErrUnsupportedFeature, "the %q API format does not support %s or its settings", f.Format, t.Kind)
	}
	if t.Kind == "native" {
		if t.APIFormat != f.Format {
			return fail()
		}
		return cloneMap(t.Definition), nil
	}
	location := func() map[string]any { m := t.UserLocation.fields(); m["type"] = "approximate"; return m }
	settings := (t.MaxUses != nil && *t.MaxUses != 0) || len(t.AllowedDomains) > 0 || len(t.BlockedDomains) > 0 || t.UserLocation != nil
	switch f.Format {
	case "chat":
		if t.Kind != "web_search" || t.MaxUses != nil && *t.MaxUses != 0 || len(t.AllowedDomains) > 0 || len(t.BlockedDomains) > 0 {
			return fail()
		}
		m := map[string]any{}
		if t.UserLocation != nil {
			m["user_location"] = map[string]any{"type": "approximate", "approximate": t.UserLocation.fields()}
		}
		return m, nil
	case "responses":
		switch t.Kind {
		case "web_search":
			if t.MaxUses != nil || len(t.BlockedDomains) > 0 {
				return fail()
			}
			m := map[string]any{"type": "web_search"}
			if len(t.AllowedDomains) > 0 {
				m["filters"] = map[string]any{"allowed_domains": t.AllowedDomains}
			}
			if t.UserLocation != nil {
				m["user_location"] = location()
			}
			return m, nil
		case "code_execution":
			return map[string]any{"type": "code_interpreter", "container": map[string]any{"type": "auto"}}, nil
		case "image_generation":
			return map[string]any{"type": "image_generation"}, nil
		}
	case "messages":
		types := map[string]string{"web_search": "web_search_20260209", "web_fetch": "web_fetch_20260209", "code_execution": "code_execution_20260521"}
		typ := types[t.Kind]
		if typ == "" {
			return fail()
		}
		m := map[string]any{"type": typ, "name": t.Kind}
		if t.Kind != "code_execution" {
			if t.MaxUses != nil {
				m["max_uses"] = *t.MaxUses
			}
			if len(t.AllowedDomains) > 0 {
				m["allowed_domains"] = t.AllowedDomains
			}
			if len(t.BlockedDomains) > 0 {
				m["blocked_domains"] = t.BlockedDomains
			}
			if t.Kind == "web_search" && t.UserLocation != nil {
				m["user_location"] = location()
			}
		}
		return m, nil
	case "gemini":
		if (t.Kind == "web_search" || t.Kind == "web_fetch") && settings {
			return fail()
		}
		key := map[string]string{"web_search": "googleSearch", "web_fetch": "urlContext", "code_execution": "codeExecution"}[t.Kind]
		if key != "" {
			return map[string]any{key: map[string]any{}}, nil
		}
	}
	return fail()
}
