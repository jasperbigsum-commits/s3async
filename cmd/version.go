package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version is overridden with -ldflags -X for tagged release builds.
var version = "0.1.0"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version)
		},
	}
}
