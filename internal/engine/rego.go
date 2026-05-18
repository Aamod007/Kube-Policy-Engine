package engine

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/opa/rego"
)

type EvalResult struct {
	Allowed bool
	Deny    []string
	Error   error
}

type Engine interface {
	Eval(ctx context.Context, policy *Policy, input map[string]any) (EvalResult, error)
	Compile(regoSrc string) (*rego.PreparedEvalQuery, error)
}

type opaEngine struct{}

func NewEngine() Engine {
	return &opaEngine{}
}

func (e *opaEngine) Compile(regoSrc string) (*rego.PreparedEvalQuery, error) {
	ctx := context.Background()
	query, err := rego.New(
		rego.Query("data.k8spe.deny"),
		rego.Module("policy.rego", regoSrc),
	).PrepareForEval(ctx)
	if err != nil {
		return nil, err
	}
	return &query, nil
}

func (e *opaEngine) Eval(ctx context.Context, policy *Policy, input map[string]any) (EvalResult, error) {
	if policy.Compiled == nil {
		return EvalResult{}, fmt.Errorf("policy %s is not compiled", policy.Name)
	}

	rs, err := policy.Compiled.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return EvalResult{Error: err}, err
	}

	if len(rs) == 0 {
		// No rules matched
		return EvalResult{Allowed: true}, nil
	}

	var denies []string
	for _, result := range rs {
		for _, expr := range result.Expressions {
			// Expecting deny to return a set or array of strings
			if msgs, ok := expr.Value.([]any); ok {
				for _, msg := range msgs {
					if m, ok := msg.(string); ok {
						denies = append(denies, m)
					}
				}
			}
		}
	}

	return EvalResult{
		Allowed: len(denies) == 0,
		Deny:    denies,
	}, nil
}
