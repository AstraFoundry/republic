package republic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Auth signs a fresh request on every attempt, allowing refreshable credentials.
type Auth interface {
	Apply(context.Context, *http.Request) error
}
type HeaderAuth struct{ Name, Value string }

func (a HeaderAuth) Apply(_ context.Context, r *http.Request) error {
	r.Header.Set(a.Name, a.Value)
	return nil
}

type AuthFunc func(context.Context, *http.Request) error

func (f AuthFunc) Apply(ctx context.Context, r *http.Request) error { return f(ctx, r) }

type ProviderConfig struct {
	APIKey, APIBase, APIFormat, EnvPrefix, Resource string
	Auth                                            Auth
	Headers                                         http.Header
	ExtraBody                                       map[string]any
	HTTPClient                                      *http.Client
	Timeout                                         time.Duration
	MaxRetries                                      *int
	RetryDelay, MaxRetryDelay                       *time.Duration
	Extensions                                      []Extension
}
type ProviderSpec struct {
	Name, APIBase, ModelsPath, AuthHeader string
	Formats                               []string
	// SelectFormat overrides chat dialect selection for custom providers.
	SelectFormat          func(model, format string) ChatFormat
	SelectEmbeddingFormat func(model, format string) EmbeddingAPIFormat
	SelectImageFormat     func(model, format string) ImageAPIFormat
	SelectDecisionFormat  func(model, format string) DecisionAPIFormat
	ModelsParams          func(previous any) map[string]string
	ParseModels           func(data any) ([]ModelInfo, error)
}
type Provider struct {
	Name, APIBase string
	spec          ProviderSpec
	config        ProviderConfig
	auth          Auth
	client        *http.Client
	ownsClient    bool
	mu            sync.Mutex
	closed        bool
}

var registryMu sync.RWMutex
var registry = map[string]ProviderSpec{
	"codex":          {Name: "codex", APIBase: "https://chatgpt.com/backend-api/codex", ModelsPath: "-", Formats: []string{"responses"}},
	"github-copilot": {Name: "github-copilot", APIBase: "https://api.githubcopilot.com", Formats: []string{"chat", "responses", "messages"}},
	"openai":         {Name: "openai", APIBase: "https://api.openai.com/v1", Formats: []string{"responses", "chat", "embeddings", "images"}},
	"anthropic":      {Name: "anthropic", APIBase: "https://api.anthropic.com/v1", AuthHeader: "x-api-key", Formats: []string{"messages"}},
	"google":         {Name: "google", APIBase: "https://generativelanguage.googleapis.com/v1beta", AuthHeader: "x-goog-api-key", Formats: []string{"gemini", "embed_content", "gemini_image"}},
	"gemini":         {Name: "google", APIBase: "https://generativelanguage.googleapis.com/v1beta", AuthHeader: "x-goog-api-key", Formats: []string{"gemini", "embed_content", "gemini_image"}},
	"deepseek":       {Name: "deepseek", APIBase: "https://api.deepseek.com", Formats: []string{"chat", "responses", "messages"}},
	"moonshot":       {Name: "moonshot", APIBase: "https://api.moonshot.ai/v1", Formats: []string{"chat"}},
	"ollama":         {Name: "ollama", APIBase: "http://localhost:11434/v1", Formats: []string{"chat", "responses", "embeddings"}},
	"azure-openai":   {Name: "azure-openai", AuthHeader: "api-key", Formats: []string{"responses", "chat", "embeddings", "images"}},
	"together":       {Name: "together", APIBase: "https://api.together.ai/v1", Formats: []string{"chat", "embeddings"}},
	"zai":            {Name: "zai", APIBase: "https://api.z.ai/api/paas/v4", Formats: []string{"chat"}},
	"mistral":        {Name: "mistral", APIBase: "https://api.mistral.ai/v1", Formats: []string{"chat", "embeddings"}},
	"minimax":        {Name: "minimax", APIBase: "https://api.minimax.io", ModelsPath: "/v1/models", Formats: []string{"messages", "chat"}},
	"magpie":         {Name: "magpie", APIBase: "http://127.0.0.1:3425/v1", Formats: []string{"chat", "responses", "messages", "system_one"}},
	"typesafe":       {Name: "typesafe", APIBase: "https://api.typesafe.ai/v1", Formats: []string{"system_one"}},
	"openrouter":     {Name: "openrouter", APIBase: "https://openrouter.ai/api/v1", Formats: []string{"responses", "messages", "chat", "embeddings", "system_one"}},
	"grok":           {Name: "grok", APIBase: "https://api.x.ai/v1", Formats: []string{"responses", "chat"}},
}

