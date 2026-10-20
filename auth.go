package republic

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const grokIssuer = "https://auth.x.ai"
const grokClientID = "b1a00492-073a-47ea-816f-4c329264a828"
const grokScope = grokIssuer + "::" + grokClientID
const codexClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
const copilotClientID = "Iv1.b507a08c87ecfe98"

// OAuth2Auth sends a caller-managed bearer token without persisting it.
type OAuth2Auth struct{ AccessToken string }

func (a OAuth2Auth) Apply(_ context.Context, r *http.Request) error {
	if a.AccessToken == "" {
		return failure(ErrAuthentication, "missing access token")
	}
	r.Header.Set("Authorization", "Bearer "+a.AccessToken)
	return nil
}

// TokenAuth maintains Codex or Grok OAuth credentials. Token returns a snapshot;
// refreshes are serialized. HTTPClient may be set before first use.
type TokenAuth struct {
	mu              sync.Mutex
	kind            string
	token           map[string]any
	accountID, path string
	HTTPClient      *http.Client
	tokenEndpoint   string
}

func NewCodexAuth(token map[string]any, accountID string) (*TokenAuth, error) {
	if str(token["access_token"]) == "" || accountID == "" {
		return nil, failure(ErrAuthentication, "Codex credentials are missing an access token or account ID")
	}
	t := cloneMap(token)
	t["token_type"] = "Bearer"
	t["expires_at"] = tokenExpiry(t, true)
	return &TokenAuth{kind: "codex", token: t, accountID: accountID, tokenEndpoint: "https://auth.openai.com/oauth/token"}, nil
}
func NewGrokAuth(token map[string]any) (*TokenAuth, error) {
	if str(token["access_token"]) == "" {
		return nil, failure(ErrAuthentication, "Grok credentials are missing an access token")
	}
	exp := tokenExpiry(token, false)
	if exp == nil {
		return nil, failure(ErrAuthentication, "Grok credentials are missing a valid expiry")
	}
	t := cloneMap(token)
	t["token_type"] = "Bearer"
	t["expires_at"] = exp
	return &TokenAuth{kind: "grok", token: t, tokenEndpoint: grokIssuer + "/oauth2/token"}, nil
}
func (a *TokenAuth) Token() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return cloneMap(a.token)
}
func (a *TokenAuth) AccountID() string { a.mu.Lock(); defer a.mu.Unlock(); return a.accountID }
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}
func CodexAuthFromFile(path string) (*TokenAuth, error) {
	if path == "" {
		home := os.Getenv("CODEX_HOME")
		if home == "" {
			home = "~/.codex"
		}
		path = filepath.Join(expandHome(home), "auth.json")
	}
	path = expandHome(path)
	_, token, account, err := readTokenFile("codex", path)
	if err != nil {
		return nil, err
	}
	a, err := NewCodexAuth(token, account)
	if err != nil {
		return nil, err
	}
	a.path = path
	return a, nil
}
func GrokAuthFromFile(path string) (*TokenAuth, error) {
	if path == "" {
		path = os.Getenv("GROK_AUTH_PATH")
	}
	if path == "" {
		home := os.Getenv("GROK_HOME")
		if home == "" {
			home = "~/.grok"
		}
		path = filepath.Join(expandHome(home), "auth.json")
	}
	path = expandHome(path)
	_, token, _, err := readTokenFile("grok", path)
	if err != nil {
		return nil, err
	}
	a, err := NewGrokAuth(token)
	if err != nil {
		return nil, err
	}
	a.path = path
	return a, nil
}
func tokenExpiry(token map[string]any, jwtFallback bool) any {
	if v, ok := finiteNumber(token["expires_at"]); ok {
		return float64(int64(v))
	}
	if s := str(token["expires_at"]); s != "" {
		if d, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return float64(d.Unix())
		}
	}
	if jwtFallback {
		parts := strings.Split(str(token["access_token"]), ".")
		if len(parts) > 1 {
			b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
			if err == nil {
				var claims map[string]any
				if json.Unmarshal(b, &claims) == nil {
					if v, ok := finiteNumber(claims["exp"]); ok {
						return float64(int64(v))
					}
				}
			}
		}
	}
	return nil
}
func finiteNumber(v any) (float64, bool) {
	var n float64
	switch x := v.(type) {
	case float64:
		n = x
	case int:
		n = float64(x)
	case int64:
		n = float64(x)
	default:
		return 0, false
	}
	return n, !math.IsNaN(n) && !math.IsInf(n, 0)
}
func tokenExpired(t map[string]any) bool {
	expiry, ok := finiteNumber(t["expires_at"])
	return ok && expiry < float64(time.Now().Unix()+60)
}
func readTokenFile(kind, path string) (map[string]any, map[string]any, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, "", failure(ErrAuthentication, "cannot read %s credentials; run %s login", kind, kind)
	}
	var data map[string]any
	if json.Unmarshal(b, &data) != nil || data == nil {
		return nil, nil, "", failure(ErrAuthentication, "cannot read %s credentials; run %s login", kind, kind)
	}
	if kind == "codex" {
		stored := obj(data["tokens"])
		if str(stored["access_token"]) == "" || str(stored["account_id"]) == "" {
			return nil, nil, "", failure(ErrAuthentication, "Codex credentials must contain a ChatGPT login with an access token and account ID")
		}
		token := map[string]any{"token_type": "Bearer", "expires_at": tokenExpiry(stored, true)}
		for _, key := range []string{"access_token", "refresh_token", "id_token"} {
			if v, ok := stored[key]; ok {
				token[key] = v
			}
		}
		return data, token, str(stored["account_id"]), nil
	}
	stored := obj(data[grokScope])
	if stored["oidc_issuer"] != grokIssuer || stored["oidc_client_id"] != grokClientID {
		return nil, nil, "", failure(ErrAuthentication, "Grok credentials must contain an xAI OAuth login")
	}
	token := cloneMap(stored)
	token["access_token"] = stored["key"]
	auth, err := NewGrokAuth(token)
	if err != nil {
		return nil, nil, "", err
	}
	return data, auth.token, "", nil
}
func (a *TokenAuth) Apply(ctx context.Context, r *http.Request) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	token, account := a.token, a.accountID
	var data map[string]any
	var err error
	if a.path != "" {
		data, token, account, err = readTokenFile(a.kind, a.path)
		if err != nil {
			return err
		}
		if a.kind == "grok" && tokenExpired(token) {
			release, err := lockCredentials(ctx, filepath.Join(filepath.Dir(a.path), "auth.json.lock"))
			if err != nil {
				return failure(ErrAuthentication, "cannot lock the Grok credential store: %v", err)
			}
			defer release()
			data, token, account, err = readTokenFile(a.kind, a.path)
			if err != nil {
				return err
			}
		}
	}
	if tokenExpired(token) {
		token, err = a.refresh(ctx, token)
		if err != nil {
			return err
		}
		if data != nil {
			now := time.Now().UTC().Format(time.RFC3339Nano)
			if a.kind == "codex" {
				stored := obj(data["tokens"])
				for _, key := range []string{"access_token", "refresh_token", "id_token"} {
					if v, ok := token[key]; ok {
						stored[key] = v
					}
				}
				if exp, ok := finiteNumber(token["expires_at"]); ok {
					stored["expires_at"] = time.Unix(int64(exp), 0).UTC().Format(time.RFC3339)
				} else {
					delete(stored, "expires_at")
				}
				data["last_refresh"] = now
			} else {
				stored := obj(data[grokScope])
				stored["key"] = token["access_token"]
				stored["refresh_token"] = token["refresh_token"]
				exp, _ := finiteNumber(token["expires_at"])
				stored["expires_at"] = time.Unix(int64(exp), 0).UTC().Format(time.RFC3339)
				stored["create_time"] = now
			}
			if err := saveCredentials(a.path, data); err != nil {
				return failure(ErrAuthentication, "cannot update %s credential store", a.kind)
			}
		}
	}
	a.token, a.accountID = token, account
	if a.kind == "codex" {
		r.Header.Set("ChatGPT-Account-Id", account)
	}
	r.Header.Set("Authorization", "Bearer "+str(token["access_token"]))
	return nil
}
func (a *TokenAuth) refresh(ctx context.Context, token map[string]any) (map[string]any, error) {
	refresh := str(token["refresh_token"])
	if refresh == "" {
		return nil, failure(ErrAuthentication, "%s token has expired and has no refresh token", a.kind)
	}
	id := codexClientID
	if a.kind == "grok" {
		id = grokClientID
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {id}}
	if a.kind == "grok" {
		for _, key := range []string{"principal_type", "principal_id"} {
			if s := str(token[key]); s != "" {
				form.Set(key, s)
			}
		}
	}
	v, err := authRequest(ctx, a.HTTPClient, "POST", a.tokenEndpoint, form, nil)
	if err != nil {
		return nil, failure(ErrAuthentication, "cannot refresh the %s token", a.kind)
	}
	if str(v["access_token"]) == "" {
		return nil, failure(ErrAuthentication, "%s refresh returned no access token", a.kind)
	}
	if v["refresh_token"] == nil {
		v["refresh_token"] = token["refresh_token"]
	}
	if v["id_token"] == nil && token["id_token"] != nil {
		v["id_token"] = token["id_token"]
	}
	for _, key := range []string{"principal_type", "principal_id"} {
		if token[key] != nil {
			v[key] = token[key]
		}
	}
	if exp, ok := finiteNumber(v["expires_in"]); ok {
		v["expires_at"] = float64(time.Now().Unix()) + exp
	}
	v["expires_at"] = tokenExpiry(v, a.kind == "codex")
	v["token_type"] = "Bearer"
	if a.kind == "grok" && v["expires_at"] == nil {
		return nil, failure(ErrAuthentication, "Grok refresh returned no valid expiry")
	}
	return v, nil
}
func saveCredentials(path string, data map[string]any) error {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".republic-auth-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func authRequest(ctx context.Context, client *http.Client, method, endpoint string, form url.Values, headers http.Header) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = http.Header{}
	}
	req.Header.Set("Accept", "application/json")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var v map[string]any
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 && v["error"] == nil {
		return nil, fmt.Errorf("authentication HTTP %d", res.StatusCode)
	}
	if v == nil {
		return nil, fmt.Errorf("authentication response is not an object")
	}
	return v, nil
}

