.PHONY: build test race vet clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/mince ./cmd/mince

test:
	CGO_ENABLED=0 go test ./...

race:
	CGO_ENABLED=0 go test -race ./...

vet:
	go vet ./...

clean:
	rm -rf bin
