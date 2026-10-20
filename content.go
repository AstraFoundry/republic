// Package republic provides one interface to AI providers. Agent loops,
// gateways, and tool execution belong to the caller.
package republic

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Part is a typed message content item. Data is encoded as base64 in JSON.
// ProviderData is opaque and may only be replayed to its originating format.
type Part struct {
	Type      string         `json:"type"`
	Text      string         `json:"-"`
	MediaType string         `json:"media_type,omitempty"`
	Data      []byte         `json:"data,omitempty"`
	URL       string         `json:"url,omitempty"`
	APIFormat string         `json:"api_format,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

func Text(s string) Part      { return Part{Type: "text", Text: s} }
func Reasoning(s string) Part { return Part{Type: "reasoning", Text: s} }
func ProviderData(format string, payload map[string]any) Part {
	return Part{Type: "provider_data", APIFormat: format, Payload: payload}
}

func (p Part) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": p.Type}
	switch p.Type {
	case "text", "reasoning":
		m["text"] = p.Text
	case "image", "audio", "video":
		m["media_type"] = p.MediaType
		if p.Data != nil {
			m["data"] = base64.StdEncoding.EncodeToString(p.Data)
		}
		if p.URL != "" {
			m["url"] = p.URL
		}
	case "provider_data":
		m["api_format"] = p.APIFormat
		m["payload"] = p.Payload
	default:
		return nil, fmt.Errorf("unknown message part type: %q", p.Type)
	}
	return json.Marshal(m)
}

func (p *Part) UnmarshalJSON(b []byte) error {
	type plain Part
	var v struct {
		plain
		Text string  `json:"text"`
		Data *string `json:"data"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch v.Type {
	case "text", "reasoning", "image", "audio", "video", "provider_data":
	default:
		return fmt.Errorf("unknown message part type: %q", v.Type)
	}
	*p = Part(v.plain)
	p.Text = v.Text
	if v.Data != nil {
		data, err := decodeMediaBase64(*v.Data)
		if err != nil {
			return err
		}
		p.Data = data
	}
	return nil
}

func decodeMediaBase64(s string) ([]byte, error) {
	if strings.ContainsAny(s, "\r\n") {
		return nil, fmt.Errorf("invalid base64 media data")
	}
	return base64.StdEncoding.DecodeString(s)
}

func (p Part) Base64Data() (string, error) {
	if p.Data == nil {
		return "", fmt.Errorf("this %s has no inline data", p.Type)
	}
	return base64.StdEncoding.EncodeToString(p.Data), nil
}
func (p Part) DataURL() (string, error) {
	if p.URL != "" {
		return p.URL, nil
	}
	b, err := p.Base64Data()
	if err != nil {
		return "", err
	}
	return "data:" + p.MediaType + ";base64," + b, nil
}

// LoadMedia accepts a file path, remote URL, data URL, or []byte.
func LoadMedia(kind string, source any, mediaType string) (Part, error) {
	p := Part{Type: kind, MediaType: mediaType}
	if kind != "image" && kind != "audio" && kind != "video" {
		return p, fmt.Errorf("unknown media kind %q", kind)
	}
	if b, ok := source.([]byte); ok {
		if mediaType == "" {
			return p, fmt.Errorf("media_type is required for raw %s bytes", kind)
		}
		p.Data = append([]byte{}, b...)
		return p, nil
	}
	s, ok := source.(string)
	if !ok {
		return p, fmt.Errorf("media source must be a path, URL or bytes")
	}
	if strings.HasPrefix(s, "data:") {
		header, payload, _ := strings.Cut(s, ",")
		p.MediaType = strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
		b, err := decodeMediaBase64(payload)
		if err != nil {
			return p, fmt.Errorf("invalid base64 %s data URL: %w", kind, err)
		}
		p.Data = b
		return p, nil
	}
	remote := strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "gs://")
	if p.MediaType == "" {
		name := s
		if remote {
			u, err := url.Parse(s)
			if err != nil {
				return p, err
			}
			name = u.Path
		}
		p.MediaType = mime.TypeByExtension(filepath.Ext(name))
		if i := strings.IndexByte(p.MediaType, ';'); i >= 0 {
			p.MediaType = p.MediaType[:i]
		}
		if p.MediaType == "" {
			return p, fmt.Errorf("cannot guess the media type of %q; pass media_type explicitly", s)
		}
	}
	if remote {
		p.URL = s
		return p, nil
	}
	var err error
	p.Data, err = os.ReadFile(s)
	return p, err
}
func Image(source any, mediaType string) (Part, error) { return LoadMedia("image", source, mediaType) }
func Audio(source any, mediaType string) (Part, error) { return LoadMedia("audio", source, mediaType) }
func Video(source any, mediaType string) (Part, error) { return LoadMedia("video", source, mediaType) }

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Strict      bool           `json:"strict,omitempty"`
}

