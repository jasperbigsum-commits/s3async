package cmd

import (
	"fmt"
	"os"

	"github.com/jasperbigsum-commits/s3async/internal/app"
	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration and environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			bootstrap, err := app.NewBootstrapWithConfig(configPath)
			if err != nil {
				return fmt.Errorf("create bootstrap: %w", err)
			}

			cfg := bootstrap.Config
			fmt.Fprintf(cmd.OutOrStdout(), "database_path: %s\n", cfg.DatabasePath)
			fmt.Fprintf(cmd.OutOrStdout(), "region: %s\n", cfg.Region)
			if cfg.Bucket == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "bucket: missing (set --config, config.yaml, or S3ASYNC_BUCKET)")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "bucket: %s\n", cfg.Bucket)
			}

			if _, err := os.Stat(cfg.DatabasePath); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "database: not initialized yet (%v)\n", err)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "database: ok")
			}

			hasEnvCreds := os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != ""
			if hasEnvCreds {
				fmt.Fprintln(cmd.OutOrStdout(), "aws_credentials: found in environment")
			} else if cfg.Profile != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "aws_credentials: will attempt profile %s\n", cfg.Profile)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "aws_credentials: not obvious from environment; AWS default chain may still resolve credentials")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "workers: %d\n", cfg.Workers)
			fmt.Fprintf(cmd.OutOrStdout(), "dry_run: %v\n", cfg.Security.DryRun)
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Path to config file")
	return cmd
}
