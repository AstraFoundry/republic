package republic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

var copilotHeaders = map[string]string{
	"Copilot-Integration-Id": "vscode-chat",
	"Editor-Version":         "vscode/1.107.0",
	"Editor-Plugin-Version":  "copilot-chat/0.35.0",
	"X-GitHub-Api-Version":   "2025-10-01",
}

type CopilotAuth struct {
	mu                       sync.Mutex
	githubToken, accessToken string
	expires                  time.Time
	origin                   *url.URL
	exchangeURL              string
}

func NewCopilotAuth(githubToken string) (*CopilotAuth, error) {
	if githubToken == "" {
		return nil, failure(ErrAuthentication, "Copilot exchange requires a GitHub credential")
	}
	return &CopilotAuth{githubToken: githubToken, exchangeURL: "https://api.github.com/copilot_internal/v2/token"}, nil
}
func (a *CopilotAuth) GitHubToken() string { return a.githubToken }
func (a *CopilotAuth) Apply(ctx context.Context, r *http.Request) error {
	return a.applyWithClient(ctx, r, http.DefaultClient)
}
func (a *CopilotAuth) applyWithClient(ctx context.Context, r *http.Request, client *http.Client) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.accessToken == "" || time.Now().Add(60*time.Second).After(a.expires) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, "GET", a.exchangeURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "token "+a.githubToken)
		req.Header.Set("User-Agent", "republic")
		req.Header.Set("X-GitHub-Api-Version", "2025-04-01")
		res, err := client.Do(req)
		if err != nil {
			return failure(ErrAuthentication, "cannot exchange Copilot token")
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return failure(ErrAuthentication, "GitHub rejected the Copilot token exchange (%d)", res.StatusCode)
		}
		var v map[string]any
		if json.NewDecoder(res.Body).Decode(&v) != nil {
			return failure(ErrAuthentication, "Copilot token exchange returned no JSON")
		}
		if str(v["token"]) == "" {
			return failure(ErrAuthentication, "Copilot token exchange returned no token")
		}
		origin, err := url.Parse(str(obj(v["endpoints"])["api"]))
		if err != nil || origin.Scheme != "https" || origin.Hostname() == "" || origin.User != nil || (origin.Path != "" && origin.Path != "/") || origin.RawQuery != "" || origin.Fragment != "" {
			return failure(ErrAuthentication, "Copilot token exchange returned no valid API origin")
		}
		var expiry time.Time
		for _, key := range []string{"refresh_in", "expires_at"} {
			n, ok := finiteNumber(v[key])
			if !ok || n <= 0 {
				continue
			}
			if key == "refresh_in" {
				n += float64(time.Now().Unix())
			}
			if n > float64(1<<62) {
				continue
			}
			deadline := time.Unix(int64(n), 0)
			if expiry.IsZero() || deadline.Before(expiry) {
				expiry = deadline
			}
		}
		if expiry.IsZero() {
			return failure(ErrAuthentication, "Copilot token exchange returned no valid expiry")
		}
		a.accessToken = str(v["token"])
		a.expires = expiry
		a.origin = origin
	}
	r.URL.Scheme = a.origin.Scheme
	r.URL.Host = a.origin.Host
	r.Host = a.origin.Host
	for k, v := range copilotHeaders {
		if r.Header.Get(k) == "" {
			r.Header.Set(k, v)
		}
	}
	r.Header.Set("Authorization", "Bearer "+a.accessToken)
	return nil
}

type DeviceAuthorize func(context.Context, string, string) error

