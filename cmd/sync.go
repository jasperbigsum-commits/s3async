package cmd

import (
	"fmt"

	"github.com/jasperbigsum-commits/s3async/internal/app"
	"github.com/spf13/cobra"
)

func newSyncCmd() *cobra.Command {
	var bucket string
	var prefix string
	var async bool

	cmd := &cobra.Command{
		Use:   "sync <source>",
		Short: "Create a sync task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bootstrap, err := app.NewBootstrap()
			if err != nil {
				return fmt.Errorf("create bootstrap: %w", err)
			}

			service := bootstrap.TaskService
			task, err := service.CreateTask(args[0], bucket, prefix, async)
			if err != nil {
				return fmt.Errorf("create task: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "task created: %s\n", task.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "status: %s\n", task.Status)
			return nil
		},
	}

	cmd.Flags().StringVar(&bucket, "bucket", "", "Target S3 bucket")
	cmd.Flags().StringVar(&prefix, "prefix", "", "Target S3 prefix")
	cmd.Flags().BoolVar(&async, "async", true, "Submit task in async mode")
	_ = cmd.MarkFlagRequired("bucket")

	return cmd
}
