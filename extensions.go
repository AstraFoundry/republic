package republic

import (
	"log"
	"time"
)

// Extension hooks run synchronously in registration order. A panic is isolated
// and logged; hooks must not mutate responses borrowed from the library.
type Extension interface {
	OnStart(provider, operation, model string) any
	OnRetry(state any, err error, attempt int, delay time.Duration)
	OnFinish(state, response any, err error)
}

// ExtensionFuncs allows callers to implement only the hooks they need.
type ExtensionFuncs struct {
	Start  func(provider, operation, model string) any
	Retry  func(state any, err error, attempt int, delay time.Duration)
	Finish func(state, response any, err error)
}

func (e ExtensionFuncs) OnStart(p, o, m string) any {
	if e.Start != nil {
		return e.Start(p, o, m)
	}
	return nil
}
func (e ExtensionFuncs) OnRetry(s any, err error, a int, d time.Duration) {
	if e.Retry != nil {
		e.Retry(s, err, a, d)
	}
}
func (e ExtensionFuncs) OnFinish(s, r any, err error) {
	if e.Finish != nil {
		e.Finish(s, r, err)
	}
}

type observation struct {
	extensions []Extension
	states     []any
	response   any
	done       bool
}

func invoke(fn func() any) (value any) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("republic: extension callback panicked: %v", r)
		}
	}()
	return fn()
}
func observe(p *Provider, operation, model string) *observation {
	o := &observation{extensions: p.config.Extensions}
	for _, e := range o.extensions {
		o.states = append(o.states, invoke(func() any { return e.OnStart(p.Name, operation, model) }))
	}
	return o
}
func (o *observation) retry(err error, attempt int, delay time.Duration) {
	for i, e := range o.extensions {
		invoke(func() any { e.OnRetry(o.states[i], err, attempt, delay); return nil })
	}
}
func (o *observation) finish(err error) {
	if o.done {
		return
	}
	o.done = true
	for i, e := range o.extensions {
		invoke(func() any { e.OnFinish(o.states[i], o.response, err); return nil })
	}
}
