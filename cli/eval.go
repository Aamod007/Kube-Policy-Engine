package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"kube-policy-engine/api/v1alpha1"
	"kube-policy-engine/internal/engine"
)

var (
	evalPolicy string
	evalInput  string
)

var evalCmd = &cobra.Command{
	Use:   "eval",
	Short: "Evaluate one resource against a policy",
	RunE: func(cmd *cobra.Command, args []string) error {
		if evalPolicy == "" || evalInput == "" {
			return fmt.Errorf("--policy and --input flags are required")
		}

		eng := engine.NewEngine()

		// 1. Find policy
		policyPath := filepath.Join("policies", evalPolicy, "policy.yaml")
		policyData, err := os.ReadFile(policyPath)
		if err != nil {
			return fmt.Errorf("failed to read policy.yaml: %w", err)
		}

		var policyCR v1alpha1.Policy
		if err := yaml.Unmarshal(policyData, &policyCR); err != nil {
			return fmt.Errorf("failed to parse policy.yaml: %w", err)
		}

		p := &engine.Policy{
			Name:    policyCR.Name,
			RegoSrc: policyCR.Spec.Rego,
		}

		compiled, err := eng.Compile(p.RegoSrc)
		if err != nil {
			return fmt.Errorf("failed to compile rego: %w", err)
		}
		p.Compiled = compiled

		// 2. Read Input
		inputData, err := os.ReadFile(evalInput)
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		var obj unstructured.Unstructured
		if err := yaml.Unmarshal(inputData, &obj.Object); err != nil {
			return fmt.Errorf("failed to parse input YAML/JSON: %w", err)
		}

		input := map[string]any{
			"request": map[string]any{
				"object": obj.Object,
			},
		}

		// 3. Evaluate
		res, err := eng.Eval(context.Background(), p, input)
		if err != nil {
			return fmt.Errorf("evaluation error: %w", err)
		}

		if res.Allowed {
			fmt.Println("Result: ALLOW")
		} else {
			fmt.Println("Result: DENY")
			if len(res.Deny) > 0 {
				msgBytes, _ := json.MarshalIndent(res.Deny, "", "  ")
				fmt.Println("Messages:")
				fmt.Println(string(msgBytes))
			}
		}

		return nil
	},
}

func init() {
	evalCmd.Flags().StringVar(&evalPolicy, "policy", "", "Policy name (directory name in ./policies/)")
	evalCmd.Flags().StringVar(&evalInput, "input", "", "Path to resource JSON/YAML")
	RootCmd.AddCommand(evalCmd)
}
