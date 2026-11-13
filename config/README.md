# Runtime configuration

`mince.vkd.hcl` is the checked-in sample. Pass it with `--config` to `proxy`
or `wrap`. Command-line flags override file values when both are provided.

The `proxy` block controls operational settings. `addr` applies to the long-
running proxy command; `wrap` always binds an ephemeral loopback port and uses
the other proxy settings from this block.

```hcl
proxy {
  addr               = "127.0.0.1:8787"
  ccr_dir            = ""
  anthropic_upstream = "https://api.anthropic.com"
  openai_upstream    = "https://api.openai.com"
  min_bytes          = 1024
  protect_tail       = 2
  compress_timeout   = "20ms"
}
```

The file also declares named capabilities, wire agents, salience engines,
RALPH loops, and FELIX functions. Enabled `cache_guard`, `tier_shunt`, and
`inline_ccr` wire agents are currently applied to proxy and wrapper requests.
`panic_splice` is response-side behavior and is not connected to the current
request-only pipeline. Capability budgets, salience model declarations, RALPH
settings, and FELIX function declarations are parsed but are not yet applied
to the corresponding runtime components.

Algorithm invariants, protocol constants, and serialization formats remain in
code. The config file is for operator choices that can reasonably vary between
deployments.