func RegisterProvider(name string, spec ProviderSpec) {
	registryMu.Lock()
	defer registryMu.Unlock()
	spec.Formats = append([]string(nil), spec.Formats...)
	registry[name] = spec
}
func AllProviders() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
func GetProvider(name string, c ProviderConfig) (*Provider, error) {
	registryMu.RLock()
	spec, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, failure(ErrProviderNotFound, "no provider registered as %q; known: %v", name, AllProviders())
	}
	if c.EnvPrefix == "" {
		c.EnvPrefix = "REPUBLIC_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	}
	return NewProvider(spec, c)
}
func NewProvider(spec ProviderSpec, c ProviderConfig) (*Provider, error) {
	if c.MaxRetries != nil && *c.MaxRetries < 0 {
		return nil, fmt.Errorf("max_retries must be nonnegative")
	}
	if c.RetryDelay != nil && *c.RetryDelay < 0 || c.MaxRetryDelay != nil && *c.MaxRetryDelay < 0 {
		return nil, fmt.Errorf("retry delays must be nonnegative")
	}
	if len(spec.Formats) == 0 {
		return nil, failure(ErrUnsupportedFormat, "provider must support at least one format")
	}
	if c.APIFormat != "" && !contains(spec.Formats, c.APIFormat) {
		return nil, failure(ErrUnsupportedFormat, "provider %q supports %v, not %q", spec.Name, spec.Formats, c.APIFormat)
	}
	if c.EnvPrefix == "" {
		c.EnvPrefix = "REPUBLIC_" + strings.ToUpper(strings.ReplaceAll(spec.Name, "-", "_"))
	}
	if c.APIKey == "" && spec.Name == "magpie" {
		c.APIKey = "magpie"
	}
	if c.APIKey == "" {
		c.APIKey = os.Getenv(c.EnvPrefix + "_API_KEY")
	}
	if c.APIBase == "" && spec.Name == "azure-openai" && c.Resource != "" {
		c.APIBase = "https://" + c.Resource + ".openai.azure.com/openai/v1"
	}
	if c.APIBase == "" {
		c.APIBase = os.Getenv(c.EnvPrefix + "_API_BASE")
	}
	if c.APIBase == "" {
		c.APIBase = spec.APIBase
	}
	if c.APIBase == "" && spec.Name == "azure-openai" {
		if resource := os.Getenv(c.EnvPrefix + "_RESOURCE"); resource != "" {
			c.APIBase = "https://" + resource + ".openai.azure.com/openai/v1"
		}
	}
	if c.APIBase == "" {
		return nil, fmt.Errorf("provider %s requires api_base or a resource name", spec.Name)
	}
	auth := c.Auth
	if auth == nil && c.APIKey != "" {
		header, value := spec.AuthHeader, c.APIKey
		if header == "" {
			header = "Authorization"
			value = "Bearer " + value
		}
		auth = HeaderAuth{header, value}
	}
	if auth == nil {
		var err error
		switch spec.Name {
		case "codex":
			auth, err = CodexAuthFromFile("")
		case "grok":
			auth, err = GrokAuthFromFile("")
		case "github-copilot":
			auth = GitHubCLIAuth{}
		}
		if err != nil {
			return nil, err
		}
	}
	c.Headers = c.Headers.Clone()
	if c.Headers == nil {
		c.Headers = http.Header{}
	}
	if c.Headers.Get("User-Agent") == "" {
		c.Headers.Set("User-Agent", "go-republic/0.0.0")
	}
	if spec.Name == "codex" {
		if c.Headers.Get("OpenAI-Beta") == "" {
			c.Headers.Set("OpenAI-Beta", "responses=experimental")
		}
		if c.Headers.Get("originator") == "" {
			c.Headers.Set("originator", "republic")
		}
	}
	c.ExtraBody = cloneMap(c.ExtraBody)
	c.Extensions = append([]Extension(nil), c.Extensions...)
	spec.Formats = append([]string(nil), spec.Formats...)
	client := c.HTTPClient
	owned := client == nil
	if owned {
		timeout := c.Timeout
		if timeout == 0 {
			timeout = 600 * time.Second
		}
		if timeout < 0 {
			return nil, fmt.Errorf("timeout must not be negative")
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		connectTimeout := 10 * time.Second
		if c.Timeout != 0 {
			connectTimeout = c.Timeout
		}
		dialer := &net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}
		transport.TLSHandshakeTimeout = connectTimeout
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := dialer.DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			return &idleTimeoutConn{Conn: conn, timeout: timeout}, nil
		}
		client = &http.Client{Transport: transport}
	}
	return &Provider{Name: spec.Name, APIBase: strings.TrimRight(c.APIBase, "/"), spec: spec, config: c, auth: auth, client: client, ownsClient: owned}, nil
}

