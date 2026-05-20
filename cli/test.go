package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"kube-policy-engine/internal/engine"
)

var testCmd = &cobra.Command{
	Use:   "test [path]",
	Short: "Run policy tests",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := args[0]

		eng := engine.NewEngine()
		runner := &Runner{Engine: eng}

		var testFiles []string

		stat, err := os.Stat(path)
		if err != nil {
			return err
		}

		if stat.IsDir() {
			err = filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() && filepath.Base(p) == "test.yaml" {
					testFiles = append(testFiles, p)
				}
				return nil
			})
			if err != nil {
				return err
			}
		} else {
			testFiles = append(testFiles, path)
		}

		allPassed := true
		for _, tf := range testFiles {
			fmt.Printf("Running tests in %s\n", tf)
			passed, err := runner.RunTest(tf)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				allPassed = false
			}
			if !passed {
				allPassed = false
			}
			fmt.Println()
		}

		if !allPassed {
			os.Exit(1)
		}

		return nil
	},
}

func init() {
	RootCmd.AddCommand(testCmd)
}
