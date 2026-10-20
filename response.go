package republic

import "net/http"

type TokenUsage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	ReasoningTokens  int `json:"reasoning_tokens"`
	CachedTokens     int `json:"cached_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
}

func (u TokenUsage) TotalTokens() int { return u.InputTokens + u.OutputTokens }

type Citation struct {
	URL       string `json:"url"`
	Title     string `json:"title,omitempty"`
	CitedText string `json:"cited_text,omitempty"`
}
type BuiltinToolCall struct {
	Name   string         `json:"name"`
	Input  map[string]any `json:"input"`
	Output any            `json:"output"`
	ID     string         `json:"id,omitempty"`
}
type Response struct {
	Message          Message           `json:"message"`
	TokenUsage       TokenUsage        `json:"token_usage"`
	Output           any               `json:"output"`
	FinishReason     string            `json:"finish_reason,omitempty"`
	Refusal          string            `json:"refusal,omitempty"`
	Citations        []Citation        `json:"citations"`
	BuiltinToolCalls []BuiltinToolCall `json:"builtin_tool_calls"`
	ID               string            `json:"id,omitempty"`
	Model            string            `json:"model,omitempty"`
	RequestID        string            `json:"request_id,omitempty"`
	Headers          http.Header       `json:"-"`
}

func (r Response) Text() string          { return r.Message.Text() }
func (r Response) Reasoning() string     { return r.Message.Reasoning() }
func (r Response) ToolCalls() []ToolCall { return append([]ToolCall(nil), r.Message.ToolCalls...) }
func (r Response) ImageParts() []Part {
	p := []Part{}
	for _, v := range r.Message.Parts {
		if v.Type == "image" {
			p = append(p, v)
		}
	}
	return p
}

// Event is emitted in protocol order. Completed always contains the final result.
type Event struct {
	Type        string           `json:"type"`
	Chunk       string           `json:"chunk,omitempty"`
	CallID      string           `json:"call_id,omitempty"`
	Name        string           `json:"name,omitempty"`
	Usage       TokenUsage       `json:"usage,omitempty"`
	Image       *Part            `json:"image,omitempty"`
	Call        *ToolCall        `json:"call,omitempty"`
	BuiltinCall *BuiltinToolCall `json:"builtin_call,omitempty"`
	Citation    *Citation        `json:"citation,omitempty"`
	Response    *Response        `json:"response,omitempty"`
}

// Delta contains either a public event or an internal cumulative update.
type Delta struct {
	Event        *Event
	ProviderData *Part
	Fragment     *ToolCallFragment
	Usage        map[string]int
	ID           string
	Model        string
	FinishReason string
}
type ToolCallFragment struct {
	Key       string
	ID        string
	Name      string
	Arguments string
	Metadata  map[string]any
	Done      bool
}
type partialCall struct {
	call     ToolCall
	released bool
}
type ResponseBuilder struct {
	events            []Event
	providerData      []Part
	calls             map[string]*partialCall
	order             []string
	usage             TokenUsage
	id, model, finish string
}

func (b *ResponseBuilder) Add(d Delta) []Event {
	if d.ProviderData != nil {
		b.providerData = append(b.providerData, *d.ProviderData)
		return nil
	}
	if f := d.Fragment; f != nil {
		if b.calls == nil {
			b.calls = map[string]*partialCall{}
		}
		c := b.calls[f.Key]
		if c == nil {
			c = &partialCall{call: ToolCall{Metadata: map[string]any{}}}
			b.calls[f.Key] = c
			b.order = append(b.order, f.Key)
		}
		if f.ID != "" {
			c.call.ID = f.ID
		}
		if f.Name != "" {
			c.call.Name = f.Name
		}
		c.call.Arguments += f.Arguments
		for k, v := range f.Metadata {
			c.call.Metadata[k] = v
		}
		events := []Event{}
		if f.Arguments != "" {
			events = append(events, Event{Type: "tool_call_delta", CallID: c.call.ID, Name: c.call.Name, Chunk: f.Arguments})
		}
		if f.Done && !c.released {
			c.released = true
			call := buildCall(c)
			events = append(events, Event{Type: "tool_call_ready", Call: &call})
		}
		return events
	}
	if d.Usage != nil {
		prev := b.usage
		fields := map[string]*int{"input_tokens": &b.usage.InputTokens, "output_tokens": &b.usage.OutputTokens, "reasoning_tokens": &b.usage.ReasoningTokens, "cached_tokens": &b.usage.CachedTokens, "cache_write_tokens": &b.usage.CacheWriteTokens}
		for k, v := range d.Usage {
			if p := fields[k]; p != nil {
				*p = v
			}
		}
		if prev != b.usage {
			return []Event{{Type: "usage_delta", Usage: TokenUsage{b.usage.InputTokens - prev.InputTokens, b.usage.OutputTokens - prev.OutputTokens, b.usage.ReasoningTokens - prev.ReasoningTokens, b.usage.CachedTokens - prev.CachedTokens, b.usage.CacheWriteTokens - prev.CacheWriteTokens}}}
		}
		return nil
	}
	if d.ID != "" {
		b.id = d.ID
	}
	if d.Model != "" {
		b.model = d.Model
	}
	if d.FinishReason != "" {
		b.finish = d.FinishReason
	}
	if d.Event != nil {
		b.events = append(b.events, *d.Event)
		return []Event{*d.Event}
	}
	return nil
}
func buildCall(c *partialCall) ToolCall {
	v := c.call
	if v.Arguments == "" {
		v.Arguments = "{}"
	}
	v.Metadata = cloneMap(v.Metadata)
	return v
}
func (b *ResponseBuilder) ReleaseToolCalls() []Event {
	out := []Event{}
	for _, k := range b.order {
		out = append(out, b.Add(Delta{Fragment: &ToolCallFragment{Key: k, Done: true}})...)
	}
	return out
}
func (b *ResponseBuilder) Response() Response {
	r := Response{Message: Assistant(), TokenUsage: b.usage, ID: b.id, Model: b.model, FinishReason: b.finish, Citations: []Citation{}, BuiltinToolCalls: []BuiltinToolCall{}}
	r.Message.Parts = append([]Part(nil), b.providerData...)
	text, reasoning := "", ""
	images := []Part{}
	seen := map[Citation]bool{}
	for _, e := range b.events {
		switch e.Type {
		case "text_delta":
			text += e.Chunk
		case "reasoning_delta":
			reasoning += e.Chunk
		case "refusal_delta":
			r.Refusal += e.Chunk
		case "image_ready":
			if e.Image != nil {
				images = append(images, *e.Image)
			}
		case "citation_added":
			if e.Citation != nil && !seen[*e.Citation] {
				seen[*e.Citation] = true
				r.Citations = append(r.Citations, *e.Citation)
			}
		case "builtin_tool_call_ready":
			if e.BuiltinCall != nil {
				r.BuiltinToolCalls = append(r.BuiltinToolCalls, *e.BuiltinCall)
			}
		}
	}
	if reasoning != "" {
		r.Message.Parts = append(r.Message.Parts, Reasoning(reasoning))
	}
	if text != "" {
		r.Message.Parts = append(r.Message.Parts, Text(text))
	}
	r.Message.Parts = append(r.Message.Parts, images...)
	for _, k := range b.order {
		r.Message.ToolCalls = append(r.Message.ToolCalls, buildCall(b.calls[k]))
	}
	if r.FinishReason == "" || r.FinishReason == "stop" {
		if r.Refusal != "" {
			r.FinishReason = "refusal"
		} else if len(b.order) > 0 {
			r.FinishReason = "tool_calls"
		}
	}
	return r
}
func chunkDelta(kind, text string) Delta { return Delta{Event: &Event{Type: kind, Chunk: text}} }
