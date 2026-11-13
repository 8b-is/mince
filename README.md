# mince

`mince` is a local, reversible context-compression tool for AI coding agents.
Its current user interface is a Go CLI and a local HTTP proxy. The proxy
compresses eligible tool results before forwarding requests to Anthropic or
OpenAI-compatible APIs, and keeps the exact originals in a local CCR store for
retrieval.

The implementation uses Go's standard library and does not require CGO or
third-party Go modules. The repository does not yet define a stable public Go
library API.

## Build

```sh
make build
make test
make race
make vet
```

The optional Rust SafeTensors exporter has separate build targets:

```sh
make rust-test
make rust-build
```

## Use

Run a local proxy using the checked-in runtime config:

```sh
mince proxy --config config/mince.vkd.hcl
```

The config controls the listen address, upstream URLs, CCR directory,
compression size threshold, protected message tail, compression deadline, and
enabled request wire agents. Explicit command-line flags override file values.

Point an SDK at it:

```sh
export ANTHROPIC_BASE_URL=http://127.0.0.1:8787
export OPENAI_BASE_URL=http://127.0.0.1:8787/v1
```

Run the proxy in Docker with persistent CCR storage:

```sh
docker compose up --build
```

The proxy listens on host loopback at port `8787`; Swagger UI displays the
starter API document at `http://127.0.0.1:8081`. The OpenAPI document is empty
until mince defines its own HTTP operations. The calling SDK sends its normal
provider credentials through the proxy.

Run an agent through a temporary proxy:

```sh
mince wrap --config config/mince.vkd.hcl claude
mince wrap -- opencode run "fix the failing test"
```

Compress a file directly and retrieve its original content later:

```sh
mince compress --kind log build.log > build.minced
mince retrieve ccr_ab12cd34ef56 --grep 'panic|ERROR' --limit 50
```

Run the MCP server over stdio:

```sh
mince mcp serve
```

Other commands include `mince stats`, `mince init`, `mince felix eval`, and
`mince ralph run`. Run `mince help` for flags and complete usage.

## How compression works

The proxy targets tool results in older messages and preserves a configurable
tail of recent messages. JSON arrays, logs, and code use separate deterministic
summarizers. Small inputs, unsupported content, errors, and weak savings pass
through unchanged. A successful compression includes a CCR identifier that
can be used to retrieve the exact source.

For the package map, request flow, and notes on experimental components, see
[`docs/architecture.md`](docs/architecture.md).

## Configuration status

`mince init` writes a sample `config/mince.vkd.hcl` file. Proxy and wrapper
runtime settings and supported request wire agents are applied. Capability
budgets, salience model declarations, and RALPH policy settings are parsed but
remain unconnected to the proxy's compression behavior.

## Requirements

- Go 1.26 or newer
- macOS or Linux for the documented deployment targets
- Rust 1.96 or newer for the optional exporter

## License

MIT