type GitHubCLIAuth struct{ Hostname, Executable string }

func (a GitHubCLIAuth) Apply(ctx context.Context, r *http.Request) error {
	executable, host := a.Executable, a.Hostname
	if executable == "" {
		executable = "gh"
	}
	if host == "" {
		host = "github.com"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, executable, "auth", "token", "--hostname", host).Output()
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return failure(ErrAuthentication, "cannot read GitHub CLI login; run gh auth login")
	}
	r.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(b)))
	return nil
}

// LoginCLI runs an explicit user-initiated login in the caller's terminal.
func LoginCLI(ctx context.Context, executable string, args ...string) error {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return failure(ErrAuthentication, "%s login failed: %v", executable, err)
	}
	return nil
}
func LoginCodex(ctx context.Context, executable string, device bool) (*TokenAuth, error) {
	if executable == "" {
		executable = "codex"
	}
	args := []string{"login", "--config", `cli_auth_credentials_store="file"`}
	if device {
		args = append(args, "--device-auth")
	}
	if err := LoginCLI(ctx, executable, args...); err != nil {
		return nil, err
	}
	return CodexAuthFromFile("")
}
func LoginGrok(ctx context.Context, executable string, device bool) (*TokenAuth, error) {
	if executable == "" {
		executable = "grok"
	}
	args := []string{"login"}
	if device {
		args = append(args, "--device-auth")
	}
	if err := LoginCLI(ctx, executable, args...); err != nil {
		return nil, err
	}
	return GrokAuthFromFile("")
}
func LoginGitHubCLI(ctx context.Context, executable, hostname string) (GitHubCLIAuth, error) {
	if executable == "" {
		executable = "gh"
	}
	if hostname == "" {
		hostname = "github.com"
	}
	err := LoginCLI(ctx, executable, "auth", "login", "--hostname", hostname, "--web")
	return GitHubCLIAuth{Hostname: hostname, Executable: executable}, err
}

