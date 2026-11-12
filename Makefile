.PHONY: build test race vet rust-test rust-build clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/mince ./cmd/mince

test:
	CGO_ENABLED=0 go test ./...

race:
	CGO_ENABLED=0 go test -race ./...

vet:
	go vet ./...

rust-test:
	cargo test --manifest-path tools/mlx-quant-kompress/Cargo.toml

rust-build:
	cargo build --release --manifest-path tools/mlx-quant-kompress/Cargo.toml

clean:
	rm -rf bin tools/mlx-quant-kompress/target
