package republic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
)

type ChatModel struct {
	Provider *Provider
	Name     string
	Format   ChatFormat
	History  History
}

func (m *ChatModel) Close() error { return m.Provider.Close() }
func toMessages(prompt any) ([]Message, error) {
	switch p := prompt.(type) {
	case string:
		return []Message{User(Text(p))}, nil
	case Message:
		return []Message{p}, nil
	case []Message:
		return append([]Message(nil), p...), nil
	case []string:
		a := []Message{}
		for _, s := range p {
			a = append(a, User(Text(s)))
		}
		return a, nil
	case []any:
		a := []Message{}
		for _, x := range p {
			switch v := x.(type) {
			case string:
				a = append(a, User(Text(v)))
			case Message:
				a = append(a, v)
			default:
				return nil, fmt.Errorf("prompt item must be string or Message, got %T", x)
			}
		}
		return a, nil
	}
	return nil, fmt.Errorf("prompt must be string, Message or a slice of them, got %T", prompt)
}
func (m *ChatModel) build(ctx context.Context, prompt any, o Options, stream bool) (HTTPRequest, []Message, error) {
	messages, err := toMessages(prompt)
	if err != nil {
		return HTTPRequest{}, nil, err
	}
	options := Options(cloneMap(o))
	options["extra_body"] = DeepMerge(m.Provider.config.ExtraBody, obj(options["extra_body"]))
	past := []Message{}
	if m.History != nil {
		past, err = m.History.Read(ctx)
		if err != nil {
			return HTTPRequest{}, nil, err
		}
	}
	normalized, err := Normalize(append(past, messages...))
	if err != nil {
		return HTTPRequest{}, nil, err
	}
	var schema *OutputSchema
	if v, ok := o["output_schema"]; ok {
		schema, ok = v.(*OutputSchema)
		if !ok {
			return HTTPRequest{}, nil, fmt.Errorf("output_schema must be *OutputSchema")
		}
	}
	r, err := m.Format.Build(ChatRequest{Model: m.Name, Messages: normalized, Options: options, OutputSchema: schema}, stream)
	return r, messages, err
}
func (m *ChatModel) finish(ctx context.Context, messages []Message, b *ResponseBuilder, o Options, h http.Header, ob *observation) (Response, error) {
	r := b.Response()
	r.Headers = h.Clone()
	r.RequestID = requestID(h)
	ob.response = r
	if o["output_schema"] != nil && r.Refusal == "" {
		if err := json.Unmarshal([]byte(r.Text()), &r.Output); err != nil {
			return r, err
		}
		schema, ok := o["output_schema"].(*OutputSchema)
		if !ok {
			return r, fmt.Errorf("output_schema must be *OutputSchema")
		}
		if err := validateOutput(schema.Schema, r.Output); err != nil {
			return r, err
		}
		if target := o["output"]; target != nil {
			if err := json.Unmarshal([]byte(r.Text()), target); err != nil {
				return r, err
			}
			r.Output = target
		}
		ob.response = r
	}
	if m.History != nil {
		if err := m.History.Write(ctx, append(append([]Message(nil), messages...), r.Message)); err != nil {
			return r, err
		}
	}
	return r, nil
}
func (m *ChatModel) Chat(ctx context.Context, prompt any, o Options) (result Response, err error) {
	if m.Provider.Name == "codex" {
		s, err := m.stream(ctx, prompt, o, "chat")
		if err != nil {
			return result, err
		}
		defer s.Close()
		for _, err := range s.Events() {
			if err != nil {
				return result, err
			}
		}
		return s.Response()
	}
	ob := observe(m.Provider, "chat", m.Name)
	defer func() { ob.finish(err) }()
	req, messages, err := m.build(ctx, prompt, o, false)
	if err != nil {
		return result, err
	}
	res, err := m.Provider.request(ctx, "POST", m.Format.Name(), req, m.Format.Headers(), ob, false)
	if err != nil {
		return result, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return result, connectionError(err, res.Header)
	}
	deltas, err := m.Format.Parse(data)
	if err != nil {
		return result, withHeaders(err, res.Header)
	}
	builder := ResponseBuilder{}
	for _, d := range deltas {
		builder.Add(d)
	}
	return m.finish(ctx, messages, &builder, o, res.Header, ob)
}
func withHeaders(err error, h http.Header) error {
	var e *Error
	if errors.As(err, &e) {
		copy := *e
		copy.Headers = h.Clone()
		copy.RequestID = requestID(h)
		return &copy
	}
	return &Error{Kind: ErrAPIResponse, Message: err.Error(), Cause: err, Headers: h.Clone(), RequestID: requestID(h)}
}

