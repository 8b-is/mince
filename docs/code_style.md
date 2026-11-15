# Go and Rust code style

The project follows each language's usual formatter and standard lint tools.
The goal is consistent, readable code and checks that catch likely mistakes;
we do not enable every optional lint or enforce personal style preferences.

## Go

- Format Go files with `gofmt`.
- Run `golangci-lint` using the checked-in v2 configuration. It enables the
  standard linter set, including `govet`, `staticcheck`, `errcheck`,
  `ineffassign`, and `unused`, and checks `gofmt` formatting.
- Keep cyclomatic complexity at **15 or below per function**. The `cyclop`
  linter counts decision points such as branches, loops, and boolean conditions.
  Split functions with too many decisions into focused helpers. This applies
  to production and test code; package-average complexity is not enforced.
  The threshold is a project policy, not a requirement of the Go language.
- Keep exported names documented, use MixedCaps, pass `context.Context`
  explicitly as the first argument where needed, and handle returned errors.
- Keep packages focused and package names short; avoid generic names such as
  `util` or `common`.

The CI action reports only new lint findings so existing code can be cleaned
up incrementally. The selected linter set and version are pinned in CI for
repeatable results.

A full local `golangci-lint run` also reports existing complexity violations;
the PR check's existing new-findings filter does not require a repository-wide
refactor when this rule is introduced.

## Rust

- Use the Rust 2021 edition declared in the crate manifest.
- Run Clippy on all targets. The crate enables the `clippy::all` lint group;
  CI treats warnings as errors so new findings appear as a failing Rust lint
  check.
- Prefer clear error propagation with `Result` and `?`; use `unwrap` only when
  the invariant is guaranteed and documented by nearby checks.
- Use names and types that make ownership and data flow clear. Avoid adding
  allowances for a lint unless the code explains why the suggestion is not
  appropriate.

The CI uses Rust's stable toolchain. We do not enable the Clippy `pedantic`
group globally because it includes stricter suggestions that are not always
useful for this small standalone exporter.

## Run the checks locally

```sh
golangci-lint run
go vet ./...
cargo clippy --manifest-path tools/mlx-quant-kompress/Cargo.toml --all-targets -- -D warnings
```

References: [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments),
[Effective Go](https://go.dev/doc/effective_go),
[golangci-lint configuration](https://golangci-lint.run/docs/configuration/file/),
and [Clippy lint groups](https://doc.rust-lang.org/clippy/index.html).
