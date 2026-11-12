# mince

Local, reversible context compression for AI coding agents. A single static
Go binary that sits between an agent and Anthropic/OpenAI-compatible APIs,
mincing bulky tool output while keeping the exact original retrievable.

No Python, no CGO, no third-party Go modules.

```
agent ──► mince proxy :8787 ──► api.anthropic.com / api.openai.com
              │
              ├─ CCR store (~/.cache/mince/ccr) ── exact raw retrieval
              ├─ wire agents (<2ms): tier shunt, cache guard, inline tools
              └─ RALPH replay loop ── exact-rational policy swaps
```

## Why

Tool results dominate agent context: build logs, JSON dumps, file reads.
`mince` folds them locally and leaves a short stub with a `ccr_<id>` pointer.
The agent can call `mince_retrieve` for exact lines at any time. System
prompts, tool schemas and the last `--protect-tail` turns are never touched,
so provider KV-cache prefixes stay byte-identical.

## Guarantees

- **Fail-open**: malformed JSON, unknown envelopes, weak savings, or a
  compression deadline all forward the original body byte-for-byte.
- **Byte-exact cache prefix**: untouched JSON fields and messages are spliced
  through as raw bytes, never re-serialized.
- **Reversible**: every minced payload is stored gzip+CRC32 framed with
  `0600` files under a `0700` directory.
- **Zero floats**: savings are tracked as reduced `p/q` rationals.
- **No network model dependency**: salience is local bitboards; optional
  Ollama calls are explicit and bounded.

## Build

```sh
make build          # bin/mince, CGO_ENABLED=0
make test
make race
make vet
make rust-test      # tools/mlx-quant-kompress
make rust-build
```

## Usage

```sh
# Transparent proxy for any SDK
mince proxy --addr 127.0.0.1:8787
export ANTHROPIC_BASE_URL=http://127.0.0.1:8787
export OPENAI_BASE_URL=http://127.0.0.1:8787/v1

# Capability contract
mince init                       # writes mince.vkd.hcl
mince proxy --config mince.vkd.hcl

# One-command agent wrapper (ephemeral port, child exit code preserved)
mince wrap claude
mince wrap -- opencode run "fix the failing test"

# One-shot compression + exact retrieval
mince compress --kind log build.log > build.minced
mince retrieve ccr_ab12cd34ef56 --grep 'panic|ERROR' --limit 50

# FELIX VM
mince felix eval 'fn main() -> int { return 7; }'

# RALPH replay loop (plan/verify)
mince ralph run --plan --verify --max-iter 64 --every 8

# Ledger
mince stats
```

## Capability contract

`mince.vkd.hcl` is the single source of truth for capability masks, budgets,
wire agents, RALPH cadence and FELIX functions:

```hcl
mince { version = "0.2.0" }

capability "cache_guard" {
  allow     = ["mince_retrieve", "mince_stats"]
  budget    = "3/4"
  caps      = ["retrieve", "telemetry"]
}

wire_agent "tier_shunt" {
  kind    = "tier_shunt"
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
```

## MCP

```sh
mince mcp serve
```

Tools:

| Tool | Purpose |
| --- | --- |
| `mince_compress` | Compress text/JSON/log/code, return minced text + `ccr_id` |
| `mince_retrieve` | Exact raw lines by id, optional regex/substring + offset/limit |
| `mince_stats` | Session and store savings as exact `p/q` |
| `felix_eval` | Compile/run a FELIX `fn main()` and return its value |

Example client config:

```json
{
  "mcpServers": {
    "mince": { "command": "/absolute/path/bin/mince", "args": ["mcp", "serve"] }
  }
}
```

## How it minces

| Content | Strategy |
| --- | --- |
| JSON arrays | Unified schema, first 2 + last 1 rows, semantic anomalies, distance-ranked outliers |
| Logs | ANSI strip, ternary SimHash run folding, 100% anomaly + stack retention |
| Code | Keep imports/declarations/top-level structure, fold dense bodies |
| Prose | Passthrough |

Salience uses balanced-ternary `TritPlane64` bitboards and `POPCNT`
similarity; no model weights, no GPU, no daemon dependency. Plan 9 asm
kernels (`dot_amd64.s`, `dot_arm64.s`) accelerate plane dot products, with a
pure-Go fallback.

## v0.2 components

- `internal/felix`: Pratt compiler + bounded-gas stack VM for a tiny
  linear-capsule language (`pq`, `trit`, `int`, `bool`, `sym`,
  `capsule<T>`). Double-`consume` is rejected at compile time.
- `internal/litany`: 64-byte LTNY v3 frame, CRC32C, SHA-256 hash chain and
  capability POLA.
- `internal/vakedhcl`: strict HCL subset parser and validated capability
  contract.
- `internal/wireagent`: cache guard, tier shunt, panic splice and inline
  `mince_retrieve`/`felix_eval`.
- `internal/hf`: pure-Go SafeTensors reader with SHA-256 manifest gate and
  integer-only `KompressUltra` salience head.
- `internal/rl`: exact-rational `BanditPQ` and JSONL trajectory log.
- `internal/ralph`: 64-turn replay ring, 1/20 improvement gate, atomic policy
  swap and Litany provenance.
- `internal/ollama`: NDJSON `/api/chat` client pinned to `keep_alive=-1` and
  `think=false`.
- `tools/mlx-quant-kompress`: zero-dependency Rust exporter. SafeTensors
  BF16/F16/I8 decode, five-stage packing, CRC-8, SafeTensors output and
  Ollama blob/create push.

## Environment

- Go 1.23+
- macOS / Linux
- Rust 1.96+ only for `tools/mlx-quant-kompress`
- Listens on `127.0.0.1` by default

## License

MIT