type OpenRouterAuth struct{ APIKey string }

func NewOpenRouterAuth(key string) (OpenRouterAuth, error) {
	if strings.TrimSpace(key) == "" {
		return OpenRouterAuth{}, failure(ErrAuthentication, "OpenRouter requires an API key")
	}
	return OpenRouterAuth{key}, nil
}
func (a OpenRouterAuth) Apply(ctx context.Context, r *http.Request) error {
	return (OAuth2Auth{a.APIKey}).Apply(ctx, r)
}

// LoginOpenRouter delegates displaying the authorization URL and collecting its
// code to the caller. The resulting API key is persisted only by the caller.
func LoginOpenRouter(ctx context.Context, authorize func(context.Context, string) (string, error)) (OpenRouterAuth, error) {
	return loginOpenRouter(ctx, authorize, nil, "https://openrouter.ai/auth", "https://openrouter.ai/api/v1/auth/keys")
}
func loginOpenRouter(ctx context.Context, authorize func(context.Context, string) (string, error), client *http.Client, authURL, exchangeURL string) (OpenRouterAuth, error) {
	if authorize == nil {
		return OpenRouterAuth{}, fmt.Errorf("authorize callback is required")
	}
	b := make([]byte, 36)
	if _, err := rand.Read(b); err != nil {
		return OpenRouterAuth{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(b)
	digest := sha256.Sum256([]byte(verifier))
	u, err := url.Parse(authURL)
	if err != nil {
		return OpenRouterAuth{}, err
	}
	q := u.Query()
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(digest[:]))
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	code, err := authorize(ctx, u.String())
	if err != nil {
		return OpenRouterAuth{}, err
	}
	if strings.TrimSpace(code) == "" {
		return OpenRouterAuth{}, failure(ErrAuthentication, "OpenRouter authorization returned no code")
	}
	data := jsonText(map[string]any{"code": strings.TrimSpace(code), "code_verifier": verifier, "code_challenge_method": "S256"})
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", exchangeURL, strings.NewReader(data))
	if err != nil {
		return OpenRouterAuth{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return OpenRouterAuth{}, failure(ErrAuthentication, "cannot exchange the OpenRouter authorization code")
	}
	defer res.Body.Close()
	var v map[string]any
	if res.StatusCode >= 400 || json.NewDecoder(res.Body).Decode(&v) != nil {
		return OpenRouterAuth{}, failure(ErrAuthentication, "cannot exchange the OpenRouter authorization code")
	}
	return NewOpenRouterAuth(str(v["key"]))
}
