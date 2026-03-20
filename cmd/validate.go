package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration and environment",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "validation placeholder: config and AWS checks will be added in next iteration")
		},
	}
}
