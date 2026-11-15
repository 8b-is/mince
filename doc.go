// Package mince provides the supported Go API for reducing eligible tool
// output and retrieving its exact original content.
//
// The default behavior is deterministic and local. Small, unsupported, or
// unrecognized inputs pass through unchanged. Mince keeps originals in a
// Content-Coupled Retrieval (CCR) store and returns a CCR ID when it shortens
// content. Use OpenStore to keep originals on disk across process restarts, or
// NewMemoryStore for process-local storage. If no store is supplied, Mince
// uses a shared in-memory store for this process.
//
// The module path is github.com/8b-is/mince.
package mince
