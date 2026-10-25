package republic

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestRequestCompatibility(t *testing.T) {
	data, err := os.ReadFile("testdata/compatibility/requests.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases map[string]struct {
		Provider     string            `json:"provider"`
		Format       string            `json:"format"`
		Messages     []Message         `json:"messages"`
		Options      Options           `json:"options"`
		Tools        bool              `json:"tools"`
		BuiltinTools []BuiltinTool     `json:"builtin_tools"`
		NamedChoice  bool              `json:"named_choice"`
		Schema       map[string]any    `json:"schema"`
		Stream       bool              `json:"stream"`
		Path         string            `json:"path"`
		Body         map[string]any    `json:"body"`
		Params       map[string]string `json:"params"`
		Error        string            `json:"error"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 570 {
		t.Fatalf("expected 570 request baselines, got %d", len(cases))
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			tool := Tool{Name: "weather", Description: "Get weather", Parameters: map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}}}
			if c.Tools {
				c.Options["tools"] = []Tool{tool}
			}
			if len(c.BuiltinTools) > 0 {
				c.Options["tools"] = c.BuiltinTools
			}
			if c.NamedChoice {
				c.Options["tool_choice"] = tool
			}
			var schema *OutputSchema
			if c.Schema != nil {
				schema = &OutputSchema{Name: "Answer", Schema: c.Schema}
			}
			messages, err := Normalize(c.Messages)
			if err != nil {
				t.Fatal(err)
			}
			got, err := (WireFormat{Format: c.Format, Dialect: c.Provider}).Build(ChatRequest{Model: "test-model", Messages: messages, Options: c.Options, OutputSchema: schema}, c.Stream)
			if c.Error != "" {
				if err == nil {
					t.Fatalf("accepted request rejected by the baseline (%s)", c.Error)
				}
				var apiError *Error
				if c.Error == "UnsupportedFeatureError" && (!errors.As(err, &apiError) || apiError.Kind != ErrUnsupportedFeature) {
					t.Fatalf("wrong error category: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Path != c.Path || !reflect.DeepEqual(got.Params, c.Params) {
				t.Errorf("path/params: %s %v want %s %v", got.Path, got.Params, c.Path, c.Params)
			}
			b, err := json.Marshal(got.Body)
			if err != nil {
				t.Fatal(err)
			}
			var canonical map[string]any
			if err := json.Unmarshal(b, &canonical); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(canonical, c.Body) {
				want, _ := json.Marshal(c.Body)
				t.Errorf("Actual: %s\nBaseline: %s", b, want)
			}
		})
	}
}
func TestReasoningNone(t *testing.T) {
	cases := []struct {
		format, dialect, key string
		want                 any
	}{
		{"chat", "deepseek", "thinking", map[string]any{"type": "disabled"}},
		{"chat", "openai", "reasoning_effort", "none"},
		{"chat", "openrouter", "reasoning", map[string]any{"effort": "none"}},
		{"responses", "openai", "reasoning", map[string]any{"effort": "none"}},
		{"responses", "deepseek", "reasoning", map[string]any{"effort": "none"}},
		{"responses", "codex", "reasoning", map[string]any{"effort": "none"}},
		{"messages", "anthropic", "thinking", map[string]any{"type": "disabled"}},
		{"gemini", "google", "generationConfig", map[string]any{"thinkingConfig": map[string]any{"thinkingBudget": float64(0)}}},
	}
	for _, c := range cases {
		t.Run(c.dialect+"/"+c.format, func(t *testing.T) {
			got, err := (WireFormat{Format: c.format, Dialect: c.dialect}).Build(ChatRequest{
				Model: "m", Messages: []Message{User(Text("hi"))}, Options: Options{"reasoning_effort": "none"},
			}, false)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(got.Body)
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(body[c.key], c.want) {
				t.Fatalf("%s: got %#v", c.key, body[c.key])
			}
			if c.dialect == "deepseek" && c.format == "chat" {
				if _, ok := body["reasoning_effort"]; ok {
					t.Fatal("deepseek chat kept reasoning_effort")
				}
			}
		})
	}
}

func TestFormatsReplayTools(t *testing.T) {
	call := ToolCall{ID: "c1", Name: "weather", Arguments: `{"city":"Paris"}`, Metadata: map[string]any{"gemini_thought_signature": "sig"}}
	messages, err := Normalize([]Message{User(Text("hi")), ToolResult(call, false, Text("sunny"))})
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"chat", "responses", "messages", "gemini"} {
		t.Run(format, func(t *testing.T) {
			r, err := (WireFormat{Format: format}).Build(ChatRequest{Model: "test", Messages: messages}, false)
			if err != nil {
				t.Fatal(err)
			}
			switch format {
			case "chat":
				a := arr(r.Body["messages"])
				if len(a) != 3 || obj(a[2])["tool_call_id"] != "c1" {
					t.Fatal(r.Body)
				}
			case "responses":
				a := arr(r.Body["input"])
				if len(a) != 3 || obj(a[1])["call_id"] != "c1" || obj(a[2])["output"] != "sunny" {
					t.Fatal(r.Body)
				}
			case "messages":
				a := arr(r.Body["messages"])
				if obj(arr(obj(a[1])["content"])[0])["type"] != "tool_use" {
					t.Fatal(r.Body)
				}
			case "gemini":
				a := arr(r.Body["contents"])
				part := obj(arr(obj(a[1])["parts"])[0])
				if part["thoughtSignature"] != "sig" || obj(part["functionCall"])["id"] != nil {
					t.Fatal(r.Body)
				}
			}
		})
	}
}
func TestUnsupportedOptionsAndMedia(t *testing.T) {
	for _, format := range []string{"responses", "messages", "gemini"} {
		option := "seed"
		if format == "gemini" {
			option = "parallel_tool_calls"
		}
		_, err := (WireFormat{Format: format}).Build(ChatRequest{Model: "x", Options: Options{option: false}}, false)
		if err == nil {
			t.Errorf("%s silently ignored %s", format, option)
		}
	}
	p := Part{Type: "audio", MediaType: "audio/mpeg", URL: "https://example.org/audio.mp3"}
	for _, format := range []string{"chat", "responses", "messages"} {
		if _, err := (WireFormat{Format: format}).Build(ChatRequest{Model: "x", Messages: []Message{User(p)}}, false); err == nil {
			t.Errorf("%s accepted unsupported audio", format)
		}
	}
}
