// Package wireagent is the sub-2ms request/response pipeline that sits
// between the proxy transport and the client. Agents are pure, ordered and
// fail-open: a broken agent must never break the stream.
package wireagent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"mince/internal/compression/router"
	"mince/internal/felix"
	"mince/internal/storage/ccr"
)

// Context is one wire turn.
type Context struct {
	Body        []byte
	Headers     http.Header
	Status      int
	RawBytes    int64
	MincedBytes int64
	Events      []string
	Duration    time.Duration
}

// Event records a pipeline decision.
func (c *Context) Event(s string) { c.Events = append(c.Events, s) }

// Agent transforms a wire turn.
type Agent interface {
	Name() string
	Apply(*Context) error
}

// Pipeline runs agents in order.
type Pipeline struct{ agents []Agent }

// NewPipeline builds a pipeline.
func NewPipeline(agents ...Agent) *Pipeline { return &Pipeline{agents: agents} }

// Run executes all agents and records total duration.
func (p *Pipeline) Run(c *Context) error {
	start := time.Now()
	for _, a := range p.agents {
		if err := a.Apply(c); err != nil {
			c.Event("agent_error:" + a.Name())
			return err
		}
	}
	c.Duration = time.Since(start)
	return nil
}

// CacheGuardAgent restores a body from CCR when the client asks for it.
type CacheGuardAgent struct{ Store *ccr.Store }

func (a *CacheGuardAgent) Name() string { return "cache_guard" }

func (a *CacheGuardAgent) Apply(c *Context) error {
	id := c.Headers.Get("X-Mince-CCR")
	if id == "" {
		return nil
	}
	if a.Store == nil {
		return fmt.Errorf("wireagent: cache guard has no store")
	}
	b, err := a.Store.Get(id)
	if err != nil {
		c.Event("cache_miss")
		return nil
	}
	c.Body = b
	c.Event("cache_hit")
	return nil
}

// TierShuntAgent classifies the body and records the route tier.
type TierShuntAgent struct{}

func (a *TierShuntAgent) Name() string { return "tier_shunt" }

func (a *TierShuntAgent) Apply(c *Context) error {
	kind := router.Classify(c.Body)
	c.Headers.Set("X-Mince-Kind", kind.String())
	c.Event("tier:" + kind.String())
	return nil
}

// PanicSpliceAgent replaces a malformed JSON 5xx body with a safe envelope.
type PanicSpliceAgent struct{}

func (a *PanicSpliceAgent) Name() string { return "panic_splice" }

func (a *PanicSpliceAgent) Apply(c *Context) error {
	if c.Status < 500 || !strings.Contains(strings.ToLower(c.Headers.Get("Content-Type")), "json") {
		return nil
	}
	if json.Valid(c.Body) {
		return nil
	}
	c.Body = []byte(`{"error":"mince: upstream panic"}`)
	c.Status = 502
	c.Event("panic_spliced")
	return nil
}

// InlineCCRAgent executes mince_retrieve or felix_eval inline, avoiding a
// second round trip through the model.
type InlineCCRAgent struct{ Store *ccr.Store }

func (a *InlineCCRAgent) Name() string { return "inline_ccr" }

func (a *InlineCCRAgent) Apply(c *Context) error {
	tool := c.Headers.Get("X-Mince-Inline-Tool")
	switch tool {
	case "":
		return nil
	case "mince_retrieve":
		if a.Store == nil {
			return fmt.Errorf("wireagent: inline retrieve has no store")
		}
		id := c.Headers.Get("X-Mince-CCR")
		text, err := a.Store.Retrieve(id, string(c.Body), 0, 0)
		if err != nil {
			return err
		}
		c.Body = []byte(text)
		c.Event("inline_retrieve")
		return nil
	case "felix_eval":
		p, err := felix.Compile(string(c.Body))
		if err != nil {
			return err
		}
		v, err := p.Call("main", nil)
		if err != nil {
			return err
		}
		c.Body = []byte(v.String(p))
		c.Event("inline_felix")
		return nil
	default:
		return fmt.Errorf("wireagent: unknown inline tool %q", tool)
	}
}
