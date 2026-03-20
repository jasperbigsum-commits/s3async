package cmd

import (
	"fmt"
	"time"

	"github.com/jasperbigsum-commits/s3async/internal/app"
	taskpkg "github.com/jasperbigsum-commits/s3async/internal/task"
	"github.com/spf13/cobra"
)

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "task", Short: "Manage sync tasks"}
	cmd.AddCommand(newTaskListCmd())
	cmd.AddCommand(newTaskStatusCmd())
	cmd.AddCommand(newTaskRetryCmd())
	cmd.AddCommand(newTaskRunCmd())
	cmd.AddCommand(newTaskWorkerCmd())
	return cmd
}

func newTaskListCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			bootstrap, err := app.NewBootstrapWithConfig(configPath)
			if err != nil {
				return fmt.Errorf("create bootstrap: %w", err)
			}

			tasks, err := bootstrap.TaskService.ListTasks()
			if err != nil {
				return fmt.Errorf("list tasks: %w", err)
			}

			for _, task := range tasks {
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"%s\t%s\titems=%d/%d failed=%d pending=%d running=%d\tupdated=%s\tsource=%s\n",
					task.ID,
					task.Status,
					task.SuccessItems+task.SkippedItems,
					task.TotalItems,
					task.FailedItems,
					task.PendingItems,
					task.UploadingItems,
					formatTimestamp(task.UpdatedAt),
					task.Source,
				)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Path to config file")
	return cmd
}

func newTaskStatusCmd() *cobra.Command {
	var configPath string
	var failedLimit int

	cmd := &cobra.Command{
		Use:   "status <task-id>",
		Short: "Show task status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bootstrap, err := app.NewBootstrapWithConfig(configPath)
			if err != nil {
				return fmt.Errorf("create bootstrap: %w", err)
			}

			t, err := bootstrap.TaskService.GetTask(args[0])
			if err != nil {
				return fmt.Errorf("get task: %w", err)
			}

			items, err := bootstrap.TaskService.ListTaskItems(args[0])
			if err != nil {
				return fmt.Errorf("list task items: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "id: %s\n", t.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "status: %s\n", t.Status)
			fmt.Fprintf(cmd.OutOrStdout(), "source: %s\n", t.Source)
			fmt.Fprintf(cmd.OutOrStdout(), "bucket: %s\n", t.Bucket)
			fmt.Fprintf(cmd.OutOrStdout(), "prefix: %s\n", t.Prefix)
			fmt.Fprintf(cmd.OutOrStdout(), "items_total: %d\n", t.TotalItems)
			fmt.Fprintf(cmd.OutOrStdout(), "items_pending: %d\n", t.PendingItems)
			fmt.Fprintf(cmd.OutOrStdout(), "items_uploading: %d\n", t.UploadingItems)
			fmt.Fprintf(cmd.OutOrStdout(), "items_success: %d\n", t.SuccessItems)
			fmt.Fprintf(cmd.OutOrStdout(), "items_failed: %d\n", t.FailedItems)
			fmt.Fprintf(cmd.OutOrStdout(), "items_skipped: %d\n", t.SkippedItems)
			fmt.Fprintf(cmd.OutOrStdout(), "created_at: %s\n", formatTimestamp(t.CreatedAt))
			fmt.Fprintf(cmd.OutOrStdout(), "updated_at: %s\n", formatTimestamp(t.UpdatedAt))
			fmt.Fprintf(cmd.OutOrStdout(), "started_at: %s\n", formatOptionalTimestamp(t.StartedAt))
			fmt.Fprintf(cmd.OutOrStdout(), "completed_at: %s\n", formatOptionalTimestamp(t.CompletedAt))
			if t.LastError != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "last_error: %s\n", t.LastError)
			}

			failedPrinted := 0
			for _, item := range items {
				if item.Status != taskpkg.ItemStatusFailed {
					continue
				}
				if failedLimit >= 0 && failedPrinted >= failedLimit {
					break
				}
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"failed_item[%d]: path=%s attempts=%d updated_at=%s completed_at=%s error=%s\n",
					failedPrinted+1,
					item.RelativePath,
					item.AttemptCount,
					formatTimestamp(item.UpdatedAt),
					formatOptionalTimestamp(item.CompletedAt),
					item.Error,
				)
				failedPrinted++
			}
			if t.FailedItems > failedPrinted {
				fmt.Fprintf(cmd.OutOrStdout(), "failed_items_remaining: %d\n", t.FailedItems-failedPrinted)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Path to config file")
	cmd.Flags().IntVar(&failedLimit, "failed-limit", 10, "Maximum number of failed item details to print (-1 for all)")
	return cmd
}

func newTaskRetryCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "retry <task-id>",
		Short: "Retry a task by moving it back to queued state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bootstrap, err := app.NewBootstrapWithConfig(configPath)
			if err != nil {
				return fmt.Errorf("create bootstrap: %w", err)
			}

			if err := bootstrap.TaskService.RetryTask(args[0]); err != nil {
				return fmt.Errorf("retry task: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "task queued again: %s\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Path to config file")
	return cmd
}

func newTaskRunCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "run <task-id>",
		Short: "Execute a queued task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := runTaskOnce(args[0], configPath); err != nil {
				return fmt.Errorf("run task once: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "task execution finished: %s\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Path to config file")
	return cmd
}

func newTaskWorkerCmd() *cobra.Command {
	var configPath string
	var once bool
	var taskID string
	var pollInterval time.Duration
	var idleTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Run a task worker / queue supervisor loop",
		RunE: func(cmd *cobra.Command, args []string) error {
			if taskID != "" {
				if err := runTaskOnce(taskID, configPath); err != nil {
					return fmt.Errorf("run requested task: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "task execution finished: %s\n", taskID)
				return nil
			}
			return runWorkerLoop(cmd, configPath, once, pollInterval, idleTimeout)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Path to config file")
	cmd.Flags().BoolVar(&once, "once", false, "Claim and execute at most one queued task before exiting")
	cmd.Flags().StringVar(&taskID, "task-id", "", "Specific task ID to execute once instead of polling the queue")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 2*time.Second, "How often the worker polls for queued tasks")
	cmd.Flags().DurationVar(&idleTimeout, "idle-timeout", 0, "Exit after this much idle time without queued tasks (0 = never)")
	return cmd
}

func formatTimestamp(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.UTC().Format(time.RFC3339)
}

func formatOptionalTimestamp(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "-"
	}
	return value.UTC().Format(time.RFC3339)
}