// Stream opens the HTTP response immediately. Always close it, even if no events
// are consumed. Events is a single-pass iterator; breaking the loop closes it.
func (m *ChatModel) Stream(ctx context.Context, prompt any, o Options) (*Stream, error) {
	return m.stream(ctx, prompt, o, "stream")
}
func (m *ChatModel) stream(ctx context.Context, prompt any, o Options, operation string) (*Stream, error) {
	ob := observe(m.Provider, operation, m.Name)
	req, messages, err := m.build(ctx, prompt, o, true)
	if err != nil {
		ob.finish(err)
		return nil, err
	}
	res, err := m.Provider.request(ctx, "POST", m.Format.Name(), req, m.Format.Headers(), ob, true)
	if err != nil {
		ob.finish(err)
		return nil, err
	}
	return &Stream{model: m, ctx: ctx, messages: messages, options: o, httpResponse: res, Headers: res.Header.Clone(), RequestID: requestID(res.Header), ob: ob}, nil
}

type Stream struct {
	model            *ChatModel
	ctx              context.Context
	messages         []Message
	options          Options
	httpResponse     *http.Response
	Headers          http.Header
	RequestID        string
	ob               *observation
	result           *Response
	iterated, closed bool
}

func (s *Stream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	err := s.httpResponse.Body.Close()
	s.ob.finish(nil)
	return err
}
func (s *Stream) Response() (Response, error) {
	if s.result == nil {
		return Response{}, failure(ErrStreamNotFinished, "stream has not finished")
	}
	return *s.result, nil
}

var errStopIteration = errors.New("stop iteration")

func (s *Stream) Events() iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		if s.iterated || s.closed {
			yield(Event{}, failure(ErrStreamNotFinished, "stream is closed or has already been iterated"))
			return
		}
		s.iterated = true
		defer s.Close()
		parser := s.model.Format.NewParser()
		if parser == nil {
			err := failure(ErrUnsupportedFormat, "format has no stream parser")
			s.ob.finish(err)
			yield(Event{}, err)
			return
		}
		builder := ResponseBuilder{}
		emit := func(events []Event) error {
			for _, e := range events {
				if !yield(e, nil) {
					return errStopIteration
				}
			}
			return nil
		}
		marker := ""
		if f := s.model.Format.Name(); f == "chat" || f == "responses" {
			marker = "[DONE]"
		}
		if s.model.Provider.Name == "github-copilot" {
			marker = "[DONE]"
		}
		copilotIDs := map[int]string{}
		err := ReadSSE(s.httpResponse.Body, marker, func(e ServerSentEvent) error {
			if s.model.Provider.Name == "github-copilot" {
				var err error
				e, err = stableCopilotEvent(e, s.model.Format.Name(), copilotIDs)
				if err != nil {
					return withHeaders(err, s.Headers)
				}
			}
			ds, err := parser.Feed(e.Event, e.Data)
			if err != nil {
				return withHeaders(err, s.Headers)
			}
			for _, d := range ds {
				if err := emit(builder.Add(d)); err != nil {
					return err
				}
			}
			return nil
		})
		if errors.Is(err, errStopIteration) {
			return
		}
		if err != nil {
			var e *Error
			if !errors.As(err, &e) {
				if s.ctx.Err() != nil {
					err = s.ctx.Err()
				} else {
					err = connectionError(err, s.Headers)
				}
			}
			s.ob.finish(err)
			yield(Event{}, err)
			return
		}
		if err = parser.Finish(); err != nil {
			err = withHeaders(err, s.Headers)
			s.ob.finish(err)
			yield(Event{}, err)
			return
		}
		if err = emit(builder.ReleaseToolCalls()); err != nil {
			return
		}
		response, err := s.model.finish(s.ctx, s.messages, &builder, s.options, s.Headers, s.ob)
		if err != nil {
			s.ob.finish(err)
			yield(Event{}, err)
			return
		}
		s.result = &response
		s.ob.finish(nil)
		yield(Event{Type: "completed", Response: &response}, nil)
	}
}
