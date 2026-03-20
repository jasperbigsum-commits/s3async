package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jasperbigsum-commits/s3async/internal/app"
	"github.com/jasperbigsum-commits/s3async/internal/filter"
	"github.com/jasperbigsum-commits/s3async/internal/scanner"
	"github.com/jasperbigsum-commits/s3async/internal/task"
	"github.com/jasperbigsum-commits/s3async/internal/uploader"
	"github.com/spf13/cobra"
)

func newSyncCmd() *cobra.Command {
	var bucket string
	var prefix string
	var async bool
	var configPath string
	var include []string
	var exclude []string

	cmd := &cobra.Command{
		Use:   "sync <source>",
		Short: "Create a sync task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bootstrap, err := app.NewBootstrapWithConfig(configPath)
			if err != nil {
				return fmt.Errorf("create bootstrap: %w", err)
			}

			source := args[0]
			resolvedBucket := bucket
			if resolvedBucket == "" {
				resolvedBucket = bootstrap.Config.Bucket
			}
			resolvedPrefix := prefix
			if resolvedPrefix == "" {
				resolvedPrefix = bootstrap.Config.Prefix
			}
			resolvedInclude := include
			if len(resolvedInclude) == 0 {
				resolvedInclude = bootstrap.Config.Filters.Include
			}
			resolvedExclude := exclude
			if len(resolvedExclude) == 0 {
				resolvedExclude = bootstrap.Config.Filters.Exclude
			}
			if resolvedBucket == "" {
				return fmt.Errorf("bucket is required via --bucket, config file, or S3ASYNC_BUCKET")
			}

			entries, err := scanner.Scan(source)
			if err != nil {
				return fmt.Errorf("scan source: %w", err)
			}

			items := make([]task.Item, 0, len(entries))
			for _, entry := range entries {
				if !filter.Match(entry.RelativePath, resolvedInclude, resolvedExclude) {
					continue
				}
				items = append(items, task.Item{
					Path:         entry.Path,
					RelativePath: entry.RelativePath,
					Size:         entry.Size,
					Status:       task.ItemStatusPending,
				})
			}

			service := bootstrap.TaskService
			createdTask, err := service.CreateTask(source, resolvedBucket, resolvedPrefix, async, items)
			if err != nil {
				return fmt.Errorf("create task: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "task created: %s\n", createdTask.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "status: %s\n", createdTask.Status)
			fmt.Fprintf(cmd.OutOrStdout(), "planned items: %d\n", len(items))

			if async || len(items) == 0 {
				return nil
			}

			uploaderClient, err := uploader.New(context.Background(), bootstrap.Config)
			if err != nil {
				return fmt.Errorf("create uploader client: %w", err)
			}
			for _, item := range items {
				key := item.RelativePath
				if resolvedPrefix != "" {
					key = filepath.ToSlash(filepath.Join(resolvedPrefix, item.RelativePath))
				}

				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				err = uploaderClient.UploadFile(ctx, resolvedBucket, key, item.Path)
				cancel()
				if err != nil {
					return fmt.Errorf("upload file %s: %w", item.Path, err)
				}
			}

			fmt.Fprintln(cmd.OutOrStdout(), "foreground upload completed")
			return nil
		},
	}

	cmd.Flags().StringVar(&bucket, "bucket", "", "Target S3 bucket")
	cmd.Flags().StringVar(&prefix, "prefix", "", "Target S3 prefix")
	cmd.Flags().BoolVar(&async, "async", true, "Submit task in async mode")
	cmd.Flags().StringVar(&configPath, "config", "", "Path to config file")
	cmd.Flags().StringSliceVar(&include, "include", nil, "Include glob patterns")
	cmd.Flags().StringSliceVar(&exclude, "exclude", nil, "Exclude glob patterns")

	return cmd
}
