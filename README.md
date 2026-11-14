# 8b-is Organization

## Overview
The **8b-is** organization hosts sovereign, privacy-respecting, and computationally verified open-source projects focused on:
- **Balanced ternary arithmetic** and exact `p/q` rational computation (`#![no_std]` Rust)
- **Context-compression proxy** (`mince`) for AI coding agent output reduction
- **Erdős–Lódri UltraGraph** theory and application (`helio-core` Rust workspace)
- **HelioGraph** social network protocol (3-realm: Human `+1`, Hybrid `0`, LLM `-1`)
- **Cryptographic capability gates** via WebAuthn/FIDO2 and Erdős number vouching

## Repositories

### Private Repositories (Invite-Only)

| Repo | Description | Admins |
|------|-------------|--------|
| `mince` | Pure Go `CGO_ENABLED=0` context-compression proxy, agent wrapper, MCP server | `8bit-wraith`, `poe757`, `standardgalactic` |
| `felix` | `#![no_std]` Rust cryptographic core engine (Fp31 Mersenne, Trotter graphs, kompress-super-ultra) | `8bit-wraith`, `poe757` |
| `felix-codegen` | Local companion to `felix` without remote Git; compiles `felix` crates and pushes to `origin` + `codeberg` | `standardgalactic` |
| `smart-tree` | Main MCP server (`st --mcp`) across all projects; semantic file discovery, project context, git status, `.m8` consciousness | `8bit-wraith` |
| `space-bender` | Codeberg + GitHub mirror (`8b-is/*`); Codewars API v1 kata engine | `poe757` |
| `wukong-zeno` | OS-agnostic `#![no_std]` 5-crate Rust workspace: `pivot-abi` + `felix-simd` + `mem8-wave` + `regularity-oracle` + `phoenix-capsule` | `standardgalactic` |
| `omni-todos` | REST + MCP server at `127.0.0.1:8099`; API `POST /api/todos` for dependency resolution sync | `8bit-wraith` |

### Public Mirroring Policy
- `felix` and `space-bender` mirrored across **GitHub** (`origin/main`) and **Codeberg** (`codeberg/main`)
- All `8b-is/*` repos on **GitHub** have a Codeberg `codeberg/main` counterpart
- Forks welcome under [8b-is Contributor Guidelines](CONTRIBUTING.md)

## Code of Conduct
- **Sovereign first**: All projects respect user sovereignty, `#![no_std]` where applicable, and zero IEEE-754 float drift
- **No malicious code**: Security review required before any dependency addition
- **Erdős number vouching**: Write access to `+1` Human lane requires existing human voucher (max 2 vouchers/30-day epoch)
- ** publik discussion**: Technical discourse on GitHub Discussions; private matters via direct mention

## Quick Start

### Clone a repo
```bash
git clone git@github.com:8b-is/<repo>.git
# or via HTTPS (for environments without SSH key auth):
git clone https://github.com/8b-is/<repo>.git
```

### First-time setup
```bash
# 1. Generate Go SSR components
templ generate ./internal/ui/...

# 2. Compile #![no_std] Rust enclave to tiny WebAssembly
cargo build --release -p helio-wasm --target wasm32-unknown-unknown
wasm-opt -Oz target/wasm32-unknown-unknown/release/helio_wasm.wasm -o static/helio.wasm

# 3. Compile pure-Go HTTP/3 + WebAuthn + Debrid + SSR gateway
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/helio-edge ./cmd/helio-edge

# 4. Verify
./bin/helio-edge server --addr 127.0.0.1:8787
# Expected: HTTP 200 at /_mince/healthz {"status":"ok","version":"0.2.0"}
```

