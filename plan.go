package mince

import (
	"context"
	"fmt"
	"strings"

	"github.com/8b-is/mince/internal/compression/mincer"
	"github.com/8b-is/mince/internal/compression/plan"
)

// StageName identifies one stage in a caller-ordered mincing plan.
type StageName string

const (
	// StageDeterministic runs mince's built-in local summarizers.
	StageDeterministic StageName = "deterministic"
)

// Candidate is the current safe content passed between plan stages. A stage
// receives only candidate content and its classified kind, without a CCR ID
// or store. Before any reduction, candidate content is the original input.
type Candidate struct {
	Content []byte
	Kind    Kind
}

// StageFunc proposes a candidate for the next step. Returning an error, an
// empty result, or content that is not shorter leaves the previous candidate
// in place. The runner continues after ordinary errors and stops on context
// cancellation or deadline expiration.
type StageFunc func(context.Context, Candidate) (Candidate, error)

// Plan lists stage names in execution order. Stages run sequentially under the
// context supplied to MinceWithPlan.
type Plan struct {
	Stages []StageName
}

// StageRegistry maps caller-defined stage names to their implementations.
// StageDeterministic is built in and must not be registered.
type StageRegistry map[StageName]StageFunc

// DefaultPlan returns an explicit deterministic-only plan equivalent to Mince.
func DefaultPlan() Plan {
	return Plan{Stages: []StageName{StageDeterministic}}
}

// MinceWithPlan runs a validated, caller-ordered sequence of stages and
// finalizes the last safe candidate once. It returns an error only when the
// plan is invalid (including an empty plan, blank name, or unregistered stage)
// or the context is nil. Stage failures are treated as declines so a later
// stage can still improve the last safe candidate. Cancellation stops later
// stages and finalizes the last safe candidate, or passes through the original
// if none was accepted.
//
// Stage callbacks receive a copy of the current candidate, classified kind,
// and context. The initial candidate contains the original input; subsequent
// stages see the last accepted reduction. Callbacks must honor ctx for timely
// cancellation: the runner cannot interrupt a callback that ignores it.
// A successful finalization writes the original to the selected
// store at most once.
func MinceWithPlan(ctx context.Context, content []byte, opts Options, selected Plan, registry StageRegistry) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("mince: context must not be nil")
	}
	if len(selected.Stages) == 0 {
		return Result{}, fmt.Errorf("mince: plan must contain at least one stage")
	}
	if _, exists := registry[StageDeterministic]; exists {
		return Result{}, fmt.Errorf("mince: stage %q is reserved", StageDeterministic)
	}

	stages := make([]plan.Stage, 0, len(selected.Stages))
	for _, name := range selected.Stages {
		if strings.TrimSpace(string(name)) == "" {
			return Result{}, fmt.Errorf("mince: plan contains a blank stage name")
		}
		if name == StageDeterministic {
			stages = append(stages, plan.Stage{
				Name:          string(name),
				CanChangeKind: true,
				Run: func(ctx context.Context, candidate plan.Candidate) (plan.Candidate, error) {
					out := mincer.Transform(ctx, candidate.Content, mincer.TransformOptions{
						Kind:        toRouterKind(opts.Kind),
						MinBytes:    opts.MinBytes,
						MaxOutliers: opts.MaxOutliers,
					})
					return plan.Candidate{Content: out.Content, Kind: out.Kind}, nil
				},
			})
			continue
		}

		stage, ok := registry[name]
		if !ok || stage == nil {
			return Result{}, fmt.Errorf("mince: stage %q is not registered", name)
		}
		stages = append(stages, plan.Stage{
			Name: string(name),
			Run: func(ctx context.Context, current plan.Candidate) (plan.Candidate, error) {
				proposal, err := stage(ctx, Candidate{
					Content: current.Content,
					Kind:    fromRouterKind(current.Kind),
				})
				return plan.Candidate{
					Content: proposal.Content,
					Kind:    toRouterKind(proposal.Kind),
				}, err
			},
		})
	}

	result, err := plan.Execute(ctx, content, internalOptions(opts), stages)
	if err != nil {
		return Result{}, err
	}
	return fromInternalResult(result), nil
}