// Match per-I/O timeouts: an active long stream must not time out merely because
// its total duration exceeds the configured read timeout.
type idleTimeoutConn struct {
	net.Conn
	timeout time.Duration
}

func (c *idleTimeoutConn) Read(b []byte) (int, error) {
	if err := c.Conn.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
		return 0, err
	}
	return c.Conn.Read(b)
}
func (c *idleTimeoutConn) Write(b []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(time.Now().Add(c.timeout)); err != nil {
		return 0, err
	}
	return c.Conn.Write(b)
}
func contains(values []string, v string) bool {
	for _, s := range values {
		if s == v {
			return true
		}
	}
	return false
}
func (p *Provider) IsClosed() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.closed }

// Close is idempotent. Caller-owned clients are never closed.
func (p *Provider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		if p.ownsClient {
			p.client.CloseIdleConnections()
		}
	}
	return nil
}
func (p *Provider) format(kind string) (string, error) {
	candidates := append([]string{p.config.APIFormat}, p.spec.Formats...)
	for _, name := range candidates {
		k := map[string]string{"chat": "chat", "responses": "chat", "messages": "chat", "gemini": "chat", "embeddings": "embedding", "embed_content": "embedding", "images": "image", "gemini_image": "image", "system_one": "decision"}[name]
		if k == kind {
			return name, nil
		}
	}
	return "", failure(ErrUnsupportedFormat, "provider %q supports no %s API format", p.Name, kind)
}
func (p *Provider) GetModel(name string) (*ChatModel, error) {
	format, err := p.format("chat")
	if err != nil {
		return nil, err
	}
	var f ChatFormat = WireFormat{Format: format, Dialect: p.Name}
	if p.spec.SelectFormat != nil {
		f = p.spec.SelectFormat(name, format)
	}
	if f == nil {
		return nil, failure(ErrUnsupportedFormat, "no format for model %q", name)
	}
	return &ChatModel{Provider: p, Name: name, Format: f}, nil
}
func splitSpec(spec string) (string, string, error) {
	provider, model, ok := strings.Cut(spec, ":")
	if !ok || model == "" {
		return "", "", fmt.Errorf("expected a 'provider:model' spec, got %q", spec)
	}
	return provider, model, nil
}
func GetModel(spec string, c ProviderConfig) (*ChatModel, error) {
	name, model, err := splitSpec(spec)
	if err != nil {
		return nil, err
	}
	p, err := GetProvider(name, c)
	if err != nil {
		return nil, err
	}
	m, err := p.GetModel(model)
	if err != nil {
		_ = p.Close()
	}
	return m, err
}
func (p *Provider) request(ctx context.Context, method, format string, r HTTPRequest, headers http.Header, ob *observation, stream bool) (*http.Response, error) {
	if p.IsClosed() {
		return nil, failure(ErrClosed, "provider is closed")
	}
	path := r.Path
	if p.Name == "codex" {
		var err error
		r, err = codexRequest(r)
		if err != nil {
			return nil, err
		}
	}
	if p.Name == "github-copilot" && format == "messages" {
		path = "/v1/messages"
	}
	if p.Name == "deepseek" && format == "messages" {
		path = "/anthropic/v1" + path
	}
	if p.Name == "minimax" && method == "POST" {
		if format == "messages" {
			path = "/anthropic/v1" + path
		} else {
			path = "/v1" + path
		}
	}
	endpoint, err := url.Parse(p.APIBase + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	for k, v := range r.Params {
		query.Set(k, v)
	}
	endpoint.RawQuery = query.Encode()
	body, contentType, err := encodeBody(r)
	if err != nil {
		return nil, err
	}
	retries := 2
	if p.config.MaxRetries != nil {
		retries = *p.config.MaxRetries
	}
	baseDelay, maxDelay := 500*time.Millisecond, 60*time.Second
	if p.config.RetryDelay != nil {
		baseDelay = *p.config.RetryDelay
	}
	if p.config.MaxRetryDelay != nil {
		maxDelay = *p.config.MaxRetryDelay
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header = headers.Clone()
		if req.Header == nil {
			req.Header = http.Header{}
		}
		for k, vs := range p.config.Headers {
			req.Header[k] = append([]string(nil), vs...)
		}
		if method != "GET" {
			req.Header.Set("Content-Type", contentType)
		}
		if p.auth != nil {
			var err error
			if a, ok := p.auth.(*CopilotAuth); ok {
				err = a.applyWithClient(ctx, req, p.client)
			} else {
				err = p.auth.Apply(ctx, req)
			}
			if err != nil {
				return nil, err
			}
		}
		res, sendErr := p.client.Do(req)
		if sendErr == nil && !stream {
			data, readErr := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if readErr != nil {
				sendErr = readErr
			} else {
				res.Body = io.NopCloser(bytes.NewReader(data))
			}
		}
		if ctx.Err() != nil {
			if res != nil {
				_ = res.Body.Close()
			}
			return nil, ctx.Err()
		}
		var retryErr error
		var rh http.Header
		if sendErr != nil {
			retryErr = connectionError(sendErr, nil)
		} else if res.StatusCode == 408 || res.StatusCode == 409 || res.StatusCode == 429 || res.StatusCode >= 500 {
			rh = res.Header
			retryErr = &Error{Kind: ErrAPIStatus, StatusCode: res.StatusCode, Message: fmt.Sprintf("provider returned HTTP %d", res.StatusCode), Headers: rh.Clone(), RequestID: requestID(rh)}
		}
		if retryErr == nil || attempt >= retries {
			if sendErr != nil {
				if res != nil {
					_ = res.Body.Close()
				}
				return nil, retryErr
			}
			if res.StatusCode >= 400 {
				b, err := io.ReadAll(res.Body)
				_ = res.Body.Close()
				if err != nil {
					return nil, connectionError(err, res.Header)
				}
				return nil, &Error{Kind: ErrAPIStatus, StatusCode: res.StatusCode, Body: string(b), Message: fmt.Sprintf("provider returned HTTP %d: %s", res.StatusCode, b), Headers: res.Header.Clone(), RequestID: requestID(res.Header)}
			}
			return res, nil
		}
		if res != nil {
			_ = res.Body.Close()
		}
		delay, ok := retryAfter(rh, time.Now())
		if !ok {
			factor := math.Pow(2, float64(min(attempt, 30)))
			seconds := math.Min(float64(maxDelay), float64(baseDelay)*factor) * (0.75 + rand.Float64()*0.25)
			delay = time.Duration(seconds)
		}
		delay = min(delay, maxDelay)
		if ob != nil {
			ob.retry(retryErr, attempt+1, delay)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
func encodeBody(r HTTPRequest) ([]byte, string, error) {
	if len(r.Files) == 0 {
		b, err := json.Marshal(r.Body)
		return b, "application/json", err
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range r.Body {
		var s string
		switch x := v.(type) {
		case string:
			s = x
		default:
			s = fmt.Sprint(x)
		}
		if err := w.WriteField(k, s); err != nil {
			return nil, "", err
		}
	}
	for _, f := range r.Files {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeMultipart(f.Field), escapeMultipart(f.Filename)))
		h.Set("Content-Type", f.MediaType)
		part, err := w.CreatePart(h)
		if err != nil {
			return nil, "", err
		}
		if _, err = part.Write(f.Data); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}
func escapeMultipart(s string) string {
	return strings.NewReplacer("\\", "\\\\", `"`, `\"`, "\r", "%0D", "\n", "%0A").Replace(s)
}
func connectionError(err error, h http.Header) error {
	kind, msg := ErrConnection, "provider connection failed"
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		kind, msg = ErrTimeout, "provider request timed out"
	}
	return &Error{Kind: kind, Message: msg, Cause: err, Headers: h.Clone(), RequestID: requestID(h)}
}
func retryAfter(h http.Header, now time.Time) (time.Duration, bool) {
	for _, key := range []string{"retry-after-ms", "retry-after"} {
		v := h.Get(key)
		if v == "" {
			continue
		}
		n, err := strconv.ParseFloat(v, 64)
		if err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) {
			if key == "retry-after-ms" {
				n /= 1000
			}
			if n <= 0 {
				return 0, true
			}
			if n >= float64(math.MaxInt64)/float64(time.Second) {
				return time.Duration(math.MaxInt64), true
			}
			return time.Duration(n * float64(time.Second)), true
		}
		if key == "retry-after" {
			if d, err := http.ParseTime(v); err == nil {
				return max(0, d.Sub(now)), true
			}
		}
	}
	return 0, false
}
