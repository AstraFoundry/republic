package republic

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func recordedResponse(t *testing.T, path, format string) (Response, []Event) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := newParser(format, "")
	if err != nil {
		t.Fatal(err)
	}
	b := ResponseBuilder{}
	events := []Event{}
	add := func(ds []Delta) {
		for _, d := range ds {
			events = append(events, b.Add(d)...)
		}
	}
	if strings.HasSuffix(path, ".json") {
		ds, err := p.parse(data)
		if err != nil {
			t.Fatal(err)
		}
		add(ds)
	} else {
		marker := ""
		if format == "chat" || format == "responses" {
			marker = "[DONE]"
		}
		err = ReadSSE(strings.NewReader(string(data)), marker, func(e ServerSentEvent) error {
			ds, err := p.Feed(e.Event, e.Data)
			if err != nil {
				return err
			}
			add(ds)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Finish(); err != nil {
			t.Fatal(err)
		}
		events = append(events, b.ReleaseToolCalls()...)
	}
	return b.Response(), events
}
func canonicalResponse(t *testing.T, r Response) any {
	t.Helper()
	for i := range r.Message.ToolCalls {
		c := &r.Message.ToolCalls[i]
		args, err := c.Args()
		if err != nil {
			t.Fatal(err)
		}
		c.Arguments = jsonText(args)
		if strings.HasPrefix(c.ID, "call_") && len(c.ID) == 37 {
			c.ID = "generated"
		}
		if len(c.Metadata) == 0 {
			c.Metadata = nil
		}
	}
	if len(r.Message.Parts) == 0 {
		r.Message.Parts = nil
	}
	if len(r.Message.ToolCalls) == 0 {
		r.Message.ToolCalls = nil
	}
	if len(r.Citations) == 0 {
		r.Citations = nil
	}
	if len(r.BuiltinToolCalls) == 0 {
		r.BuiltinToolCalls = nil
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// Python is used only when this explicit differential-test switch is enabled.
// Ordinary Go tests and the Go library have no Python runtime dependency.
func TestPythonDifferential(t *testing.T) {
	if os.Getenv("REPUBLIC_PYTHON_PARITY") != "1" {
		t.Skip("set REPUBLIC_PYTHON_PARITY=1 to compare with the Python oracle")
	}
	cmd := exec.Command("uv", "run", "python", "tests/go_oracle.py")
	data, err := cmd.Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			t.Fatal(string(e.Stderr))
		}
		t.Fatal(err)
	}
	var expected map[string]struct {
		Response
		EventTypes []string `json:"event_types"`
	}
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	if len(expected) < 50 {
		t.Fatalf("oracle only returned %d recordings", len(expected))
	}
	names := map[string]string{"text_delta": "TextDelta", "reasoning_delta": "ReasoningDelta", "refusal_delta": "RefusalDelta", "tool_call_delta": "ToolCallDelta", "tool_call_ready": "ToolCallReady", "usage_delta": "UsageDelta", "image_ready": "ImageReady", "citation_added": "CitationAdded", "builtin_tool_call_ready": "BuiltinToolCallReady"}
	for path, want := range expected {
		t.Run(path, func(t *testing.T) {
			format := strings.Split(path, "_")[1]
			got, events := recordedResponse(t, filepath.Join("tests", "test_provider_golden", "cases", path), format)
			a, b := canonicalResponse(t, got), canonicalResponse(t, want.Response)
			if !reflect.DeepEqual(a, b) {
				ga, _ := json.MarshalIndent(a, "", "  ")
				wb, _ := json.MarshalIndent(b, "", "  ")
				t.Errorf("Go:\n%s\nPython:\n%s", ga, wb)
			}
			types := []string{}
			for _, e := range events {
				types = append(types, names[e.Type])
			}
			if len(types) != len(want.EventTypes) || strings.Join(types, ",") != strings.Join(want.EventTypes, ",") {
				t.Errorf("event sequence:\nGo %v\nPython %v", types, want.EventTypes)
			}
		})
	}
}

func TestRecordedProtocols(t *testing.T) {
	paths, err := filepath.Glob("tests/test_provider_golden/cases/*/*")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, path := range paths {
		if !strings.HasSuffix(path, ".json") && !strings.HasSuffix(path, ".sse") {
			continue
		}
		count++
		t.Run(filepath.Base(filepath.Dir(path))+"/"+filepath.Base(path), func(t *testing.T) {
			format := strings.Split(filepath.Base(filepath.Dir(path)), "_")[1]
			r, events := recordedResponse(t, path, format)
			if r.Message.Role != "assistant" {
				t.Fatal("response must be an assistant message")
			}
			sum := TokenUsage{}
			ready := []ToolCall{}
			for _, e := range events {
				if e.Type == "usage_delta" {
					sum.InputTokens += e.Usage.InputTokens
					sum.OutputTokens += e.Usage.OutputTokens
					sum.CachedTokens += e.Usage.CachedTokens
					sum.CacheWriteTokens += e.Usage.CacheWriteTokens
					sum.ReasoningTokens += e.Usage.ReasoningTokens
				}
				if e.Call != nil {
					ready = append(ready, *e.Call)
				}
			}
			if sum != r.TokenUsage {
				t.Fatalf("usage deltas %v != final %v", sum, r.TokenUsage)
			}
			if len(ready) != len(r.Message.ToolCalls) {
				t.Fatal("ready calls differ from final calls")
			}
			ids := map[string]bool{}
			for _, c := range r.Message.ToolCalls {
				if c.ID == "" || ids[c.ID] {
					t.Fatalf("invalid/duplicate call id %q", c.ID)
				}
				ids[c.ID] = true
				if _, err := c.Args(); err != nil {
					t.Fatal(err)
				}
			}
			if strings.Contains(path, "goose_chat_length") && (r.Text() != "Partial answer" || r.FinishReason != "length") {
				t.Fatalf("length case: %+v", r)
			}
			if strings.Contains(path, "goose_messages_thinking_streaming") && strings.HasSuffix(path, "0.sse") {
				if r.Reasoning() != "Let me analyze this problem." {
					t.Errorf("reasoning: %q", r.Reasoning())
				}
				if len(r.Message.Parts) == 0 || r.Message.Parts[0].Payload["signature"] != "sig_abc123" {
					t.Fatal("thinking signature fragments were not preserved")
				}
			}
		})
	}
	if count < 50 {
		t.Fatalf("only %d recordings", count)
	}
}
