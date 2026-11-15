# Go library

Applications import `github.com/8b-is/mince`. The supported API lives in the
root `mince` package; packages under `internal` are implementation details.

## Mince and retrieve

Use a disk-backed store to retrieve originals after the process exits. A
memory store is useful when records only need to live for the current process.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/8b-is/mince"
)

func main() {
	store, err := mince.OpenStore("./mince-ccr")
	if err != nil {
		log.Fatal(err)
	}

	result := mince.Mince(context.Background(), []byte("large tool output..."), mince.Options{
		Kind:  mince.KindAuto,
		Store: store,
	})
	fmt.Printf("shortened=%t ccr=%s\n", !result.Passthrough, result.CCRID)

	if result.CCRID != "" {
		original, err := store.Get(result.CCRID)
		if err != nil {
			log.Fatal(err)
		}
		_ = original // exact original bytes
	}
}
```

## Defaults and results

- The zero `Options` value classifies input automatically and considers content
  at least 1 KiB long.
- Mince uses deterministic local JSON, log, and code summarizers. It does not
  call a model.
- Small, unsupported, unrecognized, or weak-saving inputs pass through
  unchanged. Check `Result.Passthrough` before treating a result as shortened.
- A shortened result has a `CCRID` that can be passed to `Store.Get` for exact
  bytes or `Store.Retrieve` for selected lines.
- `Result.SavedFraction` is an exact fraction; `Result.SavedPercent` is a
  display value.
- If `Options.Store` is nil, the package uses one process-wide in-memory store.
  Use `OpenStore` to persist originals on disk or `NewMemoryStore` for a
  separate in-memory store.

`Mince` is the current deterministic library entry point. Sequential model
stages and adaptive plan selection are planned extensions and are not exposed
by this API yet.
