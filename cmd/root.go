package cmd

import (
	"fmt"

	"github.com/jasperbigsum-commits/s3async/internal/app"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "s3async",
	Short: "Asynchronous S3 sync CLI for Windows and Linux",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.SilenceUsage = true
	rootCmd.AddCommand(newSyncCmd())
	rootCmd.AddCommand(newTaskCmd())
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.AddCommand(newVersionCmd())
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "version" {
			return nil
		}

		_, err := app.NewBootstrap()
		if err != nil {
			return fmt.Errorf("bootstrap failed: %w", err)
		}

		return nil
	}
}
