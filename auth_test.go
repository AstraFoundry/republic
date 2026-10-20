package republic

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodexTokenRefreshAndPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	stored := map[string]any{"tokens": map[string]any{"access_token": "old", "refresh_token": "refresh", "account_id": "account", "id_token": "id", "expires_at": 1}, "unrelated": "keep"}
	if err := saveCredentials(path, stored); err != nil {
		t.Fatal(err)
	}
	a, err := CodexAuthFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var refreshes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshes.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("client_id") != codexClientID || r.Form.Get("refresh_token") != "refresh" {
			t.Error(r.Form)
		}
		fmt.Fprint(w, `{"access_token":"new","refresh_token":"rotated","expires_in":3600}`)
	}))
	defer server.Close()
	a.tokenEndpoint = server.URL
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("POST", "https://example.org/responses", nil)
			if err := a.Apply(context.Background(), req); err != nil {
				t.Error(err)
			}
			if req.Header.Get("Authorization") != "Bearer new" || req.Header.Get("ChatGPT-Account-Id") != "account" {
				t.Error(req.Header)
			}
		}()
	}
	wg.Wait()
	if refreshes.Load() != 1 {
		t.Fatal(refreshes.Load())
	}
	data, token, account, err := readTokenFile("codex", path)
	if err != nil {
		t.Fatal(err)
	}
	if data["unrelated"] != "keep" || token["id_token"] != "id" || token["refresh_token"] != "rotated" || account != "account" {
		t.Fatal(data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("credential mode %v", info.Mode())
	}
	tokenCopy := a.Token()
	tokenCopy["access_token"] = "bad"
	if a.Token()["access_token"] != "new" {
		t.Fatal("token snapshot mutated credentials")
	}
	// A fresh CLI login is observed on the next request.
	data["tokens"] = map[string]any{"access_token": "external", "account_id": "new-account"}
	if err := saveCredentials(path, data); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", "https://example.org", nil)
	if err := a.Apply(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "Bearer external" || a.AccountID() != "new-account" {
		t.Fatal(req.Header)
	}
}
func TestGrokRefreshLocksAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	data := map[string]any{grokScope: map[string]any{"oidc_issuer": grokIssuer, "oidc_client_id": grokClientID, "key": "old", "refresh_token": "refresh", "expires_at": "2000-01-01T00:00:00Z", "principal_id": "pid", "principal_type": "user"}, "keep": true}
	if err := saveCredentials(path, data); err != nil {
		t.Fatal(err)
	}
	var refreshes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshes.Add(1)
		_ = r.ParseForm()
		if r.Form.Get("principal_id") != "pid" || r.Form.Get("client_id") != grokClientID {
			t.Error(r.Form)
		}
		fmt.Fprint(w, `{"access_token":"new","expires_in":3600}`)
	}))
	defer server.Close()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		a, err := GrokAuthFromFile(path)
		if err != nil {
			t.Fatal(err)
		}
		a.tokenEndpoint = server.URL
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("GET", "https://example.org", nil)
			if err := a.Apply(context.Background(), req); err != nil {
				t.Error(err)
			}
			if req.Header.Get("Authorization") != "Bearer new" {
				t.Error(req.Header)
			}
		}()
	}
	wg.Wait()
	if refreshes.Load() != 1 {
		t.Fatalf("refresh count %d", refreshes.Load())
	}
	saved, token, _, err := readTokenFile("grok", path)
	if err != nil {
		t.Fatal(err)
	}
	if saved["keep"] != true || token["refresh_token"] != "refresh" {
		t.Fatal(saved)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "auth.json.lock")); err != nil {
		t.Fatal("lock file was removed", err)
	}
}
func TestAuthValidation(t *testing.T) {
	if _, err := NewCodexAuth(map[string]any{}, ""); err == nil {
		t.Fatal("empty Codex credentials accepted")
	}
	if _, err := NewGrokAuth(map[string]any{"access_token": "x"}); err == nil {
		t.Fatal("missing Grok expiry accepted")
	}
	if _, err := CodexAuthFromFile(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing credentials accepted")
	}
	token := "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"exp":2000000000}`)) + ".signature"
	if tokenExpiry(map[string]any{"access_token": token}, true) != float64(2000000000) {
		t.Fatal("JWT expiry not read")
	}
	if tokenExpiry(map[string]any{"access_token": "opaque", "expires_in": 3600}, true) != nil {
		t.Fatal("persisted relative expiry accepted")
	}
	if _, err := NewOpenRouterAuth(" "); err == nil {
		t.Fatal("blank key accepted")
	}
	a, _ := NewCodexAuth(map[string]any{"access_token": "x", "expires_at": 1}, "account")
	req, _ := http.NewRequest("GET", "https://example.org", nil)
	if err := a.Apply(context.Background(), req); err == nil {
		t.Fatal("expired token accepted without refresh")
	}
}
func TestGitHubCLIAuth(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	path := filepath.Join(t.TempDir(), "gh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n[ \"$1 $2 $3 $4\" = 'auth token --hostname github.test' ] || exit 1\nprintf ' token-value\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", "https://example.org", nil)
	if err := (GitHubCLIAuth{Executable: path, Hostname: "github.test"}).Apply(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "Bearer token-value" {
		t.Fatal(req.Header)
	}
	if err := LoginCLI(context.Background(), path, "wrong"); err == nil {
		t.Fatal("failed login accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := LoginCLI(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestOpenRouterPKCE(t *testing.T) {
	var challenge string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(str(body["code_verifier"])))
		if body["code"] != "code" || body["code_challenge_method"] != "S256" || base64.RawURLEncoding.EncodeToString(digest[:]) != challenge {
			t.Error(body)
		}
		fmt.Fprint(w, `{"key":"api-key"}`)
	}))
	defer s.Close()
	auth, err := loginOpenRouter(context.Background(), func(ctx context.Context, link string) (string, error) {
		u, err := url.Parse(link)
		if err != nil {
			t.Fatal(err)
		}
		challenge = u.Query().Get("code_challenge")
		return " code ", nil
	}, s.Client(), "https://openrouter.ai/auth", s.URL)
	if err != nil || auth.APIKey != "api-key" {
		t.Fatalf("%v %v", auth, err)
	}
}
func TestCopilotExchangeAndOrigin(t *testing.T) {
	var origin string
	var exchanges atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/exchange" {
			exchanges.Add(1)
			if r.Header.Get("Authorization") != "token github-token" {
				t.Error(r.Header)
			}
			fmt.Fprintf(w, `{"token":"inference","refresh_in":1800,"expires_at":%d,"endpoints":{"api":%q}}`, time.Now().Add(time.Hour).Unix(), origin)
			return
		}
		if r.Header.Get("Authorization") != "Bearer inference" || r.Header.Get("Editor-Version") != "custom" || r.Header.Get("Copilot-Integration-Id") != "vscode-chat" {
			t.Error(r.Header)
		}
		fmt.Fprint(w, chatOK)
	}))
	defer server.Close()
	origin = server.URL
	a, _ := NewCopilotAuth("github-token")
	a.exchangeURL = server.URL + "/exchange"
	m, err := GetModel("github-copilot:test", ProviderConfig{APIBase: "https://unused.example", Auth: a, HTTPClient: server.Client(), Headers: http.Header{"Editor-Version": []string{"custom"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for i := 0; i < 2; i++ {
		r, err := m.Chat(context.Background(), "hi", nil)
		if err != nil || r.Text() != "Hello" {
			t.Fatalf("%+v %v", r, err)
		}
	}
	if exchanges.Load() != 1 {
		t.Fatal(exchanges.Load())
	}
}
func TestCopilotRejectsUntrustedOrigin(t *testing.T) {
	for _, origin := range []string{"http://example.org", "https://example.org/path", "https://user:pass@example.org", "https://example.org?query=1", "https://example.org#fragment"} {
		t.Run(origin, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"token":"x","refresh_in":3600,"endpoints":{"api":%q}}`, origin)
			}))
			defer s.Close()
			a, _ := NewCopilotAuth("token")
			a.exchangeURL = s.URL
			req, _ := http.NewRequest("GET", "https://example.org", nil)
			if err := a.Apply(context.Background(), req); err == nil {
				t.Fatal("invalid origin accepted")
			}
		})
	}
}
func TestCopilotDeviceFlow(t *testing.T) {
	polls := 0
	authorized := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/device" {
			fmt.Fprint(w, `{"device_code":"device","user_code":"ABCD","verification_uri":"https://github.com/login/device","expires_in":1000,"interval":1}`)
			return
		}
		polls++
		switch polls {
		case 1:
			fmt.Fprint(w, `{"error":"authorization_pending"}`)
		case 2:
			fmt.Fprint(w, `{"error":"slow_down"}`)
		default:
			fmt.Fprint(w, `{"access_token":"plugin-token"}`)
		}
	}))
	defer s.Close()
	a, err := loginCopilot(context.Background(), func(_ context.Context, uri, code string) error {
		authorized = true
		if code != "ABCD" || !strings.HasPrefix(uri, "https://") {
			t.Error(uri, code)
		}
		return nil
	}, s.Client(), s.URL+"/device", s.URL+"/token", time.Millisecond)
	if err != nil || a.GitHubToken() != "plugin-token" || !authorized || polls != 3 {
		t.Fatalf("%v %v %d", a, err, polls)
	}
}
func TestCodexAlwaysStreams(t *testing.T) {
	var operation string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := readRequest(t, r)
		if body["stream"] != true || body["store"] != false || body["instructions"] != "You are Codex." || r.Header.Get("OpenAI-Beta") != "responses=experimental" {
			t.Error(body, r.Header)
		}
		includes := arr(body["include"])
		if len(includes) != 1 || includes[0] != "reasoning.encrypted_content" {
			t.Error(includes)
		}
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hi\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"id\":\"r\",\"model\":\"m\"}}\n\n")
	}))
	defer s.Close()
	m, err := GetModel("codex:m", ProviderConfig{APIKey: "test", APIBase: s.URL, Extensions: []Extension{ExtensionFuncs{Start: func(_, op, _ string) any { operation = op; return nil }}}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	r, err := m.Chat(context.Background(), "hi", nil)
	if err != nil || r.Text() != "Hi" || operation != "chat" {
		t.Fatalf("%+v %v %s", r, err, operation)
	}
	if _, err := m.Chat(context.Background(), "hi", Options{"max_tokens": 1}); err == nil {
		t.Fatal("Codex accepted max_tokens")
	}
}
func TestCopilotStreamRepairs(t *testing.T) {
	p, _ := NewStreamParser("responses")
	ids := map[int]string{}
	b := ResponseBuilder{}
	for _, data := range []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"first","call_id":"call","name":"weather"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"changed","delta":"{}"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"changed-again","call_id":"call","name":"weather","arguments":"{}"}}`,
		`{"type":"response.completed","response":{"status":"completed"}}`,
	} {
		e, err := stableCopilotEvent(ServerSentEvent{"message", data}, "responses", ids)
		if err != nil {
			t.Fatal(err)
		}
		ds, err := p.Feed(e.Event, e.Data)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range ds {
			b.Add(d)
		}
	}
	r := b.Response()
	if len(r.ToolCalls()) != 1 || r.ToolCalls()[0].Arguments != "{}" || r.ToolCalls()[0].ID != "call" {
		t.Fatal(r)
	}
	e, err := stableCopilotEvent(ServerSentEvent{"message", "[DONE]"}, "messages", nil)
	if err != nil {
		t.Fatal(err)
	}
	p, _ = NewStreamParser("messages")
	if _, err := p.Feed(e.Event, e.Data); err != nil {
		t.Fatal(err)
	}
	if err := p.Finish(); err != nil {
		t.Fatal(err)
	}
}
func TestStructuredOutputValidationAndHistory(t *testing.T) {
	for _, c := range []struct {
		text  string
		valid bool
	}{{`{"answer":"yes"}`, true}, {`{"answer":5}`, false}, {`{}`, false}, {`not json`, false}} {
		t.Run(c.text, func(t *testing.T) {
			m := localModel(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"choices":[{"message":{"content":%q},"finish_reason":"stop"}]}`, c.text)
			}, ProviderConfig{})
			h, _ := NewHistory(0)
			m.History = h
			schema := &OutputSchema{Name: "Answer", Schema: map[string]any{"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}}, "required": []string{"answer"}}}
			_, err := m.Chat(context.Background(), "hi", Options{"output_schema": schema})
			if (err == nil) != c.valid {
				t.Fatalf("%v", err)
			}
			history, _ := h.Read(context.Background())
			if c.valid && len(history) != 2 || !c.valid && len(history) != 0 {
				t.Fatal(history)
			}
		})
	}
}
