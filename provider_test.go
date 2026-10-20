package republic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }
func localModel(t *testing.T, handler http.HandlerFunc, config ProviderConfig) *ChatModel {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	config.APIBase = server.URL
	config.APIFormat = "chat"
	m, err := GetModel("openai:model", config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

const chatOK = `{"id":"answer","model":"served","choices":[{"message":{"content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`

func TestHTTPChatRetryHistoryExtensions(t *testing.T) {
	var attempts atomic.Int32
	order := []string{}
	ext := ExtensionFuncs{
		Start: func(p, o, m string) any { order = append(order, p+":"+o+":"+m); return "state" },
		Retry: func(s any, e error, a int, d time.Duration) {
			if s != "state" || a != 1 || d != 0 {
				t.Errorf("retry %v %d %v", s, a, d)
			}
			order = append(order, "retry")
		},
		Finish: func(s, r any, e error) {
			if s != "state" || r == nil || e != nil {
				t.Errorf("finish %v %v %v", s, r, e)
			}
			order = append(order, "finish")
		},
	}
	m := localModel(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("request %s %v", r.URL, r.Header)
		}
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		w.Header().Set("x-request-id", "req-1")
		fmt.Fprint(w, chatOK)
	}, ProviderConfig{APIKey: "key", Extensions: []Extension{ext}})
	h, _ := NewHistory(0)
	m.History = h
	r, err := m.Chat(context.Background(), "Hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 || r.Text() != "Hello" || r.RequestID != "req-1" || r.TokenUsage.TotalTokens() != 5 {
		t.Fatalf("%+v", r)
	}
	if strings.Join(order, ",") != "openai:chat:model,retry,finish" {
		t.Fatal(order)
	}
	history, _ := h.Read(context.Background())
	if len(history) != 2 || history[0].Text() != "Hi" || history[1].Text() != "Hello" {
		t.Fatal(history)
	}
}
func TestHTTPStatusAndCancellation(t *testing.T) {
	m := localModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("request-id", "bad")
		w.WriteHeader(401)
		fmt.Fprint(w, "denied")
	}, ProviderConfig{})
	_, err := m.Chat(context.Background(), "Hi", nil)
	var e *Error
	if !errors.As(err, &e) || e.Kind != ErrAPIStatus || e.StatusCode != 401 || e.Body != "denied" || e.RequestID != "bad" {
		t.Fatalf("%#v", err)
	}
	var calls atomic.Int32
	m = localModel(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(503)
	}, ProviderConfig{})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = m.Chat(ctx, "Hi", nil)
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("%v calls=%d", err, calls.Load())
	}
}
func TestStreamCompletionAndEarlyExit(t *testing.T) {
	for _, mode := range []string{"complete", "incomplete", "early"} {
		t.Run(mode, func(t *testing.T) {
			finishes := 0
			var finishedError error
			var finishedResponse any
			m := localModel(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-request-id", "stream-1")
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n")
				if mode != "incomplete" {
					fmt.Fprint(w, "data: [DONE]\n\n")
				}
			}, ProviderConfig{Extensions: []Extension{ExtensionFuncs{Finish: func(_, response any, err error) { finishes++; finishedError = err; finishedResponse = response }}}})
			h, _ := NewHistory(0)
			m.History = h
			stream, err := m.Stream(context.Background(), "Hi", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			if _, err := stream.Response(); err == nil {
				t.Fatal("unfinished stream has a response")
			}
			completed := false
			var streamErr error
			for event, err := range stream.Events() {
				if err != nil {
					streamErr = err
					break
				}
				if mode == "early" {
					break
				}
				if event.Type == "completed" {
					completed = true
				}
			}
			history, _ := h.Read(context.Background())
			if mode == "complete" {
				if !completed || streamErr != nil || len(history) != 2 || finishedResponse == nil || finishedError != nil {
					t.Fatalf("completed %v err %v history %v", completed, streamErr, history)
				}
				r, err := stream.Response()
				if err != nil || r.Text() != "Hello" || r.RequestID != "stream-1" {
					t.Fatalf("%+v %v", r, err)
				}
			} else {
				if completed || len(history) != 0 || finishedResponse != nil {
					t.Fatalf("partial stream committed %v %v", completed, history)
				}
				if mode == "incomplete" {
					var e *Error
					if !errors.As(streamErr, &e) || e.Kind != ErrStreamIncomplete || e.RequestID != "stream-1" {
						t.Fatalf("%#v", streamErr)
					}
				}
			}
			if finishes != 1 {
				t.Fatalf("finish callbacks=%d", finishes)
			}
		})
	}
}
func TestConcurrentCallsAndClose(t *testing.T) {
	m := localModel(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, chatOK) }, ProviderConfig{})
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := m.Chat(context.Background(), "Hi", nil)
			if err != nil || r.Text() != "Hello" {
				t.Errorf("%v %v", r, err)
			}
		}()
	}
	wg.Wait()
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := m.Chat(context.Background(), "Hi", nil)
	var e *Error
	if !errors.As(err, &e) || e.Kind != ErrClosed {
		t.Fatal(err)
	}
}
func TestRetryAfter(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	for _, c := range []struct {
		value string
		want  time.Duration
		valid bool
	}{{"2", 2 * time.Second, true}, {"-1", 0, true}, {"NaN", 0, false}, {"inf", 0, false}, {now.Add(3 * time.Second).UTC().Format(http.TimeFormat), 3 * time.Second, true}, {"bad", 0, false}} {
		h := http.Header{}
		h.Set("Retry-After", c.value)
		got, ok := retryAfter(h, now)
		if got != c.want || ok != c.valid {
			t.Errorf("%s = %v %v", c.value, got, ok)
		}
	}
	h := http.Header{"Retry-After-Ms": []string{"50"}, "Retry-After": []string{"9"}}
	got, _ := retryAfter(h, now)
	if got != 50*time.Millisecond {
		t.Fatal(got)
	}
}

type closingTransport struct{ closed bool }

func (t *closingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(chatOK)), Request: r}, nil
}
func (t *closingTransport) CloseIdleConnections() { t.closed = true }
func TestBorrowedHTTPClient(t *testing.T) {
	transport := &closingTransport{}
	m, err := GetModel("openai:x", ProviderConfig{APIFormat: "chat", HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Chat(context.Background(), "Hi", nil); err != nil {
		t.Fatal(err)
	}
	_ = m.Close()
	if transport.closed {
		t.Fatal("provider closed caller's client")
	}
}
func TestProviderEnvironmentAndValidation(t *testing.T) {
	t.Setenv("REPUBLIC_GEMINI_API_KEY", "secret")
	t.Setenv("REPUBLIC_GEMINI_API_BASE", "http://localhost:8888/")
	p, err := GetProvider("gemini", ProviderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.Name != "google" || p.APIBase != "http://localhost:8888" {
		t.Fatal(p)
	}
	req, _ := http.NewRequest("GET", p.APIBase, nil)
	if err := p.auth.Apply(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("x-goog-api-key") != "secret" {
		t.Fatal("wrong header")
	}
	if _, err := GetProvider("openai", ProviderConfig{APIFormat: "gemini"}); err == nil {
		t.Fatal("unsupported format accepted")
	}
	if _, err := GetProvider("openai", ProviderConfig{MaxRetries: ptr(-1)}); err == nil {
		t.Fatal("negative retries accepted")
	}
	if _, err := GetModel("invalid", ProviderConfig{}); err == nil {
		t.Fatal("invalid spec accepted")
	}
}
