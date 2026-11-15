package mince

import (
	"context"

	"github.com/8b-is/mince/internal/compression/mincer"
	"github.com/8b-is/mince/internal/compression/router"
	"github.com/8b-is/mince/internal/storage/ccr"
)

// Kind selects the content family to process. KindAuto lets mince classify
// the input. Unknown kinds are treated as automatic classification.
type Kind string

const (
	KindAuto Kind = "auto"
	KindJSON Kind = "json"
	KindLog  Kind = "log"
	KindCode Kind = "code"
)

// Store wraps a CCR store without exposing mince's internal storage package.
// Create one with OpenStore or NewMemoryStore and reuse it across calls.
type Store struct {
	inner *ccr.Store
}

var processStore = &Store{inner: ccr.NewMemory()}

// OpenStore opens a disk-backed store at dir. An empty dir creates a memory
// store. Disk-backed stores retain originals across process restarts.
func OpenStore(dir string) (*Store, error) {
	store, err := ccr.Open(dir)
	if err != nil {
		return nil, err
	}
	return &Store{inner: store}, nil
}

// NewMemoryStore creates an in-memory store. Its contents are lost when the
// process exits.
func NewMemoryStore() *Store {
	return &Store{inner: ccr.NewMemory()}
}

// DefaultStore returns the process-wide in-memory store used when Options.Store
// is nil. Applications that need independent stores should create their own.
func DefaultStore() *Store { return processStore }

func (s *Store) storage() *ccr.Store {
	if s == nil || s.inner == nil {
		return processStore.inner
	}
	return s.inner
}

// Get returns the exact original bytes stored under id. The id may include the
// "ccr_" prefix returned by Mince.
func (s *Store) Get(id string) ([]byte, error) {
	return s.storage().Get(stripCCRPrefix(id))
}

// Retrieve returns exact original lines for id. A non-empty query is treated
// as a regular expression when valid, or as a literal substring otherwise.
// Offset and limit select lines from the matching results; limit <= 0 returns
// all remaining lines. The id may include the "ccr_" prefix returned by Mince.
func (s *Store) Retrieve(id, query string, offset, limit int) (string, error) {
	return s.storage().Retrieve(stripCCRPrefix(id), query, offset, limit)
}

func stripCCRPrefix(id string) string {
	if len(id) > 4 && id[:4] == "ccr_" {
		return id[4:]
	}
	return id
}

// Options controls one deterministic mincing operation.
type Options struct {
	// Kind chooses auto, JSON, log, or code processing. The zero value is auto.
	Kind Kind
	// MinBytes is the smallest input size to consider. Values <= 0 use 1024.
	MinBytes int
	// Store receives exact originals for accepted shortened results. Nil uses
	// the process-wide in-memory store.
	Store *Store
	// MaxOutliers limits the exceptional values retained in JSON summaries.
	// Zero uses the mincer's default.
	MaxOutliers int
}

// Result describes a mincing attempt. Minced is the content to forward. When
// Passthrough is true, Minced contains the original input unchanged and CCRID
// is empty. A non-empty CCRID identifies the exact original in the selected
// Store. SavedFraction is an exact fraction such as "3/5".
type Result struct {
	Minced        []byte
	CCRID         string
	RawBytes      int
	MincedBytes   int
	Kind          Kind
	SavedFraction string
	SavedPercent  string
	Passthrough   bool
}

// Mince applies deterministic local summarizers to eligible content. The
// candidate must save at least 10% before retrieval metadata is added, and the
// final output must be smaller than the input. Small, unsupported, cancelled,
// or otherwise ineligible inputs pass through unchanged. The zero Options
// value uses automatic classification and a 1 KiB minimum. The original is
// retained in opts.Store or DefaultStore when the result is shortened.
func Mince(ctx context.Context, content []byte, opts Options) Result {
	result := mincer.Mince(ctx, content, internalOptions(opts))
	return fromInternalResult(result)
}

// fromInternalResult converts the internal result without exposing storage or
// compression implementation types in the public package.
func fromInternalResult(result mincer.Result) Result {
	// The internal result uses the zero value for untouched inputs. Expose a
	// well-formed exact ratio instead of leaking its zero denominator.
	savedFraction := "0/1"
	savedPercent := "0.00%"
	if result.Saved.Den != 0 {
		savedFraction = result.Saved.String()
		savedPercent = result.Saved.Percent()
	}

	return Result{
		Minced:        result.Minced,
		CCRID:         result.CCRID,
		RawBytes:      result.RawBytes,
		MincedBytes:   result.MincedBytes,
		Kind:          fromRouterKind(result.Kind),
		SavedFraction: savedFraction,
		SavedPercent:  savedPercent,
		Passthrough:   result.Passthrough,
	}
}

func internalOptions(opts Options) mincer.Options {
	var store *ccr.Store
	if opts.Store != nil {
		store = opts.Store.storage()
	} else {
		store = processStore.inner
	}
	return mincer.Options{
		Kind:        toRouterKind(opts.Kind),
		MinBytes:    opts.MinBytes,
		Store:       store,
		MaxOutliers: opts.MaxOutliers,
	}
}

func toRouterKind(kind Kind) router.Kind {
	switch kind {
	case KindJSON:
		return router.KindJSON
	case KindLog:
		return router.KindLog
	case KindCode:
		return router.KindCode
	default:
		return router.KindText
	}
}

func fromRouterKind(kind router.Kind) Kind {
	switch kind {
	case router.KindJSON:
		return KindJSON
	case router.KindLog:
		return KindLog
	case router.KindCode:
		return KindCode
	default:
		return KindAuto
	}
}