func (t Tool) Schema() map[string]any {
	if t.Parameters == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return t.Parameters
}

type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments string         `json:"arguments"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

func (c ToolCall) Args() (any, error) {
	s := c.Arguments
	if s == "" {
		s = "{}"
	}
	var v any
	err := json.Unmarshal([]byte(s), &v)
	return v, err
}

type Message struct {
	Role      string
	Parts     []Part
	ToolCalls []ToolCall
	ToolCall  *ToolCall
	IsError   bool
}

func (m Message) Validate() error {
	if (m.Role == "tool") != (m.ToolCall != nil) {
		return fmt.Errorf("tool_call is required for tool messages and only allowed on them")
	}
	return nil
}
func (m Message) Text() string      { return m.join("text") }
func (m Message) Reasoning() string { return m.join("reasoning") }
func (m Message) join(kind string) string {
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Type == kind {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}
func (m Message) MarshalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	v := map[string]any{"role": m.Role}
	if len(m.Parts) == 1 && m.Parts[0].Type == "text" {
		v["content"] = m.Parts[0].Text
	} else if len(m.Parts) > 0 {
		v["content"] = m.Parts
	}
	if len(m.ToolCalls) > 0 {
		v["tool_calls"] = m.ToolCalls
	}
	if m.ToolCall != nil {
		v["tool_call"] = m.ToolCall
	}
	if m.IsError {
		v["is_error"] = true
	}
	return json.Marshal(v)
}
func (m *Message) UnmarshalJSON(b []byte) error {
	var v struct {
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		ToolCalls []ToolCall      `json:"tool_calls"`
		ToolCall  *ToolCall       `json:"tool_call"`
		IsError   bool            `json:"is_error"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	n := Message{Role: v.Role, ToolCalls: v.ToolCalls, ToolCall: v.ToolCall, IsError: v.IsError}
	if len(v.Content) > 0 && string(v.Content) != "null" {
		if v.Content[0] == '"' {
			var s string
			if err := json.Unmarshal(v.Content, &s); err != nil {
				return err
			}
			n.Parts = []Part{Text(s)}
		} else if err := json.Unmarshal(v.Content, &n.Parts); err != nil {
			return err
		}
	}
	if err := n.Validate(); err != nil {
		return err
	}
	*m = n
	return nil
}
func System(s string) Message         { return Message{Role: "system", Parts: []Part{Text(s)}} }
func User(parts ...Part) Message      { return Message{Role: "user", Parts: parts} }
func Assistant(parts ...Part) Message { return Message{Role: "assistant", Parts: parts} }
func ToolResult(call ToolCall, isError bool, parts ...Part) Message {
	return Message{Role: "tool", Parts: parts, ToolCall: &call, IsError: isError}
}

// Normalize announces orphan tool calls without changing the caller's messages.
func Normalize(messages []Message) ([]Message, error) {
	out := []Message{}
	announced := map[string]bool{}
	for _, m := range messages {
		if err := m.Validate(); err != nil {
			return nil, err
		}
		m.ToolCalls = append([]ToolCall(nil), m.ToolCalls...)
		if m.Role == "assistant" {
			for _, c := range m.ToolCalls {
				announced[c.ID] = true
			}
		}
		if m.ToolCall != nil && !announced[m.ToolCall.ID] {
			announced[m.ToolCall.ID] = true
			i := len(out)
			for i > 0 && out[i-1].Role == "tool" {
				i--
			}
			if i > 0 && out[i-1].Role == "assistant" {
				out[i-1].ToolCalls = append(out[i-1].ToolCalls, *m.ToolCall)
			} else {
				out = append(out, Message{})
				copy(out[i+1:], out[i:])
				out[i] = Message{Role: "assistant", ToolCalls: []ToolCall{*m.ToolCall}}
			}
		}
		out = append(out, m)
	}
	result := out[:0]
	for _, m := range out {
		if m.Role != "assistant" || len(m.Parts) > 0 || len(m.ToolCalls) > 0 {
			result = append(result, m)
		}
	}
	return result, nil
}
