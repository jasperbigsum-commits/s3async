package cmd

import "github.com/spf13/cobra"

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
}
