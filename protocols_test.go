package republic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuiltinResponseRoundTrips(t *testing.T) {
	cases := []struct {
		format, payload          string
		calls, citations, images int
		reasoning                string
	}{
		{"chat", `{"choices":[{"message":{"content":"answer","refusal":"no","reasoning_content":"think","annotations":[{"type":"url_citation","url_citation":{"url":"https://example.org","title":"source"}},{"type":"url_citation","url_citation":{"url":"https://example.org","title":"source"}}],"images":[{"image_url":{"url":"data:image/png;base64,aGk="}}]},"finish_reason":"stop"}]}`, 0, 1, 1, "think"},
		{"responses", `{"id":"r","status":"completed","output":[{"type":"reasoning","id":"think","encrypted_content":"opaque","summary":[{"type":"summary_text","text":"thought"}]},{"type":"web_search_call","id":"s","action":{"query":"x"},"output":["result"]},{"type":"image_generation_call","id":"i","result":"aGk=","output_format":"png"},{"type":"message","content":[{"type":"output_text","text":"answer","annotations":[{"type":"url_citation","url":"https://example.org","title":"source"}]}]}]}`, 2, 1, 1, "thought"},
		{"messages", `{"id":"m","content":[{"type":"text","text":"before"},{"type":"tool_use","id":"c","name":"f","input":{}},{"type":"server_tool_use","id":"s","name":"web_search","input":{"query":"x"}},{"type":"web_search_tool_result","tool_use_id":"s","content":[{"url":"https://example.org"}]},{"type":"text","text":"after","citations":[{"url":"https://example.org","title":"source","cited_text":"quote"}]}],"usage":{"input_tokens":5,"cache_read_input_tokens":2,"cache_creation_input_tokens":3,"output_tokens":4},"stop_reason":"tool_use"}`, 1, 1, 0, ""},
		{"gemini", `{"candidates":[{"content":{"parts":[{"thought":true,"text":"think"},{"executableCode":{"language":"PYTHON","code":"print(1)"}},{"codeExecutionResult":{"outcome":"OUTCOME_OK","output":"1"}},{"inlineData":{"mimeType":"image/png","data":"aGk="}},{"text":"answer"}]},"groundingMetadata":{"webSearchQueries":["q"],"groundingChunks":[{"web":{"uri":"https://example.org","title":"source"}}]},"urlContextMetadata":{"urlMetadata":[{"retrievedUrl":"https://example.org"}]},"finishReason":"STOP"}]}`, 3, 1, 1, "think"},
	}
	for _, c := range cases {
		t.Run(c.format, func(t *testing.T) {
			r, err := ParseChat(c.format, []byte(c.payload))
			if err != nil {
				t.Fatal(err)
			}
			if len(r.BuiltinToolCalls) != c.calls || len(r.Citations) != c.citations || len(r.ImageParts()) != c.images || r.Reasoning() != c.reasoning {
				t.Fatalf("%+v", r)
			}
			req, err := (WireFormat{Format: c.format}).Build(ChatRequest{Model: "m", Messages: []Message{User(Text("hi")), r.Message}}, false)
			if err != nil {
				t.Fatal(err)
			}
			if c.format == "messages" {
				blocks := arr(obj(arr(req.Body["messages"])[1])["content"])
				types := []string{}
				for _, x := range blocks {
					types = append(types, str(obj(x)["type"]))
				}
				if strings.Join(types, ",") != "text,tool_use,server_tool_use,web_search_tool_result,text" {
					t.Fatalf("lost order: %v", types)
				}
				if r.TokenUsage.InputTokens != 10 {
					t.Fatal(r.TokenUsage)
				}
				// Editing text invalidates old positional markers, preserving opaque blocks.
				r.Message.Parts = append(r.Message.Parts, Text("extra"))
				if _, err := assistantBlocks(r.Message); err != nil {
					t.Fatal(err)
				}
			}
			if c.format == "responses" && len(arr(req.Body["input"])) != 5 {
				t.Fatal(req.Body)
			}
		})
	}
}
func TestProtocolErrorsAndRefusals(t *testing.T) {
	for _, format := range []string{"chat", "responses", "messages", "gemini"} {
		if _, err := ParseChat(format, []byte(`{"error":{"message":"failure"}}`)); err == nil {
			t.Errorf("%s accepted error body", format)
		}
	}
	r, err := ParseChat("messages", []byte(`{"content":[],"usage":{},"stop_reason":"refusal","stop_details":{"explanation":"no"}}`))
	if err != nil || r.Refusal != "no" || r.FinishReason != "refusal" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := ParseChat("gemini", []byte(`{"promptFeedback":{"blockReason":"SAFETY"}}`)); err == nil {
		t.Fatal("blocked prompt accepted")
	}
	r, err = ParseChat("responses", []byte(`{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[]}`))
	if err != nil || r.FinishReason != "length" {
		t.Fatalf("%+v %v", r, err)
	}
}
func TestSSELineEndings(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		input := strings.Join([]string{"event: custom", "data: one", "data: two", "", "data: [DONE]", "", ""}, ending)
		events := []ServerSentEvent{}
		err := ReadSSE(strings.NewReader(input), "[DONE]", func(e ServerSentEvent) error { events = append(events, e); return nil })
		if err != nil || len(events) != 2 || events[0].Data != "one\ntwo" || events[0].Event != "custom" {
			t.Fatalf("%q: %v %v", ending, events, err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type brokenBody struct{}

func (brokenBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (brokenBody) Close() error             { return nil }
func TestReadFailureRetriesOnlyBeforeStreaming(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprint(streaming), func(t *testing.T) {
			attempts := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				attempts++
				var body io.ReadCloser = brokenBody{}
				if attempts > 1 {
					body = io.NopCloser(strings.NewReader(chatOK))
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"X-Request-Id": []string{"read-failed"}}, Body: body, Request: r}, nil
			})}
			m, err := GetModel("openai:m", ProviderConfig{APIFormat: "chat", HTTPClient: client, RetryDelay: ptr(time.Duration(0))})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			if !streaming {
				r, err := m.Chat(context.Background(), "hi", nil)
				if err != nil || r.Text() != "Hello" || attempts != 2 {
					t.Fatalf("%+v %v attempts=%d", r, err, attempts)
				}
			} else {
				s, err := m.Stream(context.Background(), "hi", nil)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				var streamErr error
				for _, err := range s.Events() {
					if err != nil {
						streamErr = err
					}
				}
				var e *Error
				if !errors.As(streamErr, &e) || e.Kind != ErrConnection || e.RequestID != "read-failed" || attempts != 1 {
					t.Fatalf("%v attempts=%d", streamErr, attempts)
				}
			}
		})
	}
}
func TestTransportTimeoutAndExtensionsIsolation(t *testing.T) {
	var count atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { count.Add(1); return nil, context.DeadlineExceeded })}
	m, err := GetModel("openai:m", ProviderConfig{APIFormat: "chat", HTTPClient: client, MaxRetries: ptr(0), Extensions: []Extension{ExtensionFuncs{Start: func(_, _, _ string) any { panic("start") }, Finish: func(_, _ any, _ error) { panic("finish") }}}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	_, err = m.Chat(context.Background(), "hi", nil)
	var e *Error
	if !errors.As(err, &e) || e.Kind != ErrTimeout || !errors.Is(err, context.DeadlineExceeded) || count.Load() != 1 {
		t.Fatal(err)
	}
}
func TestCustomFormatAndRegistry(t *testing.T) {
	const name = "test-custom-provider"
	RegisterProvider(name, ProviderSpec{Name: name, APIBase: "http://localhost", Formats: []string{"chat"}, SelectFormat: func(model, format string) ChatFormat { return WireFormat{Format: format, Dialect: "deepseek"} }})
	defer func() { registryMu.Lock(); delete(registry, name); registryMu.Unlock() }()
	if !contains(AllProviders(), name) {
		t.Fatal("custom provider not listed")
	}
	p, err := GetProvider(name, ProviderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	m, err := p.GetModel("custom")
	if err != nil {
		t.Fatal(err)
	}
	req, err := m.Format.Build(ChatRequest{Model: m.Name, Options: Options{"max_tokens": 12}}, false)
	if err != nil || req.Body["max_tokens"] != 12 {
		t.Fatalf("%v %v", req, err)
	}
}
func TestStructuredRefusalSkipsValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"","refusal":"cannot"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	m, err := GetModel("openai:m", ProviderConfig{APIBase: server.URL, APIFormat: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	r, err := m.Chat(context.Background(), "hi", Options{"output_schema": &OutputSchema{Name: "x", Schema: map[string]any{"type": "object"}}})
	if err != nil || r.Output != nil || r.Refusal != "cannot" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestOwnedClientUsesIdleTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 8; i++ {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(25 * time.Millisecond)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	m, err := GetModel("openai:m", ProviderConfig{APIBase: server.URL, APIFormat: "chat", Timeout: 150 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	s, err := m.Stream(context.Background(), "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, err := range s.Events() {
		if err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.Response()
	if err != nil || r.Text() != "xxxxxxxx" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestHTTPRetryStatusMatrix(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 408, 409, 422, 429, 500, 502, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			m := localModel(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				fmt.Fprint(w, "last body")
			}, ProviderConfig{MaxRetries: ptr(2), RetryDelay: ptr(time.Duration(0))})
			_, err := m.Chat(context.Background(), "hi", nil)
			want := int32(1)
			if status == 408 || status == 409 || status == 429 || status >= 500 {
				want = 3
			}
			var e *Error
			if calls.Load() != want || !errors.As(err, &e) || e.StatusCode != status || e.Body != "last body" {
				t.Fatalf("%v calls=%d", err, calls.Load())
			}
		})
	}
}
