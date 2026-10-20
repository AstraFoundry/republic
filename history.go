package republic

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

type History interface {
	Read(context.Context) ([]Message, error)
	Write(context.Context, []Message) error
}

// InMemoryHistory keeps whole user turns when bounded. Zero means unbounded.
type InMemoryHistory struct {
	mu         sync.Mutex
	maxEntries int
	messages   []Message
}

func NewHistory(maxEntries int) (*InMemoryHistory, error) {
	if maxEntries < 0 {
		return nil, fmt.Errorf("max_entries must be positive or zero (unbounded)")
	}
	return &InMemoryHistory{maxEntries: maxEntries}, nil
}
func cloneMessages(messages []Message) ([]Message, error) {
	b, err := json.Marshal(messages)
	if err != nil {
		return nil, err
	}
	var v []Message
	err = json.Unmarshal(b, &v)
	return v, err
}
func (h *InMemoryHistory) Read(ctx context.Context) ([]Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return cloneMessages(h.messages)
}
func (h *InMemoryHistory) Write(ctx context.Context, messages []Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	copy, err := cloneMessages(messages)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, copy...)
	if h.maxEntries > 0 && len(h.messages) > h.maxEntries {
		kept := h.messages[len(h.messages)-h.maxEntries:]
		start := 0
		for start < len(kept) && kept[start].Role != "user" {
			start++
		}
		h.messages = append([]Message(nil), kept[start:]...)
	}
	return nil
}
