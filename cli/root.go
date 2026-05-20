package cli

import (
	"github.com/spf13/cobra"
)

var RootCmd = &cobra.Command{
	Use:   "policy",
	Short: "kube-policy-engine CLI tool",
}
