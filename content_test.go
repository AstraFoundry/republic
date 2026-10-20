package republic

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestMessageRoundTrip(t *testing.T) {
	call := ToolCall{ID: "c1", Name: "lookup", Arguments: `{"city":"Paris"}`, Metadata: map[string]any{"signature": "abc"}}
	cases := []Message{User(Text("")), User(Text("hello")), Assistant(), {Role: "assistant", Parts: []Part{Reasoning("think"), Text("answer"), ProviderData("messages", map[string]any{"type": "thinking", "signature": "sig"})}, ToolCalls: []ToolCall{call}}, ToolResult(call, true, Text("bad"), Part{Type: "image", MediaType: "image/png", Data: []byte{}})}
	for _, m := range cases {
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		var got Message
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(m, got) {
			t.Fatalf("%s: got %#v want %#v", b, got, m)
		}
	}
	for _, input := range []string{`{"role":"tool"}`, `{"role":"user","tool_call":{"id":"x"}}`, `{"role":"user","content":[{"type":"unknown"}]}`, `{"role":"user","content":[{"type":"image","data":"!!"}]}`} {
		var m Message
		if err := json.Unmarshal([]byte(input), &m); err == nil {
			t.Errorf("accepted %s", input)
		}
	}
}
func TestNormalizeOrphanCalls(t *testing.T) {
	a, b := ToolCall{ID: "a"}, ToolCall{ID: "b"}
	in := []Message{User(Text("hi")), Assistant(Text("calling")), ToolResult(a, false, Text("a")), ToolResult(b, false, Text("b")), Assistant()}
	out, err := Normalize(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 4 || len(out[1].ToolCalls) != 2 || out[1].ToolCalls[1].ID != "b" {
		t.Fatalf("%+v", out)
	}
	if len(in[1].ToolCalls) != 0 {
		t.Fatal("mutated input")
	}
	again, err := Normalize(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, again) {
		t.Fatal("normalization not idempotent")
	}
	out, err = Normalize([]Message{ToolResult(a, false, Text("done"))})
	if err != nil || len(out) != 2 || out[0].Role != "assistant" {
		t.Fatalf("%v %v", out, err)
	}
}
func TestMedia(t *testing.T) {
	p, err := Image("data:image/png;base64,aGk=", "")
	if err != nil {
		t.Fatal(err)
	}
	if string(p.Data) != "hi" || p.MediaType != "image/png" {
		t.Fatal(p)
	}
	u, err := p.DataURL()
	if err != nil || u != "data:image/png;base64,aGk=" {
		t.Fatalf("%s %v", u, err)
	}
	p, err = Audio("https://example.org/a.mp3?x=1", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.MediaType != "audio/mpeg" {
		t.Fatal(p)
	}
	if _, err = Image([]byte("hi"), ""); err == nil {
		t.Fatal("raw bytes accepted without MIME type")
	}
	if _, err = Video("data:video/mp4;base64,???", ""); err == nil {
		t.Fatal("bad base64 accepted")
	}
	if _, err = Image("data:image/png;base64,aG\nk=", ""); err == nil {
		t.Fatal("base64 newline accepted")
	}
	var part Part
	if err = json.Unmarshal([]byte(`{"type":"image","media_type":"image/png","data":"aG\nk="}`), &part); err == nil {
		t.Fatal("serialized media newline accepted")
	}
}
func TestHistoryTrimAndIsolation(t *testing.T) {
	ctx := context.Background()
	h, _ := NewHistory(3)
	call := ToolCall{ID: "c"}
	if err := h.Write(ctx, []Message{User(Text("one")), Assistant(), ToolResult(call, false, Text("result")), User(Text("two")), Assistant(Text("answer"))}); err != nil {
		t.Fatal(err)
	}
	got, err := h.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Text() != "two" {
		t.Fatal(got)
	}
	got[0].Parts[0].Text = "changed"
	again, _ := h.Read(ctx)
	if again[0].Text() != "two" {
		t.Fatal("history mutated")
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := h.Write(ctx, []Message{User(Text("x"))}); err != nil {
				t.Error(err)
			}
			if _, err := h.Read(ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
func TestStrictSchema(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(`{"type":"object","properties":{"a":{"$ref":"#/$defs/Value","description":"override"},"b":{"anyOf":[{"type":"string"},{"type":"null"}],"default":null}},"$defs":{"Value":{"type":"object","properties":{"x":{"type":"integer"}}}}}`), &schema); err != nil {
		t.Fatal(err)
	}
	got, err := StrictSchema(schema, true)
	if err != nil {
		t.Fatal(err)
	}
	a := obj(obj(got["properties"])["a"])
	if a["additionalProperties"] != false || a["description"] != "override" || a["$ref"] != nil {
		t.Fatal(a)
	}
	if _, ok := obj(obj(got["properties"])["b"])["default"]; ok {
		t.Fatal("null default retained")
	}
	if schema["additionalProperties"] != nil {
		t.Fatal("mutated input schema")
	}
}
func TestSSE(t *testing.T) {
	input := ": ping\r\nevent: custom\r\ndata: first\r\ndata: second\r\n\r\ndata: [DONE]\n\ndata: ignored\n\n"
	got := []ServerSentEvent{}
	err := ReadSSE(strings.NewReader(input), "[DONE]", func(e ServerSentEvent) error { got = append(got, e); return nil })
	if err != nil {
		t.Fatal(err)
	}
	want := []ServerSentEvent{{"custom", "first\nsecond"}, {"message", "[DONE]"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	for _, format := range []string{"chat", "responses", "messages", "gemini"} {
		p, _ := NewStreamParser(format)
		if err := p.Finish(); err == nil {
			t.Errorf("%s accepted incomplete stream", format)
		}
	}
	long := strings.Repeat("a", 200000)
	err = ReadSSE(strings.NewReader("data: "+long), "", func(e ServerSentEvent) error {
		if e.Data != long {
			t.Fatal("truncated event")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func FuzzMessageJSON(f *testing.F) {
	f.Add([]byte(`{"role":"user","content":"hello"}`))
	f.Add([]byte(`{"role":"assistant","content":[{"type":"text","text":""}]}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		var m Message
		if json.Unmarshal(b, &m) != nil {
			return
		}
		encoded, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		var again Message
		if err := json.Unmarshal(encoded, &again); err != nil {
			t.Fatal(err)
		}
	})
}
func FuzzStreamParser(f *testing.F) {
	f.Add("chat", `{"choices":[]}`)
	f.Add("messages", `{"type":"message_stop"}`)
	f.Add("gemini", `{}`)
	f.Add("responses", `{"type":"response.completed"}`)
	f.Fuzz(func(t *testing.T, format, data string) {
		p, err := NewStreamParser(format)
		if err != nil {
			return
		}
		_, _ = p.Feed("", data)
	})
}
