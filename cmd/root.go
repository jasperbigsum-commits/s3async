package cmd

import "github.com/spf13/cobra"

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "s3async",
		Short: "Asynchronous S3 sync CLI for Windows and Linux",
	}
	rootCmd.SilenceUsage = true
	rootCmd.AddCommand(newSyncCmd())
	rootCmd.AddCommand(newTaskCmd())
	rootCmd.AddCommand(newDaemonCmd())
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.AddCommand(newVersionCmd())
	return rootCmd
}

func Execute() error {
	return NewRootCmd().Execute()
}
