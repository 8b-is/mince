package config

import "time"

// ProxySettings are runtime options for the HTTP proxy and agent wrapper.
type ProxySettings struct {
	Addr              string
	CCRDir            string
	AnthropicUpstream string
	OpenAIUpstream    string
	MinBytes          int
	ProtectTail       int
	CompressTimeout   time.Duration
}

// DefaultProxySettings returns the built-in runtime defaults.
func DefaultProxySettings() ProxySettings {
	return ProxySettings{
		Addr:              "127.0.0.1:8787",
		AnthropicUpstream: "https://api.anthropic.com",
		OpenAIUpstream:    "https://api.openai.com",
		MinBytes:          1024,
		ProtectTail:       2,
		CompressTimeout:   20 * time.Millisecond,
	}
}
