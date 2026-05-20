package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"kube-policy-engine/api/v1alpha1"
	"kube-policy-engine/internal/engine"
)

type TestCase struct {
	Name    string `json:"name"`
	Input   string `json:"input"`
	Expect  string `json:"expect"`
	Message string `json:"message,omitempty"`
}

type TestFile struct {
	Policy string     `json:"policy"`
	Cases  []TestCase `json:"cases"`
}

type Runner struct {
	Engine engine.Engine
}

func (r *Runner) RunTest(testFilePath string) (bool, error) {
	dir := filepath.Dir(testFilePath)

	testData, err := os.ReadFile(testFilePath)
	if err != nil {
		return false, fmt.Errorf("failed to read test file: %w", err)
	}

	var tf TestFile
	if err := yaml.Unmarshal(testData, &tf); err != nil {
		return false, fmt.Errorf("failed to parse test file: %w", err)
	}

	policyPath := filepath.Join(dir, "..", "policy.yaml")
	policyData, err := os.ReadFile(policyPath)
	if err != nil {
		return false, fmt.Errorf("failed to read policy.yaml: %w", err)
	}

	var policyCR v1alpha1.Policy
	if err := yaml.Unmarshal(policyData, &policyCR); err != nil {
		return false, fmt.Errorf("failed to parse policy.yaml: %w", err)
	}

	p := &engine.Policy{
		Name:    policyCR.Name,
		RegoSrc: policyCR.Spec.Rego,
	}

	compiled, err := r.Engine.Compile(p.RegoSrc)
	if err != nil {
		return false, fmt.Errorf("failed to compile rego: %w", err)
	}
	p.Compiled = compiled

	allPassed := true
	for _, tc := range tf.Cases {
		inputPath := filepath.Join(dir, tc.Input)
		inputData, err := os.ReadFile(inputPath)
		if err != nil {
			fmt.Printf("  ❌ [%s] failed to read input %s: %v\n", tc.Name, tc.Input, err)
			allPassed = false
			continue
		}

		var obj unstructured.Unstructured
		if err := yaml.Unmarshal(inputData, &obj.Object); err != nil {
			fmt.Printf("  ❌ [%s] failed to parse input: %v\n", tc.Name, err)
			allPassed = false
			continue
		}

		input := map[string]any{
			"request": map[string]any{
				"object": obj.Object,
			},
		}

		res, err := r.Engine.Eval(context.Background(), p, input)
		if err != nil {
			fmt.Printf("  ❌ [%s] evaluation error: %v\n", tc.Name, err)
			allPassed = false
			continue
		}

		actualResult := "allow"
		if !res.Allowed {
			actualResult = "deny"
		}

		if actualResult != tc.Expect {
			fmt.Printf("  ❌ [%s] expected %s, got %s\n", tc.Name, tc.Expect, actualResult)
			allPassed = false
			continue
		}

		if tc.Expect == "deny" && tc.Message != "" {
			if len(res.Deny) == 0 || res.Deny[0] != tc.Message {
				fmt.Printf("  ❌ [%s] expected message %q, got %v\n", tc.Name, tc.Message, res.Deny)
				allPassed = false
				continue
			}
		}

		fmt.Printf("  ✅ [%s]\n", tc.Name)
	}

	return allPassed, nil
}
