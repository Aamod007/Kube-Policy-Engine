package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"kube-policy-engine/api/v1alpha1"
	"kube-policy-engine/internal/engine"
)

var lintCmd = &cobra.Command{
	Use:   "lint [path]",
	Short: "Lint policy YAML and check Rego syntax",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := args[0]

		eng := engine.NewEngine()

		var policyFiles []string

		stat, err := os.Stat(path)
		if err != nil {
			return err
		}

		if stat.IsDir() {
			err = filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() && filepath.Base(p) == "policy.yaml" {
					policyFiles = append(policyFiles, p)
				}
				return nil
			})
			if err != nil {
				return err
			}
		} else {
			policyFiles = append(policyFiles, path)
		}

		allPassed := true
		for _, pf := range policyFiles {
			fmt.Printf("Linting %s...\n", pf)

			policyData, err := os.ReadFile(pf)
			if err != nil {
				fmt.Printf("  ❌ Failed to read file: %v\n", err)
				allPassed = false
				continue
			}

			var policyCR v1alpha1.Policy
			if err := yaml.Unmarshal(policyData, &policyCR); err != nil {
				fmt.Printf("  ❌ Invalid YAML or schema: %v\n", err)
				allPassed = false
				continue
			}

			if policyCR.Spec.Rego == "" {
				fmt.Printf("  ❌ Missing Rego source code in spec.rego\n")
				allPassed = false
				continue
			}

			_, err = eng.Compile(policyCR.Spec.Rego)
			if err != nil {
				fmt.Printf("  ❌ Invalid Rego syntax: %v\n", err)
				allPassed = false
				continue
			}

			fmt.Printf("  ✅ Passed\n")
		}

		if !allPassed {
			os.Exit(1)
		}

		return nil
	},
}

func init() {
	RootCmd.AddCommand(lintCmd)
}
