package plan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/8b-is/mince/internal/compression/mincer"
	"github.com/8b-is/mince/internal/compression/router"
)

// Candidate is the content currently accepted by the pipeline. The runner
// clones it before calling a stage so mutations cannot damage the saved copy.
type Candidate struct {
	Content []byte
	Kind    router.Kind
}

// Stage transforms the current candidate. An error means this stage declined;
// the runner retains the previous candidate and continues unless ctx ended.
type Stage struct {
	Name          string
	Run           func(context.Context, Candidate) (Candidate, error)
	CanChangeKind bool
}

// Execute runs stages in order and calls the mincer finalizer exactly once.
// Only non-empty strict reductions are accepted as content changes. Caller
// stages must preserve the content kind; the built-in deterministic stage may
// update classification without changing bytes.
func Execute(ctx context.Context, original []byte, opts mincer.Options, stages []Stage) (mincer.Result, error) {
	if ctx == nil {
		return mincer.Result{}, fmt.Errorf("plan: context must not be nil")
	}
	if len(stages) == 0 {
		return mincer.Result{}, fmt.Errorf("plan: at least one stage is required")
	}
	for _, stage := range stages {
		if stage.Name == "" || stage.Run == nil {
			return mincer.Result{}, fmt.Errorf("plan: stage name and implementation are required")
		}
	}

	needsClassification := false
	for _, stage := range stages {
		if !stage.CanChangeKind {
			needsClassification = true
			break
		}
	}
	kind := router.KindText
	if needsClassification {
		kind = opts.Kind
		if kind == router.KindText {
			kind = router.Classify(original)
		}
	}
	current := Candidate{Content: clone(original), Kind: kind}
	for _, stage := range stages {
		if ctx.Err() != nil {
			break
		}

		proposal, err := stage.Run(ctx, Candidate{Content: clone(current.Content), Kind: current.Kind})
		if ctx.Err() != nil {
			break
		}
		if err != nil || len(proposal.Content) == 0 {
			continue
		}
		if !stage.CanChangeKind && proposal.Kind != current.Kind {
			continue
		}
		if len(proposal.Content) == len(current.Content) {
			if stage.CanChangeKind && bytes.Equal(proposal.Content, current.Content) {
				current.Kind = proposal.Kind
			}
			continue
		}
		if len(proposal.Content) > len(current.Content) {
			continue
		}
		// The finalizer embeds retrieval metadata in a JSON object. Reject an
		// unrenderable proposal here so it cannot displace a usable candidate.
		if proposal.Kind == router.KindJSON {
			var object map[string]json.RawMessage
			if json.Unmarshal(proposal.Content, &object) != nil || object == nil {
				continue
			}
		}
		current = Candidate{Content: clone(proposal.Content), Kind: proposal.Kind}
	}

	return mincer.Finalize(original, mincer.Candidate{
		Content: current.Content,
		Kind:    current.Kind,
		Changed: len(current.Content) < len(original),
	}, opts.Store), nil
}

func clone(content []byte) []byte {
	return append([]byte(nil), content...)
}
