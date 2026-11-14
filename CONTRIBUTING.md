# 8b-is Contributor Guidelines

## Welcome
Thank you for interest in contributing to 8b-is organization repositories. This document governs all contributions across `8b-is/*` repos.

## Contributor Requirements

### 1. Identity Verification
All contributors must verify human identity before receiving write access:
- **WebAuthn/FIDO2 passkey**: Register a hardware authenticator (YubiKey, Apple Secure Enclave, Android StrongBox)
- **Erdős number onboarding**: Receive 1 initial voucher from an existing contributor (max 2 vouchers per 30-day epoch)
- **Identity cap**: No more than 2 active vouchers per contributor per 30-day epoch

### 2. Code Standards
All contributions must adhere to:
- **`#![no_std]` compliance** where applicable (Rust crates)
- **`CGO_ENABLED=0`** for Go projects (no external C dependencies)
- **Exact `p/q` rationals** — no `f64`/`f32` in core arithmetic logic (clippy::float_arithmetic forbidden)
- **Balanced ternary** primitives where relevant (`Trit {-1,0,+1}`, `TritPlane64`, `PackedTrit5`)
- **Clippy `-D warnings`** — zero warnings across all targets
- **Test coverage**: New features must include unit tests; bug fixes must include regression tests

### 3. Pull Request Process
1. **Fork** the target repository (read-only for readers; write access granted after voucher)
2. **Branch** from `main` with descriptive name: `feat/xyz`, `fix/kappa-threshold`, `docs/update-readme`
3. **Run checks**: `cargo test --all-targets`, `CGO_ENABLED=0 go test -race ./...`, `clippy -D warnings`
4. **Squash & merge**: Squash commits into logical units; squash commit msg follows Conventional Commits format
5. **Admins** (`8bit-wraith`, `poe757`, `standardgalactic`) review and merge

### 4. Documentation Requirements
Every merge must update or add:
- `README.md` (if API/behavior changed)
- `AGENTS.md` (if new agents/hooks added)
- `CHANGELOG.md` ( Conventional Commits format, auto-generated from merge commits)

### 5. Prohibited
- `float_arithmetic` in core logic (clippy error)
- `unsafe_code` in `pivot-abi`, `mem8-wave`, `regularity-oracle`, `phoenix-capsule`
- Circular dependencies across the 5 founder crates
- Deprecation of `compile_fail` doctests without replacement

## Admin Responsibilities

### Admins: `8bit-wraith`, `poe757`, `standardgalactic`
- Review all PRs within 48 hours
- Grant/revoke Erdős vouchers (max 2/30-day per contributor)
- Merge or request changes on PRs
- Tag releases following [Semantic Versioning](https://semver.org/)
- Maintain Codeberg mirror (`codeberg/main`) sync with GitHub (`origin/main`)

### Revoke Policy
- Voucher revocation: If invitee caught piping automated LLM slop into `+1` human lane, voucher frozen for 90 days; sibling voucher weights halved on that branch
- Access revocation: If contributor violates Code of Conduct, all write access suspended pending review

## Questions?
Open a discussion on the target repository or contact an admin directly.
