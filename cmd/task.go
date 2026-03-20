package cmd

import (
	"fmt"

	"github.com/jasperbigsum-commits/s3async/internal/app"
	"github.com/spf13/cobra"
)

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "task", Short: "Manage sync tasks"}
	cmd.AddCommand(newTaskListCmd())
	cmd.AddCommand(newTaskStatusCmd())
	cmd.AddCommand(newTaskRetryCmd())
	return cmd
}

func newTaskListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			bootstrap, err := app.NewBootstrap()
			if err != nil {
				return fmt.Errorf("create bootstrap: %w", err)
			}

			tasks, err := bootstrap.TaskService.ListTasks()
			if err != nil {
				return fmt.Errorf("list tasks: %w", err)
			}

			for _, task := range tasks {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", task.ID, task.Status, task.Source)
			}

			return nil
		},
	}
}

func newTaskStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <task-id>",
		Short: "Show task status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bootstrap, err := app.NewBootstrap()
			if err != nil {
				return fmt.Errorf("create bootstrap: %w", err)
			}

			task, err := bootstrap.TaskService.GetTask(args[0])
			if err != nil {
				return fmt.Errorf("get task: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "id: %s\nstatus: %s\nsource: %s\nbucket: %s\nprefix: %s\n", task.ID, task.Status, task.Source, task.Bucket, task.Prefix)
			return nil
		},
	}
}

func newTaskRetryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "retry <task-id>",
		Short: "Retry a task by moving it back to queued state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bootstrap, err := app.NewBootstrap()
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
}
