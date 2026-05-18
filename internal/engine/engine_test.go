package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"kube-policy-engine/internal/engine"
)

func TestEngineEval(t *testing.T) {
	regoSrc := `
package k8spe
deny[msg] {
	input.request.object.spec.privileged == true
	msg := "Privileged is not allowed"
}
`
	eng := engine.NewEngine()
	registry := engine.NewRegistry(eng)

	policy := &engine.Policy{
		Name:    "test-policy",
		Mode:    engine.Enforce,
		RegoSrc: regoSrc,
	}

	err := registry.Set(policy)
	assert.NoError(t, err)

	p, ok := registry.Get("test-policy")
	assert.True(t, ok)
	assert.NotNil(t, p.Compiled)

	// Test deny
	inputDeny := map[string]any{
		"request": map[string]any{
			"object": map[string]any{
				"spec": map[string]any{
					"privileged": true,
				},
			},
		},
	}
	res, err := eng.Eval(context.Background(), p, inputDeny)
	assert.NoError(t, err)
	assert.False(t, res.Allowed)
	assert.Contains(t, res.Deny, "Privileged is not allowed")

	// Test allow
	inputAllow := map[string]any{
		"request": map[string]any{
			"object": map[string]any{
				"spec": map[string]any{
					"privileged": false,
				},
			},
		},
	}
	res, err = eng.Eval(context.Background(), p, inputAllow)
	assert.NoError(t, err)
	assert.True(t, res.Allowed)
	assert.Empty(t, res.Deny)
}
