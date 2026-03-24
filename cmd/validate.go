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
			fmt.Fprintf(cmd.OutOrStdout(), "s3.region: %s\n", cfg.S3.Region)
			if cfg.S3.Bucket == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "s3.bucket: missing (set --config, config.yaml, or S3ASYNC_BUCKET)")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "s3.bucket: %s\n", cfg.S3.Bucket)
			}

			if cfg.S3.Endpoint == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "s3.endpoint: (default AWS S3)")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "s3.endpoint: %s\n", cfg.S3.Endpoint)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "s3.force_path_style: %v\n", cfg.S3.ForcePathStyle)

			// Handle TLS-related fields with "not applicable" for HTTP endpoints
			if cfg.S3.Endpoint != "" && !hasHTTPScheme(cfg.S3.Endpoint) {
				fmt.Fprintln(cmd.OutOrStdout(), "s3.skip_tls_verify: not applicable (http endpoint)")
				fmt.Fprintln(cmd.OutOrStdout(), "s3.ca_cert_file: not applicable (http endpoint)")
			} else if cfg.S3.SkipTLSVerify {
				fmt.Fprintf(cmd.OutOrStdout(), "s3.skip_tls_verify: %v\n", cfg.S3.SkipTLSVerify)
			} else if cfg.S3.CACertFile != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "s3.ca_cert_file: %s\n", cfg.S3.CACertFile)
			}

			if _, err := os.Stat(cfg.DatabasePath); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "database: not initialized yet (%v)\n", err)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "database: ok")
			}

			// Determine credentials source
			hasEnvCreds := os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != ""
			if cfg.Security.DryRun {
				fmt.Fprintln(cmd.OutOrStdout(), "credentials_source: dry_run")
			} else if cfg.S3.StaticCredentials.AccessKeyID != "" {
				fmt.Fprintln(cmd.OutOrStdout(), "credentials_source: static_credentials")
				fmt.Fprintln(cmd.OutOrStdout(), "static_credentials.access_key_id: configured")
				fmt.Fprintln(cmd.OutOrStdout(), "static_credentials.secret_access_key: (redacted)")
			} else if hasEnvCreds {
				fmt.Fprintln(cmd.OutOrStdout(), "credentials_source: environment")
			} else if cfg.S3.Profile != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "credentials_source: profile %s\n", cfg.S3.Profile)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "credentials_source: default chain")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "workers: %d\n", cfg.Workers)
			fmt.Fprintf(cmd.OutOrStdout(), "dry_run: %v\n", cfg.Security.DryRun)
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Path to config file")
	return cmd
}

func hasHTTPScheme(endpoint string) bool {
	return len(endpoint) >= 5 && (endpoint[:5] == "https" || endpoint[:4] == "http")
}
