package republic

import (
	"fmt"
	"net/http"
)

type ErrorKind string

const (
	ErrAuthentication     ErrorKind = "authentication"
	ErrProviderNotFound   ErrorKind = "provider_not_found"
	ErrUnsupportedFormat  ErrorKind = "unsupported_api_format"
	ErrUnsupportedFeature ErrorKind = "unsupported_feature"
	ErrAPIStatus          ErrorKind = "api_status"
	ErrConnection         ErrorKind = "api_connection"
	ErrTimeout            ErrorKind = "api_timeout"
	ErrAPIResponse        ErrorKind = "api_response"
	ErrStreamIncomplete   ErrorKind = "stream_incomplete"
	ErrStreamNotFinished  ErrorKind = "stream_not_finished"
	ErrClosed             ErrorKind = "closed"
)

type Error struct {
	Kind       ErrorKind
	Message    string
	StatusCode int
	Body       string
	Headers    http.Header
	RequestID  string
	Cause      error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }
func failure(kind ErrorKind, format string, args ...any) error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...)}
}
func requestID(h http.Header) string {
	for _, key := range []string{"x-request-id", "request-id", "x-goog-request-id"} {
		if v := h.Get(key); v != "" {
			return v
		}
	}
	return ""
}