func LoginCopilot(ctx context.Context, onAuthorize DeviceAuthorize) (*CopilotAuth, error) {
	return loginCopilot(ctx, onAuthorize, nil, "https://github.com/login/device/code", "https://github.com/login/oauth/access_token", time.Second)
}
func loginCopilot(ctx context.Context, onAuthorize DeviceAuthorize, client *http.Client, deviceURL, tokenURL string, unit time.Duration) (*CopilotAuth, error) {
	headers := http.Header{"User-Agent": []string{"republic"}}
	v, err := authRequest(ctx, client, "POST", deviceURL, url.Values{"client_id": {copilotClientID}, "scope": {"read:user"}}, headers)
	if err != nil {
		return nil, failure(ErrAuthentication, "cannot start Copilot device authorization")
	}
	code, userCode, uri := str(v["device_code"]), str(v["user_code"]), str(v["verification_uri"])
	expires, ok := finiteNumber(v["expires_in"])
	u, uriErr := url.Parse(uri)
	if code == "" || userCode == "" || !ok || expires <= 0 || uriErr != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, failure(ErrAuthentication, "invalid Copilot device authorization")
	}
	interval := float64(5)
	if n, exists := v["interval"]; exists {
		var ok bool
		interval, ok = finiteNumber(n)
		if !ok || interval <= 0 {
			return nil, failure(ErrAuthentication, "invalid Copilot device polling interval")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(expires*float64(unit)))
	defer cancel()
	if onAuthorize == nil {
		fmt.Fprintf(os.Stderr, "Open %s and enter %s\n", uri, userCode)
	} else if err := onAuthorize(ctx, uri, userCode); err != nil {
		return nil, err
	}
	for {
		timer := time.NewTimer(time.Duration(interval * float64(unit)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, failure(ErrAuthentication, "Copilot device authorization expired or canceled: %v", ctx.Err())
		case <-timer.C:
		}
		token, err := authRequest(ctx, client, "POST", tokenURL, url.Values{"client_id": {copilotClientID}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {code}}, headers)
		if err != nil {
			return nil, failure(ErrAuthentication, "cannot complete Copilot device authorization")
		}
		switch str(token["error"]) {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5
			continue
		case "expired_token", "token_expired":
			return nil, failure(ErrAuthentication, "Copilot device authorization expired; log in again")
		case "access_denied":
			return nil, failure(ErrAuthentication, "Copilot device authorization was denied")
		case "":
			if str(token["access_token"]) == "" {
				return nil, failure(ErrAuthentication, "Copilot device authorization returned no token")
			}
			return NewCopilotAuth(str(token["access_token"]))
		default:
			return nil, failure(ErrAuthentication, "GitHub rejected Copilot device authorization")
		}
	}
}

// Copilot re-encodes output item IDs. Stabilize them using output_index.
func stableCopilotEvent(e ServerSentEvent, format string, ids map[int]string) (ServerSentEvent, error) {
	if e.Data == "[DONE]" {
		if format == "messages" {
			return ServerSentEvent{"message_stop", `{"type":"message_stop"}`}, nil
		}
		return e, nil
	}
	if format != "responses" {
		return e, nil
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(e.Data), &v); err != nil {
		return e, err
	}
	index := num(v["output_index"])
	item := obj(v["item"])
	switch str(v["type"]) {
	case "response.output_item.added":
		if item["type"] == "function_call" {
			ids[index] = callKey(item)
		}
	case "response.function_call_arguments.delta":
		if id, ok := ids[index]; ok {
			v["item_id"] = id
			e.Data = jsonText(v)
		}
	case "response.output_item.done":
		if id, ok := ids[index]; ok && item["type"] == "function_call" {
			item["id"] = id
			e.Data = jsonText(v)
		}
	}
	return e, nil
}
func codexRequest(r HTTPRequest) (HTTPRequest, error) {
	if _, ok := r.Body["max_output_tokens"]; ok {
		return r, failure(ErrUnsupportedFeature, "Codex does not support max_tokens")
	}
	r.Body = cloneMap(r.Body)
	r.Body["stream"] = true
	r.Body["store"] = false
	if _, ok := r.Body["instructions"]; !ok {
		r.Body["instructions"] = "You are Codex."
	}
	include := []string{}
	switch values := r.Body["include"].(type) {
	case []string:
		include = append(include, values...)
	case []any:
		for _, v := range values {
			include = append(include, str(v))
		}
	}
	include = append(include, "reasoning.encrypted_content")
	unique := []string{}
	seen := map[string]bool{}
	for _, v := range include {
		if !seen[v] && strings.TrimSpace(v) != "" {
			unique = append(unique, v)
			seen[v] = true
		}
	}
	r.Body["include"] = unique
	return r, nil
}
