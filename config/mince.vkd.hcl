# Runtime settings and capability declarations for mince.
mince {
  version = "0.2.0"
}

proxy {
  addr               = "127.0.0.1:8787"
  anthropic_upstream = "https://api.anthropic.com"
  openai_upstream    = "https://api.openai.com"
  min_bytes          = 1024
  protect_tail       = 2
  compress_timeout   = "20ms"
}

capability "cache_guard" {
  allow     = ["mince_retrieve", "mince_stats", "mince_compress"]
  max_bytes = 4096
  budget    = "3/4"
  caps      = ["retrieve", "telemetry"]
}

capability "felix_sandbox" {
  allow  = ["felix_eval"]
  budget = "1/8"
  caps   = ["felix", "retrieve"]
}

salience_engine "kompress_ultra" {
  model    = "PeetPedro/kompress-v17"
  tier     = "mlx_apple_silicon"
  resident = true
}

wire_agent "cache_guard" {
  kind    = "cache_guard"
  budget  = "1/8"
  enabled = true
}

wire_agent "tier_shunt" {
  kind    = "tier_shunt"
  budget  = "1/16"
  enabled = true
}

wire_agent "inline_ccr" {
  kind    = "inline_ccr"
  budget  = "1/16"
  enabled = true
}

ralph_loop "default" {
  every           = 64
  replay          = 64
  min_improvement = "1/20"
  policy          = "bandit_pq"
}

felix "shrink" {
  source = <<FELIX
fn shrink(c: capsule<sym>) -> sym {
  consume(c)
}
FELIX
  budget = "1/100"
}
