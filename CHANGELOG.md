# Changelog

All notable changes to `8b-is` organization repositories will be documented in this file.

## Format

This project adheres to [Semantic Versioning](https://semver.org/) and [Conventional Commits](https://conventionalcommits.org/).

### Unreleased
- ### Added
- ### Changed
- ### Deprecated
- ### Removed
- ### Fixed
- ### Security

### [0.2.0] - 2026-11-12
- **Added**: `mince` v0.2.0 — pure Go context-compression proxy, agent wrapper, MCP server
- **Added**: `felix-simd` v0.2.0 — V_3 zero-multiply ternary & bitboard kernels
- **Added**: `phoenix-capsule` v0.2.0 — linear typestate Capsule (Sealed→Active→Consumed), compile_fail doctest
- **Added**: `regularity-oracle` v0.2.0 — weighted p/q ternary Laplacian, Delta_H3==0 invariant
- **Added**: `mem8-wave` v0.2.0 — kompress-super-ultra 5-stage FOLD pipeline
- **Added**: `pivot-abi` v0.2.0 — frozen central mediator, RationalPQ, Fp31 Mersenne, balanced ternary
- **Added**: `FRONTEND_ARCHITECTURE.md` — Go templ SSR + Rust/Wasm islands design
- **Added**: `CONTRIBUTING.md` — 8b-is contributor guidelines with Erdős voucher system
- **Added**: `AGENTS.md` — OMNI-peter harness mesh shared instruction

### [0.1.0] - 2026-11-12
- **Added**: Initial workspace scaffold; `Cargo.toml` workspace config with members
- **Added**: `mince` Go module with all internal packages (pq, ternary, ccr, router, mincer, aligner, proxy, mcp, wrap, cmd)
- **Added**: Rust helio-core workspace (pivot-abi, felix-simd, mem8-wave, regularity-oracle, phoenix-capsule)
- **Added**: Go↔Rust zero-copy mmap ring buffer bridge (CGO_ENABLED=0)
